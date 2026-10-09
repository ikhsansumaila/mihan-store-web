//go:build integration

// Tes integrasi fitur katalog DB, admin (Cloudflare Access), activity log, dan login Google
// terhadap database UJI yang baru dibuat dari migrasi 001–005.
package app

import (
	"bytes"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"mihanstore/internal/admin"
	"mihanstore/internal/audit"
	"mihanstore/internal/catalog"
	"mihanstore/internal/identity"
	"mihanstore/internal/platform"
)

const integAdmin = "admin.uji@uji.test"

func setupAdminIntegration(t *testing.T) (*App, http.Handler, *gorm.DB) {
	t.Helper()
	app, _ := setupIntegration(t)
	app.cfg.AdminEmails = []string{integAdmin, "promosi@uji.test", "tangguh@uji.test", "google.admin@uji.test"}
	app.cfg.CFAccessTeamDomain, app.cfg.CFAccessAUD = testTeam, testAUD
	av, err := admin.NewAccessVerifier(app.cfg)
	if err != nil {
		t.Fatal(err)
	}
	av.JWKS = stubJWKS(map[string]*rsa.PublicKey{"k1": &testKey.PublicKey})
	app.admin.Access = av
	app.cfg.GoogleClientID, app.cfg.AuthHMACSecret = testGoogleClient, strings.Repeat("h", 48)
	app.identity.Google = identity.NewGoogleVerifier(app.cfg)
	app.identity.Google.JWKS = stubJWKS(map[string]*rsa.PublicKey{"g1": &testKey.PublicKey})
	app.admin.Limiter = platform.NewRateLimiter(100000, time.Minute)
	app.identity.GoogleLimiter = platform.NewRateLimiter(100000, time.Minute)
	return app, newRouter(app), app.db.Load()
}

func accessToken(t *testing.T, email string) string {
	return signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "email", email))
}

// adminCall memanggil rute admin seperti browser sama-origin di belakang Cloudflare Access.
func adminCall(t *testing.T, h http.Handler, method, path, email string, body any) resp {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, path, &buf)
	r.RemoteAddr = "172.22.0.2:5555"
	r.Header.Set("X-Real-IP", "198.51.100.20")
	r.Header.Set("User-Agent", "integration-admin")
	if email != "" {
		r.Header.Set("Cf-Access-Jwt-Assertion", accessToken(t, email))
	}
	if method != "GET" {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Requested-With", adminRequestedWith)
		r.Header.Set("Origin", "https://store.mihan.web.id")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m)
	return resp{w.Code, m}
}

func lastLog(t *testing.T, db *gorm.DB, action string) *audit.ActivityLog {
	t.Helper()
	var l audit.ActivityLog
	if err := db.Where("action = ?", action).Order("id DESC").Take(&l).Error; err != nil {
		return nil
	}
	return &l
}

// det mengembalikan details dalam bentuk JSON ringkas (MySQL menambah spasi pada JSON).
func det(l *audit.ActivityLog) string {
	if l == nil || l.Details == nil {
		return ""
	}
	var b bytes.Buffer
	if json.Compact(&b, []byte(*l.Details)) != nil {
		return *l.Details
	}
	return b.String()
}

func countLogs(db *gorm.DB, where string, args ...any) int64 {
	var n int64
	db.Model(&audit.ActivityLog{}).Where(where, args...).Count(&n)
	return n
}

func mysqlErrNo(err error) uint16 {
	var me *mysqldrv.MySQLError
	if errors.As(err, &me) {
		return me.Number
	}
	return 0
}

