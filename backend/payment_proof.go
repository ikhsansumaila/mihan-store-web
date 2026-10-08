package main

// Bukti transfer (konfirmasi pembayaran) pelanggan — maksimal satu per pesanan (tabel order_payment_proofs,
// migrasi 023/024).
//
// Pelanggan (sesi Bearer, pemilik pesanan; pesanan orang lain = 404 seperti pesanan yang tidak ada):
//   POST   /api/orders/{orderNo}/payment-proof   multipart "file" — hanya status pending_payment; mengganti bukti lama
//   GET    /api/orders/{orderNo}/payment-proof   berkas JPEG (privat, no-store)
//   DELETE /api/orders/{orderNo}/payment-proof   hanya pending_payment; 204, 404 bila tidak ada
// Admin (Cloudflare Access): GET /api/admin/orders/{id}/payment-proof.
//
// Penyimpanan PRIVAT (PAYMENT_PROOF_DIR, di luar /uploads, tidak disajikan nginx): nama acak (UUID v4), folder
// 0700, berkas 0600, tulis atomik (temp + rename). Isi gambar diverifikasi dengan decode sungguhan (JPEG/PNG/
// WebP), ditolak bila resolusi berlebih (dekompresi-bom), lalu diencode ulang JPEG tanpa metadata (EXIF/lokasi
// terbuang), sisi terpanjang maks. 2000 px.
//
// Alur unggah: berkas baru ditulis dulu -> SATU transaksi (kunci baris pesanan FOR UPDATE, status wajib
// pending_payment, upsert baris bukti, log aktivitas) -> gagal: berkas baru dihapus; sukses: berkas lama
// dihapus (best-effort) lalu notifikasi admin (push + Discord) asinkron.
// Hapus oleh pelanggan: baris + log dalam transaksi; berkas dihapus setelah commit (bila gagal hanya tercatat
// di log server; berkas yatim tidak bisa diakses karena tidak ada baris yang merujuknya).
// Retensi: purgePaymentProofs (goroutine harian) menghapus bukti 180 hari setelah pesanan selesai/dibatalkan.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"gorm.io/gorm"

	"mihanstore/notify"
	"mihanstore/push"
)

const (
	proofMaxUploadBytes = 6 << 20 // isi berkas mentah (klien memperkecil dulu ke <= 5 MB)
	proofMaxOutputBytes = 3 << 20 // hasil encode ulang
	proofMaxSide        = 2000
	proofJPEGQuality    = 85
	proofRetentionDays  = 180
	proofPurgeInterval  = 24 * time.Hour
	proofMime           = "image/jpeg"

	msgProofTooLarge   = "Ukuran foto bukti melebihi 6 MB. Pilih foto yang lebih kecil."
	msgProofNotPending = "Bukti transfer hanya bisa diunggah atau diubah saat pesanan menunggu pembayaran."
	msgProofMissing    = "Bukti pembayaran tidak ditemukan"
	msgProofTooMany    = "Terlalu banyak percobaan. Coba lagi sebentar lagi."
	msgProofStoreDown  = "Penyimpanan bukti transfer sedang tidak tersedia. Coba lagi nanti."
)

var errProofOutputTooLarge = errors.New("hasil gambar terlalu besar")

// ---------- Penyimpanan privat ----------

var proofKeyRe = regexp.MustCompile(`^` + uuidPattern + `\.jpg$`)

// ProofStore: penyimpanan berkas bukti privat di disk (tidak punya URL publik).
type ProofStore struct{ base string }

func NewProofStore(base string) *ProofStore { return &ProofStore{base: filepath.Clean(base)} }

func (s *ProofStore) path(key string) (string, error) {
	if !proofKeyRe.MatchString(key) {
		return "", errBadKey
	}
	return filepath.Join(s.base, key), nil
}

func newProofKey() (string, error) {
	id, err := newPublicID() // UUID v4 acak (122 bit)
	if err != nil {
		return "", err
	}
	return id + ".jpg", nil
}

