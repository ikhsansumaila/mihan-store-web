package identity

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"mihanstore/internal/audit"
	"mihanstore/internal/platform"
)

const (
	MsgLoginFailed      = "Email/username atau password salah"
	msgNeedTurnstile    = "Harap selesaikan verifikasi keamanan"
	msgTurnstileFailed  = "Verifikasi keamanan gagal, coba lagi."
	msgTurnstileOff     = "Verifikasi keamanan belum dikonfigurasi. Silakan hubungi admin toko."
	msgTurnstileError   = "Verifikasi keamanan tidak dapat diproses saat ini, coba lagi."
	msgDuplicate        = "Email atau username sudah terdaftar"
	msgInvalidSession   = "Sesi tidak valid atau sudah berakhir, silakan login kembali"
	msgTooManyLogin     = "Terlalu banyak percobaan login. Coba lagi dalam beberapa saat."
	msgTooManyRegister  = "Terlalu banyak pendaftaran dari jaringan Anda. Coba lagi nanti."
	msgAccountSuspended = "Akun Anda dinonaktifkan. Silakan hubungi admin toko."
)

type RegisterRequest struct {
	Username       string `json:"username"`
	Email          string `json:"email"`
	Phone          string `json:"phone"`
	Name           string `json:"name"`
	Password       string `json:"password"`
	TurnstileToken string `json:"turnstileToken"`
}

type LoginRequest struct {
	Identifier     string `json:"identifier"`
	Email          string `json:"email"` // field lama, diterima demi kompatibilitas
	Password       string `json:"password"`
	TurnstileToken string `json:"turnstileToken"`
}

type TokenRequest struct {
	Token string `json:"token"`
}

