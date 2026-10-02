package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------- helper JWT uji ----------

var testKey, otherKey *rsa.PrivateKey

func init() {
	var err error
	if testKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		panic(err)
	}
	if otherKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		panic(err)
	}
}

func signJWT(t *testing.T, key *rsa.PrivateKey, kid, alg string, claims map[string]any) string {
	t.Helper()
	h, _ := json.Marshal(map[string]any{"alg": alg, "kid": kid, "typ": "JWT"})
	p, _ := json.Marshal(claims)
	in := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
	sum := sha256.Sum256([]byte(in))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return in + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func stubJWKS(keys map[string]*rsa.PublicKey) *JWKSCache {
	return NewJWKSCache(func(ctx context.Context) (map[string]*rsa.PublicKey, time.Duration, error) {
		return keys, 0, nil
	})
}

const (
	testTeam = "divine-rice-4f1d.cloudflareaccess.com"
	testAUD  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func accessCfg() Config {
	return Config{
		CORSAllowedOrigins: defaultCORSOrigins, CFAccessTeamDomain: testTeam, CFAccessAUD: testAUD,
		AdminEmails: []string{"ikhsan.sumaila@gmail.com", "qomariahakmala@gmail.com"}, DBName: "x",
	}
}

func newTestAccess(t *testing.T) *AccessVerifier {
	t.Helper()
	v, err := NewAccessVerifier(accessCfg())
	if err != nil {
		t.Fatal(err)
	}
	v.jwks = stubJWKS(map[string]*rsa.PublicKey{"k1": &testKey.PublicKey})
	return v
}

func accessClaimsOK() map[string]any {
	now := time.Now().Unix()
	return map[string]any{"aud": []string{testAUD}, "iss": "https://" + testTeam, "exp": now + 600,
		"iat": now, "nbf": now - 10, "email": "Ikhsan.Sumaila@gmail.com", "type": "app"}
}

func with(m map[string]any, k string, v any) map[string]any {
	c := map[string]any{}
	for kk, vv := range m {
		c[kk] = vv
	}
	if v == nil {
		delete(c, k)
	} else {
		c[k] = v
	}
	return c
}

func TestAccessVerify(t *testing.T) {
	v := newTestAccess(t)
	ctx := context.Background()
	email, err := v.Verify(ctx, signJWT(t, testKey, "k1", "RS256", accessClaimsOK()))
	if err != nil || email != "ikhsan.sumaila@gmail.com" {
		t.Fatalf("token sah ditolak: %v %q", err, email)
	}
	now := time.Now().Unix()
	bad := map[string]string{
		"tanda tangan salah": signJWT(t, otherKey, "k1", "RS256", accessClaimsOK()),
		"kid tak dikenal":    signJWT(t, testKey, "k9", "RS256", accessClaimsOK()),
		"alg HS256":          signJWT(t, testKey, "k1", "HS256", accessClaimsOK()),
		"aud salah":          signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "aud", "lain")),
		"aud kosong":         signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "aud", nil)),
		"iss salah":          signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "iss", "https://evil.cloudflareaccess.com")),
		"kedaluwarsa":        signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "exp", now-3600)),
		"tanpa exp":          signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "exp", nil)),
		"nbf masa depan":     signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "nbf", now+3600)),
		"tanpa email":        signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "email", nil)),
		"rusak":              "a.b.c",
		"kosong":             "",
	}
	for name, tok := range bad {
		if _, err := v.Verify(ctx, tok); err == nil {
			t.Errorf("%s: harus ditolak", name)
		}
	}
	// Payload diubah setelah ditandatangani.
	good := signJWT(t, testKey, "k1", "RS256", accessClaimsOK())
	parts := strings.Split(good, ".")
	forged, _ := json.Marshal(with(accessClaimsOK(), "email", "qomariahakmala@gmail.com"))
	if _, err := v.Verify(ctx, parts[0]+"."+base64.RawURLEncoding.EncodeToString(forged)+"."+parts[2]); err == nil {
		t.Error("payload palsu harus ditolak")
	}
	// Email sah tetapi di luar ADMIN_EMAILS.
	_, err = v.Verify(ctx, signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "email", "orang@lain.com")))
	if !errors.Is(err, errEmailNotAllowed) {
		t.Errorf("email di luar ADMIN_EMAILS: %v", err)
	}
}

