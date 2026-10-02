package main

// Login Google untuk pelanggan (Google Identity Services, ID token RS256).
//
// Alur:
//   POST /api/auth/google {credential}
//     - google_sub sudah ada            -> sesi (7 hari)
//     - email cocok akun lama (password) -> akun disambungkan ke Google, password lama
//                                          DIHAPUS, semua sesi lama DICABUT (auth.google_link)
//     - belum ada sama sekali           -> {needsProfile:true, profileToken} (HMAC, 10 menit)
//   POST /api/auth/google/complete {profileToken, username, phone?} -> buat akun + sesi
// Email yang ada di ADMIN_EMAILS otomatis mendapat role admin (user.role_change).

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	googleJWKSURL          = "https://www.googleapis.com/oauth2/v3/certs"
	profileTokenTTL        = 10 * time.Minute
	msgGoogleNotConfigured = "Login Google belum dikonfigurasi"
	msgGoogleInvalid       = "Login Google gagal diverifikasi. Silakan coba lagi."
	msgGoogleProfileExpire = "Waktu pengisian profil habis. Silakan masuk dengan Google lagi."
	msgEmailIsGoogle       = "Email sudah terdaftar, silakan masuk dengan Google"
	msgTooManyGoogle       = "Terlalu banyak percobaan. Coba lagi dalam beberapa saat."
)

var googleIssuers = map[string]bool{"https://accounts.google.com": true, "accounts.google.com": true}

type GoogleVerifier struct {
	clientID string
	jwks     *JWKSCache
	now      func() time.Time
}

// NewGoogleVerifier mengembalikan nil bila GOOGLE_CLIENT_ID atau AUTH_HMAC_SECRET belum diisi.
func NewGoogleVerifier(cfg Config) *GoogleVerifier {
	if cfg.GoogleClientID == "" || len(cfg.AuthHMACSecret) < 32 {
		return nil
	}
	u := googleJWKSURL
	if cfg.TestGoogleJWKSURL != "" && cfg.IsTestDB() {
		u = cfg.TestGoogleJWKSURL
	}
	return &GoogleVerifier{clientID: cfg.GoogleClientID, jwks: NewJWKSCache(HTTPJWKSFetcher(u)), now: time.Now}
}

// GoogleIdentity adalah data terverifikasi dari ID token.
type GoogleIdentity struct {
	Sub     string
	Email   string
	Name    string
	Picture string
}

type googleClaims struct {
	Aud           json.RawMessage `json:"aud"`
	Iss           string          `json:"iss"`
	Exp           numericDate     `json:"exp"`
	Nbf           numericDate     `json:"nbf"`
	Iat           numericDate     `json:"iat"`
	Sub           string          `json:"sub"`
	Email         string          `json:"email"`
	EmailVerified json.RawMessage `json:"email_verified"`
	Name          string          `json:"name"`
	Picture       string          `json:"picture"`
}

// boolClaim menerima true atau "true" (Google pernah mengirim keduanya).
func boolClaim(raw json.RawMessage) bool {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var s string
	return json.Unmarshal(raw, &s) == nil && s == "true"
}

func (v *GoogleVerifier) Verify(ctx context.Context, credential string) (*GoogleIdentity, error) {
	pb, err := verifyRS256(ctx, v.jwks, credential)
	if err != nil {
		return nil, err
	}
	var c googleClaims
	if err := json.Unmarshal(pb, &c); err != nil {
		return nil, errors.New("payload rusak")
	}
	if !googleIssuers[c.Iss] {
		return nil, errors.New("iss tidak cocok")
	}
	if !audContains(c.Aud, v.clientID) {
		return nil, errors.New("aud tidak cocok")
	}
	if err := checkTimes(v.now(), c.Exp, c.Nbf, c.Iat); err != nil {
		return nil, err
	}
	if c.Sub == "" || len(c.Sub) > 64 {
		return nil, errors.New("sub tidak valid")
	}
	if !boolClaim(c.EmailVerified) {
		return nil, errors.New("email belum terverifikasi")
	}
	email, err := NormalizeEmail(c.Email)
	if err != nil {
		return nil, errors.New("email tidak valid")
	}
	return &GoogleIdentity{Sub: c.Sub, Email: email, Name: c.Name, Picture: safeAvatarURL(c.Picture)}, nil
}

