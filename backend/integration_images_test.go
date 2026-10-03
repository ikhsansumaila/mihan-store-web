//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"gorm.io/gorm"
)

type uploadOpts struct {
	field, filename, partType string
	contentType               string // menimpa Content-Type permintaan
	noCSRF, badOrigin, noAuth bool
	method                    string
}

// uploadCall mengirim multipart ke /api/admin/products/{id}/image seperti browser sama-origin.
func uploadCall(t *testing.T, h http.Handler, id uint64, data []byte, o uploadOpts) resp {
	t.Helper()
	if o.field == "" {
		o.field = "file"
	}
	if o.filename == "" {
		o.filename = "foto.jpg"
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, o.field, o.filename))
	if o.partType == "" {
		o.partType = "image/jpeg"
	}
	hdr.Set("Content-Type", o.partType)
	pw, _ := mw.CreatePart(hdr)
	pw.Write(data)
	mw.Close()
	method := o.method
	if method == "" {
		method = "POST"
	}
	r := httptest.NewRequest(method, fmt.Sprintf("/api/admin/products/%d/image", id), &buf)
	r.RemoteAddr = "172.22.0.2:5555"
	r.Header.Set("X-Real-IP", "198.51.100.20")
	r.Header.Set("User-Agent", "integration-admin")
	if !o.noAuth {
		r.Header.Set("Cf-Access-Jwt-Assertion", accessToken(t, integAdmin))
	}
	ct := mw.FormDataContentType()
	if o.contentType != "" {
		ct = o.contentType
	}
	r.Header.Set("Content-Type", ct)
	if !o.noCSRF {
		r.Header.Set("X-Requested-With", adminRequestedWith)
	}
	r.Header.Set("Origin", "https://store.mihan.web.id")
	if o.badOrigin {
		r.Header.Set("Origin", "https://jahat.example")
	}
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m)
	return resp{w.Code, m}
}

