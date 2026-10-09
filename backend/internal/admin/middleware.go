package admin

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

	"mihanstore/internal/audit"
	"mihanstore/internal/identity"
	"mihanstore/internal/platform"
)

const (
	msgAdminNotConfigured = "Akses admin belum dikonfigurasi"
	msgAdminNoSession     = "Sesi admin tidak valid atau sudah berakhir. Muat ulang halaman untuk masuk kembali."
	msgAdminForbidden     = "Anda tidak memiliki akses admin"
	msgAdminCSRF          = "Permintaan ditolak (pemeriksaan asal permintaan gagal)"
	adminRequestedWith    = "mihanstore-admin"

	// RequestedWith: nilai header X-Requested-With wajib untuk permintaan admin pengubah data.
	RequestedWith = adminRequestedWith
)

type ctxKey int

const adminUserKey ctxKey = 1

func From(ctx context.Context) *identity.User {
	u, _ := ctx.Value(adminUserKey).(*identity.User)
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
	return checkCSRFOpts(r, allowedOrigins, false)
}

// imageUploadPathRe: satu-satunya rute admin yang menerima multipart/form-data (unggah foto produk).
var imageUploadPathRe = regexp.MustCompile(`^/api/admin/products/[0-9]+/image$`)

// checkCSRFOpts seperti checkCSRF; allowMultipart juga menerima multipart/form-data (unggah berkas).
// Pemeriksaan header kustom X-Requested-With dan Origin/Sec-Fetch-Site tetap berlaku sama persis:
// formulir lintas situs tidak bisa menambah header kustom tanpa preflight CORS (yang tidak diizinkan).
func checkCSRFOpts(r *http.Request, allowedOrigins []string, allowMultipart bool) error {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (mt != "application/json" && !(allowMultipart && mt == "multipart/form-data")) {
		if allowMultipart {
			return errors.New("content-type bukan multipart/form-data atau application/json")
		}
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
func (a *Service) logDenied(r *http.Request, email, reason string, user *identity.User) {
	ip := a.clientIP(r)
	key := platform.RateKey(ip) + "|" + email + "|" + reason
	if ok, _ := a.DeniedLogLimiter.Allow(key); !ok {
		return
	}
	a.audit.Auth(r, audit.Entry{
		UserID: identity.UID(user), ActorLabel: email, Action: "admin.access_denied",
		Summary: "Akses admin ditolak: " + reason,
		Details: map[string]any{"alasan": reason, "path": platform.TruncateUTF8(r.URL.Path, 200), "metode": r.Method},
	})
}

// RequireAdmin membungkus semua rute /api/admin/*.
func (a *Service) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Access == nil {
			platform.WriteError(w, http.StatusServiceUnavailable, msgAdminNotConfigured)
			return
		}
		ip := a.clientIP(r)
		if ok, wait := a.Limiter.Allow(platform.RateKey(ip)); !ok {
			platform.RetryAfter(w, wait)
			platform.WriteError(w, http.StatusTooManyRequests, platform.MsgTooManyAdmin)
			return
		}
		token := strings.TrimSpace(r.Header.Get("Cf-Access-Jwt-Assertion"))
		if token == "" {
			a.logDenied(r, "", "tanpa token Access", nil)
			platform.WriteError(w, http.StatusUnauthorized, msgAdminNoSession)
			return
		}
		email, err := a.Access.Verify(r.Context(), token)
		if errors.Is(err, errEmailNotAllowed) {
			a.logDenied(r, email, "email tidak ada di ADMIN_EMAILS", nil)
			platform.WriteError(w, http.StatusForbidden, msgAdminForbidden)
			return
		}
		if err != nil {
			a.logDenied(r, "", "token Access tidak valid: "+err.Error(), nil)
			platform.WriteError(w, http.StatusUnauthorized, msgAdminNoSession)
			return
		}
		if isMutation(r.Method) {
			multipartOK := r.Method == http.MethodPost && imageUploadPathRe.MatchString(r.URL.Path)
			if err := checkCSRFOpts(r, a.cfg.CORSAllowedOrigins, multipartOK); err != nil {
				a.logDenied(r, email, "CSRF: "+err.Error(), nil)
				platform.WriteError(w, http.StatusForbidden, msgAdminCSRF)
				return
			}
		}
		db := a.db()
		if db == nil {
			platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
			return
		}
		u, err := a.ensureAdminUser(r, db, email)
		if err != nil {
			log.Printf("admin: gagal menyiapkan akun admin: %v", err)
			platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
			return
		}
		// Role dan status selalu dibaca ulang dari database pada setiap permintaan.
		if u.Status != "active" || u.Role != "admin" {
			a.logDenied(r, email, "akun "+u.Status+"/"+u.Role, u)
			platform.WriteError(w, http.StatusForbidden, msgAdminForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminUserKey, u)))
	})
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
		if err := db.Model(&identity.User{}).Where("username = ?", cand).Count(&n).Error; err != nil {
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
func (a *Service) ensureAdminUser(r *http.Request, db *gorm.DB, email string) (*identity.User, error) {
	ctx := r.Context()
	for attempt := 0; attempt < 3; attempt++ {
		var u identity.User
		err := db.WithContext(ctx).Where("email = ?", email).Take(&u).Error
		if err == nil {
			if u.Role == "admin" || u.Status != "active" {
				return &u, nil
			}
			err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				res := tx.Model(&identity.User{}).Where("id = ? AND role <> 'admin'", u.ID).Update("role", "admin")
				if res.Error != nil || res.RowsAffected == 0 {
					return res.Error
				}
				return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
					UserID: identity.UID(&u), ActorLabel: email, Action: "user.role_change",
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
		username, err := uniqueUsername(db.WithContext(ctx), identity.UsernameBase(email))
		if err != nil {
			return nil, err
		}
		pid, err := platform.NewPublicID()
		if err != nil {
			return nil, err
		}
		now := a.now()
		name, nerr := identity.NormalizeName(strings.SplitN(email, "@", 2)[0])
		if nerr != nil {
			name = username
		}
		nu := identity.User{PublicID: pid, Username: username, Email: email, Name: name,
			Role: "admin", Status: "active", EmailVerifiedAt: &now}
		err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&nu).Error; err != nil {
				return err
			}
			return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
				UserID: identity.UID(&nu), ActorLabel: email, Action: "user.role_change",
				EntityType: "user", EntityID: nu.PublicID,
				Summary: "Akun admin " + username + " dibuat (ADMIN_EMAILS, Cloudflare Access)",
				Details: map[string]any{"role": map[string]any{"dari": nil, "menjadi": "admin"}, "sumber": "cloudflare_access", "akunBaru": true},
			}))
		})
		if err == nil {
			return &nu, nil
		}
		if !platform.IsDuplicateKey(err) {
			return nil, err
		}
		// Bentrok (permintaan paralel/username): coba lagi.
	}
	return nil, errors.New("gagal membuat akun admin")
}
