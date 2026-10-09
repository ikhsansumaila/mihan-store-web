package admin

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

	"mihanstore/internal/platform"
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

func stubJWKS(keys map[string]*rsa.PublicKey) *platform.JWKSCache {
	return platform.NewJWKSCache(func(ctx context.Context) (map[string]*rsa.PublicKey, time.Duration, error) {
		return keys, 0, nil
	})
}

const (
	testTeam = "divine-rice-4f1d.cloudflareaccess.com"
	testAUD  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func accessCfg() platform.Config {
	return platform.Config{
		CORSAllowedOrigins: platform.DefaultCORSOrigins, CFAccessTeamDomain: testTeam, CFAccessAUD: testAUD,
		AdminEmails: []string{"ikhsan.sumaila@gmail.com", "qomariahakmala@gmail.com"}, DBName: "x",
	}
}

func newTestAccess(t *testing.T) *AccessVerifier {
	t.Helper()
	v, err := NewAccessVerifier(accessCfg())
	if err != nil {
		t.Fatal(err)
	}
	v.JWKS = stubJWKS(map[string]*rsa.PublicKey{"k1": &testKey.PublicKey})
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
	for name, mut := range map[string]func(*platform.Config){
		"team kosong":   func(c *platform.Config) { c.CFAccessTeamDomain = "" },
		"team salah":    func(c *platform.Config) { c.CFAccessTeamDomain = "evil.example.com" },
		"aud kosong":    func(c *platform.Config) { c.CFAccessAUD = "" },
		"admins kosong": func(c *platform.Config) { c.AdminEmails = nil },
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
	if c := platform.LoadConfig(); c.TestCFAccessJWKSURL != "" || c.TestGoogleJWKSURL != "" {
		t.Fatal("override JWKS tidak boleh berlaku untuk database produksi")
	}
	t.Setenv("DB_NAME", "mihanstore_test")
	if c := platform.LoadConfig(); c.TestCFAccessJWKSURL == "" || c.TestGoogleJWKSURL == "" {
		t.Fatal("override JWKS harus berlaku untuk database uji")
	}
}

func TestCheckCSRF(t *testing.T) {
	origins := platform.DefaultCORSOrigins
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

// ---------- CSRF multipart ----------

func TestCSRFMultipartOnlyForUpload(t *testing.T) {
	origins := []string{"https://store.mihan.web.id"}
	mk := func(ct string, xrw bool, origin string) struct{ r *http.Request } {
		r := httptest.NewRequest("POST", "/api/admin/products/1/image", nil)
		r.Header.Set("Content-Type", ct)
		if xrw {
			r.Header.Set("X-Requested-With", adminRequestedWith)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return struct{ r *http.Request }{r}
	}
	mp := "multipart/form-data; boundary=abc"
	if err := checkCSRFOpts(mk(mp, true, origins[0]).r, origins, true); err != nil {
		t.Fatalf("multipart sah ditolak: %v", err)
	}
	if err := checkCSRFOpts(mk(mp, true, origins[0]).r, origins, false); err == nil {
		t.Fatal("multipart di rute lain harus ditolak")
	}
	if err := checkCSRF(mk(mp, true, origins[0]).r, origins); err == nil {
		t.Fatal("checkCSRF lama tetap hanya JSON")
	}
	if err := checkCSRFOpts(mk(mp, false, origins[0]).r, origins, true); err == nil {
		t.Fatal("tanpa X-Requested-With harus ditolak")
	}
	if err := checkCSRFOpts(mk(mp, true, "https://jahat.example").r, origins, true); err == nil {
		t.Fatal("origin asing harus ditolak")
	}
	if err := checkCSRFOpts(mk("application/x-www-form-urlencoded", true, origins[0]).r, origins, true); err == nil {
		t.Fatal("form biasa harus ditolak")
	}
	if err := checkCSRFOpts(mk(mp, true, "").r, origins, true); err == nil {
		t.Fatal("tanpa Origin dan Sec-Fetch-Site harus ditolak")
	}
	for _, p := range []string{"/api/admin/products/1/image", "/api/admin/products/123/image"} {
		if !imageUploadPathRe.MatchString(p) {
			t.Errorf("%s harus cocok", p)
		}
	}
	for _, p := range []string{"/api/admin/products/1", "/api/admin/products/1/image/x", "/api/admin/products/a/image", "/api/admin/settings"} {
		if imageUploadPathRe.MatchString(p) {
			t.Errorf("%s tidak boleh cocok", p)
		}
	}
}
