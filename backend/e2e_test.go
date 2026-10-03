//go:build e2e

// Tes end-to-end terhadap container backend/frontend UJI (bukan produksi).
// Proses tes ini juga menyajikan JWKS tiruan (Cloudflare Access + Google) di :9000;
// backend uji diarahkan ke sana lewat TEST_ONLY_*_JWKS_URL (hanya berlaku bila DB_NAME *_test).
// Turnstile memakai kunci uji resmi Cloudflare (selalu lulus).
//
// Env: E2E_BACKEND_URL, E2E_FRONTEND_URL, E2E_AUD, E2E_GOOGLE_CLIENT, E2E_ADMIN_EMAIL,
//
//	PRODUCTS_SNAPSHOT (opsional), DB_* (database uji, untuk pemeriksaan langsung).
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const e2eTurnstileToken = "XXXX.DUMMY.TOKEN.XXXX"

var (
	e2eAccessKey, e2eGoogleKey *rsa.PrivateKey
	e2eBackend, e2eFrontend    string
)

func jwksJSON(kid string, k *rsa.PublicKey) []byte {
	b, _ := json.Marshal(map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "kid": kid, "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes()),
	}}})
	return b
}

func TestMain(m *testing.M) {
	var err error
	if e2eAccessKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		panic(err)
	}
	if e2eGoogleKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		panic(err)
	}
	testKey, otherKey = e2eAccessKey, e2eGoogleKey // dipakai helper signJWT (admin_unit_test.go)
	mux := http.NewServeMux()
	mux.HandleFunc("/access/certs", func(w http.ResponseWriter, r *http.Request) { w.Write(jwksJSON("ak1", &e2eAccessKey.PublicKey)) })
	mux.HandleFunc("/google/certs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=21600")
		w.Write(jwksJSON("gk1", &e2eGoogleKey.PublicKey))
	})
	mux.HandleFunc("/discord/", discordMockHandler) // server tiruan webhook Discord (e2e_orders_test.go)
	go http.ListenAndServe(":9000", mux)
	e2eBackend = strings.TrimSuffix(os.Getenv("E2E_BACKEND_URL"), "/")
	e2eFrontend = strings.TrimSuffix(os.Getenv("E2E_FRONTEND_URL"), "/")
	if e2eBackend == "" {
		fmt.Println("E2E_BACKEND_URL kosong")
		os.Exit(2)
	}
	// Tunggu backend siap.
	for i := 0; i < 60; i++ {
		if r, err := http.Get(e2eBackend + "/health/ready"); err == nil && r.StatusCode == 200 {
			r.Body.Close()
			break
		}
		time.Sleep(time.Second)
	}
	os.Exit(m.Run())
}

type e2eResp struct {
	Code int
	Body map[string]any
	Raw  []byte
	Hdr  http.Header
}

func e2e(t *testing.T, method, url string, hdr map[string]string, body any) e2eResp {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "mihanstore-e2e")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	json.Unmarshal(raw, &m)
	return e2eResp{res.StatusCode, m, raw, res.Header}
}

func accessJWT(t *testing.T, email string, mut ...func(map[string]any)) string {
	now := time.Now().Unix()
	c := map[string]any{"aud": []string{os.Getenv("E2E_AUD")}, "iss": "https://divine-rice-4f1d.cloudflareaccess.com",
		"exp": now + 600, "iat": now, "nbf": now - 5, "email": email, "type": "app"}
	for _, f := range mut {
		f(c)
	}
	return signJWT(t, e2eAccessKey, "ak1", "RS256", c)
}

func googleJWT(t *testing.T, sub, email, name string) string {
	now := time.Now().Unix()
	return signJWT(t, e2eGoogleKey, "gk1", "RS256", map[string]any{
		"iss": "https://accounts.google.com", "aud": os.Getenv("E2E_GOOGLE_CLIENT"), "sub": sub,
		"email": email, "email_verified": true, "name": name, "picture": "https://lh3.googleusercontent.com/a/e2e",
		"iat": now, "exp": now + 3600})
}

func adminHdr(t *testing.T, email string, mutating bool) map[string]string {
	h := map[string]string{"Cf-Access-Jwt-Assertion": accessJWT(t, email)}
	if mutating {
		h["X-Requested-With"] = adminRequestedWith
		h["Origin"] = "https://store.mihan.web.id"
		h["Sec-Fetch-Site"] = "same-origin"
	}
	return h
}

