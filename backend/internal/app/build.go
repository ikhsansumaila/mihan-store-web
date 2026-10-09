package app

// Composition root: Build merakit semua modul (dipakai cmd/server DAN tes integrasi/kontrak), Handler
// mengembalikan router lengkap, Start menjalankan pemeriksaan awal + goroutine latar + koneksi DB.
// Opsi fungsional menyediakan titik tukar untuk tes (jam, notifier, pusher, verifier, limiter, penyimpanan,
// sumber data wilayah, database).

import (
	"context"
	"log"
	"net/http"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/admin"
	"mihanstore/internal/catalog/media"
	"mihanstore/internal/identity"
	"mihanstore/internal/notify"
	"mihanstore/internal/orders"
	"mihanstore/internal/platform"
	"mihanstore/internal/push"
	"mihanstore/internal/regions"
)

// Option mengubah aplikasi setelah dirakit (sebelum dipakai).
type Option func(*App)

// Build merakit seluruh modul dari konfigurasi. Tidak membuka koneksi DB dan tidak memulai goroutine latar
// (lihat Start); opts diterapkan berurutan.
func Build(cfg platform.Config, opts ...Option) *App {
	a := NewApp(cfg)
	for _, o := range opts {
		o(a)
	}
	return a
}

// Handler: router HTTP lengkap (rute + CORS + header keamanan + recoverer).
func (a *App) Handler() http.Handler { return newRouter(a) }

// Start (produksi): pemeriksaan folder unggahan, goroutine latar yang berhenti saat ctx selesai
// (fetch wilayah, purge bukti transfer harian), dan koneksi DB dengan retry (run wilayah yang tertinggal
// 'running' dari proses sebelumnya ditandai gagal setelah terhubung).
func (a *App) Start(ctx context.Context) {
	a.catalog.ImageGuard().StartupCheck(a.cfg.UploadsDir)
	a.bgCtx = ctx
	a.afterConnect = a.regions.SweepRuns
	if a.orders.Proofs != nil {
		if err := a.orders.Proofs.Writable(); err != nil {
			log.Printf("PERINGATAN: folder bukti transfer tidak dapat ditulis (%v); unggah bukti akan gagal", err)
		} else {
			log.Println("folder bukti transfer (privat) dapat ditulis")
		}
		a.orders.StartProofPurger(ctx)
	}
	log.Printf("sumber data wilayah: %s", a.regions.FetchCfg.Base)
	go a.db.ConnectWithRetry(a.cfg, a.afterConnect)
}

// ---------- Opsi (titik tukar untuk tes) ----------

// WithDB memasang koneksi database langsung (tanpa ConnectWithRetry) dan mengosongkan cache wilayah.
func WithDB(db *gorm.DB) Option {
	return func(a *App) {
		a.db.Store(db)
		a.regions.InvalidateCache()
	}
}

// WithClock mengganti jam aplikasi (semua modul membaca jam lewat App).
func WithClock(now func() time.Time) Option { return func(a *App) { a.now = now } }

// WithNotifier mengganti notifier Discord.
func WithNotifier(n notify.Notifier) Option { return func(a *App) { a.notifier = n } }

// WithPusher mengganti pengirim Web Push (admin & pelanggan).
func WithPusher(p push.Sender) Option { return func(a *App) { a.pusher = p } }

// WithAccessVerifier mengganti verifier Cloudflare Access (mis. JWKS kunci uji).
func WithAccessVerifier(v *admin.AccessVerifier) Option { return func(a *App) { a.admin.Access = v } }

// WithGoogleVerifier mengganti verifier login Google.
func WithGoogleVerifier(v *identity.GoogleVerifier) Option {
	return func(a *App) { a.identity.Google = v }
}

// WithTurnstileEndpoint mengarahkan verifikasi Turnstile ke server tiruan.
func WithTurnstileEndpoint(url string) Option {
	return func(a *App) { a.identity.Turnstile.Endpoint = url }
}

// WithRateLimit mengganti SEMUA pembatas laju dengan batas yang sama (tes kontrak melonggarkan batas).
func WithRateLimit(limit int, window time.Duration) Option {
	return func(a *App) {
		for _, l := range a.limiters() {
			*l = platform.NewRateLimiter(limit, window)
		}
	}
}

// WithImageStore mengganti penyimpanan foto produk (beserta pagar disknya).
func WithImageStore(s media.ImageStore, maxMB int64) Option {
	return func(a *App) { a.catalog.SetImageStore(s, maxMB) }
}

// WithProofStore mengganti penyimpanan privat bukti transfer.
func WithProofStore(s *orders.ProofStore) Option { return func(a *App) { a.orders.Proofs = s } }

// WithRegionFetch mengganti sumber data wilayah dan jeda antar fetch.
func WithRegionFetch(cfg regions.FetchConfig, cooldown time.Duration) Option {
	return func(a *App) {
		a.regions.FetchCfg = cfg
		a.regions.Cooldown = cooldown
	}
}

// limiters: semua pembatas laju per fitur (satu instance per rute; tidak ada salinan).
func (a *App) limiters() []**platform.RateLimiter {
	return []**platform.RateLimiter{&a.identity.LoginLimiter, &a.identity.RegisterLimiter, &a.identity.GoogleLimiter,
		&a.admin.Limiter, &a.cart.Limiter, &a.orders.CheckoutLimiter, &a.orders.CancelLimiter, &a.identity.AliasLimiter,
		&a.pushSubs.AdminLimiter, &a.pushSubs.CustomerLimiter, &a.orders.ProofLimiter, &a.orders.ProofViewLimiter,
		&a.regions.Limiter, &a.catalog.ImageLimiter}
}
