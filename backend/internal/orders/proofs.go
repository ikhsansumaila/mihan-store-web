package orders

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
// Retensi: PurgePaymentProofs (proofs_purge.go) (goroutine harian) menghapus bukti 180 hari setelah pesanan selesai/dibatalkan.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"gorm.io/gorm"

	"mihanstore/internal/audit"
	"mihanstore/internal/platform"
	"mihanstore/internal/platform/imaging"
)

const (
	ProofMaxUploadBytes = 6 << 20 // isi berkas mentah (klien memperkecil dulu ke <= 5 MB)
	proofMaxOutputBytes = 3 << 20 // hasil encode ulang
	proofMaxSide        = 2000
	proofJPEGQuality    = 85
	proofRetentionDays  = 180
	proofPurgeInterval  = 24 * time.Hour
	proofMime           = "image/jpeg"

	msgProofTooLarge   = "Ukuran foto bukti melebihi 6 MB. Pilih foto yang lebih kecil."
	MsgProofNotPending = "Bukti transfer hanya bisa diunggah atau diubah saat pesanan menunggu pembayaran."
	msgProofMissing    = "Bukti pembayaran tidak ditemukan"
	msgProofTooMany    = "Terlalu banyak percobaan. Coba lagi sebentar lagi."
	msgProofStoreDown  = "Penyimpanan bukti transfer sedang tidak tersedia. Coba lagi nanti."
)

var errProofOutputTooLarge = errors.New("hasil gambar terlalu besar")

// ---------- Pemrosesan gambar ----------

type processedProof struct {
	Data          []byte
	Width, Height int
	SHA256        string
}

// processPaymentProof: verifikasi isi (bukan ekstensi/header klien), tolak dekompresi-bom, terapkan orientasi
// EXIF, perkecil (maks. 2000 px), encode ulang JPEG tanpa metadata.
func processPaymentProof(data []byte) (*processedProof, error) {
	if len(data) > ProofMaxUploadBytes {
		return nil, imaging.ErrImageTooLarge
	}
	kind := imaging.SniffImage(data)
	if kind == "" {
		return nil, imaging.ErrImageType
	}
	cfg, err := imaging.DecodeConfig(kind, data)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, imaging.ErrImageCorrupt
	}
	if cfg.Width > imaging.MaxImageSide || cfg.Height > imaging.MaxImageSide || int64(cfg.Width)*int64(cfg.Height) > imaging.MaxImagePixels {
		return nil, imaging.ErrImageResolution
	}
	src, err := imaging.DecodeImage(kind, data)
	if err != nil {
		return nil, imaging.ErrImageCorrupt
	}
	if b := src.Bounds(); b.Dx() != cfg.Width || b.Dy() != cfg.Height {
		return nil, imaging.ErrImageCorrupt
	}
	img := imaging.ScaleOriented(src, imaging.ExifOrientation(kind, data), proofMaxSide)
	out, err := imaging.EncodeJPEG(img, proofJPEGQuality)
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
	case errors.Is(err, imaging.ErrImageTooLarge):
		return http.StatusRequestEntityTooLarge, msgProofTooLarge
	case errors.Is(err, errProofOutputTooLarge):
		return http.StatusUnprocessableEntity, "Foto bukti terlalu besar setelah diproses. Coba foto lain."
	case errors.Is(err, imaging.ErrImageType):
		return http.StatusUnsupportedMediaType, "Format foto tidak didukung. Gunakan JPG, PNG, atau WebP."
	case errors.Is(err, imaging.ErrImageCorrupt), errors.Is(err, imaging.ErrImageResolution):
		code, msg := imaging.ImageErrorStatus(err)
		return code, msg
	case errors.Is(err, imaging.ErrStoreFull), errors.Is(err, imaging.ErrStoreUnavailable):
		return http.StatusServiceUnavailable, msgProofStoreDown
	}
	return http.StatusInternalServerError, platform.MsgServerError
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

func (a *Service) proofStoreOrFail(w http.ResponseWriter) *ProofStore {
	if a.Proofs == nil {
		platform.WriteError(w, http.StatusServiceUnavailable, msgProofStoreDown)
	}
	return a.Proofs
}