// Save: tulis atomik (temp + fsync + rename), folder 0700, berkas 0600.
func (s *ProofStore) Save(key string, data []byte) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.base, 0o700); err != nil {
		return wrapStoreErr(err)
	}
	tmp, err := os.CreateTemp(s.base, ".bukti-*.tmp")
	if err != nil {
		return wrapStoreErr(err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return wrapStoreErr(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return wrapStoreErr(err)
	}
	if err := tmp.Sync(); err != nil {
		return wrapStoreErr(err)
	}
	if err := tmp.Close(); err != nil {
		return wrapStoreErr(err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return wrapStoreErr(err)
	}
	ok = true
	return nil
}

// Open membuka berkas untuk dibaca.
func (s *ProofStore) Open(key string) (*os.File, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

// Delete: berkas yang sudah tidak ada bukan galat (idempoten).
func (s *ProofStore) Delete(key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Writable memeriksa folder bisa ditulis (membuat folder 0700 bila belum ada).
func (s *ProofStore) Writable() error {
	if err := os.MkdirAll(s.base, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.base, ".cek-tulis-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// ---------- Pemrosesan gambar ----------

type processedProof struct {
	Data          []byte
	Width, Height int
	SHA256        string
}

// processPaymentProof: verifikasi isi (bukan ekstensi/header klien), tolak dekompresi-bom, terapkan orientasi
// EXIF, perkecil (maks. 2000 px), encode ulang JPEG tanpa metadata.
func processPaymentProof(data []byte) (*processedProof, error) {
	if len(data) > proofMaxUploadBytes {
		return nil, errImageTooLarge
	}
	kind := sniffImage(data)
	if kind == "" {
		return nil, errImageType
	}
	cfg, err := decodeConfig(kind, data)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, errImageCorrupt
	}
	if cfg.Width > maxImageSide || cfg.Height > maxImageSide || int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return nil, errImageResolution
	}
	src, err := decodeImage(kind, data)
	if err != nil {
		return nil, errImageCorrupt
	}
	if b := src.Bounds(); b.Dx() != cfg.Width || b.Dy() != cfg.Height {
		return nil, errImageCorrupt
	}
	img := scaleOriented(src, exifOrientation(kind, data), proofMaxSide)
	out, err := encodeJPEG(img, proofJPEGQuality)
	if err != nil {
		return nil, err
	}
	if len(out) > proofMaxOutputBytes {
		return nil, errProofOutputTooLarge
	}
	sum := sha256.Sum256(out)
	return &processedProof{Data: out, Width: img.Bounds().Dx(), Height: img.Bounds().Dy(), SHA256: hex.EncodeToString(sum[:])}, nil
}

func proofErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, errImageTooLarge):
		return http.StatusRequestEntityTooLarge, msgProofTooLarge
	case errors.Is(err, errProofOutputTooLarge):
		return http.StatusUnprocessableEntity, "Foto bukti terlalu besar setelah diproses. Coba foto lain."
	case errors.Is(err, errImageType):
		return http.StatusUnsupportedMediaType, "Format foto tidak didukung. Gunakan JPG, PNG, atau WebP."
	case errors.Is(err, errImageCorrupt), errors.Is(err, errImageResolution):
		code, msg := imageErrorStatus(err)
		return code, msg
	case errors.Is(err, errStoreFull), errors.Is(err, errStoreUnavailable):
		return http.StatusServiceUnavailable, msgProofStoreDown
	}
	return http.StatusInternalServerError, msgServerError
}

// ---------- Data ----------

type paymentProofRow struct {
	ID        uint64    `gorm:"column:id"`
	OrderID   uint64    `gorm:"column:order_id"`
	UserID    uint64    `gorm:"column:user_id"`
	FileKey   string    `gorm:"column:file_key"`
	Mime      string    `gorm:"column:mime"`
	SizeBytes int64     `gorm:"column:size_bytes"`
	Width     int       `gorm:"column:width"`
	Height    int       `gorm:"column:height"`
	SHA256    string    `gorm:"column:sha256"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// PaymentProofDTO: info bukti untuk detail pesanan (tanpa kunci berkas/hash).
type PaymentProofDTO struct {
	UploadedAt time.Time `json:"uploadedAt"`
	SizeBytes  int64     `json:"sizeBytes"`
	Mime       string    `json:"mime"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
}

const proofCols = `id, order_id, user_id, file_key, mime, size_bytes, width, height, sha256, created_at, updated_at`

func loadPaymentProof(db *gorm.DB, orderID uint64, lock bool) (*paymentProofRow, error) {
	q := `SELECT ` + proofCols + ` FROM order_payment_proofs WHERE order_id = ?`
	if lock {
		q += ` FOR UPDATE`
	}
	var rows []paymentProofRow
	if err := db.Raw(q, orderID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func proofDTO(p *paymentProofRow) *PaymentProofDTO {
	if p == nil {
		return nil
	}
	return &PaymentProofDTO{UploadedAt: p.UpdatedAt, SizeBytes: p.SizeBytes, Mime: p.Mime, Width: p.Width, Height: p.Height}
}

// paymentProofDTOFor: dipakai detail pesanan pelanggan & admin (galat DB -> nil, dicatat).
func paymentProofDTOFor(db *gorm.DB, orderID uint64) *PaymentProofDTO {
	p, err := loadPaymentProof(db, orderID, false)
	if err != nil {
		log.Printf("bukti pembayaran pesanan %d: %v", orderID, err)
		return nil
	}
	return proofDTO(p)
}

// ---------- Handler pelanggan ----------

func (a *App) proofStoreOrFail(w http.ResponseWriter) *ProofStore {
	if a.proofs == nil {
		writeError(w, http.StatusServiceUnavailable, msgProofStoreDown)
	}
	return a.proofs
}

// CustomerUploadPaymentProof: POST /api/orders/{orderNo}/payment-proof.
func (a *App) CustomerUploadPaymentProof(w http.ResponseWriter, r *http.Request) {
	u := customerFrom(r.Context())
	if !limitUser(w, a.proofLimiter, u, msgProofTooMany) {
		return
	}
	store := a.proofStoreOrFail(w)
	if store == nil {
		return
	}
	db := a.db.Load().WithContext(r.Context())
	orderNo := mux.Vars(r)["orderNo"]
	// Pemeriksaan awal (tanpa kunci) agar unggahan yang pasti ditolak tidak diproses.
	pre, err := findCustomerOrder(db, u.ID, orderNo, false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	if err != nil {
		log.Printf("bukti pembayaran: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	if pre.Status != StatusPending {
		writeError(w, http.StatusConflict, msgProofNotPending)
		return
	}
	data, code, msg := readUploadFileLimit(w, r, proofMaxUploadBytes, msgProofTooLarge)
	if code != 0 {
		writeError(w, code, msg)
		return
	}
	release, ok := a.acquireImageSlot(r.Context())
	if !ok {
		writeError(w, http.StatusServiceUnavailable, msgImageBusy)
		return
	}
	proc, err := processPaymentProof(data)
	release()
	if err != nil {
		code, msg := proofErrorStatus(err)
		writeError(w, code, msg)
		return
	}
	key, err := newProofKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, msgServerError)
		return
	}
	if err := store.Save(key, proc.Data); err != nil {
		log.Printf("bukti pembayaran: simpan berkas: %v", err)
		code, msg := proofErrorStatus(err)
		writeError(w, code, msg)
		return
	}
	var oldKey string
	var orderID uint64
	replaced := false
	err = db.Transaction(func(tx *gorm.DB) error {
		o, err := findCustomerOrder(tx, u.ID, orderNo, true)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &httpError{http.StatusNotFound, msgOrderMissing}
		}
		if err != nil {
			return err
		}
		if o.Status != StatusPending {
			return &httpError{http.StatusConflict, msgProofNotPending}
		}
		orderID = o.ID
		old, err := loadPaymentProof(tx, o.ID, true)
		if err != nil {
			return err
		}
		now := a.now()
		if old != nil {
			replaced, oldKey = true, old.FileKey
			if err := tx.Exec(`UPDATE order_payment_proofs SET user_id = ?, file_key = ?, mime = ?, size_bytes = ?, width = ?, height = ?,
				sha256 = ?, updated_at = ? WHERE id = ?`, u.ID, key, proofMime, len(proc.Data), proc.Width, proc.Height, proc.SHA256, now, old.ID).Error; err != nil {
				return err
			}
		} else if err := tx.Exec(`INSERT INTO order_payment_proofs (order_id, user_id, file_key, mime, size_bytes, width, height, sha256, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, o.ID, u.ID, key, proofMime, len(proc.Data), proc.Width, proc.Height, proc.SHA256, now, now).Error; err != nil {
			return err
		}
		action, summary := "order.payment_proof_upload", "Bukti transfer diunggah pelanggan: "+o.OrderNo
		details := map[string]any{"orderNo": o.OrderNo, "ukuranByte": len(proc.Data), "sha256": proc.SHA256}
		if replaced {
			action, summary = "order.payment_proof_replace", "Bukti transfer diganti pelanggan: "+o.OrderNo
			details["sha256Lama"] = old.SHA256
			details["ukuranByteLama"] = old.SizeBytes
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(u), ActorLabel: actorOf(u), Action: action, EntityType: "order", EntityID: o.OrderNo,
			Summary: summary, Details: details,
		}))
	})
	if err != nil {
		if derr := store.Delete(key); derr != nil {
			log.Printf("bukti pembayaran: gagal menghapus berkas baru setelah transaksi gagal: %v", derr)
		}
		a.respondTxError(w, err, "unggah bukti pembayaran")
		return
	}
	if oldKey != "" {
		if err := store.Delete(oldKey); err != nil {
			log.Printf("bukti pembayaran: gagal menghapus berkas lama: %v", err)
		}
	}
	// Notifikasi admin (asinkron, kegagalan tidak memengaruhi unggahan).
	a.notifyOrder(db, notify.KindPaymentProof, orderID)
	a.pushOrder(db, push.KindPaymentProof, orderID)
	p, _ := loadPaymentProof(db, orderID, false)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "replaced": replaced, "paymentProof": proofDTO(p)})
}

// CustomerGetPaymentProof: GET /api/orders/{orderNo}/payment-proof (pemilik saja).
func (a *App) CustomerGetPaymentProof(w http.ResponseWriter, r *http.Request) {
	u := customerFrom(r.Context())
	if !limitUser(w, a.proofViewLimiter, u, msgProofTooMany) {
		return
	}
	db := a.db.Load().WithContext(r.Context())
	o, err := findCustomerOrder(db, u.ID, mux.Vars(r)["orderNo"], false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	a.serveProof(w, db, o.ID)
}

// CustomerDeletePaymentProof: DELETE /api/orders/{orderNo}/payment-proof (pemilik, hanya pending_payment).
func (a *App) CustomerDeletePaymentProof(w http.ResponseWriter, r *http.Request) {
	u := customerFrom(r.Context())
	if !limitUser(w, a.proofLimiter, u, msgProofTooMany) {
		return
	}
	store := a.proofStoreOrFail(w)
	if store == nil {
		return
	}
	db := a.db.Load().WithContext(r.Context())
	var key string
	err := db.Transaction(func(tx *gorm.DB) error {
		o, err := findCustomerOrder(tx, u.ID, mux.Vars(r)["orderNo"], true)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &httpError{http.StatusNotFound, msgOrderMissing}
		}
		if err != nil {
			return err
		}
		p, err := loadPaymentProof(tx, o.ID, true)
		if err != nil {
			return err
		}
		if p == nil {
			return &httpError{http.StatusNotFound, msgProofMissing}
		}
		if o.Status != StatusPending {
			return &httpError{http.StatusConflict, msgProofNotPending}
		}
		if err := tx.Exec(`DELETE FROM order_payment_proofs WHERE id = ?`, p.ID).Error; err != nil {
			return err
		}
		key = p.FileKey
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(u), ActorLabel: actorOf(u), Action: "order.payment_proof_delete", EntityType: "order", EntityID: o.OrderNo,
			Summary: "Bukti transfer dihapus pelanggan: " + o.OrderNo,
			Details: map[string]any{"orderNo": o.OrderNo, "ukuranByte": p.SizeBytes, "sha256": p.SHA256},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "hapus bukti pembayaran")
		return
	}
	if err := store.Delete(key); err != nil {
		log.Printf("bukti pembayaran: gagal menghapus berkas: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// AdminGetPaymentProof: GET /api/admin/orders/{id}/payment-proof.
func (a *App) AdminGetPaymentProof(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	db := a.db.Load().WithContext(r.Context())
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM orders WHERE id = ? AND deleted_at IS NULL`, id).Scan(&n).Error; err != nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	a.serveProof(w, db, id)
}

// serveProof menulis berkas bukti dengan header privat.
func (a *App) serveProof(w http.ResponseWriter, db *gorm.DB, orderID uint64) {
	if a.proofs == nil {
		writeError(w, http.StatusServiceUnavailable, msgProofStoreDown)
		return
	}
	p, err := loadPaymentProof(db, orderID, false)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, msgProofMissing)
		return
	}
	f, err := a.proofs.Open(p.FileKey)
	if err != nil {
		log.Printf("bukti pembayaran pesanan %d: berkas tidak dapat dibuka: %v", orderID, err)
		writeError(w, http.StatusNotFound, msgProofMissing)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, proofMaxOutputBytes+1))
	if err != nil || len(data) > proofMaxOutputBytes || sniffImage(data) != kindJPEG {
		log.Printf("bukti pembayaran pesanan %d: berkas tidak sah", orderID)
		writeError(w, http.StatusNotFound, msgProofMissing)
		return
	}
	h := w.Header()
	h.Set("Content-Type", proofMime)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Cache-Control", "private, no-store")
	h.Set("Pragma", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", fmt.Sprintf(`inline; filename="bukti-%d.jpg"`, orderID))
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, bytes.NewReader(data))
}

// ---------- Retensi (180 hari) ----------

// purgePaymentProofs menghapus bukti pesanan yang selesai/dibatalkan lebih dari 180 hari lalu: berkas dulu
// (berkas yang sudah hilang tidak dianggap galat), lalu baris DB + satu log aktivitas ringkasan dalam satu
// transaksi. Berkas yang gagal dihapus (selain "tidak ada") dilewati dan dicoba lagi pada putaran berikut.
func (a *App) purgePaymentProofs(ctx context.Context, now time.Time) (int, error) {
	db := a.db.Load()
	if db == nil || a.proofs == nil {
		return 0, nil
	}
	db = db.WithContext(ctx)
	cutoff := now.Add(-proofRetentionDays * 24 * time.Hour)
	var rows []struct {
		ID      uint64 `gorm:"column:id"`
		FileKey string `gorm:"column:file_key"`
		OrderNo string `gorm:"column:order_no"`
	}
	if err := db.Raw(`SELECT p.id, p.file_key, o.order_no FROM order_payment_proofs p JOIN orders o ON o.id = p.order_id
		WHERE (o.status = 'completed' AND o.completed_at IS NOT NULL AND o.completed_at < ?)
		   OR (o.status = 'cancelled' AND o.cancelled_at IS NOT NULL AND o.cancelled_at < ?)
		ORDER BY p.id LIMIT 1000`, cutoff, cutoff).Scan(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	ids := make([]uint64, 0, len(rows))
	nos := make([]string, 0, len(rows))
	for _, r := range rows {
		if err := a.proofs.Delete(r.FileKey); err != nil {
			log.Printf("purge bukti pembayaran: berkas %s gagal dihapus, dicoba lagi nanti: %v", r.OrderNo, err)
			continue
		}
		ids = append(ids, r.ID)
		if len(nos) < 50 {
			nos = append(nos, r.OrderNo)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	var deleted int64
	err := db.Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`DELETE FROM order_payment_proofs WHERE id IN ?`, ids)
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected
		return a.logActivity(tx, LogEntry{
			ActorLabel: "sistem", Action: "order.payment_proof_purge", EntityType: "order",
			Summary: fmt.Sprintf("%d bukti transfer dihapus otomatis (retensi %d hari)", deleted, proofRetentionDays),
			Details: map[string]any{"jumlah": deleted, "retensiHari": proofRetentionDays, "pesanan": nos},
		})
	})
	return int(deleted), err
}

// startProofPurger menjalankan purge saat start (setelah jeda) lalu tiap 24 jam, berhenti saat bgCtx selesai.
func (a *App) startProofPurger(ctx context.Context) {
	go func() {
		t := time.NewTimer(2 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			n, err := a.purgePaymentProofs(ctx, a.now())
			if err != nil {
				log.Printf("purge bukti pembayaran: %v", err)
			} else if n > 0 {
				log.Printf("purge bukti pembayaran: %d bukti dihapus", n)
			}
			t.Reset(proofPurgeInterval)
		}
	}()
}
