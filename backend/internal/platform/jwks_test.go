package platform

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
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

func accessClaimsOK() map[string]any {
	now := time.Now().Unix()
	return map[string]any{"aud": []string{testAUD}, "iss": "https://" + testTeam, "exp": now + 600,
		"iat": now, "nbf": now - 10, "email": "Ikhsan.Sumaila@gmail.com", "type": "app"}
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
	if _, err := VerifyRS256(context.Background(), c, signJWT(t, testKey, "k1", "RS256", accessClaimsOK())); err == nil {
		t.Fatal("JWKS gagal diambil harus menolak token")
	}
}