// CustomerUploadPaymentProof: POST /api/orders/{orderNo}/payment-proof.
func (a *Service) CustomerUploadPaymentProof(w http.ResponseWriter, r *http.Request) {
	u := a.customer(r)
	if !platform.LimitUser(w, a.ProofLimiter, u.ID, msgProofTooMany) {
		return
	}
	store := a.proofStoreOrFail(w)
	if store == nil {
		return
	}
	db := a.db().WithContext(r.Context())
	orderNo := mux.Vars(r)["orderNo"]
	// Pemeriksaan awal (tanpa kunci) agar unggahan yang pasti ditolak tidak diproses.
	pre, err := findCustomerOrder(db, u.ID, orderNo, false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	if err != nil {
		log.Printf("bukti pembayaran: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	if pre.Status != StatusPending {
		platform.WriteError(w, http.StatusConflict, MsgProofNotPending)
		return
	}
	data, code, msg := imaging.ReadUploadFileLimit(w, r, ProofMaxUploadBytes, msgProofTooLarge)
	if code != 0 {
		platform.WriteError(w, code, msg)
		return
	}
	release, ok := a.imageSlots.Acquire(r.Context())
	if !ok {
		platform.WriteError(w, http.StatusServiceUnavailable, imaging.MsgImageBusy)
		return
	}
	proc, err := processPaymentProof(data)
	release()
	if err != nil {
		code, msg := proofErrorStatus(err)
		platform.WriteError(w, code, msg)
		return
	}
	key, err := newProofKey()
	if err != nil {
		platform.WriteError(w, http.StatusInternalServerError, platform.MsgServerError)
		return
	}
	if err := store.Save(key, proc.Data); err != nil {
		log.Printf("bukti pembayaran: simpan berkas: %v", err)
		code, msg := proofErrorStatus(err)
		platform.WriteError(w, code, msg)
		return
	}
	var oldKey string
	var orderID uint64
	replaced := false
	err = db.Transaction(func(tx *gorm.DB) error {
		o, err := findCustomerOrder(tx, u.ID, orderNo, true)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &platform.HTTPError{Status: http.StatusNotFound, Msg: msgOrderMissing}
		}
		if err != nil {
			return err
		}
		if o.Status != StatusPending {
			return &platform.HTTPError{Status: http.StatusConflict, Msg: MsgProofNotPending}
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
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: u.uid(), ActorLabel: u.label(), Action: action, EntityType: "order", EntityID: o.OrderNo,
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
	a.events.PaymentProofUploaded(db, orderID)
	p, _ := loadPaymentProof(db, orderID, false)
	platform.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "replaced": replaced, "paymentProof": proofDTO(p)})
}

// CustomerGetPaymentProof: GET /api/orders/{orderNo}/payment-proof (pemilik saja).
func (a *Service) CustomerGetPaymentProof(w http.ResponseWriter, r *http.Request) {
	u := a.customer(r)
	if !platform.LimitUser(w, a.ProofViewLimiter, u.ID, msgProofTooMany) {
		return
	}
	db := a.db().WithContext(r.Context())
	o, err := findCustomerOrder(db, u.ID, mux.Vars(r)["orderNo"], false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	if err != nil {
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	a.serveProof(w, db, o.ID)
}

// CustomerDeletePaymentProof: DELETE /api/orders/{orderNo}/payment-proof (pemilik, hanya pending_payment).
func (a *Service) CustomerDeletePaymentProof(w http.ResponseWriter, r *http.Request) {
	u := a.customer(r)
	if !platform.LimitUser(w, a.ProofLimiter, u.ID, msgProofTooMany) {
		return
	}
	store := a.proofStoreOrFail(w)
	if store == nil {
		return
	}
	db := a.db().WithContext(r.Context())
	var key string
	err := db.Transaction(func(tx *gorm.DB) error {
		o, err := findCustomerOrder(tx, u.ID, mux.Vars(r)["orderNo"], true)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &platform.HTTPError{Status: http.StatusNotFound, Msg: msgOrderMissing}
		}
		if err != nil {
			return err
		}
		p, err := loadPaymentProof(tx, o.ID, true)
		if err != nil {
			return err
		}
		if p == nil {
			return &platform.HTTPError{Status: http.StatusNotFound, Msg: msgProofMissing}
		}
		if o.Status != StatusPending {
			return &platform.HTTPError{Status: http.StatusConflict, Msg: MsgProofNotPending}
		}
		if err := tx.Exec(`DELETE FROM order_payment_proofs WHERE id = ?`, p.ID).Error; err != nil {
			return err
		}
		key = p.FileKey
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: u.uid(), ActorLabel: u.label(), Action: "order.payment_proof_delete", EntityType: "order", EntityID: o.OrderNo,
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
func (a *Service) AdminGetPaymentProof(w http.ResponseWriter, r *http.Request) {
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	db := a.db().WithContext(r.Context())
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM orders WHERE id = ? AND deleted_at IS NULL`, id).Scan(&n).Error; err != nil {
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	if n == 0 {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	a.serveProof(w, db, id)
}

// serveProof menulis berkas bukti dengan header privat.
func (a *Service) serveProof(w http.ResponseWriter, db *gorm.DB, orderID uint64) {
	if a.Proofs == nil {
		platform.WriteError(w, http.StatusServiceUnavailable, msgProofStoreDown)
		return
	}
	p, err := loadPaymentProof(db, orderID, false)
	if err != nil {
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	if p == nil {
		platform.WriteError(w, http.StatusNotFound, msgProofMissing)
		return
	}
	f, err := a.Proofs.Open(p.FileKey)
	if err != nil {
		log.Printf("bukti pembayaran pesanan %d: berkas tidak dapat dibuka: %v", orderID, err)
		platform.WriteError(w, http.StatusNotFound, msgProofMissing)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, proofMaxOutputBytes+1))
	if err != nil || len(data) > proofMaxOutputBytes || imaging.SniffImage(data) != imaging.KindJPEG {
		log.Printf("bukti pembayaran pesanan %d: berkas tidak sah", orderID)
		platform.WriteError(w, http.StatusNotFound, msgProofMissing)
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