func TestIntegrationSchemaSeedAndGrants(t *testing.T) {
	_, h, db := setupAdminIntegration(t)

	// Seed: 6 kategori, 20 produk, AUTO_INCREMENT berikutnya 21.
	var nCat, nProd int64
	db.Model(&catalog.Category{}).Count(&nCat)
	db.Model(&catalog.Product{}).Count(&nProd)
	if nCat < 6 || nProd < 20 {
		t.Fatalf("seed kurang: %d kategori, %d produk", nCat, nProd)
	}

	// Akun password lama (dibuat sebelum migrasi 002) tetap utuh.
	var lama identity.User
	if err := db.Where("username = ?", "lama_sebelum_002").Take(&lama).Error; err != nil || lama.PasswordHash == nil || lama.GoogleSub != nil {
		t.Fatalf("akun lama rusak setelah migrasi: %v", err)
	}

	// API publik identik dengan snapshot produksi sebelum migrasi.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/products", nil))
	if p := os.Getenv("PRODUCTS_SNAPSHOT"); p != "" {
		want, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		// Field lama identik byte-per-byte; field baru (unit, tiers) hanya ditambahkan di belakang.
		if err := compareProductsSnapshot(want, w.Body.Bytes()); err != nil {
			t.Fatalf("/api/products berbeda dari snapshot: %v", err)
		}
	}
	// search: q + category, tanpa hasil -> []
	for path, n := range map[string]int{"/api/products/search?q=kerupuk": 4, "/api/products/search?category=saos": 3,
		"/api/products/search?q=zzz": 0, "/api/products/search?q=%25": 0, "/api/products/search?category=SAOS": 0,
		"/api/products/search?q=HAMPERS&category=box_hampers": 4} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		var arr []catalog.PublicProduct
		if err := json.Unmarshal(w.Body.Bytes(), &arr); err != nil || w.Code != 200 || len(arr) != n || (n == 0 && strings.TrimSpace(w.Body.String()) != "[]") {
			t.Errorf("%s: %d item (mau %d) %s", path, len(arr), n, w.Body.String())
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/categories", nil))
	var cats []catalog.PublicCategory
	json.Unmarshal(w.Body.Bytes(), &cats)
	if len(cats) < 6 || cats[0].Slug != "kerupuk" || cats[5].Name != "Box Hampers" {
		t.Fatalf("/api/categories: %s", w.Body.String())
	}

	// Hak akses: activity_logs append-only, users/products tanpa DELETE.
	if err := db.Exec("INSERT INTO activity_logs (action, summary, created_at) VALUES ('uji.grant', 'uji', UTC_TIMESTAMP(3))").Error; err != nil {
		t.Fatalf("INSERT log harus boleh: %v", err)
	}
	for _, q := range []string{
		"DELETE FROM activity_logs WHERE action = 'uji.grant'",
		"UPDATE activity_logs SET summary = 'ubah' WHERE action = 'uji.grant'",
		"TRUNCATE TABLE activity_logs",
		"DELETE FROM users WHERE id = 0",
		"DELETE FROM products WHERE id = 0",
		"DELETE FROM categories WHERE id = 0",
		"DROP PROCEDURE purge_activity_logs",
		"ALTER TABLE activity_logs ADD COLUMN x INT",
	} {
		err := db.Exec(q).Error
		if err == nil || (mysqlErrNo(err) != 1142 && mysqlErrNo(err) != 1370) {
			t.Errorf("harus ditolak (1142/1370): %s -> %v", q, err)
		}
	}
	if countLogs(db, "action = 'uji.grant'") != 1 {
		t.Fatal("baris log berubah walau perintah ditolak")
	}
}

func TestIntegrationPurgeProcedure(t *testing.T) {
	_, h, db := setupAdminIntegration(t)
	// Baris uji bertanda: 181 hari (dihapus), 179 hari (tetap), sekarang (tetap).
	db.Exec(`INSERT INTO activity_logs (action, summary, created_at) VALUES
		('uji.purge_tua', 'tua', UTC_TIMESTAMP(3) - INTERVAL 181 DAY),
		('uji.purge_tua', 'tua', UTC_TIMESTAMP(3) - INTERVAL 400 DAY),
		('uji.purge_batas', '179 hari', UTC_TIMESTAMP(3) - INTERVAL 179 DAY),
		('uji.purge_baru', 'baru', UTC_TIMESTAMP(3))`)
	r := adminCall(t, h, "POST", "/api/admin/activity-logs/purge", integAdmin, map[string]any{})
	if r.Code != 200 || r.Body["deleted"].(float64) < 2 {
		t.Fatalf("purge: %d %v", r.Code, r.Body)
	}
	if countLogs(db, "action = 'uji.purge_tua'") != 0 || countLogs(db, "action = 'uji.purge_batas'") != 1 || countLogs(db, "action = 'uji.purge_baru'") != 1 {
		t.Fatal("purge harus hanya menghapus baris > 180 hari")
	}
	l := lastLog(t, db, "activity_log.purge")
	if l == nil || !strings.Contains(det(l), `"jumlahDihapus"`) {
		t.Fatal("purge harus dicatat dengan jumlah terhapus")
	}
	// Panggil langsung (seperti dari aplikasi) juga hanya bisa lewat procedure.
	var n int64
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("CALL purge_activity_logs(@x)").Error; err != nil {
			return err
		}
		return tx.Raw("SELECT @x").Scan(&n).Error
	}); err != nil || n != 0 {
		t.Fatalf("CALL kedua: n=%d err=%v", n, err)
	}
}

