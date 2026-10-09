package push

// Langganan Web Push (tabel push_subscriptions, migrations/017): penyimpanan (implementasi Store) dan
// rute langganan untuk dua audiens TERPISAH: admin dan pelanggan. Pengiriman notifikasi pesanan dipicu
// modul pesanan lewat antarmuka Events (dirakit di internal/app).
//
// Rute (di balik requireAdmin: Cloudflare Access + ADMIN_EMAILS + CSRF untuk metode pengubah):
//   GET    /api/admin/push/public-key   status fitur + kunci publik VAPID (bukan rahasia)
//   POST   /api/admin/push/subscribe    simpan/perbarui langganan perangkat ini (upsert per endpoint)
//   DELETE /api/admin/push/subscribe    hapus langganan perangkat ini (berdasarkan endpoint)
//   POST   /api/admin/push/test         kirim satu notifikasi tes ke langganan milik admin ini
//
// Keputusan: langganan push TIDAK dicatat di log aktivitas (bukan perubahan data toko, dan endpoint
// berisi token perangkat yang tidak boleh masuk log). Kegagalan kirim hanya dicatat di log server
// tanpa endpoint/kunci.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/platform"
)

const (
	msgPushDisabled   = "Notifikasi push belum dikonfigurasi di server"
	msgPushInvalidSub = "Data langganan notifikasi tidak valid"
	maxPushBodyBytes  = 4 << 10 // langganan push ~ 0,5 KB
	MaxSubsPerUser    = 10
)

// ---------- Penyimpanan langganan (implementasi Store) ----------

type subStore struct {
	db          func() *gorm.DB
	now         func() time.Time
	adminEmails func() []string
}

// NewStore: penyimpanan langganan di database (implementasi Store untuk New).
// adminEmails dibaca setiap kali (ADMIN_EMAILS); hanya admin yang masih berhak menerima push admin.
func NewStore(db func() *gorm.DB, now func() time.Time, adminEmails func() []string) Store {
	return &subStore{db: db, now: now, adminEmails: adminEmails}
}

func (s *subStore) conn(ctx context.Context) (*gorm.DB, error) {
	db := s.db()
	if db == nil {
		return nil, errors.New("database belum terhubung")
	}
	return db.WithContext(ctx), nil
}

// List: hanya langganan milik admin yang masih berhak (role admin, aktif, belum dihapus, email masih di
// ADMIN_EMAILS). userID 0 = semua admin.
func (s *subStore) List(ctx context.Context, userID uint64) ([]Subscription, error) {
	emails := s.adminEmails()
	if len(emails) == 0 {
		return nil, nil
	}
	db, err := s.conn(ctx)
	if err != nil {
		return nil, err
	}
	q := `SELECT ps.id, ps.endpoint, ps.p256dh, ps.auth FROM push_subscriptions ps
		JOIN users u ON u.id = ps.user_id
		WHERE ps.audience = 'admin' AND u.role = 'admin' AND u.status = 'active' AND u.deleted_at IS NULL AND LOWER(u.email) IN ?`
	args := []any{emails}
	if userID != 0 {
		q += ` AND ps.user_id = ?`
		args = append(args, userID)
	}
	q += ` ORDER BY ps.id LIMIT 500`
	return scanSubs(db, q, args...)
}

// ListCustomer: hanya langganan audiens pelanggan milik userID (pemilik pesanan), akun aktif & belum dihapus.
func (s *subStore) ListCustomer(ctx context.Context, userID uint64) ([]Subscription, error) {
	if userID == 0 {
		return nil, nil
	}
	db, err := s.conn(ctx)
	if err != nil {
		return nil, err
	}
	return scanSubs(db, `SELECT ps.id, ps.endpoint, ps.p256dh, ps.auth FROM push_subscriptions ps
		JOIN users u ON u.id = ps.user_id
		WHERE ps.audience = 'customer' AND ps.user_id = ? AND u.status = 'active' AND u.deleted_at IS NULL
		ORDER BY ps.id LIMIT 50`, userID)
}

func scanSubs(db *gorm.DB, q string, args ...any) ([]Subscription, error) {
	var rows []struct {
		ID       uint64 `gorm:"column:id"`
		Endpoint string `gorm:"column:endpoint"`
		P256dh   string `gorm:"column:p256dh"`
		Auth     string `gorm:"column:auth"`
	}
	if err := db.Raw(q, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Subscription, 0, len(rows))
	for _, r := range rows {
		out = append(out, Subscription{ID: r.ID, Endpoint: r.Endpoint, P256dh: r.P256dh, Auth: r.Auth})
	}
	return out, nil
}

func (s *subStore) Delete(ctx context.Context, id uint64) error {
	db, err := s.conn(ctx)
	if err != nil {
		return err
	}
	return db.Exec(`DELETE FROM push_subscriptions WHERE id = ?`, id).Error
}

func (s *subStore) Touch(ctx context.Context, id uint64) error {
	db, err := s.conn(ctx)
	if err != nil {
		return err
	}
	return db.Exec(`UPDATE push_subscriptions SET last_used_at = ? WHERE id = ?`, s.now(), id).Error
}

