package platform

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxBodyBytes = 16 << 10 // 16 KB

// Pesan galat umum yang dipakai banyak modul.
const (
	// MsgTooManyAdmin: batas laju rute admin (dipakai modul admin, identity/pelanggan, push).
	MsgTooManyAdmin = "Terlalu banyak permintaan. Coba lagi beberapa saat lagi."

	MsgServiceDown = "Layanan sedang tidak tersedia, coba lagi beberapa saat lagi."
	MsgServerError = "Terjadi kesalahan pada server"
)

// Paginasi daftar admin: ?page= dan ?per_page=.
const (
	MaxPerPage     = 100
	DefaultPerPage = 20
)

// RetryAfter menulis header Retry-After (detik, dibulatkan ke atas + 1) dari sisa waktu tunggu d.
func RetryAfter(w http.ResponseWriter, d time.Duration) {
	secs := int(d.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
}

// LimitUser menerapkan batas laju per pengguna (kunci "u<id>"); false = respons 429 sudah ditulis.
func LimitUser(w http.ResponseWriter, rl *RateLimiter, userID uint64, msg string) bool {
	if ok, wait := rl.Allow("u" + strconv.FormatUint(userID, 10)); !ok {
		RetryAfter(w, wait)
		WriteError(w, http.StatusTooManyRequests, msg)
		return false
	}
	return true
}

// PageParams membaca ?page= dan ?per_page= dengan batas (page 1..100000, per_page 1..MaxPerPage).
func PageParams(r *http.Request) (page, perPage int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ = strconv.Atoi(r.URL.Query().Get("per_page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		page = 100000
	}
	if perPage < 1 {
		perPage = DefaultPerPage
	}
	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}
	return
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("gagal menulis respons: %v", err)
	}
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, ErrorResponse{Error: msg})
}

var (
	ErrBodyTooLarge = errors.New("body terlalu besar")
	ErrBadJSON      = errors.New("json tidak valid")
)

// DecodeJSON membaca body JSON maksimal 16 KB, menolak field tak dikenal dan data tambahan.
// Body kosong diperbolehkan bila allowEmpty.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, allowEmpty bool) error {
	return decodeJSONOpts(w, r, dst, allowEmpty, false)
}

// DecodeJSONLenient seperti DecodeJSON tetapi MENGABAIKAN field tak dikenal. Dipakai untuk
// keranjang/checkout pelanggan: field seperti harga/total dari browser diabaikan begitu saja
// (semua nominal dihitung ulang di server dari database).
func DecodeJSONLenient(w http.ResponseWriter, r *http.Request, dst any, allowEmpty bool) error {
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
			return ErrBodyTooLarge
		case errors.Is(err, io.EOF) && allowEmpty:
			return nil
		default:
			return ErrBadJSON
		}
	}
	// Pastikan tidak ada data lain setelah objek JSON (dan deteksi body terlalu besar).
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return ErrBodyTooLarge
		}
		return ErrBadJSON
	}
	return nil
}

// RespondDecodeError menulis respons yang sesuai untuk error DecodeJSON.
func RespondDecodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrBodyTooLarge) {
		WriteError(w, http.StatusRequestEntityTooLarge, "Ukuran permintaan terlalu besar")
		return
	}
	WriteError(w, http.StatusBadRequest, "Format permintaan tidak valid")
}

// SecurityHeaders menambah header keamanan sederhana pada semua respons.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/auth/") || strings.HasPrefix(r.URL.Path, "/api/admin/") ||
			r.URL.Path == "/api/cart" || strings.HasPrefix(r.URL.Path, "/api/cart/") ||
			r.URL.Path == "/api/orders" || strings.HasPrefix(r.URL.Path, "/api/orders/") || r.URL.Path == "/api/store-info" ||
			strings.HasPrefix(r.URL.Path, "/api/push/") {
			h.Set("Cache-Control", "no-store")
			h.Set("Pragma", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// Recoverer mencegah panic menjatuhkan server dan tidak membocorkan detail ke klien.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Printf("panic pada %s %s: %v", r.Method, r.URL.Path, v)
				WriteError(w, http.StatusInternalServerError, "Terjadi kesalahan pada server")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// HTTPError dipakai untuk membatalkan transaksi dengan respons HTTP tertentu (status + pesan).
type HTTPError struct {
	Status int
	Msg    string
}

func (e *HTTPError) Error() string { return e.Msg }

// FieldError: kesalahan validasi satu field (422 {error, field}).
type FieldError struct {
	Field string
	Msg   string
}

func (e *FieldError) Error() string { return e.Msg }
