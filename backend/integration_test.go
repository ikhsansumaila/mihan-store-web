//go:build integration

// Tes integrasi terhadap database UJI (bukan produksi).
// Jalankan: DB_NAME=mihanstore_test DB_USER=... DB_PASSWORD=... go test -tags integration ./...
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func setupIntegration(t *testing.T) (*App, http.Handler) {
	t.Helper()
	cfg := LoadConfig()
	if !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatalf("tes integrasi hanya boleh memakai database *_test, bukan %q", cfg.DBName)
	}
	db, err := openDB(cfg)
	if err != nil {
		t.Fatalf("koneksi DB uji: %v", err)
	}
	// Tutup pool koneksi setelah tes agar jumlah koneksi ke MySQL bersama tidak menumpuk
	// (max_connections server dipakai juga oleh produksi).
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	// Database uji dibuat ulang dari migrasi sebelum setiap putaran tes (user aplikasi
	// tidak punya hak DELETE pada users/activity_logs), jadi tiap tes memakai nama unik.

	// Turnstile palsu lokal (hanya di tes): token "lulus" diterima.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Write([]byte(`{"success":` + map[bool]string{true: "true", false: "false"}[r.Form.Get("response") == "lulus"] + `}`))
	}))
	t.Cleanup(ts.Close)
	cfg.TurnstileSecret = "secret-uji"
	cfg.AllowNoTurnstile = false
	initDummyHash()
	app := NewApp(cfg)
	app.turnstile.endpoint = ts.URL
	app.loginLimiter = NewRateLimiter(1000, time.Minute)
	app.registerLimiter = NewRateLimiter(1000, time.Hour)
	app.db.Store(db)
	return app, newRouter(app)
}

type resp struct {
	Code int
	Body map[string]any
}

func call(t *testing.T, h http.Handler, method, path, token string, body any) resp {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, path, &buf)
	r.RemoteAddr = "203.0.113.7:5555"
	r.Header.Set("User-Agent", "integration-test")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m)
	return resp{w.Code, m}
}

func reg(u, e, p, name, pw string) map[string]any {
	return map[string]any{"username": u, "email": e, "phone": p, "name": name, "password": pw, "turnstileToken": "lulus"}
}