func TestIntegrationAdminAccess(t *testing.T) {
	app, h, db := setupAdminIntegration(t)

	// Pelanggan biasa (sesi toko) tidak bisa masuk /api/admin tanpa Access.
	rr := call(t, h, "POST", "/api/auth/register", "", reg("pelanggan_adm", "pelanggan.adm@uji.test", "", "Pelanggan", "passwordku123"))
	custTok := rr.Body["token"].(string)
	if r := call(t, h, "GET", "/api/admin/me", custTok, nil); r.Code != 401 {
		t.Fatalf("pelanggan dengan token sesi ke admin harus 401: %d", r.Code)
	}
	// Email pelanggan bertoken Access sah tetapi bukan ADMIN_EMAILS -> 403.
	if r := adminCall(t, h, "GET", "/api/admin/me", "pelanggan.adm@uji.test", nil); r.Code != 403 {
		t.Fatalf("email non-admin: %d", r.Code)
	}
	// Header Access palsu -> 401, dicatat dengan pembatasan frekuensi.
	before := countLogs(db, "action = 'admin.access_denied'")
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest("GET", "/api/admin/me", nil)
		r.RemoteAddr = "172.22.0.2:1"
		r.Header.Set("X-Real-IP", "198.51.100.99")
		r.Header.Set("Cf-Access-Jwt-Assertion", "eyJhbGciOiJSUzI1NiJ9.e30.palsu")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("header palsu: %d", w.Code)
		}
	}
	if d := countLogs(db, "action = 'admin.access_denied'") - before; d != 1 {
		t.Fatalf("access_denied harus dicatat 1x (dibatasi), dapat %d", d)
	}

	// Admin baru dibuat otomatis: role admin, aktif, tanpa password, email terverifikasi.
	r := adminCall(t, h, "GET", "/api/admin/me", strings.ToUpper(integAdmin), nil)
	if r.Code != 200 || r.Body["user"].(map[string]any)["role"] != "admin" {
		t.Fatalf("admin me: %d %v", r.Code, r.Body)
	}
	var adm identity.User
	db.Where("email = ?", integAdmin).Take(&adm)
	if adm.PasswordHash != nil || adm.EmailVerifiedAt == nil || adm.Status != "active" || adm.Username != "admin_uji" && !strings.HasPrefix(adm.Username, "user") {
		t.Fatalf("akun admin otomatis salah: %+v", adm)
	}
	// Panggilan kedua tidak membuat duplikat.
	adminCall(t, h, "GET", "/api/admin/me", integAdmin, nil)
	var n int64
	db.Model(&identity.User{}).Where("email = ?", integAdmin).Count(&n)
	if n != 1 {
		t.Fatalf("akun admin terduplikasi: %d", n)
	}

	// Pelanggan lama yang emailnya ada di ADMIN_EMAILS dinaikkan role-nya + dicatat.
	call(t, h, "POST", "/api/auth/register", "", reg("calon_promosi", "promosi@uji.test", "", "Promosi", "passwordku123"))
	if r := adminCall(t, h, "GET", "/api/admin/me", "promosi@uji.test", nil); r.Code != 200 {
		t.Fatalf("promosi: %d", r.Code)
	}
	if l := lastLog(t, db, "user.role_change"); l == nil || !strings.Contains(det(l), `"menjadi":"admin"`) {
		t.Fatal("promosi role harus dicatat user.role_change")
	}

	// Role/status dibaca ulang tiap permintaan: suspended -> 403 + dicatat.
	adminCall(t, h, "GET", "/api/admin/me", "tangguh@uji.test", nil)
	db.Model(&identity.User{}).Where("email = ?", "tangguh@uji.test").Update("status", "suspended")
	if r := adminCall(t, h, "GET", "/api/admin/me", "tangguh@uji.test", nil); r.Code != 403 {
		t.Fatalf("admin suspended harus 403: %d", r.Code)
	}
	if l := lastLog(t, db, "admin.access_denied"); l == nil || l.ActorLabel == nil || *l.ActorLabel != "tangguh@uji.test" {
		t.Fatal("penolakan admin suspended harus dicatat")
	}

	// CSRF: POST tanpa header kustom -> 403 walau token Access sah.
	req := httptest.NewRequest("POST", "/api/admin/categories", strings.NewReader(`{"slug":"csrf_uji","name":"X"}`))
	req.Header.Set("Cf-Access-Jwt-Assertion", accessToken(t, integAdmin))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("CSRF harus 403: %d", w.Code)
	}
	var cnt int64
	db.Model(&catalog.Category{}).Where("slug = ?", "csrf_uji").Count(&cnt)
	if cnt != 0 {
		t.Fatal("permintaan CSRF tidak boleh mengubah data")
	}
	_ = app
}

