package regions

import (
	"context"
	"net"
	"net/http"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/audit"
	"mihanstore/internal/platform"
)

// ActivityLogger menulis activity log di dalam transaksi pemanggil (diimplementasikan *audit.Logger).
type ActivityLogger interface {
	Log(tx *gorm.DB, e audit.Entry) error
}

// Deps: dependensi modul wilayah, diberikan eksplisit oleh perakit.
type Deps struct {
	DB         func() *gorm.DB // koneksi saat ini (nil bila belum terhubung)
	Now        func() time.Time
	Audit      ActivityLogger
	ClientIP   func(*http.Request) net.IP // untuk batas laju API publik
	Limiter    *platform.RateLimiter      // per IP: API publik wilayah
	Background func() context.Context     // induk goroutine latar (dibatalkan saat server berhenti)
	// Actor: pelaku admin dari permintaan HTTP (rute admin).
	Actor func(*http.Request) Actor
	// RespondTxError: menerjemahkan galat transaksi (*platform.HTTPError / lainnya) ke respons.
	RespondTxError func(w http.ResponseWriter, err error, ctx string)
	FetchCfg       FetchConfig
	Cooldown       time.Duration // jeda minimal antar fetch (sopan ke wilayah.id)
}

// Service: modul data wilayah (cache, API publik, impor admin/CLI). Field yang diekspor boleh
// diganti saat uji (batas laju, konfigurasi fetch, jeda).
type Service struct {
	Limiter  *platform.RateLimiter
	FetchCfg FetchConfig
	Cooldown time.Duration

	db             func() *gorm.DB
	now            func() time.Time
	audit          ActivityLogger
	clientIP       func(*http.Request) net.IP
	bg             func() context.Context
	actor          func(*http.Request) Actor
	respondTxError func(w http.ResponseWriter, err error, ctx string)
	cache          *regionCache
	runs           *regionRunRegistry
}

func New(d Deps) *Service {
	return &Service{
		Limiter: d.Limiter, FetchCfg: d.FetchCfg, Cooldown: d.Cooldown,
		db: d.DB, now: d.Now, audit: d.Audit, clientIP: d.ClientIP, bg: d.Background,
		actor: d.Actor, respondTxError: d.RespondTxError,
		cache: newRegionCache(), runs: newRegionRunRegistry(),
	}
}

// InvalidateCache membuang cache dataset di memori (dipanggil tes setelah menyemai data).
func (s *Service) InvalidateCache() { s.cache.invalidate() }

// FetchConfigFrom: konfigurasi fetch dari Config (override batas provinsi hanya untuk DB uji,
// sudah dijaga LoadConfig).
func FetchConfigFrom(cfg platform.Config) FetchConfig {
	c := DefaultFetchConfig(cfg.RegionAPIBase)
	if cfg.TestRegionMinProvinces > 0 {
		c.MinProvinces = cfg.TestRegionMinProvinces
	}
	return c
}

// CooldownFrom: jeda antar fetch (default 5 menit; override hanya untuk DB uji).
func CooldownFrom(cfg platform.Config) time.Duration {
	if cfg.TestRegionCooldown != nil {
		return *cfg.TestRegionCooldown
	}
	return 5 * time.Minute
}
