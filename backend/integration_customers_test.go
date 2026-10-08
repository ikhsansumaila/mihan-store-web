//go:build integration

// Tes integrasi menu admin "Pelanggan" dan alias pelanggan (migrations/016) terhadap database UJI.
package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

// callAuthRaw: body mentah respons (untuk memastikan alias tidak pernah muncul di API pelanggan).
func callAuthRaw(t *testing.T, h http.Handler, method, path, token string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(""))
	r.RemoteAddr = "203.0.113.7:5555"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if method != "GET" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// aliasCustomer mendaftarkan pelanggan dengan telepon unik; mengembalikan token dan id users.
func aliasCustomer(t *testing.T, h http.Handler, db *gorm.DB, name, phone string) (string, uint64) {
	t.Helper()
	userSeq++
	u := fmt.Sprintf("plgn_%d_%d", time.Now().UnixNano()%1000000, userSeq)
	r := call(t, h, "POST", "/api/auth/register", "", reg(u, u+"@uji.test", phone, name, "passwordku123"))
	if r.Code != 201 {
		t.Fatalf("register %s: %d %v", u, r.Code, r.Body)
	}
	var id uint64
	db.Raw("SELECT id FROM users WHERE username = ?", u).Scan(&id)
	return r.Body["token"].(string), id
}

func orderIDByNo(db *gorm.DB, no string) uint64 {
	var id uint64
	db.Raw("SELECT id FROM orders WHERE order_no = ?", no).Scan(&id)
	return id
}

func adminSetStatus(t *testing.T, h http.Handler, id uint64, from, to string) {
	t.Helper()
	body := map[string]any{"from": from, "to": to}
	if to == StatusCancelled {
		body["reason"] = "uji"
	}
	if r := adminCall(t, h, "PATCH", fmt.Sprintf("/api/admin/orders/%d/status", id), integAdmin, body); r.Code != 200 {
		t.Fatalf("status %s->%s: %d %v", from, to, r.Code, r.Body)
	}
}

func TestIntegrationAliasSchemaAndGrants(t *testing.T) {
	_, _, db, _ := setupOrders(t)
	var cols []struct {
		Type string `gorm:"column:COLUMN_TYPE"`
		Null string `gorm:"column:IS_NULLABLE"`
	}
	db.Raw(`SELECT COLUMN_TYPE, IS_NULLABLE FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'alias'`).Scan(&cols)
	if len(cols) != 1 || cols[0].Type != "varchar(100)" || cols[0].Null != "YES" {
		t.Fatalf("kolom users.alias: %+v", cols)
	}
	// Hak user aplikasi uji pada users tetap SELECT, INSERT, UPDATE (tanpa DELETE).
	var grants []string
	db.Raw("SHOW GRANTS").Scan(&grants)
	found := false
	for _, g := range grants {
		if strings.Contains(g, "`users`") {
			found = true
			if !strings.Contains(g, "SELECT, INSERT, UPDATE ON") || strings.Contains(g, "DELETE") {
				t.Errorf("hak users: %s", g)
			}
		}
	}
	if !found {
		t.Fatalf("hak users tidak ditemukan: %v", grants)
	}
	if err := db.Exec("ALTER TABLE users DROP COLUMN alias").Error; mysqlErrNo(err) != 1142 {
		t.Errorf("DDL harus ditolak: %v", err)
	}
}