func TestIntegrationAdminCatalog(t *testing.T) {
	_, h, db := setupAdminIntegration(t)
	adminCall(t, h, "GET", "/api/admin/me", integAdmin, nil)
	var adm identity.User
	db.Where("email = ?", integAdmin).Take(&adm)

	// Kategori: buat, slug duplikat -> 409, slug invalid -> 400.
	r := adminCall(t, h, "POST", "/api/admin/categories", integAdmin, map[string]any{"slug": "bumbu_uji", "name": "Bumbu Uji", "sortOrder": 70})
	if r.Code != 201 {
		t.Fatalf("buat kategori: %d %v", r.Code, r.Body)
	}
	catID := uint64(r.Body["id"].(float64))
	if r := adminCall(t, h, "POST", "/api/admin/categories", integAdmin, map[string]any{"slug": "bumbu_uji", "name": "Lagi"}); r.Code != 409 {
		t.Fatalf("slug duplikat harus 409: %d", r.Code)
	}
	if r := adminCall(t, h, "POST", "/api/admin/categories", integAdmin, map[string]any{"slug": "Bumbu Uji", "name": "X"}); r.Code != 400 {
		t.Fatalf("slug invalid harus 400: %d", r.Code)
	}

	// Produk: buat -> muncul di API publik, created_by/updated_by terisi, tercatat.
	r = adminCall(t, h, "POST", "/api/admin/products", integAdmin, map[string]any{
		"name": "Bumbu Rendang Uji", "description": "Bumbu instan", "price": 12500, "categoryId": catID, "imagePath": "bumbu/rendang.jpg"})
	if r.Code != 201 || r.Body["createdBy"] == nil {
		t.Fatalf("buat produk: %d %v", r.Code, r.Body)
	}
	pid := uint64(r.Body["id"].(float64))
	if pid <= 20 {
		t.Fatalf("id produk baru harus > 20: %d", pid)
	}
	var p catalog.Product
	db.Take(&p, pid)
	if p.CreatedBy == nil || *p.CreatedBy != adm.ID || p.UpdatedBy == nil {
		t.Fatal("created_by/updated_by tidak terisi")
	}
	if l := lastLog(t, db, "product.create"); l == nil || *l.EntityID != itoa(pid) || l.UserID == nil || *l.UserID != adm.ID || len(l.IP) != 4 {
		t.Fatal("product.create tidak tercatat dengan benar")
	}
	if !publicHas(t, h, "Bumbu Rendang Uji") {
		t.Fatal("produk aktif harus tampil di API publik")
	}
	// Validasi: harga pecahan / kategori tidak ada / harga > 1M.
	for _, body := range []map[string]any{
		{"name": "X", "price": 1.5, "categoryId": catID},
		{"name": "X", "price": 100, "categoryId": 999999},
		{"name": "X", "price": 1000000001, "categoryId": catID},
		{"name": "X", "price": 100, "categoryId": catID, "imagePath": "../../etc/passwd"},
		{"name": "X", "price": 100, "categoryId": catID, "stok": 5},
	} {
		if r := adminCall(t, h, "POST", "/api/admin/products", integAdmin, body); r.Code != 400 {
			t.Errorf("validasi %v harus 400: %d", body, r.Code)
		}
	}

	// Ubah -> selisih tercatat.
	r = adminCall(t, h, "PUT", "/api/admin/products/"+itoa(pid), integAdmin, map[string]any{
		"name": "Bumbu Rendang Uji", "description": "Bumbu instan", "price": 15000, "categoryId": catID, "imagePath": "bumbu/rendang.jpg"})
	if r.Code != 200 || r.Body["price"].(float64) != 15000 {
		t.Fatalf("ubah produk: %d %v", r.Code, r.Body)
	}
	l := lastLog(t, db, "product.update")
	if l == nil || !strings.Contains(det(l), `"price":{"dari":12500,"menjadi":15000}`) || strings.Contains(det(l), `"name"`) {
		t.Fatalf("selisih update salah: %v", l)
	}
	// Ubah tanpa perubahan -> tidak dicatat.
	before := countLogs(db, "action = 'product.update'")
	adminCall(t, h, "PUT", "/api/admin/products/"+itoa(pid), integAdmin, map[string]any{
		"name": "Bumbu Rendang Uji", "description": "Bumbu instan", "price": 15000, "categoryId": catID, "imagePath": "bumbu/rendang.jpg"})
	if countLogs(db, "action = 'product.update'") != before {
		t.Fatal("update tanpa perubahan tidak boleh dicatat")
	}

	// Hapus kategori yang masih punya produk aktif -> 409.
	if r := adminCall(t, h, "DELETE", "/api/admin/categories/"+itoa(catID), integAdmin, map[string]any{}); r.Code != 409 {
		t.Fatalf("hapus kategori berproduk aktif harus 409: %d %v", r.Code, r.Body)
	}

	// Nonaktifkan -> hilang dari API publik, tetap di daftar admin.
	r = adminCall(t, h, "PATCH", "/api/admin/products/"+itoa(pid)+"/active", integAdmin, map[string]any{"active": false})
	if r.Code != 200 || r.Body["isActive"] != false {
		t.Fatalf("nonaktifkan: %d %v", r.Code, r.Body)
	}
	if publicHas(t, h, "Bumbu Rendang Uji") {
		t.Fatal("produk nonaktif tidak boleh tampil di API publik")
	}
	if lastLog(t, db, "product.deactivate") == nil {
		t.Fatal("product.deactivate tidak tercatat")
	}
	r = adminCall(t, h, "GET", "/api/admin/products?status=nonaktif&q=rendang", integAdmin, nil)
	if r.Code != 200 || r.Body["total"].(float64) != 1 {
		t.Fatalf("daftar admin nonaktif: %d %v", r.Code, r.Body)
	}
	r = adminCall(t, h, "GET", "/api/admin/products?per_page=1000", integAdmin, nil)
	if r.Body["perPage"].(float64) != 100 {
		t.Fatal("per_page harus dibatasi 100")
	}
	adminCall(t, h, "PATCH", "/api/admin/products/"+itoa(pid)+"/active", integAdmin, map[string]any{"active": true})
	if lastLog(t, db, "product.activate") == nil || !publicHas(t, h, "Bumbu Rendang Uji") {
		t.Fatal("aktifkan ulang gagal")
	}

	// Hapus produk (soft delete) -> hilang dari publik & admin, baris tetap ada.
	if r := adminCall(t, h, "DELETE", "/api/admin/products/"+itoa(pid), integAdmin, map[string]any{}); r.Code != 200 {
		t.Fatalf("hapus produk: %d", r.Code)
	}
	if publicHas(t, h, "Bumbu Rendang Uji") {
		t.Fatal("produk terhapus masih tampil")
	}
	if r := adminCall(t, h, "GET", "/api/admin/products/"+itoa(pid), integAdmin, nil); r.Code != 404 {
		t.Fatalf("produk terhapus harus 404 di admin: %d", r.Code)
	}
	var raw int64
	db.Unscoped().Model(&catalog.Product{}).Where("id = ? AND deleted_at IS NOT NULL", pid).Count(&raw)
	if raw != 1 || lastLog(t, db, "product.delete") == nil {
		t.Fatal("soft delete produk tidak benar")
	}

	// Kategori kini tanpa produk aktif -> boleh dihapus; slug bisa dipakai ulang (alive).
	if r := adminCall(t, h, "PUT", "/api/admin/categories/"+itoa(catID), integAdmin, map[string]any{"slug": "bumbu_uji", "name": "Bumbu Uji Baru"}); r.Code != 200 {
		t.Fatalf("ubah kategori: %d %v", r.Code, r.Body)
	}
	if l := lastLog(t, db, "category.update"); l == nil || !strings.Contains(det(l), "Bumbu Uji Baru") {
		t.Fatal("category.update tidak tercatat dengan selisih")
	}
	if r := adminCall(t, h, "DELETE", "/api/admin/categories/"+itoa(catID), integAdmin, map[string]any{}); r.Code != 200 {
		t.Fatalf("hapus kategori kosong: %d %v", r.Code, r.Body)
	}
	if lastLog(t, db, "category.delete") == nil {
		t.Fatal("category.delete tidak tercatat")
	}
	r = adminCall(t, h, "POST", "/api/admin/categories", integAdmin, map[string]any{"slug": "bumbu_uji", "name": "Bumbu Uji Lagi"})
	if r.Code != 201 {
		t.Fatalf("slug kategori terhapus harus bisa dipakai ulang: %d %v", r.Code, r.Body)
	}
	// Produk tidak bisa dibuat di kategori terhapus.
	if r := adminCall(t, h, "POST", "/api/admin/products", integAdmin, map[string]any{"name": "X", "price": 1, "categoryId": catID}); r.Code != 400 {
		t.Fatalf("produk di kategori terhapus harus 400: %d", r.Code)
	}

	// Log aktivitas: filter + urut terbaru.
	r = adminCall(t, h, "GET", "/api/admin/activity-logs?entity_type=product&actor=admin.uji&per_page=5", integAdmin, nil)
	items := r.Body["items"].([]any)
	if r.Code != 200 || len(items) == 0 || items[0].(map[string]any)["action"] != "product.delete" || items[0].(map[string]any)["ip"] != "198.51.100.20" {
		t.Fatalf("daftar log: %d %v", r.Code, r.Body)
	}
	today := time.Now().In(platform.WIB).Format("2006-01-02")
	r = adminCall(t, h, "GET", "/api/admin/activity-logs?action=product.create&from="+today+"&to="+today, integAdmin, nil)
	if r.Code != 200 || r.Body["total"].(float64) < 1 {
		t.Fatalf("filter tanggal: %d %v", r.Code, r.Body)
	}
	if r := adminCall(t, h, "GET", "/api/admin/activity-logs?from=02-10-2026", integAdmin, nil); r.Code != 400 {
		t.Fatalf("tanggal salah harus 400: %d", r.Code)
	}
	r = adminCall(t, h, "GET", "/api/admin/summary", integAdmin, nil)
	if r.Code != 200 || len(r.Body["recentActivity"].([]any)) == 0 {
		t.Fatalf("summary: %d %v", r.Code, r.Body)
	}
}

