package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Parameter argon2id sesuai rekomendasi minimum OWASP.
const (
	argonMemoryKiB = 19456
	argonTime      = 2
	argonThreads   = 1
	argonSaltLen   = 16
	argonKeyLen    = 32

	passwordMinLen = 8
	passwordMaxLen = 128
)

var errInvalidHash = errors.New("format hash tidak valid")

// hashSem membatasi jumlah perhitungan argon2 yang berjalan bersamaan
// (tiap perhitungan memakai ~19 MiB RAM) agar banjir request tidak menghabiskan memori.
var hashSem = make(chan struct{}, 8)

func withHashSlot(f func()) {
	hashSem <- struct{}{}
	defer func() { <-hashSem }()
	f()
}

// HashPassword menghasilkan string PHC: $argon2id$v=19$m=...,t=...,p=...$salt$hash
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	var key []byte
	withHashSlot(func() {
		key = argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	})
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword membandingkan password dengan hash PHC secara constant-time.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errInvalidHash
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, errInvalidHash
	}
	// Batas wajar agar hash yang rusak/berbahaya tidak memicu pemakaian sumber daya berlebihan.
	if m < 8 || m > 1<<20 || t < 1 || t > 10 || p < 1 || p > 16 {
		return false, errInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false, errInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) < 16 || len(want) > 64 {
		return false, errInvalidHash
	}
	var got []byte
	withHashSlot(func() {
		got = argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	})
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash dipakai saat akun tidak ditemukan/terkunci, agar waktu respons sama
// dengan kasus akun ada (tidak membocorkan keberadaan akun).
var dummyHash string

func initDummyHash() {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	h, err := HashPassword(base64.RawStdEncoding.EncodeToString(b))
	if err != nil {
		panic(err)
	}
	dummyHash = h
}

func burnDummyVerify(password string) {
	_, _ = VerifyPassword(password, dummyHash)
}

// ValidatePassword memeriksa panjang password (dalam karakter Unicode).
func ValidatePassword(pw string) error {
	if !utf8.ValidString(pw) {
		return errors.New("Password mengandung karakter tidak valid")
	}
	n := utf8.RuneCountInString(pw)
	if n < passwordMinLen || n > passwordMaxLen {
		return fmt.Errorf("Password harus %d–%d karakter", passwordMinLen, passwordMaxLen)
	}
	return nil
}
