package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

var errTurnstileNotConfigured = errors.New("turnstile belum dikonfigurasi")

type TurnstileResponse struct {
	Success    bool     `json:"success"`
	Hostname   string   `json:"hostname"`
	ErrorCodes []string `json:"error-codes"`
}

type TurnstileVerifier struct {
	secret   string
	allowOff bool
	client   *http.Client
	endpoint string
}

func NewTurnstileVerifier(cfg Config) *TurnstileVerifier {
	v := &TurnstileVerifier{
		secret:   cfg.TurnstileSecret,
		allowOff: cfg.AllowNoTurnstile,
		client:   &http.Client{Timeout: 10 * time.Second},
		endpoint: turnstileVerifyURL,
	}
	switch {
	case v.secret == "" && v.allowOff:
		log.Println("PERINGATAN: TURNSTILE_SECRET_KEY kosong dan ALLOW_NO_TURNSTILE=true — verifikasi Turnstile DIMATIKAN. Hanya untuk pengembangan, jangan dipakai di produksi.")
	case v.secret == "":
		log.Println("PERINGATAN: TURNSTILE_SECRET_KEY kosong — login dan registrasi akan DITOLAK (fail-closed) sampai kunci diisi.")
	}
	return v
}

// Verify memeriksa token Turnstile ke Cloudflare.
// Mengembalikan errTurnstileNotConfigured bila kunci rahasia tidak ada (fail-closed).
func (v *TurnstileVerifier) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	if v.secret == "" {
		if v.allowOff {
			return true, nil
		}
		log.Println("PERINGATAN: permintaan ditolak karena TURNSTILE_SECRET_KEY belum diisi")
		return false, errTurnstileNotConfigured
	}
	if token == "" || len(token) > 2048 {
		return false, nil
	}
	form := url.Values{}
	form.Set("secret", v.secret)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := v.client.Do(req)
	if err != nil {
		log.Printf("turnstile: gagal menghubungi Cloudflare: %v", err)
		return false, err
	}
	defer resp.Body.Close()
	var result TurnstileResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil {
		log.Printf("turnstile: respons tidak bisa dibaca: %v", err)
		return false, err
	}
	if !result.Success {
		log.Printf("turnstile: verifikasi gagal %v", result.ErrorCodes)
	}
	return result.Success, nil
}