func TestIntegrationCustomersAdmin(t *testing.T) {
	app, h, db, rec := setupOrders(t)
	app.aliasLimiter = NewRateLimiter(100000, time.Minute)
	tag := fmt.Sprintf("Kuncix%d", time.Now().UnixNano()%100000)

	tokA, idA := aliasCustomer(t, h, db, "Siti "+tag, "0812-7000-1111")
	_, idB := aliasCustomer(t, h, db, "Bayu "+tag, "0812-7000-2222")
	_, idC := aliasCustomer(t, h, db, "Hapus "+tag, "0812-7000-3333")
	// C dihapus (soft delete) -> tidak tampil.
	if err := db.Exec("UPDATE users SET deleted_at = NOW(3) WHERE id = ?", idC).Error; err != nil {
		t.Fatal(err)
	}

	// Pesanan A: paid, completed, pending, cancelled (sebelum alias -> notifikasi memakai nama akun).
	o1 := createOrderFor(t, h, tokA, 1)
	o2 := createOrderFor(t, h, tokA, 2)
	o3 := createOrderFor(t, h, tokA, 3)
	o4 := createOrderFor(t, h, tokA, 4)
	for _, no := range []string{o1, o2, o3, o4} {
		confirmCall(t, h, orderIDByNo(db, no), 0, 0)
	}
	adminSetStatus(t, h, orderIDByNo(db, o1), StatusPending, StatusPaid)
	adminSetStatus(t, h, orderIDByNo(db, o2), StatusPending, StatusPaid)
	adminSetStatus(t, h, orderIDByNo(db, o2), StatusPaid, StatusCompleted)
	adminSetStatus(t, h, orderIDByNo(db, o4), StatusPending, StatusCancelled)
	var spent, cancelledTotal int64
	db.Raw("SELECT SUM(total) FROM orders WHERE order_no IN (?, ?)", o1, o2).Scan(&spent)
	db.Raw("SELECT total FROM orders WHERE order_no = ?", o4).Scan(&cancelledTotal)
	rec.mu.Lock()
	for _, e := range rec.ev {
		if e.OrderNo == o1 && e.CustomerName != "Siti "+tag {
			t.Errorf("notifikasi tanpa alias harus memakai nama akun: %q", e.CustomerName)
		}
	}
	rec.mu.Unlock()

	// ----- Ubah alias -----
	base := fmt.Sprintf("/api/admin/customers/%d", idA)
	if r := call(t, h, "PATCH", base+"/alias", tokA, map[string]any{"alias": "x"}); r.Code != 401 {
		t.Fatalf("pelanggan (Bearer) ubah alias harus 401: %d", r.Code)
	}
	if r := adminCall(t, h, "PATCH", base+"/alias", integAdmin, map[string]any{}); r.Code != 400 {
		t.Fatalf("tanpa field alias harus 400: %d", r.Code)
	}
	if r := adminCall(t, h, "PATCH", base+"/alias", integAdmin, map[string]any{"alias": "x", "name": "y"}); r.Code != 400 {
		t.Fatalf("field tak dikenal harus 400: %d", r.Code)
	}
	if r := adminCall(t, h, "PATCH", base+"/alias", integAdmin, map[string]any{"alias": strings.Repeat("a", 101)}); r.Code != 400 ||
		!strings.Contains(fmt.Sprint(r.Body["error"]), "maksimal 100") {
		t.Fatalf("alias 101 karakter harus 400: %d %v", r.Code, r.Body)
	}
	nLog := countLogs(db, "action = 'customer.alias_update'")
	r := adminCall(t, h, "PATCH", base+"/alias", integAdmin, map[string]any{"alias": "  Bu\u202e Siti \t Toko   Maju " + tag + " "})
	wantAlias := "Bu Siti Toko Maju " + tag
	if r.Code != 200 || r.Body["alias"] != wantAlias || num(r.Body["id"]) != int64(idA) {
		t.Fatalf("ubah alias: %d %v", r.Code, r.Body)
	}
	var stored *string
	db.Raw("SELECT alias FROM users WHERE id = ?", idA).Scan(&stored)
	if stored == nil || *stored != wantAlias {
		t.Fatalf("alias tersimpan: %v", stored)
	}
	l := lastLog(t, db, "customer.alias_update")
	if l == nil || countLogs(db, "action = 'customer.alias_update'") != nLog+1 || *l.EntityType != "user" ||
		det(l) != `{"alias":{"dari":null,"menjadi":"`+wantAlias+`"}}` {
		t.Fatalf("log alias: %v", det(l))
	}
	for _, bad := range []string{"@uji.test", "+62812", "Siti " + tag, "plgn_"} {
		if strings.Contains(det(l)+l.Summary, bad) {
			t.Errorf("log alias memuat data akun lain %q: %s %s", bad, l.Summary, det(l))
		}
	}
	// Sama persis -> tanpa log baru.
	adminCall(t, h, "PATCH", base+"/alias", integAdmin, map[string]any{"alias": wantAlias})
	if countLogs(db, "action = 'customer.alias_update'") != nLog+1 {
		t.Fatal("alias sama tidak boleh menulis log")
	}
	// Bukan customer / tidak ada / dihapus -> 404.
	var adminID uint64
	db.Raw("SELECT id FROM users WHERE email = ?", integAdmin).Scan(&adminID)
	for _, id := range []uint64{adminID, idC, 99999999} {
		if r := adminCall(t, h, "PATCH", fmt.Sprintf("/api/admin/customers/%d/alias", id), integAdmin, map[string]any{"alias": "x"}); r.Code != 404 {
			t.Errorf("alias untuk %d harus 404: %d", id, r.Code)
		}
		if r := adminCall(t, h, "GET", fmt.Sprintf("/api/admin/customers/%d", id), integAdmin, nil); r.Code != 404 {
			t.Errorf("detail %d harus 404: %d", id, r.Code)
		}
	}
	var adminAlias *string
	db.Raw("SELECT alias FROM users WHERE id = ?", adminID).Scan(&adminAlias)
	if adminAlias != nil {
		t.Fatal("alias admin tidak boleh berubah")
	}

	// ----- Daftar pelanggan -----
	r = adminCall(t, h, "GET", "/api/admin/customers?q="+tag, integAdmin, nil)
	if r.Code != 200 || num(r.Body["total"]) != 2 {
		t.Fatalf("daftar (yang dihapus tidak tampil): %d %v", r.Code, r.Body)
	}
	items := r.Body["items"].([]any)
	first := items[0].(map[string]any)
	if num(first["id"]) != int64(idB) { // terbaru dulu
		t.Fatalf("urutan bawaan terbaru: %v", items)
	}
	a := items[1].(map[string]any)
	if a["alias"] != wantAlias || num(a["orderCount"]) != 4 || num(a["totalSpent"]) != spent || a["status"] != "active" ||
		a["lastOrderAt"] == nil || a["email"] == nil || a["username"] == nil || a["phone"] != "+6281270001111" {
		t.Fatalf("statistik A (total hanya paid/completed = %d, batal %d): %v", spent, cancelledTotal, a)
	}
	if b := first; num(b["orderCount"]) != 0 || num(b["totalSpent"]) != 0 || b["alias"] != nil {
		t.Fatalf("statistik B: %v", b)
	}
	for _, s := range []string{"orders", "spent"} {
		r := adminCall(t, h, "GET", "/api/admin/customers?sort="+s+"&q="+tag, integAdmin, nil)
		if r.Code != 200 || num(r.Body["items"].([]any)[0].(map[string]any)["id"]) != int64(idA) {
			t.Fatalf("urut %s: %v", s, r.Body)
		}
	}
	for _, s := range []string{"name", "u.id", "spent%3BDROP", "SPENT"} {
		if r := adminCall(t, h, "GET", "/api/admin/customers?sort="+s, integAdmin, nil); r.Code != 400 {
			t.Errorf("urut %q harus 400: %d", s, r.Code)
		}
	}
	// Pencarian: alias, nama, username, email, telepon (format lokal), LIKE di-escape.
	for _, q := range []string{"Toko%20Maju%20" + tag, "Siti%20" + tag, "0812-7000-1111", "6281270001111", "plgn_"} {
		r := adminCall(t, h, "GET", "/api/admin/customers?per_page=100&q="+q, integAdmin, nil)
		found := false
		for _, it := range r.Body["items"].([]any) {
			if num(it.(map[string]any)["id"]) == int64(idA) {
				found = true
			}
		}
		if r.Code != 200 || !found {
			t.Errorf("cari %q tidak menemukan A: %d %v", q, r.Code, r.Body["total"])
		}
	}
	if r := adminCall(t, h, "GET", "/api/admin/customers?q=%25", integAdmin, nil); num(r.Body["total"]) != 0 {
		t.Errorf("'%%' harus di-escape: %v", r.Body["total"])
	}
	// Admin tidak tampil.
	if r := adminCall(t, h, "GET", "/api/admin/customers?q="+integAdmin, integAdmin, nil); num(r.Body["total"]) != 0 {
		t.Errorf("admin tidak boleh tampil: %v", r.Body)
	}
	if r := adminCall(t, h, "GET", "/api/admin/customers?per_page=500", integAdmin, nil); num(r.Body["perPage"]) != 100 {
		t.Errorf("per_page maks 100: %v", r.Body["perPage"])
	}
	if r := adminCall(t, h, "GET", "/api/admin/customers?per_page=1&page=2&q="+tag, integAdmin, nil); len(r.Body["items"].([]any)) != 1 ||
		num(r.Body["items"].([]any)[0].(map[string]any)["id"]) != int64(idA) {
		t.Errorf("paginasi: %v", r.Body)
	}

	// ----- Detail pelanggan -----
	r = adminCall(t, h, "GET", base, integAdmin, nil)
	c := r.Body["customer"].(map[string]any)
	recent := r.Body["recentOrders"].([]any)
	if r.Code != 200 || c["alias"] != wantAlias || num(c["totalSpent"]) != spent || num(c["orderCount"]) != 4 || len(recent) != 4 ||
		recent[0].(map[string]any)["orderNo"] != o4 || recent[0].(map[string]any)["statusLabel"] == nil {
		t.Fatalf("detail: %d %v", r.Code, r.Body)
	}

	// ----- Pesanan admin: alias di daftar/detail + pencarian -----
	r = adminCall(t, h, "GET", "/api/admin/orders?per_page=100&q=Toko%20Maju%20"+tag, integAdmin, nil)
	if r.Code != 200 || num(r.Body["total"]) != 4 {
		t.Fatalf("cari pesanan lewat alias: %d %v", r.Code, r.Body["total"])
	}
	cu := r.Body["items"].([]any)[0].(map[string]any)["customer"].(map[string]any)
	if cu["alias"] != wantAlias || num(cu["id"]) != int64(idA) {
		t.Fatalf("customer di daftar pesanan: %v", cu)
	}
	r = adminCall(t, h, "GET", fmt.Sprintf("/api/admin/orders/%d", orderIDByNo(db, o3)), integAdmin, nil)
	cu = r.Body["customer"].(map[string]any)
	if cu["alias"] != wantAlias || num(cu["id"]) != int64(idA) || cu["name"] != "Siti "+tag {
		t.Fatalf("customer di detail pesanan: %v", cu)
	}

	// ----- Notifikasi Discord memakai alias -----
	o5 := createOrderFor(t, h, tokA, 5)
	rec.mu.Lock()
	var got string
	for _, e := range rec.ev {
		if e.OrderNo == o5 {
			got = e.CustomerName
		}
	}
	rec.mu.Unlock()
	if got != wantAlias {
		t.Fatalf("notifikasi dengan alias: %q", got)
	}

	// ----- Alias TIDAK PERNAH ada di API pelanggan -----
	call(t, h, "POST", "/api/cart/items", tokA, map[string]any{"productId": 6, "qty": 1})
	for _, p := range []struct{ m, path string }{
		{"GET", "/api/auth/me"}, {"POST", "/api/auth/verify"}, {"GET", "/api/orders"}, {"GET", "/api/orders/" + o3},
		{"GET", "/api/orders/" + o5}, {"GET", "/api/cart"}, {"GET", "/api/store-info"},
	} {
		code, body := callAuthRaw(t, h, p.m, p.path, tokA)
		if code != 200 {
			t.Errorf("%s %s: %d", p.m, p.path, code)
		}
		if strings.Contains(body, "Toko Maju") || strings.Contains(strings.ToLower(body), "alias") {
			t.Errorf("alias bocor di %s: %s", p.path, body)
		}
	}
	// Login ulang juga tidak memuat alias.
	var uname string
	db.Raw("SELECT username FROM users WHERE id = ?", idA).Scan(&uname)
	lr := call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": uname, "password": "passwordku123", "turnstileToken": "lulus"})
	if lr.Code != 200 || strings.Contains(fmt.Sprint(lr.Body), "Toko Maju") || strings.Contains(fmt.Sprint(lr.Body), "alias") {
		t.Fatalf("login: %d %v", lr.Code, lr.Body)
	}
	// Pelanggan memakai API admin -> ditolak.
	for _, p := range []string{"/api/admin/customers", base} {
		if code, _ := callAuthRaw(t, h, "GET", p, tokA); code != 401 {
			t.Errorf("pelanggan -> %s: %d", p, code)
		}
	}

	// ----- Kosongkan alias -> hapus -----
	r = adminCall(t, h, "PATCH", base+"/alias", integAdmin, map[string]any{"alias": "   "})
	if r.Code != 200 || r.Body["alias"] != nil {
		t.Fatalf("hapus alias: %d %v", r.Code, r.Body)
	}
	db.Raw("SELECT alias FROM users WHERE id = ?", idA).Scan(&stored)
	if stored != nil {
		t.Fatal("alias harus NULL")
	}
	if l := lastLog(t, db, "customer.alias_update"); det(l) != `{"alias":{"dari":"`+wantAlias+`","menjadi":null}}` {
		t.Fatalf("log hapus alias: %s", det(l))
	}

	// Ringkasan admin memuat jumlah pelanggan (tanpa admin & yang dihapus).
	var nCust int64
	db.Raw("SELECT COUNT(*) FROM users WHERE role = 'customer' AND deleted_at IS NULL").Scan(&nCust)
	if r := adminCall(t, h, "GET", "/api/admin/summary", integAdmin, nil); num(r.Body["customers"]) != nCust {
		t.Fatalf("ringkasan pelanggan: %v vs %d", r.Body["customers"], nCust)
	}
}

