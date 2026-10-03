package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"mihanstore/notify"
)

const (
	msgLoginFailed      = "Email/username atau password salah"
	msgNeedTurnstile    = "Harap selesaikan verifikasi keamanan"
	msgTurnstileFailed  = "Verifikasi keamanan gagal, coba lagi."
	msgTurnstileOff     = "Verifikasi keamanan belum dikonfigurasi. Silakan hubungi admin toko."
	msgTurnstileError   = "Verifikasi keamanan tidak dapat diproses saat ini, coba lagi."
	msgServiceDown      = "Layanan sedang tidak tersedia, coba lagi beberapa saat lagi."
	msgServerError      = "Terjadi kesalahan pada server"
	msgDuplicate        = "Email atau username sudah terdaftar"
	msgInvalidSession   = "Sesi tidak valid atau sudah berakhir, silakan login kembali"
	msgTooManyLogin     = "Terlalu banyak percobaan login. Coba lagi dalam beberapa saat."
	msgTooManyRegister  = "Terlalu banyak pendaftaran dari jaringan Anda. Coba lagi nanti."
	msgAccountSuspended = "Akun Anda dinonaktifkan. Silakan hubungi admin toko."
)

type App struct {
	cfg              Config
	db               atomic.Pointer[gorm.DB]
	ips              *IPResolver
	turnstile        *TurnstileVerifier
	access           *AccessVerifier // nil = akses admin belum dikonfigurasi (503)
	google           *GoogleVerifier // nil = login Google belum dikonfigurasi (503)
	loginLimiter     *RateLimiter
	registerLimiter  *RateLimiter
	googleLimiter    *RateLimiter
	adminLimiter     *RateLimiter
	deniedLogLimiter *RateLimiter
	cartLimiter      *RateLimiter // per pengguna: tambah/ubah keranjang
	checkoutLimiter  *RateLimiter // per pengguna: buat pesanan
	cancelLimiter    *RateLimiter // per pengguna: batalkan pesanan
	aliasLimiter     *RateLimiter // per admin: ubah alias pelanggan
	notifier         notify.Notifier
	now              func() time.Time

	// Data wilayah (regions*.go).
	regions        *regionCache
	regionLimiter  *RateLimiter // per IP: API publik wilayah
	regionRuns     *regionRunRegistry
	regionFetchCfg regionFetchConfig
	regionCooldown time.Duration   // jeda minimal antar fetch (sopan ke wilayah.id)
	bgCtx          context.Context // induk goroutine latar (dibatalkan saat server berhenti)
	afterConnect   func(db *gorm.DB)

	// Foto produk (admin_images.go).
	images       ImageStore
	imageGuard   *diskGuard
	imageLimiter *RateLimiter  // per admin: unggah/hapus foto
	imageSem     chan struct{} // batas pemrosesan gambar paralel (memori)
	// imageTxHook KHUSUS TES: dipanggil di dalam transaksi foto sebelum commit (nil di produksi).
	imageTxHook func(tx *gorm.DB) error
}