// safeAvatarURL hanya menerima URL https maksimal 500 karakter.
func safeAvatarURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 500 {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return s
}

// ---------- profileToken (HMAC-SHA256) ----------

type profileClaims struct {
	Purpose string `json:"p"`
	Sub     string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Avatar  string `json:"avatar"`
	Exp     int64  `json:"exp"`
}

const profilePurpose = "google_profile"

func signProfileToken(secret string, c profileClaims) (string, error) {
	c.Purpose = profilePurpose
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("mihanstore.profile.v1." + payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

var errProfileToken = errors.New("profileToken tidak valid")

func verifyProfileToken(secret, token string, now time.Time) (*profileClaims, error) {
	if len(secret) < 32 || len(token) > 4096 {
		return nil, errProfileToken
	}
	payload, sig, ok := strings.Cut(token, ".")
	if !ok {
		return nil, errProfileToken
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return nil, errProfileToken
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("mihanstore.profile.v1." + payload))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return nil, errProfileToken
	}
	b, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, errProfileToken
	}
	var c profileClaims
	if err := json.Unmarshal(b, &c); err != nil || c.Purpose != profilePurpose || c.Sub == "" || c.Email == "" {
		return nil, errProfileToken
	}
	if now.Unix() > c.Exp {
		return nil, errors.New("profileToken kedaluwarsa")
	}
	return &c, nil
}

// ---------- Handler ----------

type GoogleLoginRequest struct {
	Credential string `json:"credential"`
}

type GoogleCompleteRequest struct {
	ProfileToken string `json:"profileToken"`
	Username     string `json:"username"`
	Phone        string `json:"phone"`
}

// googlePrecheck: konfigurasi, batas laju, DB. Mengembalikan db atau nil (respons sudah ditulis).
func (a *App) googlePrecheck(w http.ResponseWriter, r *http.Request) *gorm.DB {
	if a.google == nil {
		writeError(w, http.StatusServiceUnavailable, msgGoogleNotConfigured)
		return nil
	}
	if ok, wait := a.googleLimiter.Allow(rateKey(a.ips.ClientIP(r))); !ok {
		retryAfter(w, wait)
		writeError(w, http.StatusTooManyRequests, msgTooManyGoogle)
		return nil
	}
	db := a.db.Load()
	if db == nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return nil
	}
	return db
}

// issueSession membuat sesi dan menulis respons sukses.
func (a *App) issueSession(w http.ResponseWriter, r *http.Request, db *gorm.DB, u *User, status int, msg string) {
	token, exp, err := a.createSession(r.Context(), db, u.ID, r, a.ips.ClientIP(r))
	if err != nil {
		log.Printf("google: gagal membuat sesi: %v", err)
		writeError(w, http.StatusInternalServerError, msgServerError)
		return
	}
	writeJSON(w, status, AuthResponse{Success: true, Message: msg, User: toDTO(u), Token: token, ExpiresAt: &exp})
}

// applyAdminRole menyetel role admin bila email ada di ADMIN_EMAILS (dalam tx) dan mencatatnya.
func (a *App) applyAdminRole(tx *gorm.DB, r *http.Request, u *User, source string) error {
	if u.Role == "admin" || !a.cfg.isAdminEmail(u.Email) {
		return nil
	}
	if err := tx.Model(&User{}).Where("id = ?", u.ID).Update("role", "admin").Error; err != nil {
		return err
	}
	old := u.Role
	u.Role = "admin"
	return a.logActivity(tx, a.reqMeta(r, LogEntry{
		UserID: uid(u), ActorLabel: u.Email, Action: "user.role_change",
		EntityType: "user", EntityID: u.PublicID,
		Summary: "Role " + u.Username + " diubah menjadi admin (ADMIN_EMAILS)",
		Details: map[string]any{"role": map[string]any{"dari": old, "menjadi": "admin"}, "sumber": source},
	}))
}

