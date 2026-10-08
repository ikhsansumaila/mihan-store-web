package main

import (
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config berisi seluruh konfigurasi aplikasi yang dibaca dari environment.
type Config struct {
	Port string

	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string

	SessionTTL time.Duration

	CORSAllowedOrigins []string

	TurnstileSecret  string
	AllowNoTurnstile bool // HANYA untuk pengembangan lokal. Jangan diset di produksi.

	// TrustedProxyCIDRs: alamat peer yang header IP-nya (X-Real-IP, X-Forwarded-For)
	// dipercaya. Backend hanya bisa dijangkau lewat Nginx Proxy Manager di jaringan
	// Docker, jadi defaultnya rentang privat.
	TrustedProxyCIDRs []string

	// Admin lewat Cloudflare Access (fail-closed bila salah satu kosong).
	AdminEmails        []string // huruf kecil
	CFAccessTeamDomain string   // mis. namateam.cloudflareaccess.com
	CFAccessAUD        string   // Application Audience (AUD) tag aplikasi "Mihan Store Admin"

	// Login Google pelanggan (503 bila GOOGLE_CLIENT_ID kosong).
	GoogleClientID string
	AuthHMACSecret string // menandatangani profileToken (min. 32 karakter)

	// Override URL JWKS KHUSUS UJI. Hanya dipakai bila DB_NAME berakhiran "_test";
	// di produksi diabaikan (lihat LoadConfig). Jangan diset di produksi.
	TestCFAccessJWKSURL string
	TestGoogleJWKSURL   string

	// Notifikasi pesanan ke Discord (rahasia; kosong = nonaktif dengan peringatan di log).
	DiscordOrderWebhookURL string
	// URL publik toko untuk tautan di notifikasi (default https://store.mihan.web.id).
	PublicBaseURL string

	// Web Push admin (VAPID). Kunci privat RAHASIA; kosong = fitur nonaktif (Noop), backend tetap jalan.
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string // mailto:... atau https://... (default PUBLIC_BASE_URL bila https)

	// Sumber data wilayah (default https://wilayah.id/api). Di produksi hanya https://wilayah.id/...;
	// URL lain (server tiruan) hanya berlaku bila DB_NAME berakhiran _test.
	RegionAPIBase string
	// KHUSUS UJI (DB *_test): batas minimal jumlah provinsi (server tiruan kecil) dan jeda antar fetch.
	TestRegionMinProvinces int
	TestRegionCooldown     *time.Duration

	// Foto produk: lapisan penyimpanan (saat ini hanya "local" = disk VPS), direktori unggahan,
	// dan batas total ukuran direktori unggahan (MB) sebelum unggahan baru ditolak.
	ImageStore   string
	UploadsDir   string
	MaxUploadsMB int64

	// Bukti transfer pelanggan: folder PRIVAT (di luar UploadsDir/uploads publik, tidak disajikan nginx).
	PaymentProofDir string
}

// IsTestDB: true bila memakai database uji (*_test).
func (c Config) IsTestDB() bool { return strings.HasSuffix(c.DBName, "_test") }

var defaultCORSOrigins = []string{"https://store.mihan.web.id", "https://mihankids.my.id"}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func LoadConfig() Config {
	ttlHours := 168
	if v := os.Getenv("SESSION_TTL_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 24*90 {
			ttlHours = n
		} else {
			log.Printf("PERINGATAN: SESSION_TTL_HOURS tidak valid, memakai default %d jam", ttlHours)
		}
	}

	origins := splitList(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if len(origins) == 0 {
		origins = defaultCORSOrigins
	}

	proxies := splitList(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if len(proxies) == 0 {
		proxies = []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "::1/128", "fc00::/7"}
	}

	var admins []string
	for _, e := range splitList(os.Getenv("ADMIN_EMAILS")) {
		admins = append(admins, strings.ToLower(e))
	}

	cfg := Config{
		Port:               getenv("PORT", "8080"),
		DBHost:             getenv("DB_HOST", "mysql_db"),
		DBPort:             getenv("DB_PORT", "3306"),
		DBName:             getenv("DB_NAME", "mihanstore"),
		DBUser:             getenv("DB_USER", "mihanstore_app"),
		DBPassword:         os.Getenv("DB_PASSWORD"),
		SessionTTL:         time.Duration(ttlHours) * time.Hour,
		CORSAllowedOrigins: origins,
		TurnstileSecret:    strings.TrimSpace(os.Getenv("TURNSTILE_SECRET_KEY")),
		AllowNoTurnstile:   os.Getenv("ALLOW_NO_TURNSTILE") == "true",
		TrustedProxyCIDRs:  proxies,
		AdminEmails:        admins,
		CFAccessTeamDomain: normalizeTeamDomain(os.Getenv("CF_ACCESS_TEAM_DOMAIN")),
		CFAccessAUD:        strings.TrimSpace(os.Getenv("CF_ACCESS_AUD_STORE")),
		GoogleClientID:     strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")),
		AuthHMACSecret:     strings.TrimSpace(os.Getenv("AUTH_HMAC_SECRET")),

		DiscordOrderWebhookURL: strings.TrimSpace(os.Getenv("DISCORD_ORDER_WEBHOOK_URL")),
		PublicBaseURL:          normalizeBaseURL(os.Getenv("PUBLIC_BASE_URL")),

		VAPIDPublicKey:  strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY")),
		VAPIDPrivateKey: strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY")),
		VAPIDSubject:    strings.TrimSpace(os.Getenv("VAPID_SUBJECT")),
	}
	if cfg.VAPIDSubject == "" {
		if strings.HasPrefix(cfg.PublicBaseURL, "https://") {
			cfg.VAPIDSubject = cfg.PublicBaseURL
		} else {
			cfg.VAPIDSubject = defaultPublicBaseURL
		}
	}
	tAccess := strings.TrimSpace(os.Getenv("TEST_ONLY_CF_ACCESS_JWKS_URL"))
	tGoogle := strings.TrimSpace(os.Getenv("TEST_ONLY_GOOGLE_JWKS_URL"))
	if tAccess != "" || tGoogle != "" {
		if cfg.IsTestDB() {
			log.Println("PERINGATAN: override JWKS uji aktif (TEST_ONLY_*_JWKS_URL) — hanya untuk container uji")
			cfg.TestCFAccessJWKSURL, cfg.TestGoogleJWKSURL = tAccess, tGoogle
		} else {
			log.Println("PERINGATAN: TEST_ONLY_*_JWKS_URL diabaikan karena DB_NAME bukan database uji (*_test)")
		}
	}
	cfg.ImageStore = strings.ToLower(getenv("IMAGE_STORE", "local"))
	if cfg.ImageStore != "local" {
		log.Printf("PERINGATAN: IMAGE_STORE=%q belum didukung, memakai \"local\"", cfg.ImageStore)
		cfg.ImageStore = "local"
	}
	cfg.UploadsDir = getenv("UPLOADS_DIR", "/data/uploads")
	cfg.PaymentProofDir = getenv("PAYMENT_PROOF_DIR", "/data/private-uploads/payment-proofs")
	cfg.MaxUploadsMB = 2048
	if v := strings.TrimSpace(os.Getenv("MAX_UPLOADS_MB")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 && n <= 1<<20 {
			cfg.MaxUploadsMB = n
		} else {
			log.Println("PERINGATAN: MAX_UPLOADS_MB tidak valid, memakai default 2048")
		}
	}
	cfg.RegionAPIBase = regionAPIBaseFromEnv(os.Getenv("REGION_API_BASE"), cfg.IsTestDB())
	if v := strings.TrimSpace(os.Getenv("TEST_ONLY_REGION_MIN_PROVINCES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && cfg.IsTestDB() {
			cfg.TestRegionMinProvinces = n
		} else {
			log.Println("PERINGATAN: TEST_ONLY_REGION_MIN_PROVINCES diabaikan (hanya untuk database uji *_test)")
		}
	}
	if v := strings.TrimSpace(os.Getenv("TEST_ONLY_REGION_COOLDOWN_SECONDS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && cfg.IsTestDB() {
			d := time.Duration(n) * time.Second
			cfg.TestRegionCooldown = &d
		} else {
			log.Println("PERINGATAN: TEST_ONLY_REGION_COOLDOWN_SECONDS diabaikan (hanya untuk database uji *_test)")
		}
	}
	return cfg
}

// regionAPIBaseFromEnv: https://wilayah.id/... selalu diterima; URL lain (http, host lain) hanya untuk DB uji.
func regionAPIBaseFromEnv(v string, testDB bool) string {
	v = strings.TrimRight(strings.TrimSpace(v), "/")
	if v == "" {
		return defaultRegionAPIBase
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		log.Println("PERINGATAN: REGION_API_BASE tidak valid, memakai default " + defaultRegionAPIBase)
		return defaultRegionAPIBase
	}
	if u.Scheme == "https" && strings.EqualFold(u.Hostname(), "wilayah.id") && u.Port() == "" {
		return v
	}
	if testDB {
		log.Println("PERINGATAN: REGION_API_BASE uji aktif (server tiruan) — hanya untuk container uji")
		return v
	}
	log.Println("PERINGATAN: REGION_API_BASE diabaikan karena DB_NAME bukan database uji (*_test); memakai " + defaultRegionAPIBase)
	return defaultRegionAPIBase
}

// normalizeTeamDomain menerima "nama.cloudflareaccess.com" atau "https://nama.cloudflareaccess.com/".
func normalizeTeamDomain(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "https://")
	return strings.TrimSuffix(s, "/")
}

const defaultPublicBaseURL = "https://store.mihan.web.id"

// normalizeBaseURL menerima http(s)://host[:port][/path] tanpa query; selain itu default.
func normalizeBaseURL(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	if s == "" {
		return defaultPublicBaseURL
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		log.Println("PERINGATAN: PUBLIC_BASE_URL tidak valid, memakai default " + defaultPublicBaseURL)
		return defaultPublicBaseURL
	}
	return s
}