func NewApp(cfg Config) *App {
	a := &App{
		cfg:              cfg,
		ips:              NewIPResolver(cfg.TrustedProxyCIDRs),
		turnstile:        NewTurnstileVerifier(cfg),
		google:           NewGoogleVerifier(cfg),
		loginLimiter:     NewRateLimiter(10, time.Minute),
		registerLimiter:  NewRateLimiter(5, time.Hour),
		googleLimiter:    NewRateLimiter(20, time.Minute),
		adminLimiter:     NewRateLimiter(300, time.Minute),
		deniedLogLimiter: NewRateLimiter(1, 5*time.Minute),
		cartLimiter:      NewRateLimiter(120, time.Minute),
		checkoutLimiter:  NewRateLimiter(10, 10*time.Minute),
		cancelLimiter:    NewRateLimiter(10, 10*time.Minute),
		aliasLimiter:     NewRateLimiter(60, time.Minute),
		// Server tiruan (http, host bebas) hanya diizinkan untuk database uji.
		notifier: notify.New(cfg.DiscordOrderWebhookURL, cfg.IsTestDB()),
		now:      func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) },

		regions:        newRegionCache(),
		regionLimiter:  NewRateLimiter(240, time.Minute),
		regionRuns:     newRegionRunRegistry(),
		regionFetchCfg: defaultRegionFetchConfig(cfg.RegionAPIBase),
		regionCooldown: 5 * time.Minute,
		bgCtx:          context.Background(),

		imageLimiter: NewRateLimiter(20, time.Minute),
		imageSem:     make(chan struct{}, 2),
	}
	a.setImageStore(NewLocalStore(cfg.UploadsDir, "/uploads/"), cfg.MaxUploadsMB)
	if cfg.TestRegionMinProvinces > 0 {
		a.regionFetchCfg.MinProvinces = cfg.TestRegionMinProvinces
	}
	if cfg.TestRegionCooldown != nil {
		a.regionCooldown = *cfg.TestRegionCooldown
	}
	if av, err := NewAccessVerifier(cfg); err == nil {
		a.access = av
	} else {
		log.Println("PERINGATAN: akses admin belum dikonfigurasi (CF_ACCESS_TEAM_DOMAIN / CF_ACCESS_AUD_STORE / ADMIN_EMAILS) — /api/admin/* menjawab 503")
	}
	if a.google == nil {
		log.Println("PERINGATAN: login Google belum dikonfigurasi (GOOGLE_CLIENT_ID / AUTH_HMAC_SECRET) — /api/auth/google* menjawab 503")
	}
	return a
}

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

func retryAfter(w http.ResponseWriter, d time.Duration) {
	secs := int(d.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
}

func truncateUTF8(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// checkTurnstile mengembalikan true bila lulus; bila tidak, respons error sudah ditulis.
func (a *App) checkTurnstile(w http.ResponseWriter, r *http.Request, token string, ip net.IP) bool {
	remote := ""
	if ip != nil {
		remote = ip.String()
	}
	ok, err := a.turnstile.Verify(r.Context(), token, remote)
	switch {
	case errors.Is(err, errTurnstileNotConfigured):
		writeError(w, http.StatusServiceUnavailable, msgTurnstileOff)
		return false
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, msgTurnstileError)
		return false
	case !ok:
		writeError(w, http.StatusUnauthorized, msgTurnstileFailed)
		return false
	}
	return true
}

func (a *App) createSession(ctx context.Context, db *gorm.DB, userID uint64, r *http.Request, ip net.IP) (string, time.Time, error) {
	token, hash, err := NewSessionToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := a.now().Add(a.cfg.SessionTTL)
	s := Session{UserID: userID, TokenHash: hash, IP: ipBytes(ip), ExpiresAt: expires}
	if ua := truncateUTF8(r.UserAgent(), 255); ua != "" {
		s.UserAgent = &ua
	}
	if err := db.WithContext(ctx).Create(&s).Error; err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

func (a *App) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := decodeJSON(w, r, &req, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	username, err := NormalizeUsername(req.Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	email, err := NormalizeEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	phone, err := NormalizePhone(req.Phone)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, err := NormalizeName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.TurnstileToken) == "" {
		writeError(w, http.StatusBadRequest, msgNeedTurnstile)
		return
	}

	ip := a.ips.ClientIP(r)
	if ok, wait := a.registerLimiter.Allow(rateKey(ip)); !ok {
		retryAfter(w, wait)
		writeError(w, http.StatusTooManyRequests, msgTooManyRegister)
		return
	}
	db := a.db.Load()
	if db == nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
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
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	if googleOwned > 0 {
		writeError(w, http.StatusConflict, msgEmailIsGoogle)
		return
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		log.Printf("register: gagal membuat hash: %v", err)
		writeError(w, http.StatusInternalServerError, msgServerError)
		return
	}
	pid, err := newPublicID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, msgServerError)
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
		if isDuplicateKey(err) {
			writeError(w, http.StatusConflict, msgDuplicate)
			return
		}
		log.Printf("register: gagal menyimpan user: %v", err)
		writeError(w, http.StatusInternalServerError, msgServerError)
		return
	}

	a.logAuth(r, LogEntry{UserID: uid(&u), ActorLabel: u.Email, Action: "auth.register",
		EntityType: "user", EntityID: u.PublicID, Summary: "Pendaftaran akun: " + u.Username,
		Details: map[string]any{"metode": "password", "username": u.Username}})

	token, exp, err := a.createSession(r.Context(), db, u.ID, r, ip)
	if err != nil {
		log.Printf("register: gagal membuat sesi: %v", err)
		// Akun sudah dibuat; pengguna cukup login.
		writeJSON(w, http.StatusCreated, AuthResponse{Success: true, Message: "Registrasi berhasil, silakan login", User: toDTO(&u)})
		return
	}
	writeJSON(w, http.StatusCreated, AuthResponse{Success: true, Message: "Registrasi berhasil", User: toDTO(&u), Token: token, ExpiresAt: &exp})
}

