package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
)

const maxBodyBytes = 16 << 10 // 16 KB

type ErrorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("gagal menulis respons: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

var (
	errBodyTooLarge = errors.New("body terlalu besar")
	errBadJSON      = errors.New("json tidak valid")
)

// decodeJSON membaca body JSON maksimal 16 KB, menolak field tak dikenal dan data tambahan.
// Body kosong diperbolehkan bila allowEmpty.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, allowEmpty bool) error {
	return decodeJSONOpts(w, r, dst, allowEmpty, false)
}

// decodeJSONLenient seperti decodeJSON tetapi MENGABAIKAN field tak dikenal. Dipakai untuk
// keranjang/checkout pelanggan: field seperti harga/total dari browser diabaikan begitu saja
// (semua nominal dihitung ulang di server dari database).
func decodeJSONLenient(w http.ResponseWriter, r *http.Request, dst any, allowEmpty bool) error {
	return decodeJSONOpts(w, r, dst, allowEmpty, true)
}

func decodeJSONOpts(w http.ResponseWriter, r *http.Request, dst any, allowEmpty, allowUnknown bool) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if !allowUnknown {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			return errBodyTooLarge
		case errors.Is(err, io.EOF) && allowEmpty:
			return nil
		default:
			return errBadJSON
		}
	}
	// Pastikan tidak ada data lain setelah objek JSON (dan deteksi body terlalu besar).
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return errBodyTooLarge
		}
		return errBadJSON
	}
	return nil
}

// respondDecodeError menulis respons yang sesuai untuk error decodeJSON.
func respondDecodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errBodyTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "Ukuran permintaan terlalu besar")
		return
	}
	writeError(w, http.StatusBadRequest, "Format permintaan tidak valid")
}

// securityHeaders menambah header keamanan sederhana pada semua respons.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/auth/") || strings.HasPrefix(r.URL.Path, "/api/admin/") ||
			r.URL.Path == "/api/cart" || strings.HasPrefix(r.URL.Path, "/api/cart/") ||
			r.URL.Path == "/api/orders" || strings.HasPrefix(r.URL.Path, "/api/orders/") || r.URL.Path == "/api/store-info" {
			h.Set("Cache-Control", "no-store")
			h.Set("Pragma", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// recoverer mencegah panic menjatuhkan server dan tidak membocorkan detail ke klien.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Printf("panic pada %s %s: %v", r.Method, r.URL.Path, v)
				writeError(w, http.StatusInternalServerError, "Terjadi kesalahan pada server")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
