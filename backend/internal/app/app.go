package app

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/admin"
	"mihanstore/internal/audit"
	"mihanstore/internal/cart"
	"mihanstore/internal/catalog"
	"mihanstore/internal/catalog/media"
	"mihanstore/internal/identity"
	"mihanstore/internal/notify"
	"mihanstore/internal/orders"
	"mihanstore/internal/platform"
	"mihanstore/internal/platform/imaging"
	"mihanstore/internal/push"
	"mihanstore/internal/regions"
)

const (
	msgServiceDown = platform.MsgServiceDown
	msgServerError = platform.MsgServerError
)

type App struct {
	cfg        platform.Config
	db         platform.DBHolder
	ips        *platform.IPResolver
	identity   *identity.Service // akun pelanggan, sesi, Google, pelanggan admin (internal/identity)
	admin      *admin.Service    // middleware /api/admin/* (Cloudflare Access), me, summary (internal/admin)
	cart       *cart.Service     // keranjang (internal/cart)
	orders     *orders.Service   // pesanan, bukti transfer, ongkir (internal/orders)
	pushSubs   *push.Service     // rute langganan Web Push admin & pelanggan (internal/push)
	notifier   notify.Notifier
	pusher     push.Sender // Web Push admin (Noop bila VAPID belum dikonfigurasi)
	now        func() time.Time
	audit      *audit.Logger   // activity log (internal/audit)
	auditAdmin *audit.AdminAPI // rute admin activity log

	// Data wilayah (internal/regions).
	regions      *regions.Service
	bgCtx        context.Context // induk goroutine latar (dibatalkan saat server berhenti)
	afterConnect func(db *gorm.DB)

	// Katalog: produk, kategori, jenjang harga, foto produk (internal/catalog).
	catalog *catalog.Service
	// imageSlots: batas pemrosesan gambar paralel (memori), SATU instance untuk foto produk dan bukti transfer.
	imageSlots imaging.Slots
}