func (a *App) Login(w http.ResponseWriter, r *http.Request) {
	ip := a.ips.ClientIP(r)
	if ok, wait := a.loginLimiter.Allow(rateKey(ip)); !ok {
		retryAfter(w, wait)
		writeError(w, http.StatusTooManyRequests, msgTooManyLogin)
		return
	}
	var req LoginRequest
	if err := decodeJSON(w, r, &req, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	rawID := req.Identifier
	if strings.TrimSpace(rawID) == "" {
		rawID = req.Email
	}
	ident, isEmail, identOK := normalizeIdentifier(rawID)
	if ident == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "Email/username dan password wajib diisi")
		return
	}
	if strings.TrimSpace(req.TurnstileToken) == "" {
		writeError(w, http.StatusBadRequest, msgNeedTurnstile)
		return
	}
	db := a.db.Load()
	if db == nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	if !a.checkTurnstile(w, r, req.TurnstileToken, ip) {
		return
	}
	failed := func(u *User, reason string) {
		e := LogEntry{UserID: uid(u), ActorLabel: truncateUTF8(ident, 100), Action: "auth.login_failed",
			Summary: "Login gagal", Details: map[string]any{"metode": "password", "alasan": reason}}
		if u != nil {
			e.EntityType, e.EntityID = "user", u.PublicID
		}
		a.logAuth(r, e)
	}
	if !identOK || len(req.Password) > 4*passwordMaxLen {
		burnDummyVerify(req.Password)
		failed(nil, "format_tidak_valid")
		writeError(w, http.StatusUnauthorized, msgLoginFailed)
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
		writeError(w, http.StatusUnauthorized, msgLoginFailed)
		return
	}
	if err != nil {
		log.Printf("login: gagal membaca user: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}

	now := a.now()
	if isLocked(u.LockedUntil, now) {
		// Akun sedang dikunci: tetap hitung hash dummy agar waktu respons seragam.
		burnDummyVerify(req.Password)
		failed(&u, "akun_terkunci")
		writeError(w, http.StatusUnauthorized, msgLoginFailed)
		return
	}
	if u.PasswordHash == nil {
		// Aturan B3: akun tanpa password (Google) -> gagal dengan pesan umum,
		// tetap hitung hash dummy agar waktu respons sama. Tidak menaikkan penghitung kunci.
		burnDummyVerify(req.Password)
		failed(&u, "akun_tanpa_password")
		writeError(w, http.StatusUnauthorized, msgLoginFailed)
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
			a.logAuth(r, LogEntry{UserID: uid(&u), ActorLabel: u.Email, Action: "auth.locked",
				EntityType: "user", EntityID: u.PublicID,
				Summary: "Akun " + u.Username + " dikunci 15 menit setelah 5 kali gagal login"})
		}
		writeError(w, http.StatusUnauthorized, msgLoginFailed)
		return
	}
	if u.Status != "active" {
		failed(&u, "suspended")
		writeError(w, http.StatusForbidden, msgAccountSuspended)
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
		writeError(w, http.StatusInternalServerError, msgServerError)
		return
	}
	u.LastLoginAt = &now
	a.logAuth(r, LogEntry{UserID: uid(&u), ActorLabel: u.Email, Action: "auth.login",
		EntityType: "user", EntityID: u.PublicID, Summary: "Login: " + u.Username,
		Details: map[string]any{"metode": "password"}})
	writeJSON(w, http.StatusOK, AuthResponse{Success: true, Message: "Login berhasil", User: toDTO(&u), Token: token, ExpiresAt: &exp})
}