type AuthResponse struct {
	Success   bool       `json:"success"`
	Message   string     `json:"message"`
	User      *UserDTO   `json:"user,omitempty"`
	Token     string     `json:"token,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// checkTurnstile mengembalikan true bila lulus; bila tidak, respons error sudah ditulis.
func (a *Service) checkTurnstile(w http.ResponseWriter, r *http.Request, token string, ip net.IP) bool {
	remote := ""
	if ip != nil {
		remote = ip.String()
	}
	ok, err := a.Turnstile.Verify(r.Context(), token, remote)
	switch {
	case errors.Is(err, errTurnstileNotConfigured):
		platform.WriteError(w, http.StatusServiceUnavailable, msgTurnstileOff)
		return false
	case err != nil:
		platform.WriteError(w, http.StatusServiceUnavailable, msgTurnstileError)
		return false
	case !ok:
		platform.WriteError(w, http.StatusUnauthorized, msgTurnstileFailed)
		return false
	}
	return true
}

func (a *Service) createSession(ctx context.Context, db *gorm.DB, userID uint64, r *http.Request, ip net.IP) (string, time.Time, error) {
	token, hash, err := NewSessionToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := a.now().Add(a.cfg.SessionTTL)
	s := Session{UserID: userID, TokenHash: hash, IP: platform.IPBytes(ip), ExpiresAt: expires}
	if ua := platform.TruncateUTF8(r.UserAgent(), 255); ua != "" {
		s.UserAgent = &ua
	}
	if err := db.WithContext(ctx).Create(&s).Error; err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

func (a *Service) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := platform.DecodeJSON(w, r, &req, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	username, err := NormalizeUsername(req.Username)
	if err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	email, err := NormalizeEmail(req.Email)
	if err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	phone, err := NormalizePhone(req.Phone)
	if err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, err := NormalizeName(req.Name)
	if err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := ValidatePassword(req.Password); err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.TurnstileToken) == "" {
		platform.WriteError(w, http.StatusBadRequest, msgNeedTurnstile)
		return
	}

	ip := a.clientIP(r)
	if ok, wait := a.RegisterLimiter.Allow(platform.RateKey(ip)); !ok {
		platform.RetryAfter(w, wait)
		platform.WriteError(w, http.StatusTooManyRequests, msgTooManyRegister)
		return
	}
	db := a.db()
	if db == nil {
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	if !a.checkTurnstile(w, r, req.TurnstileToken, ip) {
		return
	}

	// Aturan B2: email yang sudah terdaftar lewat Google tidak bisa didaftarkan dengan password.
	var googleOwned int64
	if err := db.WithContext(r.Context()).Model(&User{}).
		Where("email = ? AND google_sub IS NOT NULL", email).Count(&googleOwned).Error; err != nil {
		log.Printf("register: gagal memeriksa email: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	if googleOwned > 0 {
		platform.WriteError(w, http.StatusConflict, msgEmailIsGoogle)
		return
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		log.Printf("register: gagal membuat hash: %v", err)
		platform.WriteError(w, http.StatusInternalServerError, platform.MsgServerError)
		return
	}
	pid, err := platform.NewPublicID()
	if err != nil {
		platform.WriteError(w, http.StatusInternalServerError, platform.MsgServerError)
		return
	}
	now := a.now()
	u := User{
		PublicID: pid, Username: username, Email: email, Name: name,
		PasswordHash: &hash, Role: "customer", Status: "active", PasswordChangedAt: &now,
	}
	if phone != "" {
		u.Phone = &phone
	}
	if err := db.WithContext(r.Context()).Create(&u).Error; err != nil {
		if platform.IsDuplicateKey(err) {
			platform.WriteError(w, http.StatusConflict, msgDuplicate)
			return
		}
		log.Printf("register: gagal menyimpan user: %v", err)
		platform.WriteError(w, http.StatusInternalServerError, platform.MsgServerError)
		return
	}

	a.audit.Auth(r, audit.Entry{UserID: UID(&u), ActorLabel: u.Email, Action: "auth.register",
		EntityType: "user", EntityID: u.PublicID, Summary: "Pendaftaran akun: " + u.Username,
		Details: map[string]any{"metode": "password", "username": u.Username}})

	token, exp, err := a.createSession(r.Context(), db, u.ID, r, ip)
	if err != nil {
		log.Printf("register: gagal membuat sesi: %v", err)
		// Akun sudah dibuat; pengguna cukup login.
		platform.WriteJSON(w, http.StatusCreated, AuthResponse{Success: true, Message: "Registrasi berhasil, silakan login", User: toDTO(&u)})
		return
	}
	platform.WriteJSON(w, http.StatusCreated, AuthResponse{Success: true, Message: "Registrasi berhasil", User: toDTO(&u), Token: token, ExpiresAt: &exp})
}

func (a *Service) Login(w http.ResponseWriter, r *http.Request) {
	ip := a.clientIP(r)
	if ok, wait := a.LoginLimiter.Allow(platform.RateKey(ip)); !ok {
		platform.RetryAfter(w, wait)
		platform.WriteError(w, http.StatusTooManyRequests, msgTooManyLogin)
		return
	}
	var req LoginRequest
	if err := platform.DecodeJSON(w, r, &req, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	rawID := req.Identifier
	if strings.TrimSpace(rawID) == "" {
		rawID = req.Email
	}
	ident, isEmail, identOK := normalizeIdentifier(rawID)
	if ident == "" || req.Password == "" {
		platform.WriteError(w, http.StatusBadRequest, "Email/username dan password wajib diisi")
		return
	}
	if strings.TrimSpace(req.TurnstileToken) == "" {
		platform.WriteError(w, http.StatusBadRequest, msgNeedTurnstile)
		return
	}
	db := a.db()
	if db == nil {
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	if !a.checkTurnstile(w, r, req.TurnstileToken, ip) {
		return
	}
	failed := func(u *User, reason string) {
		e := audit.Entry{UserID: UID(u), ActorLabel: platform.TruncateUTF8(ident, 100), Action: "auth.login_failed",
			Summary: "Login gagal", Details: map[string]any{"metode": "password", "alasan": reason}}
		if u != nil {
			e.EntityType, e.EntityID = "user", u.PublicID
		}
		a.audit.Auth(r, e)
	}
	if !identOK || len(req.Password) > 4*passwordMaxLen {
		burnDummyVerify(req.Password)
		failed(nil, "format_tidak_valid")
		platform.WriteError(w, http.StatusUnauthorized, MsgLoginFailed)
		return
	}

	ctx := r.Context()
	var u User
	q := db.WithContext(ctx)
	if isEmail {
		q = q.Where("email = ?", ident)
	} else {
		q = q.Where("username = ?", ident)
	}
	err := q.Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		burnDummyVerify(req.Password)
		failed(nil, "akun_tidak_ada")
		platform.WriteError(w, http.StatusUnauthorized, MsgLoginFailed)
		return
	}
	if err != nil {
		log.Printf("login: gagal membaca user: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}

	now := a.now()
	if isLocked(u.LockedUntil, now) {
		// Akun sedang dikunci: tetap hitung hash dummy agar waktu respons seragam.
		burnDummyVerify(req.Password)
		failed(&u, "akun_terkunci")
		platform.WriteError(w, http.StatusUnauthorized, MsgLoginFailed)
		return
	}
	if u.PasswordHash == nil {
		// Aturan B3: akun tanpa password (Google) -> gagal dengan pesan umum,
		// tetap hitung hash dummy agar waktu respons sama. Tidak menaikkan penghitung kunci.
		burnDummyVerify(req.Password)
		failed(&u, "akun_tanpa_password")
		platform.WriteError(w, http.StatusUnauthorized, MsgLoginFailed)
		return
	}

	ok, err := VerifyPassword(req.Password, *u.PasswordHash)
	if err != nil {
		log.Printf("login: hash user id=%d tidak valid", u.ID)
	}
	if !ok {
		lockedNow, err := a.recordFailedLogin(ctx, db, u.ID)
		if err != nil {
			log.Printf("login: gagal mencatat kegagalan: %v", err)
		}
		failed(&u, "password_salah")
		if lockedNow {
			a.audit.Auth(r, audit.Entry{UserID: UID(&u), ActorLabel: u.Email, Action: "auth.locked",
				EntityType: "user", EntityID: u.PublicID,
				Summary: "Akun " + u.Username + " dikunci 15 menit setelah 5 kali gagal login"})
		}
		platform.WriteError(w, http.StatusUnauthorized, MsgLoginFailed)
		return
	}
	if u.Status != "active" {
		failed(&u, "suspended")
		platform.WriteError(w, http.StatusForbidden, msgAccountSuspended)
		return
	}

	if err := db.WithContext(ctx).Model(&User{}).Where("id = ?", u.ID).
		Updates(map[string]any{"failed_logins": 0, "locked_until": nil, "last_login_at": now}).Error; err != nil {
		log.Printf("login: gagal memperbarui user: %v", err)
	}
	// Bersihkan sesi kedaluwarsa secara lazy (tanpa timer/cron).
	if err := db.WithContext(ctx).Exec("DELETE FROM sessions WHERE expires_at < ? LIMIT 1000", now).Error; err != nil {
		log.Printf("login: gagal membersihkan sesi kedaluwarsa: %v", err)
	}

	token, exp, err := a.createSession(ctx, db, u.ID, r, ip)
	if err != nil {
		log.Printf("login: gagal membuat sesi: %v", err)
		platform.WriteError(w, http.StatusInternalServerError, platform.MsgServerError)
		return
	}
	u.LastLoginAt = &now
	a.audit.Auth(r, audit.Entry{UserID: UID(&u), ActorLabel: u.Email, Action: "auth.login",
		EntityType: "user", EntityID: u.PublicID, Summary: "Login: " + u.Username,
		Details: map[string]any{"metode": "password"}})
	platform.WriteJSON(w, http.StatusOK, AuthResponse{Success: true, Message: "Login berhasil", User: toDTO(&u), Token: token, ExpiresAt: &exp})
}

// recordFailedLogin menaikkan failed_logins secara atomik (SELECT ... FOR UPDATE)
// dan mengunci akun 15 menit pada kegagalan ke-5 berturut-turut.
// Mengembalikan true bila kegagalan ini membuat akun terkunci.
func (a *Service) recordFailedLogin(ctx context.Context, db *gorm.DB, userID uint64) (bool, error) {
	lockedNow := false
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cur User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "failed_logins", "locked_until").Where("id = ?", userID).Take(&cur).Error; err != nil {
			return err
		}
		now := a.now()
		if isLocked(cur.LockedUntil, now) {
			return nil // request paralel lain sudah mengunci
		}
		failed, until := nextFailedState(cur.FailedLogins, now)
		lockedNow = until != nil
		return tx.Model(&User{}).Where("id = ?", userID).
			Updates(map[string]any{"failed_logins": failed, "locked_until": until}).Error
	})
	return lockedNow && err == nil, err
}

// authenticate mencari sesi aktif untuk token. Mengembalikan (nil, nil, nil) bila tidak valid.
func (a *Service) authenticate(ctx context.Context, db *gorm.DB, token string) (*User, *Session, error) {
	if !wellFormedToken(token) {
		return nil, nil, nil
	}
	now := a.now()
	var s Session
	err := db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", HashToken(token), now).
		Take(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var u User
	err = db.WithContext(ctx).Where("id = ? AND status = ?", s.UserID, "active").Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &u, &s, nil
}

// tokenFromRequest: Authorization Bearer diutamakan, lalu body {"token": "..."}.
func tokenFromRequest(w http.ResponseWriter, r *http.Request) (string, error) {
	if t := bearerToken(r); t != "" {
		return t, nil
	}
	if r.Body == nil || r.ContentLength == 0 {
		return "", nil
	}
	var req TokenRequest
	if err := platform.DecodeJSON(w, r, &req, true); err != nil {
		return "", err
	}
	return strings.TrimSpace(req.Token), nil
}

func (a *Service) Verify(w http.ResponseWriter, r *http.Request) {
	token, err := tokenFromRequest(w, r)
	if err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	if !wellFormedToken(token) {
		platform.WriteJSON(w, http.StatusUnauthorized, map[string]any{"valid": false})
		return
	}
	db := a.db()
	if db == nil {
		platform.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"valid": false, "error": platform.MsgServiceDown})
		return
	}
	u, _, err := a.authenticate(r.Context(), db, token)
	if err != nil {
		log.Printf("verify: %v", err)
		platform.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"valid": false, "error": platform.MsgServiceDown})
		return
	}
	if u == nil {
		platform.WriteJSON(w, http.StatusUnauthorized, map[string]any{"valid": false})
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"valid": true, "user": toDTO(u)})
}

func (a *Service) Me(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if !wellFormedToken(token) {
		platform.WriteError(w, http.StatusUnauthorized, msgInvalidSession)
		return
	}
	db := a.db()
	if db == nil {
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	u, _, err := a.authenticate(r.Context(), db, token)
	if err != nil {
		log.Printf("me: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	if u == nil {
		platform.WriteError(w, http.StatusUnauthorized, msgInvalidSession)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id": u.PublicID, "username": u.Username, "email": u.Email, "phone": u.Phone,
			"name": u.Name, "role": u.Role, "emailVerified": u.EmailVerifiedAt != nil,
			"avatarUrl": u.AvatarURL, "googleLinked": u.GoogleSub != nil, "hasPassword": u.PasswordHash != nil,
			"createdAt": u.CreatedAt, "lastLoginAt": u.LastLoginAt,
		},
	})
}

func (a *Service) Logout(w http.ResponseWriter, r *http.Request) {
	token, err := tokenFromRequest(w, r)
	if err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	if wellFormedToken(token) {
		db := a.db()
		if db == nil {
			platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
			return
		}
		u, _, _ := a.authenticate(r.Context(), db, token)
		res := db.WithContext(r.Context()).Model(&Session{}).
			Where("token_hash = ? AND revoked_at IS NULL", HashToken(token)).
			Update("revoked_at", a.now())
		if res.Error != nil {
			log.Printf("logout: %v", res.Error)
			platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
			return
		}
		if u != nil && res.RowsAffected > 0 {
			a.audit.Auth(r, audit.Entry{UserID: UID(u), ActorLabel: u.Email, Action: "auth.logout",
				EntityType: "user", EntityID: u.PublicID, Summary: "Logout: " + u.Username})
		}
	}
	// Idempoten: token tidak dikenal/sudah dicabut tetap dianggap berhasil logout.
	platform.WriteJSON(w, http.StatusOK, AuthResponse{Success: true, Message: "Berhasil logout"})
}
