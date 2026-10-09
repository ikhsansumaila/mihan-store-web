package identity

// Modul identitas pelanggan: daftar/masuk/verifikasi/keluar (sesi Bearer, argon2id, penguncian akun,
// Turnstile), login Google (+ lengkapi profil), middleware pelanggan, dan menu admin "Pelanggan"
// (+ alias). Dependensi lintas modul diberikan eksplisit lewat Deps oleh perakit.

import (
	"net"
	"net/http"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/audit"
	"mihanstore/internal/platform"
)

// Deps: dependensi modul identity.
type Deps struct {
	// Cfg: konfigurasi aplikasi (dibaca saat permintaan: SessionTTL, AuthHMACSecret, ADMIN_EMAILS).
	Cfg      *platform.Config
	DB       func() *gorm.DB // koneksi saat ini (nil bila belum terhubung)
	Now      func() time.Time
	Audit    *audit.Logger
	ClientIP func(r *http.Request) net.IP
	// AdminUser: admin pelaku rute /api/admin/customers* (diisi middleware modul admin).
	AdminUser func(r *http.Request) *User
	// StatusLabel: label status pesanan (bahasa Indonesia) untuk detail pelanggan (modul pesanan).
	StatusLabel    func(status string) string
	RespondTxError func(w http.ResponseWriter, err error, ctx string)
}

// Service: modul identity. Verifier dan limiter diekspor agar bisa diganti saat uji.
type Service struct {
	Turnstile       *TurnstileVerifier
	Google          *GoogleVerifier       // nil = login Google belum dikonfigurasi (503)
	LoginLimiter    *platform.RateLimiter // per IP
	RegisterLimiter *platform.RateLimiter // per IP
	GoogleLimiter   *platform.RateLimiter // per IP
	AliasLimiter    *platform.RateLimiter // per admin: ubah alias pelanggan

	cfg            *platform.Config
	db             func() *gorm.DB
	now            func() time.Time
	audit          *audit.Logger
	clientIP       func(r *http.Request) net.IP
	adminUser      func(r *http.Request) *User
	statusLabel    func(string) string
	respondTxError func(w http.ResponseWriter, err error, ctx string)
}

func New(d Deps) *Service {
	return &Service{
		Turnstile:       NewTurnstileVerifier(*d.Cfg),
		Google:          NewGoogleVerifier(*d.Cfg),
		LoginLimiter:    platform.NewRateLimiter(10, time.Minute),
		RegisterLimiter: platform.NewRateLimiter(5, time.Hour),
		GoogleLimiter:   platform.NewRateLimiter(20, time.Minute),
		AliasLimiter:    platform.NewRateLimiter(60, time.Minute),

		cfg: d.Cfg, db: d.DB, now: d.Now, audit: d.Audit, clientIP: d.ClientIP,
		adminUser: d.AdminUser, statusLabel: d.StatusLabel, respondTxError: d.RespondTxError,
	}
}