func listFiles(t *testing.T, base string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			rel, _ := filepath.Rel(base, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func imagePathOf(db *gorm.DB, id uint64) string {
	var s *string
	db.Raw("SELECT image_path FROM products WHERE id = ?", id).Scan(&s)
	if s == nil {
		return ""
	}
	return *s
}

func publicProductByID(t *testing.T, h http.Handler, path string, id uint64) map[string]any {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var arr []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &arr); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	for _, p := range arr {
		if uint64(num(p["id"])) == id {
			return p
		}
	}
	return nil
}

func synthJPEG(t *testing.T, w, h int) []byte { return encJPEG(t, markerImage(w, h), 88) }

func TestIntegrationProductImages(t *testing.T) {
	app, _, db := setupAdminIntegration(t)
	app.cartLimiter = NewRateLimiter(100000, time.Minute)
	app.imageLimiter = NewRateLimiter(100000, time.Minute) // batas laju diuji terpisah di akhir
	base := t.TempDir()
	app.setImageStore(NewLocalStore(base, "/uploads/"), 2048)
	app.imageGuard.startupCheck(base)
	h := newRouter(app)

	pid := createTierProduct(t, h, tierBody("Produk Foto Uji", 10000, "pcs"))
	var adm User
	db.Where("email = ?", integAdmin).Take(&adm)

	// ---- unggah pertama: JPEG kamera 2000x1500 ----
	r := uploadCall(t, h, pid, synthJPEG(t, 2000, 1500), uploadOpts{filename: "../../../etc/evil.php"})
	if r.Code != 200 {
		t.Fatalf("unggah: %d %v", r.Code, r.Body)
	}
	key1 := imagePathOf(db, pid)
	if !productImageKeyFor(key1, pid) {
		t.Fatalf("image_path harus kunci baru: %q", key1)
	}
	if r.Body["image"] != "/uploads/"+key1 || r.Body["thumb"] != "/uploads/"+thumbKey(key1) || len(r.Body) != 2 {
		t.Fatalf("respons {image, thumb}: %v", r.Body)
	}
	if got := listFiles(t, base); len(got) != 2 || got[0] != key1 || got[1] != thumbKey(key1) {
		t.Fatalf("berkas di disk: %v (mau %s + thumbnail)", got, key1)
	}
	if strings.Contains(key1, "evil") || strings.Contains(key1, "..") {
		t.Fatal("nama berkas klien tidak boleh dipakai")
	}
	mainF, _ := os.ReadFile(filepath.Join(base, key1))
	thumbF, _ := os.ReadFile(filepath.Join(base, thumbKey(key1)))
	if c, _, err := image.DecodeConfig(bytes.NewReader(mainF)); err != nil || c.Width != 1200 || c.Height != 900 {
		t.Fatalf("foto utama %v %v", c, err)
	}
	if c, _, err := image.DecodeConfig(bytes.NewReader(thumbF)); err != nil || c.Width != 400 || c.Height != 300 {
		t.Fatalf("thumbnail %v %v", c, err)
	}
	var updatedBy *uint64
	db.Raw("SELECT updated_by FROM products WHERE id = ?", pid).Scan(&updatedBy)
	if updatedBy == nil || *updatedBy != adm.ID {
		t.Fatal("updated_by harus admin pengunggah")
	}
	l := lastLog(t, db, "product.image_update")
	if l == nil || l.EntityID == nil || *l.EntityID != itoa(pid) || !strings.Contains(det(l), `"kunciBaru":"`+key1+`"`) ||
		!strings.Contains(det(l), `"kunciLama":null`) || !strings.Contains(det(l), fmt.Sprintf(`"ukuranByte":%d`, len(mainF))) {
		t.Fatalf("log product.image_update: %s", det(l))
	}
	if strings.Contains(det(l), "evil") || l.UserID == nil || *l.UserID != adm.ID {
		t.Fatalf("log tidak boleh memuat nama berkas klien: %s", det(l))
	}

	// ---- API publik, pencarian, admin, keranjang ----
	for _, path := range []string{"/api/products", "/api/products/search?q=foto%20uji"} {
		p := publicProductByID(t, h, path, pid)
		if p == nil || p["image"] != "/uploads/"+key1 || p["thumb"] != "/uploads/"+thumbKey(key1) {
			t.Fatalf("%s: %v", path, p)
		}
	}
	legacy := publicProductByID(t, h, "/api/products", 1)
	if legacy == nil || legacy["image"] != "" || legacy["thumb"] != "" {
		t.Fatalf("produk lama (kerupuk1.jpg) harus image/thumb kosong: %v", legacy)
	}
	if ar := adminCall(t, h, "GET", "/api/admin/products/"+itoa(pid), integAdmin, nil); ar.Body["thumb"] != "/uploads/"+thumbKey(key1) || ar.Body["imagePath"] != key1 {
		t.Fatalf("admin DTO: %v", ar.Body)
	}
	tok := newCustomer(t, h, "Pembeli Foto")
	call(t, h, "POST", "/api/cart/items", tok, map[string]any{"productId": pid, "qty": 1})
	call(t, h, "POST", "/api/cart/items", tok, map[string]any{"productId": 1, "qty": 1})
	cr := call(t, h, "GET", "/api/cart", tok, nil)
	if line := cartLineOf(t, cr.Body, pid); line["thumb"] != "/uploads/"+thumbKey(key1) || line["image"] != "/uploads/"+key1 {
		t.Fatalf("keranjang thumb: %v", line)
	}
	if line := cartLineOf(t, cr.Body, 1); line["thumb"] != "" || line["image"] != "" {
		t.Fatalf("keranjang produk lama: %v", line)
	}

	// ---- ganti foto (PNG) -> berkas lama terhapus ----
	r = uploadCall(t, h, pid, encPNG(t, markerImage(640, 480)), uploadOpts{filename: "x.png", partType: "image/png"})
	if r.Code != 200 {
		t.Fatalf("ganti: %d %v", r.Code, r.Body)
	}
	key2 := imagePathOf(db, pid)
	if key2 == key1 || !productImageKeyFor(key2, pid) {
		t.Fatal("kunci harus baru")
	}
	if got := listFiles(t, base); len(got) != 2 || got[0] != key2 && got[1] != key2 {
		t.Fatalf("berkas lama harus terhapus: %v", got)
	}
	if l := lastLog(t, db, "product.image_update"); !strings.Contains(det(l), `"kunciLama":"`+key1+`"`) || !strings.Contains(det(l), `"kunciBaru":"`+key2+`"`) {
		t.Fatalf("log ganti: %s", det(l))
	}

	// ---- transaksi gagal (galat DB tiruan) -> berkas baru dibersihkan, DB & log tidak berubah ----
	logsBefore := countLogs(db, "action = 'product.image_update'")
	app.imageTxHook = func(tx *gorm.DB) error { return errors.New("galat DB tiruan") }
	r = uploadCall(t, h, pid, synthJPEG(t, 800, 600), uploadOpts{})
	app.imageTxHook = nil
	if r.Code != 500 {
		t.Fatalf("galat transaksi harus 500: %d %v", r.Code, r.Body)
	}
	if imagePathOf(db, pid) != key2 || countLogs(db, "action = 'product.image_update'") != logsBefore {
		t.Fatal("rollback: image_path/log tidak boleh berubah")
	}
	if got := listFiles(t, base); len(got) != 2 {
		t.Fatalf("berkas baru dari transaksi gagal harus dihapus: %v", got)
	}
	// Galat nyata dari DB: log ditolak (kolom summary terlalu panjang tidak mungkin, jadi pakai tx yang dibatalkan).
	app.imageTxHook = func(tx *gorm.DB) error { return tx.Exec("SELECT * FROM tabel_tidak_ada").Error }
	r = uploadCall(t, h, pid, synthJPEG(t, 300, 300), uploadOpts{})
	app.imageTxHook = nil
	if r.Code != 500 || imagePathOf(db, pid) != key2 || len(listFiles(t, base)) != 2 {
		t.Fatalf("galat SQL di transaksi: %d, path %s, berkas %v", r.Code, imagePathOf(db, pid), listFiles(t, base))
	}

	// ---- penolakan per kasus ----
	var gb bytes.Buffer
	gif.Encode(&gb, markerImage(10, 10), nil)
	small := synthJPEG(t, 100, 100)
	pngSmall := encPNG(t, image.NewGray(image.Rect(0, 0, 8, 8)))
	rejects := []struct {
		name string
		data []byte
		o    uploadOpts
		code int
		msg  string
	}{
		{"gif", gb.Bytes(), uploadOpts{filename: "a.gif"}, 415, "Format foto tidak didukung"},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`), uploadOpts{filename: "a.svg", partType: "image/svg+xml"}, 415, "Format"},
		{"teks .jpg", []byte("bukan gambar sama sekali"), uploadOpts{filename: "foto.jpg", partType: "image/jpeg"}, 415, "Format"},
		{"jpeg rusak", small[:len(small)/2], uploadOpts{}, 422, "rusak"},
		{"> 2 MB", append(append([]byte{}, small...), make([]byte, maxUploadBytes)...), uploadOpts{}, 413, "2 MB"},
		{"bom piksel", patchPNGSize(pngSmall, 9000, 9000), uploadOpts{partType: "image/png"}, 422, "Resolusi"},
		{"tanpa field file", small, uploadOpts{field: "gambar"}, 400, "Pilih berkas"},
		{"bukan multipart", small, uploadOpts{contentType: "application/json"}, 400, "multipart"},
	}
	for _, c := range rejects {
		r := uploadCall(t, h, pid, c.data, c.o)
		if r.Code != c.code || !strings.Contains(fmt.Sprint(r.Body["error"]), c.msg) {
			t.Errorf("%s: %d %v (mau %d %q)", c.name, r.Code, r.Body, c.code, c.msg)
		}
	}
	if imagePathOf(db, pid) != key2 || len(listFiles(t, base)) != 2 {
		t.Fatal("penolakan tidak boleh mengubah data/berkas")
	}

	// ---- produk tidak ada / dihapus -> 404 ----
	if r := uploadCall(t, h, 99999999, small, uploadOpts{}); r.Code != 404 {
		t.Fatalf("produk tidak ada: %d", r.Code)
	}
	gone := createTierProduct(t, h, tierBody("Produk Dihapus Foto", 5000, "pcs"))
	adminCall(t, h, "DELETE", "/api/admin/products/"+itoa(gone), integAdmin, map[string]any{})
	if r := uploadCall(t, h, gone, small, uploadOpts{}); r.Code != 404 {
		t.Fatalf("produk dihapus: %d", r.Code)
	}
	if r := adminCall(t, h, "DELETE", "/api/admin/products/"+itoa(gone)+"/image", integAdmin, map[string]any{}); r.Code != 404 {
		t.Fatalf("hapus foto produk dihapus: %d", r.Code)
	}

	// ---- PUT produk tidak mengubah foto unggahan; kunci foto tidak bisa disetel manual ----
	put := map[string]any{"name": "Produk Foto Uji", "price": 11000, "categoryId": 1, "description": "uji grosir", "unit": "pcs", "tiers": []any{}}
	if r := adminCall(t, h, "PUT", "/api/admin/products/"+itoa(pid), integAdmin, put); r.Code != 200 || imagePathOf(db, pid) != key2 {
		t.Fatalf("PUT tanpa imagePath harus mempertahankan foto: %d %s", r.Code, imagePathOf(db, pid))
	}
	put["imagePath"] = "kerupuk1.jpg"
	if r := adminCall(t, h, "PUT", "/api/admin/products/"+itoa(pid), integAdmin, put); r.Code != 200 || imagePathOf(db, pid) != key2 {
		t.Fatalf("PUT imagePath lama tidak boleh menimpa foto unggahan: %d %s", r.Code, imagePathOf(db, pid))
	}
	other := createTierProduct(t, h, tierBody("Produk Lain Foto", 5000, "pcs"))
	if r := adminCall(t, h, "PUT", "/api/admin/products/"+itoa(other), integAdmin, map[string]any{"name": "Produk Lain Foto", "price": 5000, "categoryId": 1, "imagePath": key2}); r.Code != 400 {
		t.Fatalf("menyalin kunci foto produk lain harus 400: %d", r.Code)
	}
	if r := adminCall(t, h, "POST", "/api/admin/products", integAdmin, map[string]any{"name": "Curang", "price": 1, "categoryId": 1, "imagePath": key2}); r.Code != 400 {
		t.Fatalf("POST dengan kunci foto harus 400: %d", r.Code)
	}

	// ---- autentikasi & CSRF ----
	if r := uploadCall(t, h, pid, small, uploadOpts{noAuth: true}); r.Code != 401 {
		t.Fatalf("tanpa Access: %d", r.Code)
	}
	if r := uploadCall(t, h, pid, small, uploadOpts{noCSRF: true}); r.Code != 403 {
		t.Fatalf("tanpa X-Requested-With: %d", r.Code)
	}
	if r := uploadCall(t, h, pid, small, uploadOpts{badOrigin: true}); r.Code != 403 {
		t.Fatalf("origin asing: %d", r.Code)
	}
	if r := uploadCall(t, h, pid, small, uploadOpts{contentType: "text/plain"}); r.Code != 403 {
		t.Fatalf("content-type lain: %d", r.Code)
	}
	// Multipart hanya untuk rute unggah: PUT produk dengan multipart -> 403.
	{
		req := httptest.NewRequest("PUT", "/api/admin/products/"+itoa(pid), strings.NewReader("--x--"))
		req.RemoteAddr = "172.22.0.2:5555"
		req.Header.Set("Cf-Access-Jwt-Assertion", accessToken(t, integAdmin))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
		req.Header.Set("X-Requested-With", adminRequestedWith)
		req.Header.Set("Origin", "https://store.mihan.web.id")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatalf("multipart ke rute lain harus 403: %d", w.Code)
		}
	}
	if imagePathOf(db, pid) != key2 {
		t.Fatal("permintaan ditolak tidak boleh mengubah foto")
	}

	// ---- hapus foto ----
	r = adminCall(t, h, "DELETE", "/api/admin/products/"+itoa(pid)+"/image", integAdmin, map[string]any{})
	if r.Code != 200 || r.Body["image"] != "" || r.Body["thumb"] != "" {
		t.Fatalf("hapus foto: %d %v", r.Code, r.Body)
	}
	if imagePathOf(db, pid) != "" || len(listFiles(t, base)) != 0 {
		t.Fatalf("hapus: path %q berkas %v", imagePathOf(db, pid), listFiles(t, base))
	}
	if l := lastLog(t, db, "product.image_delete"); l == nil || !strings.Contains(det(l), `"kunciLama":"`+key2+`"`) {
		t.Fatal("log product.image_delete")
	}
	n := countLogs(db, "action = 'product.image_delete'")
	if r := adminCall(t, h, "DELETE", "/api/admin/products/"+itoa(pid)+"/image", integAdmin, map[string]any{}); r.Code != 200 || countLogs(db, "action = 'product.image_delete'") != n {
		t.Fatal("hapus kedua: 200 tanpa log baru")
	}
	if p := publicProductByID(t, h, "/api/products", pid); p["image"] != "" || p["thumb"] != "" {
		t.Fatalf("publik setelah hapus: %v", p)
	}
	// Hapus nilai lama (legacy) mengosongkan kolom tanpa menyentuh berkas apa pun.
	db.Exec("UPDATE products SET image_path = 'warisan.jpg' WHERE id = ?", other)
	if r := adminCall(t, h, "DELETE", "/api/admin/products/"+itoa(other)+"/image", integAdmin, map[string]any{}); r.Code != 200 || imagePathOf(db, other) != "" {
		t.Fatal("hapus nilai lama")
	}

	// ---- pagar disk & penyimpanan tidak tersedia ----
	app.setImageStore(NewLocalStore(base, "/uploads/"), 1)
	app.imageGuard.startupCheck(base)
	if err := app.imageGuard.check(context.Background()); err != nil { // pengukuran awal (0 byte)
		t.Fatal(err)
	}
	app.imageGuard.add(2 << 20) // seolah 2 MB terpakai, batas 1 MB
	if r := uploadCall(t, h, pid, small, uploadOpts{}); r.Code != 507 || !strings.Contains(fmt.Sprint(r.Body["error"]), "penuh") {
		t.Fatalf("penyimpanan penuh: %d %v", r.Code, r.Body)
	}
	app.setImageStore(&brokenStore{NewLocalStore(base, "/uploads/")}, 2048)
	app.imageGuard.startupCheck(base)
	if r := uploadCall(t, h, pid, small, uploadOpts{}); r.Code != 503 || !strings.Contains(fmt.Sprint(r.Body["error"]), "tidak tersedia") {
		t.Fatalf("penyimpanan tidak dapat ditulis: %d %v", r.Code, r.Body)
	}
	// Bagian lain toko tetap jalan.
	if p := publicProductByID(t, h, "/api/products", 1); p == nil {
		t.Fatal("API publik harus tetap jalan")
	}
	// Disk penuh saat menulis (ENOSPC) -> 507, tidak ada berkas tertinggal.
	app.setImageStore(&fullStore{NewLocalStore(base, "/uploads/")}, 2048)
	if r := uploadCall(t, h, pid, small, uploadOpts{}); r.Code != 507 || len(listFiles(t, base)) != 0 {
		t.Fatalf("ENOSPC: %d %v", r.Code, listFiles(t, base))
	}

	// ---- batas laju unggah per admin ----
	app.setImageStore(NewLocalStore(base, "/uploads/"), 2048)
	app.imageLimiter = NewRateLimiter(2, time.Minute)
	codes := []int{}
	for i := 0; i < 3; i++ {
		codes = append(codes, uploadCall(t, h, pid, small, uploadOpts{}).Code)
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != 429 {
		t.Fatalf("batas laju: %v", codes)
	}
	// Bersihkan: foto terakhir dihapus lewat API (berkas hilang).
	app.imageLimiter = NewRateLimiter(100, time.Minute)
	adminCall(t, h, "DELETE", "/api/admin/products/"+itoa(pid)+"/image", integAdmin, map[string]any{})
	if got := listFiles(t, base); len(got) != 0 {
		t.Fatalf("berkas tersisa: %v", got)
	}
}

// brokenStore: direktori tidak dapat ditulis.
type brokenStore struct{ *LocalStore }

func (b *brokenStore) Writable() error { return fs.ErrPermission }

// fullStore: penulisan gagal karena disk penuh.
type fullStore struct{ *LocalStore }

func (f *fullStore) Save(ctx context.Context, key string, data []byte) error {
	return wrapStoreErr(&os.PathError{Op: "write", Path: key, Err: errENOSPCForTest})
}

var errENOSPCForTest = syscall.ENOSPC
