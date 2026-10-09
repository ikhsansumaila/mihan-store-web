package app

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mihanstore/internal/admin"
	"mihanstore/internal/identity"
	"mihanstore/internal/platform"
)

// adminRequestedWith: nilai header X-Requested-With admin (internal/admin).
const adminRequestedWith = admin.RequestedWith

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

func newTestAccess(t *testing.T) *admin.AccessVerifier {
	t.Helper()
	v, err := admin.NewAccessVerifier(accessCfg())
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

// ---------- Google ----------

const testGoogleClient = "123-uji.apps.googleusercontent.com"

func googleClaimsOK() map[string]any {
	now := time.Now().Unix()
	return map[string]any{"iss": "https://accounts.google.com", "aud": testGoogleClient, "sub": "1234567890",
		"email": "Pembeli@Gmail.com", "email_verified": true, "name": "Pembeli Uji",
		"picture": "https://lh3.googleusercontent.com/a/x", "iat": now, "exp": now + 3600}
}

func TestGoogleNotConfigured(t *testing.T) {
	if identity.NewGoogleVerifier(platform.Config{}) != nil || identity.NewGoogleVerifier(platform.Config{GoogleClientID: "x"}) != nil {
		t.Fatal("tanpa client id / HMAC secret, verifier harus nil")
	}
	h := newRouter(NewApp(platform.Config{CORSAllowedOrigins: platform.DefaultCORSOrigins}))
	for _, p := range []string{"/api/auth/google", "/api/auth/google/complete"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", p, strings.NewReader(`{}`)))
		if w.Code != 503 || !strings.Contains(w.Body.String(), "Login Google belum dikonfigurasi") {
			t.Errorf("%s: %d %s", p, w.Code, w.Body.String())
		}
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
	h := newRouter(NewApp(platform.Config{CORSAllowedOrigins: platform.DefaultCORSOrigins, CFAccessTeamDomain: testTeam, AdminEmails: []string{"a@b.co"}}))
	for _, p := range []string{"/api/admin/me", "/api/admin/products", "/api/admin/activity-logs"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, adminReq("GET", p, "x.y.z", nil))
		if w.Code != 503 || !strings.Contains(w.Body.String(), "Akses admin belum dikonfigurasi") || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s belum dikonfigurasi: %d %s", p, w.Code, w.Body.String())
		}
	}

	app := NewApp(accessCfg())
	app.admin.Access = newTestAccess(t)
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