func TestAccessConfigFailClosed(t *testing.T) {
	for name, mut := range map[string]func(*Config){
		"team kosong":   func(c *Config) { c.CFAccessTeamDomain = "" },
		"team salah":    func(c *Config) { c.CFAccessTeamDomain = "evil.example.com" },
		"aud kosong":    func(c *Config) { c.CFAccessAUD = "" },
		"admins kosong": func(c *Config) { c.AdminEmails = nil },
	} {
		c := accessCfg()
		mut(&c)
		if _, err := NewAccessVerifier(c); !errors.Is(err, errAccessNotConfigured) {
			t.Errorf("%s: harus errAccessNotConfigured, dapat %v", name, err)
		}
	}
}

func TestJWKSOverrideOnlyForTestDB(t *testing.T) {
	t.Setenv("TEST_ONLY_CF_ACCESS_JWKS_URL", "http://jwks.uji/certs")
	t.Setenv("TEST_ONLY_GOOGLE_JWKS_URL", "http://jwks.uji/google")
	t.Setenv("DB_NAME", "mihanstore")
	if c := LoadConfig(); c.TestCFAccessJWKSURL != "" || c.TestGoogleJWKSURL != "" {
		t.Fatal("override JWKS tidak boleh berlaku untuk database produksi")
	}
	t.Setenv("DB_NAME", "mihanstore_test")
	if c := LoadConfig(); c.TestCFAccessJWKSURL == "" || c.TestGoogleJWKSURL == "" {
		t.Fatal("override JWKS harus berlaku untuk database uji")
	}
}

func TestCacheMaxAge(t *testing.T) {
	cases := map[string]time.Duration{
		"public, max-age=19867, must-revalidate, no-transform": 19867 * time.Second,
		"no-store": 0, "": 0, "max-age=abc": 0,
	}
	for h, want := range cases {
		if got := cacheMaxAge(h); got != want {
			t.Errorf("cacheMaxAge(%q) = %v, mau %v", h, got, want)
		}
	}
}

func TestJWKSCacheFailClosed(t *testing.T) {
	c := NewJWKSCache(func(ctx context.Context) (map[string]*rsa.PublicKey, time.Duration, error) {
		return nil, 0, errors.New("jaringan mati")
	})
	if _, err := verifyRS256(context.Background(), c, signJWT(t, testKey, "k1", "RS256", accessClaimsOK())); err == nil {
		t.Fatal("JWKS gagal diambil harus menolak token")
	}
}

// ---------- Google ----------

const testGoogleClient = "123-uji.apps.googleusercontent.com"

func newTestGoogle() *GoogleVerifier {
	g := NewGoogleVerifier(Config{GoogleClientID: testGoogleClient, AuthHMACSecret: strings.Repeat("s", 48)})
	g.jwks = stubJWKS(map[string]*rsa.PublicKey{"g1": &testKey.PublicKey})
	return g
}

func googleClaimsOK() map[string]any {
	now := time.Now().Unix()
	return map[string]any{"iss": "https://accounts.google.com", "aud": testGoogleClient, "sub": "1234567890",
		"email": "Pembeli@Gmail.com", "email_verified": true, "name": "Pembeli Uji",
		"picture": "https://lh3.googleusercontent.com/a/x", "iat": now, "exp": now + 3600}
}

