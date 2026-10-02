package main

// Verifikasi JWT RS256 dengan kunci publik dari JWKS (stdlib saja, tanpa dependensi luar).
// Dipakai untuk Cloudflare Access (admin) dan Google ID token (login pelanggan).
// Fail-closed: JWKS tidak bisa diambil / kid tidak dikenal / tanda tangan salah -> error.

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// JWKSFetcher mengambil kunci publik (kid -> key) beserta umur cache yang disarankan
// server (Cache-Control max-age; 0 = tidak ada).
type JWKSFetcher func(ctx context.Context) (map[string]*rsa.PublicKey, time.Duration, error)

func HTTPJWKSFetcher(url string) JWKSFetcher {
	client := &http.Client{Timeout: 8 * time.Second}
	return func(ctx context.Context) (map[string]*rsa.PublicKey, time.Duration, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, 0, fmt.Errorf("JWKS HTTP %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return nil, 0, err
		}
		keys, err := ParseJWKS(body)
		if err != nil {
			return nil, 0, err
		}
		return keys, cacheMaxAge(resp.Header.Get("Cache-Control")), nil
	}
}

// cacheMaxAge membaca max-age dari header Cache-Control (0 bila tidak ada/no-store).
func cacheMaxAge(h string) time.Duration {
	for _, part := range strings.Split(h, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p == "no-store" || p == "no-cache" {
			return 0
		}
		if v, ok := strings.CutPrefix(p, "max-age="); ok {
			if n, err := strconv.Atoi(strings.Trim(v, `"`)); err == nil && n > 0 {
				return time.Duration(n) * time.Second
			}
		}
	}
	return 0
}

func ParseJWKS(body []byte) (map[string]*rsa.PublicKey, error) {
	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" || (k.Use != "" && k.Use != "sig") || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		nb, err1 := b64url(k.N)
		eb, err2 := b64url(k.E)
		if err1 != nil || err2 != nil || len(eb) == 0 || len(eb) > 4 {
			continue
		}
		n := new(big.Int).SetBytes(nb)
		if n.BitLen() < 2048 {
			continue
		}
		e := int(new(big.Int).SetBytes(eb).Int64())
		if e < 3 {
			continue
		}
		out[k.Kid] = &rsa.PublicKey{N: n, E: e}
	}
	if len(out) == 0 {
		return nil, errors.New("JWKS tidak berisi kunci RSA yang valid")
	}
	return out, nil
}

func b64url(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// JWKSCache menyimpan kunci JWKS di memori.
type JWKSCache struct {
	fetch      JWKSFetcher
	now        func() time.Time
	defaultTTL time.Duration // dipakai bila server tidak memberi max-age
	minTTL     time.Duration
	maxTTL     time.Duration
	minRefetch time.Duration // jeda minimal antar fetch (mis. kid tak dikenal)
	maxStale   time.Duration // boleh pakai cache lama bila fetch gagal

	mu          sync.Mutex
	keys        map[string]*rsa.PublicKey
	fetchedAt   time.Time
	ttl         time.Duration
	lastAttempt time.Time
}

func NewJWKSCache(fetch JWKSFetcher) *JWKSCache {
	return &JWKSCache{
		fetch: fetch, now: time.Now,
		defaultTTL: time.Hour, minTTL: 5 * time.Minute, maxTTL: 24 * time.Hour,
		minRefetch: 30 * time.Second, maxStale: 24 * time.Hour,
	}
}

func (c *JWKSCache) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	fresh := c.keys != nil && now.Sub(c.fetchedAt) < c.ttl
	if k, ok := c.keys[kid]; ok && fresh {
		return k, nil
	}
	if c.lastAttempt.IsZero() || now.Sub(c.lastAttempt) >= c.minRefetch || (c.keys == nil && now.Sub(c.lastAttempt) >= 5*time.Second) {
		c.lastAttempt = now
		keys, maxAge, err := c.fetch(ctx)
		if err == nil {
			ttl := maxAge
			if ttl <= 0 {
				ttl = c.defaultTTL
			}
			ttl = min(max(ttl, c.minTTL), c.maxTTL)
			c.keys, c.fetchedAt, c.ttl = keys, now, ttl
		} else if c.keys == nil || now.Sub(c.fetchedAt) > c.maxStale {
			return nil, fmt.Errorf("gagal mengambil JWKS: %w", err)
		}
	}
	if c.keys != nil && now.Sub(c.fetchedAt) <= c.maxStale {
		if k, ok := c.keys[kid]; ok {
			return k, nil
		}
	}
	return nil, errors.New("kid tidak dikenal")
}

// verifyRS256 memeriksa format, alg=RS256, dan tanda tangan, lalu mengembalikan payload JSON.
// Klaim (aud/iss/exp/...) diperiksa oleh pemanggil.
func verifyRS256(ctx context.Context, cache *JWKSCache, token string) ([]byte, error) {
	if token == "" || len(token) > 16384 {
		return nil, errors.New("token kosong/terlalu panjang")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("format token salah")
	}
	hb, err := b64url(parts[0])
	if err != nil {
		return nil, errors.New("header token rusak")
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(hb, &hdr); err != nil {
		return nil, errors.New("header token rusak")
	}
	if hdr.Alg != "RS256" {
		return nil, fmt.Errorf("alg tidak diizinkan: %q", truncateUTF8(hdr.Alg, 20))
	}
	sig, err := b64url(parts[2])
	if err != nil {
		return nil, errors.New("tanda tangan rusak")
	}
	pub, err := cache.key(ctx, hdr.Kid)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return nil, errors.New("tanda tangan tidak valid")
	}
	pb, err := b64url(parts[1])
	if err != nil {
		return nil, errors.New("payload rusak")
	}
	return pb, nil
}

const clockLeeway = 60 * time.Second

// numericDate menerima angka JSON (detik Unix) untuk exp/nbf/iat.
type numericDate struct {
	set bool
	t   time.Time
}

func (n *numericDate) UnmarshalJSON(b []byte) error {
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	n.set, n.t = true, time.Unix(int64(f), 0)
	return nil
}

// checkTimes: exp wajib dan belum lewat; nbf/iat (bila ada) tidak di masa depan.
func checkTimes(now time.Time, exp, nbf, iat numericDate) error {
	if !exp.set || now.After(exp.t.Add(clockLeeway)) {
		return errors.New("token kedaluwarsa")
	}
	if nbf.set && now.Add(clockLeeway).Before(nbf.t) {
		return errors.New("token belum berlaku")
	}
	if iat.set && now.Add(clockLeeway).Before(iat.t) {
		return errors.New("iat di masa depan")
	}
	return nil
}

func audContains(raw json.RawMessage, want string) bool {
	if want == "" {
		return false
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == want
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, a := range many {
			if a == want {
				return true
			}
		}
	}
	return false
}
