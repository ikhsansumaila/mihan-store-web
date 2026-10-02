package main

// Rute /api/admin/*: identitas dari Cloudflare Access (header Cf-Access-Jwt-Assertion),
// otorisasi dari ADMIN_EMAILS + baris users (role/status dibaca ulang setiap permintaan),
// perlindungan CSRF untuk metode yang mengubah data.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"gorm.io/gorm"
)

const (
	msgAdminNotConfigured = "Akses admin belum dikonfigurasi"
	msgAdminNoSession     = "Sesi admin tidak valid atau sudah berakhir. Muat ulang halaman untuk masuk kembali."
	msgAdminForbidden     = "Anda tidak memiliki akses admin"
	msgAdminCSRF          = "Permintaan ditolak (pemeriksaan asal permintaan gagal)"
	msgTooManyAdmin       = "Terlalu banyak permintaan. Coba lagi beberapa saat lagi."
	adminRequestedWith    = "mihanstore-admin"
)

type ctxKey int

const adminUserKey ctxKey = 1

func adminFrom(ctx context.Context) *User {
	u, _ := ctx.Value(adminUserKey).(*User)
	return u
}

func isMutation(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// checkCSRF untuk permintaan admin yang mengubah data (autentikasi memakai cookie Access,
// jadi browser bisa saja mengirimnya dari situs lain):
//   - Content-Type wajib application/json (form lintas situs tidak bisa memakainya tanpa preflight)
//   - header kustom X-Requested-With: mihanstore-admin
//   - Sec-Fetch-Site (bila ada) wajib same-origin
//   - Origin (bila ada) wajib salah satu origin yang diizinkan; bila Origin dan
//     Sec-Fetch-Site sama-sama tidak ada -> ditolak.
func checkCSRF(r *http.Request, allowedOrigins []string) error {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return errors.New("content-type bukan application/json")
	}
	if r.Header.Get("X-Requested-With") != adminRequestedWith {
		return errors.New("header X-Requested-With tidak ada")
	}
	sfs := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	if sfs != "" && sfs != "same-origin" {
		return errors.New("sec-fetch-site bukan same-origin")
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		if sfs == "" {
			return errors.New("origin tidak ada")
		}
		return nil
	}
	for _, o := range allowedOrigins {
		if strings.EqualFold(strings.TrimSuffix(o, "/"), origin) {
			return nil
		}
	}
	return errors.New("origin tidak diizinkan")
}

// logDenied mencatat admin.access_denied dengan pembatasan frekuensi
// (maks. 1 catatan per 5 menit per IP+email+alasan).
func (a *App) logDenied(r *http.Request, email, reason string, user *User) {
	ip := a.ips.ClientIP(r)
	key := rateKey(ip) + "|" + email + "|" + reason
	if ok, _ := a.deniedLogLimiter.Allow(key); !ok {
		return
	}
	a.logAuth(r, LogEntry{
		UserID: uid(user), ActorLabel: email, Action: "admin.access_denied",
		Summary: "Akses admin ditolak: " + reason,
		Details: map[string]any{"alasan": reason, "path": truncateUTF8(r.URL.Path, 200), "metode": r.Method},
	})
}

// requireAdmin membungkus semua rute /api/admin/*.
func (a *App) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.access == nil {
			writeError(w, http.StatusServiceUnavailable, msgAdminNotConfigured)
			return
		}
		ip := a.ips.ClientIP(r)
		if ok, wait := a.adminLimiter.Allow(rateKey(ip)); !ok {
			retryAfter(w, wait)
			writeError(w, http.StatusTooManyRequests, msgTooManyAdmin)
			return
		}
		token := strings.TrimSpace(r.Header.Get("Cf-Access-Jwt-Assertion"))
		if token == "" {
			a.logDenied(r, "", "tanpa token Access", nil)
			writeError(w, http.StatusUnauthorized, msgAdminNoSession)
			return
		}
		email, err := a.access.Verify(r.Context(), token)
		if errors.Is(err, errEmailNotAllowed) {
			a.logDenied(r, email, "email tidak ada di ADMIN_EMAILS", nil)
			writeError(w, http.StatusForbidden, msgAdminForbidden)
			return
		}
		if err != nil {
			a.logDenied(r, "", "token Access tidak valid: "+err.Error(), nil)
			writeError(w, http.StatusUnauthorized, msgAdminNoSession)
			return
		}
		if isMutation(r.Method) {
			if err := checkCSRF(r, a.cfg.CORSAllowedOrigins); err != nil {
				a.logDenied(r, email, "CSRF: "+err.Error(), nil)
				writeError(w, http.StatusForbidden, msgAdminCSRF)
				return
			}
		}
		db := a.db.Load()
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, msgServiceDown)
			return
		}
		u, err := a.ensureAdminUser(r, db, email)
		if err != nil {
			log.Printf("admin: gagal menyiapkan akun admin: %v", err)
			writeError(w, http.StatusServiceUnavailable, msgServiceDown)
			return
		}
		// Role dan status selalu dibaca ulang dari database pada setiap permintaan.
		if u.Status != "active" || u.Role != "admin" {
			a.logDenied(r, email, "akun "+u.Status+"/"+u.Role, u)
			writeError(w, http.StatusForbidden, msgAdminForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminUserKey, u)))
	})
}

var usernameCleanRe = regexp.MustCompile(`[^a-z0-9_]+`)

