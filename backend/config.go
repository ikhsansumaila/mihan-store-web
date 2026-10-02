package main

import (
	"log"
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
}

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

	return Config{
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
	}
}