func TestGoogleVerify(t *testing.T) {
	g := newTestGoogle()
	ctx := context.Background()
	id, err := g.Verify(ctx, signJWT(t, testKey, "g1", "RS256", googleClaimsOK()))
	if err != nil || id.Email != "pembeli@gmail.com" || id.Sub != "1234567890" || id.Picture == "" {
		t.Fatalf("token Google sah ditolak: %v %+v", err, id)
	}
	// iss tanpa https juga sah; email_verified berupa string "true" juga sah.
	if _, err := g.Verify(ctx, signJWT(t, testKey, "g1", "RS256", with(with(googleClaimsOK(), "iss", "accounts.google.com"), "email_verified", "true"))); err != nil {
		t.Fatalf("iss accounts.google.com harus sah: %v", err)
	}
	now := time.Now().Unix()
	bad := map[string]map[string]any{
		"aud salah":              with(googleClaimsOK(), "aud", "lain.apps.googleusercontent.com"),
		"iss salah":              with(googleClaimsOK(), "iss", "https://evil.example"),
		"kedaluwarsa":            with(googleClaimsOK(), "exp", now-120),
		"email belum verifikasi": with(googleClaimsOK(), "email_verified", false),
		"email_verified hilang":  with(googleClaimsOK(), "email_verified", nil),
		"tanpa sub":              with(googleClaimsOK(), "sub", nil),
		"email rusak":            with(googleClaimsOK(), "email", "bukan-email"),
	}
	for name, c := range bad {
		if _, err := g.Verify(ctx, signJWT(t, testKey, "g1", "RS256", c)); err == nil {
			t.Errorf("%s: harus ditolak", name)
		}
	}
	if _, err := g.Verify(ctx, signJWT(t, otherKey, "g1", "RS256", googleClaimsOK())); err == nil {
		t.Error("tanda tangan salah harus ditolak")
	}
	// URL avatar non-https dibuang.
	id, _ = g.Verify(ctx, signJWT(t, testKey, "g1", "RS256", with(googleClaimsOK(), "picture", "javascript:alert(1)")))
	if id == nil || id.Picture != "" {
		t.Error("avatar non-https harus dibuang")
	}
}

func TestGoogleNotConfigured(t *testing.T) {
	if NewGoogleVerifier(Config{}) != nil || NewGoogleVerifier(Config{GoogleClientID: "x"}) != nil {
		t.Fatal("tanpa client id / HMAC secret, verifier harus nil")
	}
	h := newRouter(NewApp(Config{CORSAllowedOrigins: defaultCORSOrigins}))
	for _, p := range []string{"/api/auth/google", "/api/auth/google/complete"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", p, strings.NewReader(`{}`)))
		if w.Code != 503 || !strings.Contains(w.Body.String(), "Login Google belum dikonfigurasi") {
			t.Errorf("%s: %d %s", p, w.Code, w.Body.String())
		}
	}
}