func itoa(n uint64) string { return strconv.FormatUint(n, 10) }

func publicHas(t *testing.T, h http.Handler, name string) bool {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/products", nil))
	return strings.Contains(w.Body.String(), name)
}

func googleCred(t *testing.T, sub, email, name string) map[string]any {
	c := with(with(with(googleClaimsOK(), "sub", sub), "email", email), "name", name)
	return map[string]any{"credential": signJWT(t, testKey, "g1", "RS256", c)}
}

func TestIntegrationGoogle(t *testing.T) {
	_, h, db := setupAdminIntegration(t)

	// Token Google palsu -> 401 dan dicatat tanpa credential.
	bad := map[string]any{"credential": signJWT(t, otherKey, "g1", "RS256", googleClaimsOK())}
	if r := call(t, h, "POST", "/api/auth/google", "", bad); r.Code != 401 {
		t.Fatalf("token palsu: %d", r.Code)
	}
	if l := lastLog(t, db, "auth.login_failed"); l == nil || strings.Contains(det(l), "eyJ") {
		t.Fatal("login Google gagal harus dicatat tanpa token")
	}

	// Pengguna baru: needsProfile -> complete -> sesi.
	r := call(t, h, "POST", "/api/auth/google", "", googleCred(t, "g-sub-baru", "Baru.Google@uji.test", "Baru Google"))
	if r.Code != 200 || r.Body["needsProfile"] != true || r.Body["profileToken"] == nil || r.Body["token"] != nil {
		t.Fatalf("google baru: %d %v", r.Code, r.Body)
	}
	pt := r.Body["profileToken"].(string)
	if r := call(t, h, "POST", "/api/auth/google/complete", "", map[string]any{"profileToken": pt + "x", "username": "baru_google"}); r.Code != 401 {
		t.Fatalf("profileToken rusak harus 401: %d", r.Code)
	}
	if r := call(t, h, "POST", "/api/auth/google/complete", "", map[string]any{"profileToken": pt, "username": "admin_toko"}); r.Code != 400 {
		t.Fatalf("username terlarang harus 400: %d", r.Code)
	}
	r = call(t, h, "POST", "/api/auth/google/complete", "", map[string]any{"profileToken": pt, "username": "baru_google", "phone": "0812 3456 7890"})
	if r.Code != 201 || r.Body["token"] == nil {
		t.Fatalf("complete: %d %v", r.Code, r.Body)
	}
	tok := r.Body["token"].(string)
	if me := call(t, h, "GET", "/api/auth/me", tok, nil); me.Code != 200 || me.Body["user"].(map[string]any)["googleLinked"] != true {
		t.Fatalf("/me akun Google: %d %v", me.Code, me.Body)
	}
	var gu identity.User
	db.Where("username = ?", "baru_google").Take(&gu)
	if gu.PasswordHash != nil || gu.GoogleSub == nil || *gu.GoogleSub != "g-sub-baru" || gu.EmailVerifiedAt == nil || gu.Role != "customer" || gu.Email != "baru.google@uji.test" || gu.Phone == nil {
		t.Fatalf("akun Google salah: %+v", gu)
	}
	// profileToken dipakai ulang -> 409 (akun sudah ada).
	if r := call(t, h, "POST", "/api/auth/google/complete", "", map[string]any{"profileToken": pt, "username": "baru_google2"}); r.Code != 409 {
		t.Fatalf("pemakaian ulang profileToken harus 409: %d", r.Code)
	}
	// Login Google berikutnya -> langsung sesi.
	r = call(t, h, "POST", "/api/auth/google", "", googleCred(t, "g-sub-baru", "baru.google@uji.test", "Baru Google"))
	if r.Code != 200 || r.Body["token"] == nil || lastLog(t, db, "auth.google_login") == nil {
		t.Fatalf("login Google ulang: %d %v", r.Code, r.Body)
	}

	// B3: akun tanpa password mencoba login password -> pesan umum.
	r = call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": "baru_google", "password": "apasajaapa", "turnstileToken": "lulus"})
	if r.Code != 401 || r.Body["error"] != identity.MsgLoginFailed {
		t.Fatalf("B3: %d %v", r.Code, r.Body)
	}
	if l := lastLog(t, db, "auth.login_failed"); l == nil || !strings.Contains(det(l), "akun_tanpa_password") || strings.Contains(det(l), "apasajaapa") {
		t.Fatal("B3 harus dicatat tanpa password")
	}

	// B2: daftar password dengan email akun Google -> 409.
	r = call(t, h, "POST", "/api/auth/register", "", reg("coba_daftar", "BARU.GOOGLE@uji.test", "", "Coba", "passwordku123"))
	if r.Code != 409 || r.Body["error"] != "Email sudah terdaftar, silakan masuk dengan Google" {
		t.Fatalf("B2: %d %v", r.Code, r.Body)
	}

	// B1: email akun password lama -> tersambung, password dihapus, sesi lama dicabut.
	rr := call(t, h, "POST", "/api/auth/register", "", reg("akun_lama_g", "lama.g@uji.test", "", "Lama G", "passwordlama1"))
	oldTok := rr.Body["token"].(string)
	lr := call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": "akun_lama_g", "password": "passwordlama1", "turnstileToken": "lulus"})
	oldTok2 := lr.Body["token"].(string)
	r = call(t, h, "POST", "/api/auth/google", "", googleCred(t, "g-sub-lama", "lama.g@uji.test", "Lama G"))
	if r.Code != 200 || r.Body["token"] == nil {
		t.Fatalf("B1 sambung: %d %v", r.Code, r.Body)
	}
	newTok := r.Body["token"].(string)
	for _, ot := range []string{oldTok, oldTok2} {
		if v := call(t, h, "POST", "/api/auth/verify", ot, nil); v.Code != 401 {
			t.Fatalf("B1: sesi lama harus dicabut: %d", v.Code)
		}
	}
	if v := call(t, h, "POST", "/api/auth/verify", newTok, nil); v.Code != 200 {
		t.Fatalf("B1: sesi baru harus valid: %d", v.Code)
	}
	r = call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": "akun_lama_g", "password": "passwordlama1", "turnstileToken": "lulus"})
	if r.Code != 401 {
		t.Fatalf("B1: password lama harus tidak berlaku: %d", r.Code)
	}
	var lg identity.User
	db.Where("username = ?", "akun_lama_g").Take(&lg)
	if lg.PasswordHash != nil || lg.GoogleSub == nil || lg.EmailVerifiedAt == nil || lg.PasswordChangedAt == nil {
		t.Fatalf("B1: kolom akun salah: %+v", lg)
	}
	l := lastLog(t, db, "auth.google_link")
	if l == nil || !strings.Contains(det(l), `"sesiLamaDicabut":2`) || !strings.Contains(det(l), `"sandiLamaDihapus":true`) {
		t.Fatalf("B1: log google_link salah: %v", l)
	}
	// Email sama, sub Google berbeda -> 409.
	if r := call(t, h, "POST", "/api/auth/google", "", googleCred(t, "g-sub-lain", "lama.g@uji.test", "Lain")); r.Code != 409 {
		t.Fatalf("sub berbeda harus 409: %d", r.Code)
	}

	// B4: email ADMIN_EMAILS lewat Google -> role admin + user.role_change.
	r = call(t, h, "POST", "/api/auth/google", "", googleCred(t, "g-sub-admin", "google.admin@uji.test", "Google Admin"))
	pt = r.Body["profileToken"].(string)
	r = call(t, h, "POST", "/api/auth/google/complete", "", map[string]any{"profileToken": pt, "username": "pengelola_g"})
	if r.Code != 201 || r.Body["user"].(map[string]any)["role"] != "admin" {
		t.Fatalf("B4: %d %v", r.Code, r.Body)
	}
	if l := lastLog(t, db, "user.role_change"); l == nil || *l.ActorLabel != "google.admin@uji.test" {
		t.Fatal("B4: role_change tidak dicatat")
	}

	// Akun suspended -> 403.
	db.Model(&identity.User{}).Where("username = ?", "baru_google").Update("status", "suspended")
	if r := call(t, h, "POST", "/api/auth/google", "", googleCred(t, "g-sub-baru", "baru.google@uji.test", "Baru Google")); r.Code != 403 {
		t.Fatalf("Google suspended harus 403: %d", r.Code)
	}
}