func bearer(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

func TestE2EPublicAndFrontend(t *testing.T) {
	r := e2e(t, "GET", e2eBackend+"/api/products", nil, nil)
	if p := os.Getenv("PRODUCTS_SNAPSHOT"); p != "" {
		want, _ := os.ReadFile(p)
		if err := compareProductsSnapshot(want, r.Raw); err != nil {
			t.Fatalf("/api/products tidak cocok dengan snapshot produksi: %v", err)
		}
	}
	r = e2e(t, "GET", e2eBackend+"/api/categories", nil, nil)
	var cats []map[string]any
	json.Unmarshal(r.Raw, &cats)
	if r.Code != 200 || len(cats) != 6 {
		t.Fatalf("/api/categories: %d %s", r.Code, r.Raw)
	}
	if e2eFrontend == "" {
		return
	}
	for _, p := range []string{"/", "/privasi", "/syarat", "/admin", "/admin/products", "/lengkapi-profil", "/login", "/invoice/create",
		"/keranjang", "/checkout", "/pesanan", "/pesanan/MS-261002-0001", "/admin/orders", "/admin/orders/1", "/admin/settings"} {
		r := e2e(t, "GET", e2eFrontend+p, nil, nil)
		if r.Code != 200 || !strings.Contains(string(r.Raw), `<div id="root">`) {
			t.Errorf("frontend %s: %d", p, r.Code)
		}
	}
	// API lewat proxy nginx frontend juga identik.
	if r := e2e(t, "GET", e2eFrontend+"/api/products", nil, nil); r.Code != 200 || !strings.Contains(string(r.Raw), "Kerupuk Finna Udang") {
		t.Errorf("proxy /api frontend: %d", r.Code)
	}
}

func TestE2EAdminFlow(t *testing.T) {
	admin := os.Getenv("E2E_ADMIN_EMAIL")
	// Pelanggan biasa ke /api/admin -> ditolak.
	r := e2e(t, "POST", e2eBackend+"/api/auth/register", nil, map[string]any{
		"username": "e2e_pelanggan", "email": "e2e.pelanggan@uji.test", "name": "Pelanggan E2E",
		"password": "passwordE2E123", "turnstileToken": e2eTurnstileToken})
	if r.Code != 201 {
		t.Fatalf("register (Turnstile uji resmi): %d %s", r.Code, r.Raw)
	}
	custTok := r.Body["token"].(string)
	if r := e2e(t, "GET", e2eBackend+"/api/admin/me", bearer(custTok), nil); r.Code != 401 {
		t.Fatalf("pelanggan -> admin harus 401: %d", r.Code)
	}
	if r := e2e(t, "GET", e2eBackend+"/api/admin/me", adminHdr(t, "e2e.pelanggan@uji.test", false), nil); r.Code != 403 {
		t.Fatalf("Access sah tapi bukan ADMIN_EMAILS harus 403: %d", r.Code)
	}
	// Header Access palsu / kunci lain / aud lain -> 401.
	fakes := map[string]string{
		"palsu":       "eyJhbGciOiJSUzI1NiIsImtpZCI6ImFrMSJ9.eyJlbWFpbCI6ImEifQ.AAAA",
		"kunci lain":  signJWT(t, e2eGoogleKey, "ak1", "RS256", map[string]any{"email": admin}),
		"aud lain":    accessJWT(t, admin, func(c map[string]any) { c["aud"] = "aud-lain-0000000000000000" }),
		"kedaluwarsa": accessJWT(t, admin, func(c map[string]any) { c["exp"] = time.Now().Unix() - 3600 }),
	}
	for name, tok := range fakes {
		if r := e2e(t, "GET", e2eBackend+"/api/admin/me", map[string]string{"Cf-Access-Jwt-Assertion": tok}, nil); r.Code != 401 {
			t.Errorf("header Access %s harus 401: %d", name, r.Code)
		}
	}

	// Admin sah.
	r = e2e(t, "GET", e2eBackend+"/api/admin/me", adminHdr(t, admin, false), nil)
	if r.Code != 200 || r.Body["user"].(map[string]any)["role"] != "admin" {
		t.Fatalf("admin me: %d %s", r.Code, r.Raw)
	}
	// CSRF: tanpa header kustom -> 403.
	if r := e2e(t, "POST", e2eBackend+"/api/admin/categories", map[string]string{"Cf-Access-Jwt-Assertion": accessJWT(t, admin)},
		map[string]any{"slug": "e2e_csrf", "name": "X"}); r.Code != 403 {
		t.Fatalf("CSRF: %d", r.Code)
	}

	// CRUD kategori + produk.
	r = e2e(t, "POST", e2eBackend+"/api/admin/categories", adminHdr(t, admin, true), map[string]any{"slug": "e2e_bumbu", "name": "Bumbu E2E", "sortOrder": 80})
	if r.Code != 201 {
		t.Fatalf("buat kategori: %d %s", r.Code, r.Raw)
	}
	catID := r.Body["id"].(float64)
	r = e2e(t, "POST", e2eBackend+"/api/admin/products", adminHdr(t, admin, true), map[string]any{
		"name": "Bumbu Soto E2E", "description": "Uji e2e", "price": 9000, "categoryId": catID})
	if r.Code != 201 {
		t.Fatalf("buat produk: %d %s", r.Code, r.Raw)
	}
	pid := fmt.Sprint(r.Body["id"].(float64))
	r = e2e(t, "PUT", e2eBackend+"/api/admin/products/"+pid, adminHdr(t, admin, true), map[string]any{
		"name": "Bumbu Soto E2E", "description": "Uji e2e", "price": 9500, "categoryId": catID})
	if r.Code != 200 || r.Body["price"].(float64) != 9500 {
		t.Fatalf("ubah produk: %d %s", r.Code, r.Raw)
	}
	if !strings.Contains(string(e2e(t, "GET", e2eBackend+"/api/products", nil, nil).Raw), "Bumbu Soto E2E") {
		t.Fatal("produk aktif harus muncul di API publik")
	}
	if r := e2e(t, "DELETE", e2eBackend+"/api/admin/categories/"+fmt.Sprint(catID), adminHdr(t, admin, true), map[string]any{}); r.Code != 409 {
		t.Fatalf("hapus kategori berproduk aktif harus 409: %d", r.Code)
	}
	if r := e2e(t, "PATCH", e2eBackend+"/api/admin/products/"+pid+"/active", adminHdr(t, admin, true), map[string]any{"active": false}); r.Code != 200 {
		t.Fatalf("nonaktifkan: %d", r.Code)
	}
	if strings.Contains(string(e2e(t, "GET", e2eBackend+"/api/products/search?q=soto", nil, nil).Raw), "Bumbu Soto E2E") {
		t.Fatal("produk nonaktif tidak boleh muncul di API publik")
	}
	if r := e2e(t, "DELETE", e2eBackend+"/api/admin/products/"+pid, adminHdr(t, admin, true), map[string]any{}); r.Code != 200 {
		t.Fatalf("hapus produk: %d", r.Code)
	}
	if r := e2e(t, "DELETE", e2eBackend+"/api/admin/categories/"+fmt.Sprint(catID), adminHdr(t, admin, true), map[string]any{}); r.Code != 200 {
		t.Fatalf("hapus kategori kosong: %d %s", r.Code, r.Raw)
	}

	// Semua perubahan tercatat (dengan selisih untuk update).
	r = e2e(t, "GET", e2eBackend+"/api/admin/activity-logs?per_page=50", adminHdr(t, admin, false), nil)
	seen := map[string]bool{}
	var updateDetails string
	for _, it := range r.Body["items"].([]any) {
		m := it.(map[string]any)
		seen[m["action"].(string)] = true
		if m["action"] == "product.update" && strings.Contains(m["summary"].(string), "Soto") {
			b, _ := json.Marshal(m["details"])
			updateDetails = string(b)
		}
	}
	for _, a := range []string{"category.create", "product.create", "product.update", "product.deactivate", "product.delete", "category.delete", "admin.access_denied", "auth.register"} {
		if !seen[a] {
			t.Errorf("aksi %s tidak tercatat", a)
		}
	}
	if !strings.Contains(updateDetails, `"dari":9000`) || !strings.Contains(updateDetails, `"menjadi":9500`) {
		t.Errorf("selisih update tidak tercatat: %s", updateDetails)
	}

	// Purge: hanya log > 180 hari.
	db, err := openDB(LoadConfig())
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO activity_logs (action, summary, created_at) VALUES ('e2e.tua','tua', UTC_TIMESTAMP(3) - INTERVAL 200 DAY), ('e2e.baru','baru', UTC_TIMESTAMP(3) - INTERVAL 10 DAY)`)
	r = e2e(t, "POST", e2eBackend+"/api/admin/activity-logs/purge", adminHdr(t, admin, true), map[string]any{})
	if r.Code != 200 || r.Body["deleted"].(float64) < 1 {
		t.Fatalf("purge: %d %s", r.Code, r.Raw)
	}
	var tua, baru int64
	db.Raw("SELECT COUNT(*) FROM activity_logs WHERE action = 'e2e.tua'").Scan(&tua)
	db.Raw("SELECT COUNT(*) FROM activity_logs WHERE action = 'e2e.baru'").Scan(&baru)
	if tua != 0 || baru != 1 {
		t.Fatalf("purge harus hanya menghapus log lama: tua=%d baru=%d", tua, baru)
	}
}

func TestE2EGoogle(t *testing.T) {
	// Pengguna baru -> needsProfile -> complete -> sesi.
	r := e2e(t, "POST", e2eBackend+"/api/auth/google", nil, map[string]any{"credential": googleJWT(t, "e2e-sub-1", "e2e.google@uji.test", "Google E2E")})
	if r.Code != 200 || r.Body["needsProfile"] != true {
		t.Fatalf("google baru: %d %s", r.Code, r.Raw)
	}
	r = e2e(t, "POST", e2eBackend+"/api/auth/google/complete", nil, map[string]any{"profileToken": r.Body["profileToken"], "username": "e2e_google"})
	if r.Code != 201 || r.Body["token"] == nil {
		t.Fatalf("complete: %d %s", r.Code, r.Raw)
	}
	if me := e2e(t, "GET", e2eBackend+"/api/auth/me", bearer(r.Body["token"].(string)), nil); me.Code != 200 {
		t.Fatalf("sesi Google: %d", me.Code)
	}
	// Daftar password dengan email akun Google -> 409.
	r = e2e(t, "POST", e2eBackend+"/api/auth/register", nil, map[string]any{"username": "e2e_coba", "email": "e2e.google@uji.test",
		"name": "Coba", "password": "passwordE2E123", "turnstileToken": e2eTurnstileToken})
	if r.Code != 409 || !strings.Contains(r.Body["error"].(string), "masuk dengan Google") {
		t.Fatalf("daftar password email Google: %d %s", r.Code, r.Raw)
	}

	// Akun password lama -> login Google -> tersambung; password & sesi lama mati.
	r = e2e(t, "POST", e2eBackend+"/api/auth/register", nil, map[string]any{"username": "e2e_lama", "email": "e2e.lama@uji.test",
		"name": "Lama", "password": "passwordLama123", "turnstileToken": e2eTurnstileToken})
	if r.Code != 201 {
		t.Fatalf("register lama: %d %s", r.Code, r.Raw)
	}
	oldTok := r.Body["token"].(string)
	login := map[string]any{"identifier": "e2e_lama", "password": "passwordLama123", "turnstileToken": e2eTurnstileToken}
	if r := e2e(t, "POST", e2eBackend+"/api/auth/login", nil, login); r.Code != 200 {
		t.Fatalf("login password lama sebelum sambung: %d %s", r.Code, r.Raw)
	}
	r = e2e(t, "POST", e2eBackend+"/api/auth/google", nil, map[string]any{"credential": googleJWT(t, "e2e-sub-2", "e2e.lama@uji.test", "Lama")})
	if r.Code != 200 || r.Body["token"] == nil {
		t.Fatalf("sambung Google: %d %s", r.Code, r.Raw)
	}
	if v := e2e(t, "POST", e2eBackend+"/api/auth/verify", bearer(oldTok), nil); v.Code != 401 {
		t.Fatalf("sesi lama harus dicabut: %d", v.Code)
	}
	if r := e2e(t, "POST", e2eBackend+"/api/auth/login", nil, login); r.Code != 401 {
		t.Fatalf("password lama harus tidak berlaku: %d", r.Code)
	}
	// Token Google untuk client lain -> 401.
	now := time.Now().Unix()
	bad := signJWT(t, e2eGoogleKey, "gk1", "RS256", map[string]any{"iss": "https://accounts.google.com", "aud": "lain.apps.googleusercontent.com",
		"sub": "x", "email": "x@uji.test", "email_verified": true, "exp": now + 600})
	if r := e2e(t, "POST", e2eBackend+"/api/auth/google", nil, map[string]any{"credential": bad}); r.Code != 401 {
		t.Fatalf("aud Google salah harus 401: %d", r.Code)
	}
}
