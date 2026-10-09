package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
)

const sessionTokenBytes = 32

// NewSessionToken menghasilkan token acak 32 byte (base64url tanpa padding, 43 karakter)
// beserta SHA-256 hex-nya. Hanya hash yang disimpan di database.
func NewSessionToken() (token, hash string, err error) {
	b := make([]byte, sessionTokenBytes)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// wellFormedToken memeriksa bentuk token sebelum menyentuh database
// (token lama seperti "token_..." langsung ditolak).
func wellFormedToken(t string) bool {
	if len(t) != 43 {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(t)
	return err == nil && len(b) == sessionTokenBytes
}

// bearerToken mengambil token dari header "Authorization: Bearer <token>".
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