func (a *App) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	db := a.googlePrecheck(w, r)
	if db == nil {
		return
	}
	var req GoogleLoginRequest
	if err := decodeJSON(w, r, &req, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	id, err := a.google.Verify(r.Context(), strings.TrimSpace(req.Credential))
	if err != nil {
		log.Printf("google: token ditolak: %v", err)
		a.logAuth(r, LogEntry{Action: "auth.login_failed", Summary: "Login Google gagal diverifikasi",
			Details: map[string]any{"metode": "google", "alasan": err.Error()}})
		writeError(w, http.StatusUnauthorized, msgGoogleInvalid)
		return
	}
	ctx := r.Context()

	// 1) Sudah tersambung (google_sub).
	var u User
	err = db.WithContext(ctx).Where("google_sub = ?", id.Sub).Take(&u).Error
	if err == nil {
		a.googleSignIn(w, r, db, &u, id)
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("google: gagal membaca user: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}

	// 2) Email cocok akun lama -> sambungkan (aturan B1).
	err = db.WithContext(ctx).Where("email = ?", id.Email).Take(&u).Error
	if err == nil {
		a.googleLink(w, r, db, &u, id)
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("google: gagal membaca user: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}

	// 3) Akun baru -> lengkapi profil.
	name, nerr := NormalizeName(id.Name)
	if nerr != nil {
		name = ""
	}
	pt, err := signProfileToken(a.cfg.AuthHMACSecret, profileClaims{
		Sub: id.Sub, Email: id.Email, Name: name, Avatar: id.Picture,
		Exp: a.now().Add(profileTokenTTL).Unix(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, msgServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": false, "needsProfile": true, "profileToken": pt,
		"profile":           map[string]any{"email": id.Email, "name": name, "avatarUrl": id.Picture},
		"suggestedUsername": usernameBase(id.Email),
	})
}

// googleSignIn: akun sudah tersambung ke Google.
func (a *App) googleSignIn(w http.ResponseWriter, r *http.Request, db *gorm.DB, u *User, id *GoogleIdentity) {
	if u.Status != "active" {
		a.logAuth(r, LogEntry{UserID: uid(u), ActorLabel: u.Email, Action: "auth.login_failed",
			Summary: "Login Google ditolak: akun dinonaktifkan", Details: map[string]any{"metode": "google", "alasan": "suspended"}})
		writeError(w, http.StatusForbidden, msgAccountSuspended)
		return
	}
	now := a.now()
	err := db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		upd := map[string]any{"last_login_at": now, "failed_logins": 0, "locked_until": nil}
		if id.Picture != "" {
			upd["avatar_url"] = id.Picture
		}
		if err := tx.Model(&User{}).Where("id = ?", u.ID).Updates(upd).Error; err != nil {
			return err
		}
		return a.applyAdminRole(tx, r, u, "google_login")
	})
	if err != nil {
		log.Printf("google: gagal memperbarui user: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	u.LastLoginAt = &now
	a.logAuth(r, LogEntry{UserID: uid(u), ActorLabel: u.Email, Action: "auth.google_login",
		EntityType: "user", EntityID: u.PublicID, Summary: "Login dengan Google: " + u.Username})
	a.issueSession(w, r, db, u, http.StatusOK, "Login berhasil")
}

// googleLink menerapkan aturan B1: sambungkan akun password lama ke Google, hapus password
// lama, cabut semua sesi lama.
func (a *App) googleLink(w http.ResponseWriter, r *http.Request, db *gorm.DB, u *User, id *GoogleIdentity) {
	if u.GoogleSub != nil && *u.GoogleSub != "" && *u.GoogleSub != id.Sub {
		a.logAuth(r, LogEntry{UserID: uid(u), ActorLabel: u.Email, Action: "auth.login_failed",
			Summary: "Login Google ditolak: email sudah tersambung ke akun Google lain",
			Details: map[string]any{"metode": "google", "alasan": "google_sub_berbeda"}})
		writeError(w, http.StatusConflict, "Email ini sudah tersambung dengan akun Google lain")
		return
	}
	if u.Status != "active" {
		a.logAuth(r, LogEntry{UserID: uid(u), ActorLabel: u.Email, Action: "auth.login_failed",
			Summary: "Login Google ditolak: akun dinonaktifkan", Details: map[string]any{"metode": "google", "alasan": "suspended"}})
		writeError(w, http.StatusForbidden, msgAccountSuspended)
		return
	}
	now := a.now()
	var revoked int64
	err := db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		// Kunci baris agar dua penyambungan paralel tidak saling menimpa.
		var cur User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", u.ID).Take(&cur).Error; err != nil {
			return err
		}
		if cur.GoogleSub != nil && *cur.GoogleSub != id.Sub {
			return &httpError{http.StatusConflict, "Email ini sudah tersambung dengan akun Google lain"}
		}
		hadPassword := cur.PasswordHash != nil
		upd := map[string]any{
			"google_sub": id.Sub, "email_verified_at": now, "password_hash": nil,
			"password_changed_at": now, "failed_logins": 0, "locked_until": nil, "last_login_at": now,
		}
		if id.Picture != "" {
			upd["avatar_url"] = id.Picture
		}
		if err := tx.Model(&User{}).Where("id = ?", u.ID).Updates(upd).Error; err != nil {
			return err
		}
		res := tx.Model(&Session{}).Where("user_id = ? AND revoked_at IS NULL", u.ID).Update("revoked_at", now)
		if res.Error != nil {
			return res.Error
		}
		revoked = res.RowsAffected
		if err := a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(u), ActorLabel: u.Email, Action: "auth.google_link",
			EntityType: "user", EntityID: u.PublicID,
			Summary: "Akun " + u.Username + " disambungkan ke Google; sandi lama dihapus dan sesi lama dicabut",
			Details: map[string]any{"sandiLamaDihapus": hadPassword, "sesiLamaDicabut": revoked},
		})); err != nil {
			return err
		}
		return a.applyAdminRole(tx, r, u, "google_link")
	})
	if err != nil {
		var he *httpError
		if errors.As(err, &he) {
			writeError(w, he.status, he.msg)
			return
		}
		if isDuplicateKey(err) {
			writeError(w, http.StatusConflict, "Akun Google ini sudah tersambung ke akun lain")
			return
		}
		log.Printf("google: gagal menyambungkan akun: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	sub := id.Sub
	u.GoogleSub, u.PasswordHash, u.EmailVerifiedAt, u.LastLoginAt = &sub, nil, &now, &now
	a.issueSession(w, r, db, u, http.StatusOK, "Akun Anda kini tersambung dengan Google. Selanjutnya masuk dengan Google.")
}

func (a *App) GoogleComplete(w http.ResponseWriter, r *http.Request) {
	db := a.googlePrecheck(w, r)
	if db == nil {
		return
	}
	var req GoogleCompleteRequest
	if err := decodeJSON(w, r, &req, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	pc, err := verifyProfileToken(a.cfg.AuthHMACSecret, strings.TrimSpace(req.ProfileToken), a.now())
	if err != nil {
		writeError(w, http.StatusUnauthorized, msgGoogleProfileExpire)
		return
	}
	username, err := NormalizeUsername(req.Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	phone, err := NormalizePhone(req.Phone)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, err := NormalizeName(pc.Name)
	if err != nil {
		name = username
	}
	pid, err := newPublicID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, msgServerError)
		return
	}
	now := a.now()
	sub := pc.Sub
	u := User{PublicID: pid, Username: username, Email: pc.Email, Name: name, Role: "customer",
		Status: "active", GoogleSub: &sub, AvatarURL: strPtr(safeAvatarURL(pc.Avatar)),
		EmailVerifiedAt: &now, LastLoginAt: &now}
	if phone != "" {
		u.Phone = &phone
	}
	err = db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&User{}).Where("email = ? OR google_sub = ?", pc.Email, pc.Sub).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return &httpError{http.StatusConflict, "Akun untuk email ini sudah ada. Silakan masuk dengan Google lagi."}
		}
		if err := tx.Create(&u).Error; err != nil {
			if isDuplicateKey(err) {
				return &httpError{http.StatusConflict, "Username sudah dipakai, silakan pilih yang lain"}
			}
			return err
		}
		if err := a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(&u), ActorLabel: u.Email, Action: "auth.register",
			EntityType: "user", EntityID: u.PublicID, Summary: "Pendaftaran lewat Google: " + username,
			Details: map[string]any{"metode": "google", "username": username},
		})); err != nil {
			return err
		}
		return a.applyAdminRole(tx, r, &u, "google_register")
	})
	if err != nil {
		var he *httpError
		if errors.As(err, &he) {
			writeError(w, he.status, he.msg)
			return
		}
		log.Printf("google complete: %v", err)
		writeError(w, http.StatusInternalServerError, msgServerError)
		return
	}
	a.logAuth(r, LogEntry{UserID: uid(&u), ActorLabel: u.Email, Action: "auth.google_login",
		EntityType: "user", EntityID: u.PublicID, Summary: "Login dengan Google: " + u.Username})
	a.issueSession(w, r, db, &u, http.StatusCreated, "Pendaftaran berhasil")
}