// usernameBase membuat kandidat username dari bagian lokal email.
func usernameBase(email string) string {
	local := strings.ToLower(email)
	if at := strings.IndexByte(local, '@'); at >= 0 {
		local = local[:at]
	}
	if plus := strings.IndexByte(local, '+'); plus >= 0 {
		local = local[:plus]
	}
	b := strings.Trim(usernameCleanRe.ReplaceAllString(local, "_"), "_")
	for strings.Contains(b, "__") {
		b = strings.ReplaceAll(b, "__", "_")
	}
	if len(b) > 24 {
		b = strings.TrimRight(b[:24], "_")
	}
	if len(b) < 3 {
		b = "user_" + b
	}
	if _, err := NormalizeUsername(b); err != nil {
		b = "user"
	}
	return b
}

func randomSuffix() string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// uniqueUsername mencari username yang belum dipakai akun aktif.
func uniqueUsername(db *gorm.DB, base string) (string, error) {
	for i := 0; i < 10; i++ {
		cand := base
		if i > 0 || base == "user" {
			cand = base + "_" + randomSuffix()
		}
		var n int64
		if err := db.Model(&User{}).Where("username = ?", cand).Count(&n).Error; err != nil {
			return "", err
		}
		if n == 0 {
			return cand, nil
		}
	}
	return "", errors.New("gagal membuat username unik")
}

// ensureAdminUser mencari baris users untuk email admin, membuatnya bila belum ada
// (role admin, aktif, email terverifikasi, tanpa password), dan menaikkan role ke admin
// bila perlu (email ada di ADMIN_EMAILS; dicatat user.role_change). Status suspended
// TIDAK diubah: akun yang ditangguhkan tetap ditolak.
func (a *App) ensureAdminUser(r *http.Request, db *gorm.DB, email string) (*User, error) {
	ctx := r.Context()
	for attempt := 0; attempt < 3; attempt++ {
		var u User
		err := db.WithContext(ctx).Where("email = ?", email).Take(&u).Error
		if err == nil {
			if u.Role == "admin" || u.Status != "active" {
				return &u, nil
			}
			err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				res := tx.Model(&User{}).Where("id = ? AND role <> 'admin'", u.ID).Update("role", "admin")
				if res.Error != nil || res.RowsAffected == 0 {
					return res.Error
				}
				return a.logActivity(tx, a.reqMeta(r, LogEntry{
					UserID: uid(&u), ActorLabel: email, Action: "user.role_change",
					EntityType: "user", EntityID: u.PublicID,
					Summary: "Role " + u.Username + " diubah menjadi admin (ADMIN_EMAILS, Cloudflare Access)",
					Details: map[string]any{"role": map[string]any{"dari": u.Role, "menjadi": "admin"}, "sumber": "cloudflare_access"},
				}))
			})
			if err != nil {
				return nil, err
			}
			u.Role = "admin"
			return &u, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		username, err := uniqueUsername(db.WithContext(ctx), usernameBase(email))
		if err != nil {
			return nil, err
		}
		pid, err := newPublicID()
		if err != nil {
			return nil, err
		}
		now := a.now()
		name, nerr := NormalizeName(strings.SplitN(email, "@", 2)[0])
		if nerr != nil {
			name = username
		}
		nu := User{PublicID: pid, Username: username, Email: email, Name: name,
			Role: "admin", Status: "active", EmailVerifiedAt: &now}
		err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&nu).Error; err != nil {
				return err
			}
			return a.logActivity(tx, a.reqMeta(r, LogEntry{
				UserID: uid(&nu), ActorLabel: email, Action: "user.role_change",
				EntityType: "user", EntityID: nu.PublicID,
				Summary: "Akun admin " + username + " dibuat (ADMIN_EMAILS, Cloudflare Access)",
				Details: map[string]any{"role": map[string]any{"dari": nil, "menjadi": "admin"}, "sumber": "cloudflare_access", "akunBaru": true},
			}))
		})
		if err == nil {
			return &nu, nil
		}
		if !isDuplicateKey(err) {
			return nil, err
		}
		// Bentrok (permintaan paralel/username): coba lagi.
	}
	return nil, errors.New("gagal membuat akun admin")
}

func (a *App) AdminMe(w http.ResponseWriter, r *http.Request) {
	u := adminFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]any{
		"id": u.PublicID, "username": u.Username, "email": u.Email, "name": u.Name, "role": u.Role,
	}})
}

func (a *App) AdminSummary(w http.ResponseWriter, r *http.Request) {
	db := a.db.Load().WithContext(r.Context())
	var counts struct {
		Total    int64 `gorm:"column:total" json:"total"`
		Active   int64 `gorm:"column:active" json:"active"`
		Inactive int64 `gorm:"column:inactive" json:"inactive"`
	}
	if err := db.Raw(`SELECT COUNT(*) AS total, COALESCE(SUM(is_active = 1), 0) AS active,
		COALESCE(SUM(is_active = 0), 0) AS inactive FROM products WHERE deleted_at IS NULL`).Scan(&counts).Error; err != nil {
		log.Printf("admin summary: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	var cats int64
	if err := db.Model(&Category{}).Count(&cats).Error; err != nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	logs, _, err := a.queryLogs(db, logFilter{Page: 1, PerPage: 10})
	if err != nil {
		log.Printf("admin summary logs: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"products":       counts,
		"categories":     cats,
		"recentActivity": logs,
	})
}
