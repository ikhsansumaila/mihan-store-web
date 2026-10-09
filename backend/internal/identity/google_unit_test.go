package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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

func newTestGoogle() *GoogleVerifier {
	g := NewGoogleVerifier(platform.Config{GoogleClientID: testGoogleClient, AuthHMACSecret: strings.Repeat("s", 48)})
	g.JWKS = stubJWKS(map[string]*rsa.PublicKey{"g1": &testKey.PublicKey})
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

func TestUsernameBase(t *testing.T) {
	cases := map[string]string{
		"ikhsan.sumaila@gmail.com": "ikhsan_sumaila",
		"qomariahakmala@gmail.com": "qomariahakmala",
		"a@b.co":                   "user_a",
		"Admin.Toko@x.com":         "user", // mengandung kata terlarang -> fallback
		"x+tag@y.com":              "user_x",
	}
	for in, want := range cases {
		if got := UsernameBase(in); got != want {
			t.Errorf("UsernameBase(%q) = %q mau %q", in, got, want)
		}
	}
}