// Bila activity log gagal ditulis, perubahan alias ikut dibatalkan (satu transaksi).
func TestIntegrationAliasTxAtomicWithLog(t *testing.T) {
	app, h, db, _ := setupOrders(t)
	app.aliasLimiter = NewRateLimiter(100000, time.Minute)
	_, id := aliasCustomer(t, h, db, "Atomik Alias", "0812-7000-4444")
	var fail atomic.Bool
	if err := db.Callback().Create().Before("gorm:create").Register("uji_gagal_log_alias", func(tx *gorm.DB) {
		if fail.Load() && tx.Statement.Table == "activity_logs" {
			tx.AddError(errors.New("uji: log gagal"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Create().Remove("uji_gagal_log_alias")
	fail.Store(true)
	if r := adminCall(t, h, "PATCH", fmt.Sprintf("/api/admin/customers/%d/alias", id), integAdmin, map[string]any{"alias": "Tidak Tersimpan"}); r.Code != 500 {
		t.Fatalf("log gagal harus 500: %d", r.Code)
	}
	var stored *string
	db.Raw("SELECT alias FROM users WHERE id = ?", id).Scan(&stored)
	if stored != nil {
		t.Fatalf("alias harus di-rollback: %v", *stored)
	}
	fail.Store(false)
	if r := adminCall(t, h, "PATCH", fmt.Sprintf("/api/admin/customers/%d/alias", id), integAdmin, map[string]any{"alias": "Tersimpan"}); r.Code != 200 {
		t.Fatalf("setelah pulih: %d", r.Code)
	}
}

// Batas laju ubah alias per admin.
func TestIntegrationAliasRateLimit(t *testing.T) {
	app, h, db, _ := setupOrders(t)
	app.aliasLimiter = NewRateLimiter(2, time.Minute)
	_, id := aliasCustomer(t, h, db, "Laju Alias", "0812-7000-5555")
	p := fmt.Sprintf("/api/admin/customers/%d/alias", id)
	for i, want := range []int{200, 200, 429} {
		if r := adminCall(t, h, "PATCH", p, integAdmin, map[string]any{"alias": fmt.Sprintf("A%d", i)}); r.Code != want {
			t.Fatalf("permintaan %d: %d mau %d", i, r.Code, want)
		}
	}
}