func EndpointHash(endpoint string) string {
	h := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(h[:])
}

// ---------- Logika langganan bersama (admin & pelanggan) ----------

const (
	audienceAdmin    = "admin"
	audienceCustomer = "customer"
)

type pushSubscribeInput struct {
	Endpoint       string   `json:"endpoint"`
	ExpirationTime *float64 `json:"expirationTime"` // dikirim PushSubscription.toJSON(); diabaikan
	Keys           struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

type pushUnsubscribeInput struct {
	Endpoint string `json:"endpoint"`
}

func (a *Service) pushEnabled() bool { p := a.sender(); return p != nil && p.Enabled() }

func pushRateOK(w http.ResponseWriter, rl *platform.RateLimiter, key, msg string) bool {
	if ok, wait := rl.Allow(key); !ok {
		platform.RetryAfter(w, wait)
		platform.WriteError(w, http.StatusTooManyRequests, msg)
		return false
	}
	return true
}

// saveSubscription: validasi + upsert per (endpoint, audiens). Endpoint yang sama didaftarkan akun lain
// memindahkan kepemilikan baris audiens itu. Maks. 10 langganan per pengguna per audiens.
func (a *Service) saveSubscription(w http.ResponseWriter, r *http.Request, userID uint64, audience string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPushBodyBytes)
	var in pushSubscribeInput
	if err := platform.DecodeJSON(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	if ValidateEndpoint(in.Endpoint) != nil || ValidateKeys(in.Keys.P256dh, in.Keys.Auth) != nil {
		platform.WriteError(w, http.StatusBadRequest, msgPushInvalidSub)
		return
	}
	hash := EndpointHash(in.Endpoint)
	ua := platform.TruncateUTF8(strings.Map(func(c rune) rune {
		if c < 0x20 || c == 0x7f {
			return -1
		}
		return c
	}, r.UserAgent()), 200)
	var uaPtr *string
	if ua != "" {
		uaPtr = &ua
	}
	now := a.now()
	db := a.db().WithContext(r.Context())
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO push_subscriptions (user_id, audience, endpoint, endpoint_hash, p256dh, auth, user_agent, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) AS new
			ON DUPLICATE KEY UPDATE user_id = new.user_id, endpoint = new.endpoint, p256dh = new.p256dh,
				auth = new.auth, user_agent = new.user_agent`,
			userID, audience, in.Endpoint, hash, in.Keys.P256dh, in.Keys.Auth, uaPtr, now).Error; err != nil {
			return err
		}
		// Kunci perangkat diperbarui juga di baris audiens lain untuk endpoint yang sama (satu browser).
		if err := tx.Exec(`UPDATE push_subscriptions SET p256dh = ?, auth = ? WHERE endpoint_hash = ? AND audience <> ?`,
			in.Keys.P256dh, in.Keys.Auth, hash, audience).Error; err != nil {
			return err
		}
		// Batas per pengguna per audiens: simpan maksimal 10 langganan; yang paling lama tidak dipakai dibuang.
		var old []uint64
		if err := tx.Raw(`SELECT id FROM push_subscriptions WHERE user_id = ? AND audience = ? AND endpoint_hash <> ?
			ORDER BY COALESCE(last_used_at, created_at) DESC, id DESC LIMIT 1000 OFFSET ?`,
			userID, audience, hash, MaxSubsPerUser-1).Scan(&old).Error; err != nil {
			return err
		}
		if len(old) > 0 {
			return tx.Exec(`DELETE FROM push_subscriptions WHERE id IN ?`, old).Error
		}
		return nil
	})
	if err != nil {
		log.Printf("push: gagal menyimpan langganan (%s): %v", audience, err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "subscribed": true})
}

// deleteSubscription: hapus baris audiens ini milik pengguna ini untuk endpoint itu.
// keepBrowserSubscription = endpoint yang sama masih dipakai audiens lain (mis. admin dan pelanggan di
// browser yang sama): browser TIDAK perlu berhenti berlangganan agar audiens lain tetap menerima.
func (a *Service) deleteSubscription(w http.ResponseWriter, r *http.Request, userID uint64, audience string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPushBodyBytes)
	var in pushUnsubscribeInput
	if err := platform.DecodeJSON(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	if in.Endpoint == "" || len(in.Endpoint) > MaxEndpointLen {
		platform.WriteError(w, http.StatusBadRequest, msgPushInvalidSub)
		return
	}
	hash := EndpointHash(in.Endpoint)
	db := a.db().WithContext(r.Context())
	res := db.Exec(`DELETE FROM push_subscriptions WHERE endpoint_hash = ? AND audience = ? AND user_id = ?`, hash, audience, userID)
	if res.Error != nil {
		log.Printf("push: gagal menghapus langganan (%s): %v", audience, res.Error)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	var others int64
	if err := db.Raw(`SELECT COUNT(*) FROM push_subscriptions WHERE endpoint_hash = ? AND audience <> ?`, hash, audience).Scan(&others).Error; err != nil {
		others = 0
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "removed": res.RowsAffected > 0, "keepBrowserSubscription": others > 0})
}

// ---------- Handler admin ----------

func (a *Service) AdminPushPublicKey(w http.ResponseWriter, r *http.Request) {
	adminID := a.adminID(r)
	if !a.pushEnabled() {
		platform.WriteJSON(w, http.StatusOK, map[string]any{"enabled": false, "publicKey": "", "subscriptions": 0})
		return
	}
	var n int64
	if err := a.db().WithContext(r.Context()).Raw(`SELECT COUNT(*) FROM push_subscriptions WHERE user_id = ? AND audience = 'admin'`, adminID).Scan(&n).Error; err != nil {
		log.Printf("admin push: gagal menghitung langganan: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"enabled": true, "publicKey": a.sender().PublicKey(), "subscriptions": n})
}

func adminPushKey(id uint64) string { return "admin:" + strconv.FormatUint(id, 10) }

func (a *Service) AdminPushSubscribe(w http.ResponseWriter, r *http.Request) {
	adminID := a.adminID(r)
	if !a.pushEnabled() {
		platform.WriteError(w, http.StatusServiceUnavailable, msgPushDisabled)
		return
	}
	if !pushRateOK(w, a.AdminLimiter, adminPushKey(adminID), platform.MsgTooManyAdmin) {
		return
	}
	a.saveSubscription(w, r, adminID, audienceAdmin)
}

func (a *Service) AdminPushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	adminID := a.adminID(r)
	if !pushRateOK(w, a.AdminLimiter, adminPushKey(adminID), platform.MsgTooManyAdmin) {
		return
	}
	a.deleteSubscription(w, r, adminID, audienceAdmin)
}

func (a *Service) AdminPushTest(w http.ResponseWriter, r *http.Request) {
	adminID := a.adminID(r)
	if !a.pushEnabled() {
		platform.WriteError(w, http.StatusServiceUnavailable, msgPushDisabled)
		return
	}
	if !pushRateOK(w, a.AdminLimiter, adminPushKey(adminID), platform.MsgTooManyAdmin) {
		return
	}
	// Body kosong / {} saja (CSRF tetap mewajibkan Content-Type JSON).
	r.Body = http.MaxBytesReader(w, r.Body, maxPushBodyBytes)
	var in struct{}
	if err := platform.DecodeJSON(w, r, &in, true); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	sent, total, err := a.sender().SendTest(ctx, adminID)
	if err != nil {
		log.Printf("admin push: tes gagal: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"sent": sent, "total": total})
}

// ---------- Handler pelanggan (sesi Bearer, dibungkus middleware pelanggan modul identity) ----------
// Token Bearer tidak dikirim otomatis oleh browser, jadi rute ini tidak butuh pemeriksaan CSRF
// (sama seperti /api/cart dan /api/orders).

const msgTooManyPush = "Terlalu banyak permintaan. Coba lagi beberapa saat lagi."

func (a *Service) PushPublicKey(w http.ResponseWriter, r *http.Request) {
	if !a.pushEnabled() {
		platform.WriteJSON(w, http.StatusOK, map[string]any{"enabled": false, "publicKey": ""})
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"enabled": true, "publicKey": a.sender().PublicKey()})
}

func (a *Service) PushSubscribe(w http.ResponseWriter, r *http.Request) {
	u := a.customerID(r)
	if !a.pushEnabled() {
		platform.WriteError(w, http.StatusServiceUnavailable, msgPushDisabled)
		return
	}
	if !platform.LimitUser(w, a.CustomerLimiter, u, msgTooManyPush) {
		return
	}
	a.saveSubscription(w, r, u, audienceCustomer)
}

func (a *Service) PushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	u := a.customerID(r)
	if !platform.LimitUser(w, a.CustomerLimiter, u, msgTooManyPush) {
		return
	}
	a.deleteSubscription(w, r, u, audienceCustomer)
}

// ---------- Modul langganan ----------

// Deps: dependensi rute langganan push.
type Deps struct {
	DB     func() *gorm.DB
	Now    func() time.Time
	Sender func() Sender // pengirim saat ini (Noop bila VAPID belum dikonfigurasi)
	// AdminID / CustomerID: pengguna yang login (rute sudah dibungkus middleware admin / pelanggan).
	AdminID    func(r *http.Request) uint64
	CustomerID func(r *http.Request) uint64
}

// Service: rute langganan push admin & pelanggan. Limiter boleh diganti saat uji.
type Service struct {
	AdminLimiter    *platform.RateLimiter // per admin: langganan/tes push
	CustomerLimiter *platform.RateLimiter // per pelanggan: langganan push

	db         func() *gorm.DB
	now        func() time.Time
	sender     func() Sender
	adminID    func(r *http.Request) uint64
	customerID func(r *http.Request) uint64
}

func NewService(d Deps) *Service {
	return &Service{
		AdminLimiter:    platform.NewRateLimiter(20, time.Minute),
		CustomerLimiter: platform.NewRateLimiter(20, time.Minute),
		db:              d.DB, now: d.Now, sender: d.Sender, adminID: d.AdminID, customerID: d.CustomerID,
	}
}