func TestIntegrationAuthLogs(t *testing.T) {
	_, h, db := setupAdminIntegration(t)
	call(t, h, "POST", "/api/auth/register", "", reg("log_uji", "log.uji@uji.test", "", "Log Uji", "passwordku123"))
	if l := lastLog(t, db, "auth.register"); l == nil || *l.ActorLabel != "log.uji@uji.test" {
		t.Fatal("auth.register tidak tercatat")
	}
	r := call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": "LOG_UJI", "password": "passwordku123", "turnstileToken": "lulus"})
	if lastLog(t, db, "auth.login") == nil {
		t.Fatal("auth.login tidak tercatat")
	}
	call(t, h, "POST", "/api/auth/logout", r.Body["token"].(string), nil)
	if l := lastLog(t, db, "auth.logout"); l == nil || l.UserID == nil {
		t.Fatal("auth.logout tidak tercatat")
	}
	long := strings.ToUpper(strings.Repeat("x", 25)) + "@" + strings.Repeat("d", 150) + ".com"
	call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": long, "password": "RahasiaSekali9", "turnstileToken": "lulus"})
	l := lastLog(t, db, "auth.login_failed")
	if l == nil || len(*l.ActorLabel) > 100 || *l.ActorLabel != strings.ToLower(*l.ActorLabel) || strings.Contains(det(l), "RahasiaSekali9") {
		t.Fatalf("login_failed: identifier harus huruf kecil, ≤100, tanpa password: %v", l)
	}
	// 5x gagal -> auth.locked
	for i := 0; i < 5; i++ {
		call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": "log_uji", "password": "salahsalah", "turnstileToken": "lulus"})
	}
	if l := lastLog(t, db, "auth.locked"); l == nil || *l.ActorLabel != "log.uji@uji.test" {
		t.Fatal("auth.locked tidak tercatat")
	}
	// Tidak ada nilai rahasia di seluruh log.
	var leak int64
	db.Model(&audit.ActivityLog{}).Where("details LIKE ? OR details LIKE ? OR details LIKE ? OR summary LIKE ?",
		"%passwordku123%", "%argon2id%", "%salahsalah%", "%passwordku123%").Count(&leak)
	if leak != 0 {
		t.Fatalf("ada %d log yang memuat rahasia", leak)
	}
}
