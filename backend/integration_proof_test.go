//go:build integration

// Tes integrasi bukti transfer (migrasi 023/024): unggah/ganti/hapus pelanggan, akses berkas privat,
// notifikasi admin, retensi 180 hari. Berkas ditulis ke folder sementara tes (bukan folder produksi).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"mihanstore/push"
)

type proofEnv struct {
	app  *App
	h    http.Handler
	db   *gorm.DB
	rec  *recNotifier
	rs   *recSender
	dir  string
	tok  string
	uid  uint64
	tok2 string
}

func setupProof(t *testing.T) *proofEnv {
	app, _, db, rec := setupOrders(t)
	dir := filepath.Join(t.TempDir(), "private-uploads", "payment-proofs")
	app.proofs = NewProofStore(dir)
	rs := &recSender{}
	app.pusher = rs
	app.proofLimiter = NewRateLimiter(100000, time.Minute)
	h := newRouter(app)
	tok, uid := aliasCustomer(t, h, db, "Pembeli Bukti", "081277775555")
	tok2, _ := aliasCustomer(t, h, db, "Orang Lain", "081277776666")
	return &proofEnv{app: app, h: h, db: db, rec: rec, rs: rs, dir: dir, tok: tok, uid: uid, tok2: tok2}
}

// uploadProof mengirim multipart "file" sebagai pelanggan (token boleh kosong).
func uploadProof(t *testing.T, h http.Handler, orderNo, tok string, data []byte, filename string) resp {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", filename)
	fw.Write(data)
	mw.Close()
	r := httptest.NewRequest("POST", "/api/orders/"+orderNo+"/payment-proof", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.RemoteAddr = "203.0.113.7:5555"
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m)
	return resp{w.Code, m}
}