// recordFailedLogin menaikkan failed_logins secara atomik (SELECT ... FOR UPDATE)
// dan mengunci akun 15 menit pada kegagalan ke-5 berturut-turut.
// Mengembalikan true bila kegagalan ini membuat akun terkunci.
func (a *App) recordFailedLogin(ctx context.Context, db *gorm.DB, userID uint64) (bool, error) {
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
func (a *App) authenticate(ctx context.Context, db *gorm.DB, token string) (*User, *Session, error) {
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
	if err := decodeJSON(w, r, &req, true); err != nil {
		return "", err
	}
	return strings.TrimSpace(req.Token), nil
}

func (a *App) Verify(w http.ResponseWriter, r *http.Request) {
	token, err := tokenFromRequest(w, r)
	if err != nil {
		respondDecodeError(w, err)
		return
	}
	if !wellFormedToken(token) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"valid": false})
		return
	}
	db := a.db.Load()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"valid": false, "error": msgServiceDown})
		return
	}
	u, _, err := a.authenticate(r.Context(), db, token)
	if err != nil {
		log.Printf("verify: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"valid": false, "error": msgServiceDown})
		return
	}
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"valid": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "user": toDTO(u)})
}

func (a *App) Me(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if !wellFormedToken(token) {
		writeError(w, http.StatusUnauthorized, msgInvalidSession)
		return
	}
	db := a.db.Load()
	if db == nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	u, _, err := a.authenticate(r.Context(), db, token)
	if err != nil {
		log.Printf("me: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	if u == nil {
		writeError(w, http.StatusUnauthorized, msgInvalidSession)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id": u.PublicID, "username": u.Username, "email": u.Email, "phone": u.Phone,
			"name": u.Name, "role": u.Role, "emailVerified": u.EmailVerifiedAt != nil,
			"avatarUrl": u.AvatarURL, "googleLinked": u.GoogleSub != nil, "hasPassword": u.PasswordHash != nil,
			"createdAt": u.CreatedAt, "lastLoginAt": u.LastLoginAt,
		},
	})
}

func (a *App) Logout(w http.ResponseWriter, r *http.Request) {
	token, err := tokenFromRequest(w, r)
	if err != nil {
		respondDecodeError(w, err)
		return
	}
	if wellFormedToken(token) {
		db := a.db.Load()
		if db == nil {
			writeError(w, http.StatusServiceUnavailable, msgServiceDown)
			return
		}
		u, _, _ := a.authenticate(r.Context(), db, token)
		res := db.WithContext(r.Context()).Model(&Session{}).
			Where("token_hash = ? AND revoked_at IS NULL", HashToken(token)).
			Update("revoked_at", a.now())
		if res.Error != nil {
			log.Printf("logout: %v", res.Error)
			writeError(w, http.StatusServiceUnavailable, msgServiceDown)
			return
		}
		if u != nil && res.RowsAffected > 0 {
			a.logAuth(r, LogEntry{UserID: uid(u), ActorLabel: u.Email, Action: "auth.logout",
				EntityType: "user", EntityID: u.PublicID, Summary: "Logout: " + u.Username})
		}
	}
	// Idempoten: token tidak dikenal/sudah dicabut tetap dianggap berhasil logout.
	writeJSON(w, http.StatusOK, AuthResponse{Success: true, Message: "Berhasil logout"})
}

// Ready memeriksa koneksi database tanpa membuka detail internal.
func (a *App) Ready(w http.ResponseWriter, r *http.Request) {
	db := a.db.Load()
	if db != nil {
		if sqlDB, err := db.DB(); err == nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if sqlDB.PingContext(ctx) == nil {
				writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
				return
			}
		}
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
}