func TestIntegrationAuthFlow(t *testing.T) {
	app, h := setupIntegration(t)
	db := app.db.Load()

	// Registrasi
	r := call(t, h, "POST", "/api/auth/register", "", reg("Budi_Uji", " Budi@Uji.Test ", "0812-3456-7890", " Budi  Uji ", "passwordku123"))
	if r.Code != 201 || r.Body["token"] == nil {
		t.Fatalf("register: %d %v", r.Code, r.Body["error"])
	}
	user := r.Body["user"].(map[string]any)
	if user["username"] != "budi_uji" || user["email"] != "budi@uji.test" || user["phone"] != "+6281234567890" || user["name"] != "Budi Uji" || user["role"] != "customer" {
		t.Fatalf("data user tidak ternormalisasi: %v", user)
	}
	regToken := r.Body["token"].(string)

	// Hash tersimpan argon2id, token tersimpan hanya sebagai SHA-256.
	var u User
	db.Where("username = ?", "budi_uji").Take(&u)
	if u.PasswordHash == nil || !strings.HasPrefix(*u.PasswordHash, "$argon2id$v=19$m=19456,t=2,p=1$") || strings.Contains(*u.PasswordHash, "passwordku123") {
		t.Fatal("password tidak di-hash argon2id")
	}
	var s Session
	db.Where("user_id = ?", u.ID).Take(&s)
	if s.TokenHash != HashToken(regToken) || len(s.IP) != 4 || s.UserAgent == nil || *s.UserAgent != "integration-test" {
		t.Fatal("sesi tidak tersimpan dengan benar")
	}
	var raw int64
	db.Raw("SELECT COUNT(*) FROM sessions WHERE token_hash = ?", regToken).Scan(&raw)
	if raw != 0 {
		t.Fatal("token mentah tersimpan di DB")
	}

	// Telepon kosong -> NULL; telepon tidak valid -> 400
	r = call(t, h, "POST", "/api/auth/register", "", reg("siti", "siti@uji.test", "", "Siti", "passwordku123"))
	if r.Code != 201 || r.Body["user"].(map[string]any)["phone"] != nil {
		t.Fatalf("register tanpa telepon: %d %v", r.Code, r.Body)
	}
	r = call(t, h, "POST", "/api/auth/register", "", reg("andi", "andi@uji.test", "12345", "Andi", "passwordku123"))
	if r.Code != 400 {
		t.Fatalf("telepon invalid harus 400: %d", r.Code)
	}
	// Telepon sama boleh (tidak unik)
	r = call(t, h, "POST", "/api/auth/register", "", reg("andi", "andi@uji.test", "081234567890", "Andi", "passwordku123"))
	if r.Code != 201 {
		t.Fatalf("telepon duplikat harus boleh: %d %v", r.Code, r.Body["error"])
	}

	// Duplikat email / username (beda huruf besar-kecil) -> 409
	for _, body := range []map[string]any{
		reg("lain", "BUDI@uji.test", "", "X", "passwordku123"),
		reg("BUDI_UJI", "lain@uji.test", "", "X", "passwordku123"),
	} {
		r = call(t, h, "POST", "/api/auth/register", "", body)
		if r.Code != 409 || r.Body["error"] != "Email atau username sudah terdaftar" {
			t.Fatalf("duplikat harus 409: %d %v", r.Code, r.Body["error"])
		}
	}

	// Login via email, username, dan field lama "email"
	for _, body := range []map[string]any{
		{"identifier": "BUDI@uji.test", "password": "passwordku123", "turnstileToken": "lulus"},
		{"identifier": "Budi_Uji", "password": "passwordku123", "turnstileToken": "lulus"},
		{"email": "budi@uji.test", "password": "passwordku123", "turnstileToken": "lulus"},
	} {
		r = call(t, h, "POST", "/api/auth/login", "", body)
		if r.Code != 200 || r.Body["token"] == nil {
			t.Fatalf("login %v: %d %v", body["identifier"], r.Code, r.Body["error"])
		}
	}
	token := r.Body["token"].(string)

	// Turnstile salah -> 401, tanpa token -> 400
	r = call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": "budi_uji", "password": "passwordku123", "turnstileToken": "gagal"})
	if r.Code != 401 {
		t.Fatalf("turnstile gagal harus 401: %d", r.Code)
	}
	r = call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": "budi_uji", "password": "passwordku123"})
	if r.Code != 400 {
		t.Fatalf("tanpa turnstile harus 400: %d", r.Code)
	}

	// /me, verify (header & body)
	r = call(t, h, "GET", "/api/auth/me", token, nil)
	if r.Code != 200 || r.Body["user"].(map[string]any)["role"] != "customer" {
		t.Fatalf("/me: %d", r.Code)
	}
	r = call(t, h, "POST", "/api/auth/verify", token, nil)
	if r.Code != 200 || r.Body["valid"] != true {
		t.Fatalf("verify header: %d", r.Code)
	}
	r = call(t, h, "POST", "/api/auth/verify", "", map[string]any{"token": token})
	if r.Code != 200 || r.Body["valid"] != true {
		t.Fatalf("verify body: %d", r.Code)
	}

	// Logout -> verify gagal; token lain tetap valid
	r = call(t, h, "POST", "/api/auth/logout", token, nil)
	if r.Code != 200 {
		t.Fatalf("logout: %d", r.Code)
	}
	r = call(t, h, "POST", "/api/auth/verify", token, nil)
	if r.Code != 401 || r.Body["valid"] != false {
		t.Fatalf("verify setelah logout harus 401: %d", r.Code)
	}
	r = call(t, h, "POST", "/api/auth/verify", regToken, nil)
	if r.Code != 200 {
		t.Fatalf("sesi lain harus tetap valid: %d", r.Code)
	}

	// Token palsu gaya lama
	r = call(t, h, "POST", "/api/auth/verify", "", map[string]any{"token": "token_budi_uji.test_1"})
	if r.Code != 401 {
		t.Fatalf("token palsu harus 401: %d", r.Code)
	}

	// Pesan error login seragam: user tidak ada vs password salah
	r1 := call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": "tidakada", "password": "passwordku123", "turnstileToken": "lulus"})
	r2 := call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": "siti", "password": "salahsalah", "turnstileToken": "lulus"})
	if r1.Code != 401 || r2.Code != 401 || r1.Body["error"] != r2.Body["error"] || r1.Body["error"] != "Email/username atau password salah" {
		t.Fatalf("pesan login harus seragam: %v / %v", r1.Body["error"], r2.Body["error"])
	}
	// Login siti sukses mereset failed_logins
	call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": "siti", "password": "passwordku123", "turnstileToken": "lulus"})
	var siti User
	db.Where("username = ?", "siti").Take(&siti)
	if siti.FailedLogins != 0 || siti.LastLoginAt == nil {
		t.Fatal("login sukses harus mereset failed_logins dan mengisi last_login_at")
	}
}