func TestProfileToken(t *testing.T) {
	secret := strings.Repeat("k", 48)
	now := time.Now()
	tok, err := signProfileToken(secret, profileClaims{Sub: "1", Email: "a@b.co", Name: "A", Exp: now.Add(10 * time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if c, err := verifyProfileToken(secret, tok, now); err != nil || c.Sub != "1" || c.Email != "a@b.co" {
		t.Fatalf("token sah ditolak: %v", err)
	}
	if _, err := verifyProfileToken(secret, tok, now.Add(11*time.Minute)); err == nil {
		t.Error("token kedaluwarsa harus ditolak")
	}
	if _, err := verifyProfileToken(strings.Repeat("x", 48), tok, now); err == nil {
		t.Error("secret lain harus ditolak")
	}
	payload, sig, _ := strings.Cut(tok, ".")
	forged, _ := json.Marshal(profileClaims{Purpose: profilePurpose, Sub: "2", Email: "evil@b.co", Exp: now.Add(time.Hour).Unix()})
	if _, err := verifyProfileToken(secret, base64.RawURLEncoding.EncodeToString(forged)+"."+sig, now); err == nil {
		t.Error("payload palsu harus ditolak")
	}
	_ = payload
	if _, err := verifyProfileToken("pendek", tok, now); err == nil {
		t.Error("secret pendek harus ditolak")
	}
}

// ---------- requireAdmin & CSRF ----------

func adminReq(method, path, token string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(`{}`))
	r.RemoteAddr = "172.22.0.2:1234"
	if token != "" {
		r.Header.Set("Cf-Access-Jwt-Assertion", token)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestRequireAdminFailClosed(t *testing.T) {
	// Belum dikonfigurasi -> 503 untuk semua rute admin, bahkan dengan header.
	h := newRouter(NewApp(Config{CORSAllowedOrigins: defaultCORSOrigins, CFAccessTeamDomain: testTeam, AdminEmails: []string{"a@b.co"}}))
	for _, p := range []string{"/api/admin/me", "/api/admin/products", "/api/admin/activity-logs"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, adminReq("GET", p, "x.y.z", nil))
		if w.Code != 503 || !strings.Contains(w.Body.String(), "Akses admin belum dikonfigurasi") || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s belum dikonfigurasi: %d %s", p, w.Code, w.Body.String())
		}
	}

	app := NewApp(accessCfg())
	app.access = newTestAccess(t)
	h = newRouter(app)
	good := signJWT(t, testKey, "k1", "RS256", accessClaimsOK())
	cases := []struct {
		name   string
		r      *http.Request
		status int
	}{
		{"tanpa header", adminReq("GET", "/api/admin/me", "", nil), 401},
		{"header palsu", adminReq("GET", "/api/admin/me", "palsu", nil), 401},
		{"tanda tangan lain", adminReq("GET", "/api/admin/me", signJWT(t, otherKey, "k1", "RS256", accessClaimsOK()), nil), 401},
		{"email bukan admin", adminReq("GET", "/api/admin/me", signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "email", "x@y.co")), nil), 403},
		{"POST tanpa CSRF header", adminReq("POST", "/api/admin/products", good, nil), 403},
		{"POST origin asing", adminReq("POST", "/api/admin/products", good, map[string]string{
			"Content-Type": "application/json", "X-Requested-With": adminRequestedWith, "Origin": "https://evil.example"}), 403},
		// Token sah + CSRF lolos, tapi DB belum ada -> 503 (bukan 200).
		{"token sah tanpa DB", adminReq("GET", "/api/admin/me", good, nil), 503},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, c.r)
		if w.Code != c.status {
			t.Errorf("%s: %d mau %d (%s)", c.name, w.Code, c.status, w.Body.String())
		}
	}
}

func TestCheckCSRF(t *testing.T) {
	origins := defaultCORSOrigins
	base := map[string]string{"Content-Type": "application/json", "X-Requested-With": adminRequestedWith}
	mk := func(extra map[string]string, drop ...string) *http.Request {
		r := httptest.NewRequest("POST", "/api/admin/products", nil)
		for k, v := range base {
			r.Header.Set(k, v)
		}
		for k, v := range extra {
			r.Header.Set(k, v)
		}
		for _, d := range drop {
			r.Header.Del(d)
		}
		return r
	}
	ok := []*http.Request{
		mk(map[string]string{"Origin": "https://store.mihan.web.id", "Sec-Fetch-Site": "same-origin"}),
		mk(map[string]string{"Origin": "https://store.mihan.web.id"}),
		mk(map[string]string{"Sec-Fetch-Site": "same-origin"}),
		mk(map[string]string{"Origin": "https://store.mihan.web.id", "Content-Type": "application/json; charset=utf-8"}),
	}
	for i, r := range ok {
		if err := checkCSRF(r, origins); err != nil {
			t.Errorf("kasus sah %d ditolak: %v", i, err)
		}
	}
	bad := map[string]*http.Request{
		"tanpa origin & sec-fetch":   mk(nil),
		"origin asing":               mk(map[string]string{"Origin": "https://evil.example"}),
		"cross-site":                 mk(map[string]string{"Origin": "https://store.mihan.web.id", "Sec-Fetch-Site": "cross-site"}),
		"same-site (subdomain lain)": mk(map[string]string{"Sec-Fetch-Site": "same-site"}),
		"form-urlencoded":            mk(map[string]string{"Origin": "https://store.mihan.web.id", "Content-Type": "application/x-www-form-urlencoded"}),
		"text/plain":                 mk(map[string]string{"Origin": "https://store.mihan.web.id", "Content-Type": "text/plain"}),
		"tanpa X-Requested-With":     mk(map[string]string{"Origin": "https://store.mihan.web.id"}, "X-Requested-With"),
		"X-Requested-With salah":     mk(map[string]string{"Origin": "https://store.mihan.web.id", "X-Requested-With": "XMLHttpRequest"}),
		"origin null":                mk(map[string]string{"Origin": "null"}),
	}
	for name, r := range bad {
		if err := checkCSRF(r, origins); err == nil {
			t.Errorf("%s: harus ditolak", name)
		}
	}
}