func getRaw(h http.Handler, path, tok string, admin string, t *testing.T) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.RemoteAddr = "172.22.0.2:5555"
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	if admin != "" {
		r.Header.Set("Cf-Access-Jwt-Assertion", accessToken(t, admin))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func proofKeyOf(db *gorm.DB, orderID uint64) string {
	var k string
	db.Raw("SELECT file_key FROM order_payment_proofs WHERE order_id = ?", orderID).Scan(&k)
	return k
}

func TestIntegrationPaymentProofFlow(t *testing.T) {
	e := setupProof(t)
	h, db := e.h, e.db
	no := createOrderFor(t, h, e.tok, 4)
	id := orderIDByNo(db, no)
	jpg := withJPEGExif(encJPEG(t, markerImage(1600, 900), 90), 6)

	// Belum dikonfirmasi (menunggu konfirmasi) -> 409; tanpa login 401; pesanan orang lain 404.
	if r := uploadProof(t, h, no, e.tok, jpg, "bukti.jpg"); r.Code != 409 || r.Body["error"] != msgProofNotPending {
		t.Fatalf("upload sebelum konfirmasi: %d %v", r.Code, r.Body)
	}
	confirmCall(t, h, id, 0, 10000)
	if r := uploadProof(t, h, no, "", jpg, "bukti.jpg"); r.Code != 401 {
		t.Fatalf("tanpa login: %d", r.Code)
	}
	if r := uploadProof(t, h, no, e.tok2, jpg, "bukti.jpg"); r.Code != 404 {
		t.Fatalf("bukan pemilik: %d", r.Code)
	}
	if r := uploadProof(t, h, "MS-200101-9999", e.tok, jpg, "bukti.jpg"); r.Code != 404 {
		t.Fatalf("pesanan tidak ada: %d", r.Code)
	}
	// Tipe palsu, terlalu besar.
	if r := uploadProof(t, h, no, e.tok, []byte("ini teks, bukan foto"), "palsu.jpg"); r.Code != 415 {
		t.Fatalf("teks .jpg: %d %v", r.Code, r.Body)
	}
	if r := uploadProof(t, h, no, e.tok, make([]byte, proofMaxUploadBytes+10), "besar.jpg"); r.Code != 413 {
		t.Fatalf("terlalu besar: %d", r.Code)
	}
	if n := len(filesIn(t, e.dir)); n != 0 {
		t.Fatalf("unggahan gagal tidak boleh meninggalkan berkas: %d", n)
	}

	// Sukses (JPEG dengan EXIF).
	r := uploadProof(t, h, no, e.tok, jpg, "IMG_1234 rekening saya.jpg")
	if r.Code != 200 || r.Body["replaced"] != false || r.Body["paymentProof"] == nil {
		t.Fatalf("upload: %d %v", r.Code, r.Body)
	}
	key1 := proofKeyOf(db, id)
	if !proofKeyRe.MatchString(key1) || strings.Contains(key1, "IMG") {
		t.Fatalf("nama berkas harus acak: %s", key1)
	}
	if strings.Contains(e.dir, "/uploads/") || filepath.Base(filepath.Dir(e.dir)) != "private-uploads" {
		t.Fatalf("folder bukti di luar /uploads: %s", e.dir)
	}
	fi, err := os.Stat(filepath.Join(e.dir, key1))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("berkas privat 0600: %v %v", err, fi)
	}
	stored, _ := os.ReadFile(filepath.Join(e.dir, key1))
	if bytes.Contains(stored, []byte("RAHASIA-GPS-UJI")) || bytes.Contains(stored, []byte("Exif")) {
		t.Fatal("EXIF harus dibuang")
	}
	if l := lastLog(t, db, "order.payment_proof_upload"); l == nil || !strings.Contains(det(l), no) || !strings.Contains(det(l), "sha256") {
		t.Fatalf("log upload: %v", det(l))
	}
	// Notifikasi admin: Discord + push.
	if !strings.Contains(e.rec.kinds(), "payment_proof:"+no) {
		t.Fatalf("discord: %s", e.rec.kinds())
	}
	e.rs.mu.Lock()
	last := e.rs.ev[len(e.rs.ev)-1]
	e.rs.mu.Unlock()
	if last.Kind != push.KindPaymentProof || last.OrderNo != no || last.Customer != "Pembeli Bukti" || last.Recipient != "Budi Penerima" {
		t.Fatalf("push: %+v", last)
	}
	// Status tidak berubah.
	var st string
	db.Raw("SELECT status FROM orders WHERE id = ?", id).Scan(&st)
	if st != StatusPending {
		t.Fatalf("status harus tetap pending_payment: %s", st)
	}
	// Detail & daftar pelanggan / admin.
	if r := call(t, h, "GET", "/api/orders/"+no, e.tok, nil); r.Body["paymentProof"] == nil || r.Body["canUploadProof"] != true {
		t.Fatalf("detail pelanggan: %v", r.Body["paymentProof"])
	}
	if r := call(t, h, "GET", "/api/orders", e.tok, nil); r.Body["items"].([]any)[0].(map[string]any)["hasPaymentProof"] != true {
		t.Fatalf("daftar pelanggan: %v", r.Body["items"])
	}
	if r := adminCall(t, h, "GET", fmt.Sprintf("/api/admin/orders/%d", id), integAdmin, nil); r.Body["paymentProof"] == nil {
		t.Fatalf("detail admin: %v", r.Body["paymentProof"])
	}
	if r := adminCall(t, h, "GET", "/api/admin/orders?proof=1&q="+no, integAdmin, nil); num(r.Body["total"]) != 1 ||
		r.Body["items"].([]any)[0].(map[string]any)["hasPaymentProof"] != true {
		t.Fatalf("daftar admin ?proof=1: %v", r.Body)
	}

	// GET berkas: pemilik 200 + header privat; orang lain 404; tanpa login 401; admin 200; admin tanpa token 401.
	path := "/api/orders/" + no + "/payment-proof"
	w := getRaw(h, path, e.tok, "", t)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("Cache-Control") != "private, no-store" ||
		w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline") ||
		!bytes.Equal(w.Body.Bytes(), stored) {
		t.Fatalf("GET pemilik: %d %v", w.Code, w.Header())
	}
	if w := getRaw(h, path, e.tok2, "", t); w.Code != 404 {
		t.Fatalf("GET orang lain: %d", w.Code)
	}
	if w := getRaw(h, path, "", "", t); w.Code != 401 {
		t.Fatalf("GET tanpa login: %d", w.Code)
	}
	apath := fmt.Sprintf("/api/admin/orders/%d/payment-proof", id)
	if w := getRaw(h, apath, "", integAdmin, t); w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" || !bytes.Equal(w.Body.Bytes(), stored) {
		t.Fatalf("GET admin: %d %v", w.Code, w.Header())
	}
	if w := getRaw(h, apath, "", "", t); w.Code != 401 {
		t.Fatalf("GET admin tanpa token: %d", w.Code)
	}
	if w := getRaw(h, apath, e.tok, "", t); w.Code != 401 {
		t.Fatalf("GET admin dengan sesi pelanggan: %d", w.Code)
	}

	// Ganti (PNG): berkas lama dihapus, baris diperbarui, log replace.
	r = uploadProof(t, h, no, e.tok, encPNG(t, markerImage(400, 300)), "lagi.png")
	if r.Code != 200 || r.Body["replaced"] != true {
		t.Fatalf("ganti: %d %v", r.Code, r.Body)
	}
	key2 := proofKeyOf(db, id)
	if key2 == key1 || fmt.Sprint(filesIn(t, e.dir)) != "["+key2+"]" {
		t.Fatalf("berkas lama harus terhapus: %v (baru %s)", filesIn(t, e.dir), key2)
	}
	var nRows int64
	db.Raw("SELECT COUNT(*) FROM order_payment_proofs WHERE order_id = ?", id).Scan(&nRows)
	if nRows != 1 {
		t.Fatalf("maks. 1 bukti per pesanan: %d", nRows)
	}
	if l := lastLog(t, db, "order.payment_proof_replace"); l == nil || !strings.Contains(det(l), "sha256Lama") {
		t.Fatalf("log ganti: %v", det(l))
	}

	// Hapus oleh pelanggan: orang lain 404; pemilik 204 (berkas & baris hilang, log); kedua kali 404.
	del := func(tok string) int {
		rq := httptest.NewRequest("DELETE", path, nil)
		rq.Header.Set("Authorization", "Bearer "+tok)
		rq.RemoteAddr = "203.0.113.7:5555"
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, rq)
		return rw.Code
	}
	if c := del(e.tok2); c != 404 {
		t.Fatalf("hapus orang lain: %d", c)
	}
	if c := del(e.tok); c != 204 {
		t.Fatalf("hapus: %d", c)
	}
	if len(filesIn(t, e.dir)) != 0 || proofKeyOf(db, id) != "" {
		t.Fatal("berkas & baris harus terhapus")
	}
	if l := lastLog(t, db, "order.payment_proof_delete"); l == nil || !strings.Contains(det(l), no) {
		t.Fatalf("log hapus: %v", det(l))
	}
	if c := del(e.tok); c != 404 {
		t.Fatalf("hapus kedua: %d", c)
	}

	// Terkunci setelah dibayar: unggah & hapus 409, lihat tetap boleh.
	if r := uploadProof(t, h, no, e.tok, jpg, "b.jpg"); r.Code != 200 {
		t.Fatalf("unggah ulang: %d", r.Code)
	}
	adminSetStatus(t, h, id, StatusPending, StatusPaid)
	if r := uploadProof(t, h, no, e.tok, jpg, "b.jpg"); r.Code != 409 {
		t.Fatalf("unggah setelah dibayar: %d", r.Code)
	}
	if c := del(e.tok); c != 409 {
		t.Fatalf("hapus setelah dibayar: %d", c)
	}
	if w := getRaw(h, path, e.tok, "", t); w.Code != 200 {
		t.Fatalf("lihat setelah dibayar: %d", w.Code)
	}
	if r := call(t, h, "GET", "/api/orders/"+no, e.tok, nil); r.Body["canUploadProof"] != false {
		t.Fatal("canUploadProof harus false setelah dibayar")
	}
}