func TestIntegrationLockout(t *testing.T) {
	app, h := setupIntegration(t)
	db := app.db.Load()
	call(t, h, "POST", "/api/auth/register", "", reg("korban", "korban@uji.test", "", "Korban", "benarbenar1"))
	bad := map[string]any{"identifier": "korban", "password": "salahsalah", "turnstileToken": "lulus"}
	good := map[string]any{"identifier": "korban", "password": "benarbenar1", "turnstileToken": "lulus"}
	for i := 0; i < 5; i++ {
		if r := call(t, h, "POST", "/api/auth/login", "", bad); r.Code != 401 {
			t.Fatalf("gagal ke-%d: %d", i+1, r.Code)
		}
	}
	var u User
	db.Where("username = ?", "korban").Take(&u)
	if u.LockedUntil == nil {
		t.Fatal("akun harus terkunci setelah 5 kali gagal")
	}
	r := call(t, h, "POST", "/api/auth/login", "", good)
	if r.Code != 401 || r.Body["error"] != "Email/username atau password salah" {
		t.Fatalf("akun terkunci harus menolak password benar dengan pesan umum: %d", r.Code)
	}
	// Majukan jam 16 menit -> terbuka
	real := app.now
	app.now = func() time.Time { return real().Add(16 * time.Minute) }
	r = call(t, h, "POST", "/api/auth/login", "", good)
	if r.Code != 200 {
		t.Fatalf("setelah 15 menit harus bisa login: %d %v", r.Code, r.Body["error"])
	}
	var u2 User
	db.Where("username = ?", "korban").Take(&u2)
	if u2.FailedLogins != 0 || u2.LockedUntil != nil {
		t.Fatal("login sukses harus mereset penguncian")
	}
}

func TestIntegrationSoftDeleteSuspendExpiry(t *testing.T) {
	app, h := setupIntegration(t)
	db := app.db.Load()
	r := call(t, h, "POST", "/api/auth/register", "", reg("hapus", "hapus@uji.test", "", "Hapus", "passwordku123"))
	oldToken := r.Body["token"].(string)

	// Soft delete -> sesi lama tidak valid, email/username boleh didaftarkan ulang
	if err := db.Where("username = ?", "hapus").Delete(&User{}).Error; err != nil {
		t.Fatal(err)
	}
	if r := call(t, h, "POST", "/api/auth/verify", oldToken, nil); r.Code != 401 {
		t.Fatalf("sesi user terhapus harus 401: %d", r.Code)
	}
	r = call(t, h, "POST", "/api/auth/register", "", reg("hapus", "hapus@uji.test", "", "Hapus Baru", "passwordku123"))
	if r.Code != 201 {
		t.Fatalf("daftar ulang setelah soft delete: %d %v", r.Code, r.Body["error"])
	}
	var n int64
	db.Unscoped().Model(&User{}).Where("email = ?", "hapus@uji.test").Count(&n)
	if n != 2 {
		t.Fatalf("harus ada 2 baris (1 terhapus): %d", n)
	}
	tok := r.Body["token"].(string)

	// Suspended -> sesi tidak valid, login password benar -> 403
	db.Model(&User{}).Where("username = ?", "hapus").Update("status", "suspended")
	if r := call(t, h, "GET", "/api/auth/me", tok, nil); r.Code != 401 {
		t.Fatalf("user suspended /me harus 401: %d", r.Code)
	}
	r = call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": "hapus", "password": "passwordku123", "turnstileToken": "lulus"})
	if r.Code != 403 {
		t.Fatalf("login user suspended harus 403: %d", r.Code)
	}
	db.Model(&User{}).Where("username = ?", "hapus").Update("status", "active")

	// Sesi kedaluwarsa: ditolak, lalu dihapus lazy saat login berikutnya
	db.Model(&Session{}).Where("token_hash = ?", HashToken(tok)).Update("expires_at", time.Now().UTC().Add(-time.Hour))
	if r := call(t, h, "POST", "/api/auth/verify", tok, nil); r.Code != 401 {
		t.Fatalf("sesi kedaluwarsa harus 401: %d", r.Code)
	}
	call(t, h, "POST", "/api/auth/login", "", map[string]any{"identifier": "hapus", "password": "passwordku123", "turnstileToken": "lulus"})
	db.Unscoped().Model(&Session{}).Where("token_hash = ?", HashToken(tok)).Count(&n)
	if n != 0 {
		t.Fatal("sesi kedaluwarsa harus terhapus saat login")
	}
	// TTL default 7 hari
	var s Session
	db.Order("id DESC").Take(&s)
	if d := time.Until(s.ExpiresAt); d < 167*time.Hour || d > 169*time.Hour {
		t.Fatalf("TTL sesi bukan 7 hari: %v", d)
	}
}