// ---------- Validasi ----------

func i64(v int64) *int64   { return &v }
func u64(v uint64) *uint64 { return &v }
func sp(s string) *string  { return &s }

func TestValidateProduct(t *testing.T) {
	ok := ProductInput{Name: "  Kerupuk\tBaru  ", Description: sp("Baris 1\nBaris 2\x00"), Price: i64(15000), CategoryID: u64(1), ImagePath: sp("produk/kerupuk-5.jpg")}
	f, err := validateProduct(ok, true)
	if err != nil || f.Name != "Kerupuk Baru" || *f.Description != "Baris 1\nBaris 2" || f.Price != 15000 || !f.IsActive || *f.ImagePath != "produk/kerupuk-5.jpg" {
		t.Fatalf("produk sah: %v %+v", err, f)
	}
	if f, _ := validateProduct(ProductInput{Name: "A", Price: i64(0), CategoryID: u64(2), ImagePath: sp("  ")}, true); f.ImagePath != nil || f.Description != nil {
		t.Error("image/deskripsi kosong harus NULL")
	}
	bad := map[string]ProductInput{
		"nama kosong":      {Name: "  ", Price: i64(1), CategoryID: u64(1)},
		"nama 151":         {Name: strings.Repeat("a", 151), Price: i64(1), CategoryID: u64(1)},
		"harga negatif":    {Name: "A", Price: i64(-1), CategoryID: u64(1)},
		"harga > 1M":       {Name: "A", Price: i64(1_000_000_001), CategoryID: u64(1)},
		"tanpa harga":      {Name: "A", CategoryID: u64(1)},
		"tanpa kategori":   {Name: "A", Price: i64(1)},
		"deskripsi 2001":   {Name: "A", Price: i64(1), CategoryID: u64(1), Description: sp(strings.Repeat("é", 2001))},
		"image traversal":  {Name: "A", Price: i64(1), CategoryID: u64(1), ImagePath: sp("../etc/passwd")},
		"image absolut":    {Name: "A", Price: i64(1), CategoryID: u64(1), ImagePath: sp("/etc/passwd")},
		"image spasi":      {Name: "A", Price: i64(1), CategoryID: u64(1), ImagePath: sp("a b.jpg")},
		"image url":        {Name: "A", Price: i64(1), CategoryID: u64(1), ImagePath: sp("https://x/y.jpg")},
		"image terlalu pj": {Name: "A", Price: i64(1), CategoryID: u64(1), ImagePath: sp(strings.Repeat("a", 256))},
	}
	for name, in := range bad {
		if _, err := validateProduct(in, true); err == nil {
			t.Errorf("%s: harus ditolak", name)
		}
	}
	if f, _ := validateProduct(ProductInput{Name: "A", Price: i64(1_000_000_000), CategoryID: u64(1)}, false); f.Price != 1_000_000_000 || f.IsActive {
		t.Error("harga maksimum harus diterima; default nonaktif harus dipakai")
	}
}