func NewApp(cfg platform.Config) *App {
	a := &App{
		cfg: cfg,
		ips: platform.NewIPResolver(cfg.TrustedProxyCIDRs),
		// Server tiruan (http, host bebas) hanya diizinkan untuk database uji.
		notifier: notify.New(cfg.DiscordOrderWebhookURL, cfg.IsTestDB()),
		now:      platform.NowUTC,

		bgCtx: context.Background(),

		imageSlots: imaging.NewSlots(2),
	}
	a.audit = audit.NewLogger(a.db.Load, func(r *http.Request) net.IP { return a.ips.ClientIP(r) }, func() time.Time { return a.now() })
	a.auditAdmin = &audit.AdminAPI{Logger: a.audit, DB: a.db.Load, RespondTxError: a.respondTxError,
		Actor: func(r *http.Request) (*uint64, string) {
			u := admin.From(r.Context())
			return identity.UID(u), identity.ActorOf(u)
		}}
	a.admin = admin.New(admin.Deps{
		Cfg:      &a.cfg,
		DB:       a.db.Load,
		Now:      func() time.Time { return a.now() },
		Audit:    a.audit,
		ClientIP: func(r *http.Request) net.IP { return a.ips.ClientIP(r) },
	})
	a.identity = identity.New(identity.Deps{
		Cfg:            &a.cfg,
		DB:             a.db.Load,
		Now:            func() time.Time { return a.now() },
		Audit:          a.audit,
		ClientIP:       func(r *http.Request) net.IP { return a.ips.ClientIP(r) },
		AdminUser:      func(r *http.Request) *identity.User { return admin.From(r.Context()) },
		StatusLabel:    orders.StatusLabel,
		RespondTxError: a.respondTxError,
	})
	a.catalog = catalog.New(catalog.Deps{
		DB:    a.db.Load,
		Now:   func() time.Time { return a.now() },
		Audit: a.audit,
		Actor: func(r *http.Request) (*uint64, string) {
			u := admin.From(r.Context())
			return identity.UID(u), identity.ActorOf(u)
		},
		RespondTxError: a.respondTxError,
		ImageLimiter:   platform.NewRateLimiter(20, time.Minute), // per admin: unggah/hapus foto
		ImageSlots:     a.imageSlots,
	})
	a.cart = cart.New(cart.Deps{
		DB:             a.db.Load,
		Limiter:        platform.NewRateLimiter(120, time.Minute), // per pengguna: tambah/ubah keranjang
		Tiers:          catalog.LoadTiersFor,
		ImageURLs:      a.catalog.ImageURLs,
		UserID:         func(r *http.Request) uint64 { return identity.CustomerFrom(r.Context()).ID },
		RespondTxError: a.respondTxError,
	})
	a.regions = regions.New(regions.Deps{
		DB:             a.db.Load,
		Now:            func() time.Time { return a.now() },
		Audit:          a.audit,
		ClientIP:       func(r *http.Request) net.IP { return a.ips.ClientIP(r) },
		Limiter:        platform.NewRateLimiter(240, time.Minute), // per IP: API publik wilayah
		Background:     func() context.Context { return a.bgCtx },
		Actor:          a.adminRegionActor,
		RespondTxError: a.respondTxError,
		FetchCfg:       regions.FetchConfigFrom(cfg),
		Cooldown:       regions.CooldownFrom(cfg), // jeda antar fetch (sopan ke wilayah.id)
	})
	var proofs *orders.ProofStore
	if cfg.PaymentProofDir != "" {
		proofs = orders.NewProofStore(cfg.PaymentProofDir)
	}
	a.orders = orders.New(orders.Deps{
		DB:             a.db.Load,
		Now:            func() time.Time { return a.now() },
		Audit:          a.audit,
		Customer:       func(r *http.Request) *orders.Person { return personOf(identity.CustomerFrom(r.Context())) },
		Admin:          func(r *http.Request) *orders.Person { return personOf(admin.From(r.Context())) },
		NormalizePhone: identity.NormalizePhone,
		RespondTxError: a.respondTxError,
		Events:         orderEvents{a: a},
		Cart:           cartReader{c: a.cart},
		MaxCartLines:   cart.MaxLines,
		MaxItemQty:     cart.MaxItemQty,
		Pricing:        catalogPricing{},
		Regions:        a.regions,
		ImageSlots:     a.imageSlots,
		Proofs:         proofs,
	})
	a.pusher = push.New(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject,
		push.NewStore(a.db.Load, func() time.Time { return a.now() }, func() []string { return a.cfg.AdminEmails }))
	a.pushSubs = push.NewService(push.Deps{
		DB:         a.db.Load,
		Now:        func() time.Time { return a.now() },
		Sender:     func() push.Sender { return a.pusher },
		AdminID:    func(r *http.Request) uint64 { return admin.From(r.Context()).ID },
		CustomerID: func(r *http.Request) uint64 { return identity.CustomerFrom(r.Context()).ID },
	})
	a.catalog.SetImageStore(media.NewLocalStore(cfg.UploadsDir, "/uploads/"), cfg.MaxUploadsMB)
	if a.admin.Access == nil {
		log.Println("PERINGATAN: akses admin belum dikonfigurasi (CF_ACCESS_TEAM_DOMAIN / CF_ACCESS_AUD_STORE / ADMIN_EMAILS) — /api/admin/* menjawab 503")
	}
	if a.identity.Google == nil {
		log.Println("PERINGATAN: login Google belum dikonfigurasi (GOOGLE_CLIENT_ID / AUTH_HMAC_SECRET) — /api/auth/google* menjawab 503")
	}
	return a
}

// Ready memeriksa koneksi database tanpa membuka detail internal.
func (a *App) Ready(w http.ResponseWriter, r *http.Request) {
	db := a.db.Load()
	if db != nil {
		if sqlDB, err := db.DB(); err == nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if sqlDB.PingContext(ctx) == nil {
				platform.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
				return
			}
		}
	}
	platform.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
}

// personOf: pelaku untuk modul pesanan (nil bila tidak ada pengguna).
func personOf(u *identity.User) *orders.Person {
	if u == nil {
		return nil
	}
	return &orders.Person{ID: u.ID, Username: u.Username, Label: identity.ActorOf(u)}
}