// Retensi: bukti pesanan selesai/dibatalkan > 180 hari dihapus (berkas + baris + log ringkasan);
// berkas yang sudah hilang tidak menggagalkan purge.
func TestIntegrationPaymentProofPurge(t *testing.T) {
	e := setupProof(t)
	h, db, app := e.h, e.db, e.app
	mk := func() (string, uint64) {
		no := createOrderFor(t, h, e.tok, 4)
		id := orderIDByNo(db, no)
		confirmCall(t, h, id, 0, 5000)
		if r := uploadProof(t, h, no, e.tok, encJPEG(t, markerImage(200, 100), 85), "b.jpg"); r.Code != 200 {
			t.Fatalf("upload: %d %v", r.Code, r.Body)
		}
		return no, id
	}
	now := time.Now().UTC()
	oldDone, idA := mk()   // selesai 181 hari lalu -> dihapus
	_, idB := mk()         // selesai 179 hari lalu -> tetap
	oldCancel, idC := mk() // batal 200 hari lalu, berkas sudah hilang -> baris tetap dihapus
	_, idD := mk()         // menunggu pembayaran -> tetap
	db.Exec("UPDATE orders SET status = 'completed', completed_at = ? WHERE id = ?", now.Add(-181*24*time.Hour), idA)
	db.Exec("UPDATE orders SET status = 'completed', completed_at = ? WHERE id = ?", now.Add(-179*24*time.Hour), idB)
	db.Exec("UPDATE orders SET status = 'cancelled', cancelled_at = ? WHERE id = ?", now.Add(-200*24*time.Hour), idC)
	keyA, keyB, keyC, keyD := proofKeyOf(db, idA), proofKeyOf(db, idB), proofKeyOf(db, idC), proofKeyOf(db, idD)
	os.Remove(filepath.Join(e.dir, keyC))

	// Bukti milik tes lain di DB uji bersama mungkin ikut terhapus; yang diperiksa hanya milik tes ini.
	n, err := app.purgePaymentProofs(t.Context(), now)
	if err != nil || n < 2 {
		t.Fatalf("purge: %d %v", n, err)
	}
	for _, c := range []struct {
		id   uint64
		key  string
		keep bool
	}{{idA, keyA, false}, {idB, keyB, true}, {idC, keyC, false}, {idD, keyD, true}} {
		has := proofKeyOf(db, c.id) != ""
		_, ferr := os.Stat(filepath.Join(e.dir, c.key))
		if has != c.keep || (ferr == nil) != c.keep {
			t.Errorf("pesanan %d: baris=%v berkas=%v mau %v", c.id, has, ferr == nil, c.keep)
		}
	}
	l := lastLog(t, db, "order.payment_proof_purge")
	if l == nil || !strings.Contains(l.Summary, "bukti transfer dihapus otomatis") || !strings.Contains(det(l), oldDone) || !strings.Contains(det(l), oldCancel) {
		t.Fatalf("log purge: %v %v", l, det(l))
	}
	// Putaran kedua: tidak ada lagi yang dihapus untuk pesanan tes ini.
	if _, err := app.purgePaymentProofs(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if proofKeyOf(db, idB) == "" || proofKeyOf(db, idD) == "" {
		t.Fatal("bukti yang belum lewat retensi tidak boleh terhapus")
	}
}

func TestIntegrationPaymentProofGrants(t *testing.T) {
	_, _, db, _ := setupOrders(t)
	for _, q := range []string{"SELECT COUNT(*) FROM order_payment_proofs", "DELETE FROM order_payment_proofs WHERE id = 0", "UPDATE order_payment_proofs SET mime = mime WHERE id = 0"} {
		if err := db.Exec(q).Error; err != nil {
			t.Errorf("harus boleh: %s -> %v", q, err)
		}
	}
	if err := db.Exec("DROP TABLE order_payment_proofs").Error; mysqlErrNo(err) != 1142 {
		t.Errorf("DDL harus ditolak: %v", err)
	}
}
