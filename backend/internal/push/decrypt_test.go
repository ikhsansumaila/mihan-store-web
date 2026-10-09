package push

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/hkdf"
)

// decryptAES128GCM: sisi penerima RFC 8291 (hanya untuk tes, satu record).
func decryptAES128GCM(body []byte, ua *ecdh.PrivateKey, auth []byte) ([]byte, error) {
	if len(body) < 21 {
		return nil, errors.New("terlalu pendek")
	}
	salt := body[:16]
	idlen := int(body[20])
	if len(body) < 21+idlen {
		return nil, errors.New("header rusak")
	}
	asPubBytes := body[21 : 21+idlen]
	ct := body[21+idlen:]
	asPub, err := ecdh.P256().NewPublicKey(asPubBytes)
	if err != nil {
		return nil, err
	}
	secret, err := ua.ECDH(asPub)
	if err != nil {
		return nil, err
	}
	info := append([]byte("WebPush: info\x00"), ua.PublicKey().Bytes()...)
	info = append(info, asPubBytes...)
	ikm := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, secret, auth, info), ikm); err != nil {
		return nil, err
	}
	cek := make([]byte, 16)
	nonce := make([]byte, 12)
	io.ReadFull(hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: aes128gcm\x00")), cek)
	io.ReadFull(hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: nonce\x00")), nonce)
	blk, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, err
	}
	plain = bytes.TrimRight(plain, "\x00")
	if len(plain) == 0 || plain[len(plain)-1] != 2 {
		return nil, errors.New("pembatas padding tidak ada")
	}
	return plain[:len(plain)-1], nil
}