func TestValidateCategory(t *testing.T) {
	so := 5
	c, err := validateCategory(CategoryInput{Slug: "bumbu_dapur", Name: " Bumbu  Dapur ", SortOrder: &so})
	if err != nil || c.Slug != "bumbu_dapur" || c.Name != "Bumbu Dapur" || c.SortOrder != 5 {
		t.Fatalf("kategori sah: %v %+v", err, c)
	}
	big := 2_000_000
	for name, in := range map[string]CategoryInput{
		"slug huruf besar": {Slug: "Bumbu", Name: "B"},
		"slug 1 huruf":     {Slug: "b", Name: "B"},
		"slug spasi":       {Slug: "bu mbu", Name: "B"},
		"slug tanda hub":   {Slug: "bu-mbu", Name: "B"},
		"slug 51":          {Slug: strings.Repeat("a", 51), Name: "B"},
		"nama kosong":      {Slug: "ok", Name: ""},
		"nama 81":          {Slug: "ok", Name: strings.Repeat("a", 81)},
		"urutan besar":     {Slug: "ok", Name: "B", SortOrder: &big},
	} {
		if _, err := validateCategory(in); err == nil {
			t.Errorf("%s: harus ditolak", name)
		}
	}
}

// ---------- Redaksi details log ----------

func TestSanitizeDetails(t *testing.T) {
	d := map[string]any{
		"password": "rahasia", "newPassword": "x", "password_hash": "$argon2id$...", "token": "abc",
		"Authorization": "Bearer abc", "cookie": "CF_Authorization=xyz", "id_token": "eyJ...", "credential": "eyJ...",
		"profileToken": "p", "nested": map[string]any{"sessionToken": "s", "aman": "ok"},
		"panjang": strings.Repeat("a", 1000), "harga": 15000,
		"perubahan": map[string]any{"price": map[string]any{"dari": 1, "menjadi": 2}},
	}
	js := *detailsJSON(d)
	for _, secret := range []string{"rahasia", "argon2id", "abc", "xyz", "eyJ", `"p"`, `"s"`} {
		if strings.Contains(js, secret) {
			t.Errorf("nilai sensitif %q bocor ke log: %s", secret, js)
		}
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(js), &back); err != nil {
		t.Fatal(err)
	}
	if back["harga"].(float64) != 15000 || back["nested"].(map[string]any)["aman"] != "ok" {
		t.Error("nilai aman harus tetap ada")
	}
	if n := len([]rune(back["panjang"].(string))); n > maxDetailString+1 {
		t.Errorf("nilai panjang tidak dipotong: %d", n)
	}
	if back["perubahan"].(map[string]any)["price"] == nil {
		t.Error("selisih perubahan harus tetap ada")
	}
	// Ukuran total dibatasi.
	huge := map[string]any{}
	for i := 0; i < 49; i++ {
		huge[strings.Repeat("k", 10)+string(rune('a'+i%26))+string(rune('a'+i/26))] = strings.Repeat("x", 300)
	}
	if s := detailsJSON(huge); s == nil || len(*s) > maxDetailBytes {
		t.Error("details besar harus dipotong")
	}
	// Entry: summary/actor/UA dipotong sesuai kolom.
	row := LogEntry{Action: "auth.login_failed", ActorLabel: strings.Repeat("A", 300), UserAgent: strings.Repeat("u", 600)}.toRow()
	if len(*row.ActorLabel) > 100 || len(*row.UserAgent) > 255 || row.Summary == "" {
		t.Error("kolom log tidak dipotong dengan benar")
	}
}

func TestDiffMaps(t *testing.T) {
	d := diffMaps(map[string]any{"price": uint32(1000), "name": "A"}, map[string]any{"price": uint32(2000), "name": "A"})
	if len(d) != 1 || d["price"] == nil {
		t.Fatalf("diff salah: %v", d)
	}
}

func TestUsernameBase(t *testing.T) {
	cases := map[string]string{
		"ikhsan.sumaila@gmail.com": "ikhsan_sumaila",
		"qomariahakmala@gmail.com": "qomariahakmala",
		"a@b.co":                   "user_a",
		"Admin.Toko@x.com":         "user", // mengandung kata terlarang -> fallback
		"x+tag@y.com":              "user_x",
	}
	for in, want := range cases {
		if got := usernameBase(in); got != want {
			t.Errorf("usernameBase(%q) = %q mau %q", in, got, want)
		}
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`50%_a\b`); got != `50\%\_a\\b` {
		t.Fatalf("escapeLike: %q", got)
	}
}
