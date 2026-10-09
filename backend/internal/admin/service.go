package admin

// Modul admin: middleware /api/admin/* (Cloudflare Access JWT + ADMIN_EMAILS + baris users + CSRF),
// akun admin otomatis (ensureAdminUser), dan rute AdminMe/AdminSummary.
//
// Ketergantungan: modul ini mengimpor internal/identity (satu arah) karena prinsipal admin adalah
// baris users (identity.User) dan pembuatan akun admin memakai aturan username/nama identity.
// identity tidak mengimpor admin: ia menerima admin pelaku lewat identity.Deps.AdminUser.
// Ringkasan dashboard membaca tabel products/categories/orders dengan kueri baca-saja (tanpa
// mengimpor modul katalog atau pesanan).

import (
	"net"
	"net/http"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/audit"
	"mihanstore/internal/platform"
)

// Deps: dependensi modul admin.
type Deps struct {
	// Cfg: konfigurasi aplikasi (CORS_ALLOWED_ORIGINS untuk CSRF; Access dibangun dari sini).
	Cfg      *platform.Config
	DB       func() *gorm.DB // koneksi saat ini (nil bila belum terhubung)
	Now      func() time.Time
	Audit    *audit.Logger
	ClientIP func(r *http.Request) net.IP
}

// Service: modul admin. Access dan limiter diekspor agar bisa diganti saat uji.
type Service struct {
	Access           *AccessVerifier       // nil = akses admin belum dikonfigurasi (503)
	Limiter          *platform.RateLimiter // per IP: semua rute admin
	DeniedLogLimiter *platform.RateLimiter // catatan admin.access_denied per IP+email+alasan

	cfg      *platform.Config
	db       func() *gorm.DB
	now      func() time.Time
	audit    *audit.Logger
	clientIP func(r *http.Request) net.IP
}

// New membangun modul admin. Access nil bila konfigurasi Cloudflare Access belum lengkap
// (NewAccessVerifier gagal); perakit mencatat peringatannya.
func New(d Deps) *Service {
	s := &Service{
		Limiter:          platform.NewRateLimiter(300, time.Minute),
		DeniedLogLimiter: platform.NewRateLimiter(1, 5*time.Minute),
		cfg:              d.Cfg, db: d.DB, now: d.Now, audit: d.Audit, clientIP: d.ClientIP,
	}
	if av, err := NewAccessVerifier(*d.Cfg); err == nil {
		s.Access = av
	}
	return s
}
