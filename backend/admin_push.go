package main

// Web Push untuk ADMIN (Tahap 1): langganan per browser/perangkat admin dan pengiriman notifikasi
// "Pesanan baru" / "Pesanan dibatalkan" (oleh pelanggan) setelah commit. Lihat paket push dan
// migrations/017_push_subscriptions.sql.
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

	"mihanstore/push"
)

const (
	msgPushDisabled   = "Notifikasi push belum dikonfigurasi di server"
	msgPushInvalidSub = "Data langganan notifikasi tidak valid"
	maxPushBodyBytes  = 4 << 10 // langganan push ~ 0,5 KB
	maxPushSubsPerAdm = 10
)

// ---------- Penyimpanan langganan (implementasi push.Store) ----------

type pushStore struct{ app *App }

func (s *pushStore) conn(ctx context.Context) (*gorm.DB, error) {
	db := s.app.db.Load()
	if db == nil {
		return nil, errors.New("database belum terhubung")
	}
	return db.WithContext(ctx), nil
}

// List: hanya langganan milik admin yang masih berhak (role admin, aktif, belum dihapus, email masih di
// ADMIN_EMAILS). userID 0 = semua admin.
func (s *pushStore) List(ctx context.Context, userID uint64) ([]push.Subscription, error) {
	emails := s.app.cfg.AdminEmails
	if len(emails) == 0 {
		return nil, nil
	}
	db, err := s.conn(ctx)
	if err != nil {
		return nil, err
	}
	q := `SELECT ps.id, ps.endpoint, ps.p256dh, ps.auth FROM push_subscriptions ps
		JOIN users u ON u.id = ps.user_id
		WHERE u.role = 'admin' AND u.status = 'active' AND u.deleted_at IS NULL AND LOWER(u.email) IN ?`
	args := []any{emails}
	if userID != 0 {
		q += ` AND ps.user_id = ?`
		args = append(args, userID)
	}
	q += ` ORDER BY ps.id LIMIT 500`
	var rows []struct {
		ID       uint64 `gorm:"column:id"`
		Endpoint string `gorm:"column:endpoint"`
		P256dh   string `gorm:"column:p256dh"`
		Auth     string `gorm:"column:auth"`
	}
	if err := db.Raw(q, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]push.Subscription, 0, len(rows))
	for _, r := range rows {
		out = append(out, push.Subscription{ID: r.ID, Endpoint: r.Endpoint, P256dh: r.P256dh, Auth: r.Auth})
	}
	return out, nil
}

func (s *pushStore) Delete(ctx context.Context, id uint64) error {
	db, err := s.conn(ctx)
	if err != nil {
		return err
	}
	return db.Exec(`DELETE FROM push_subscriptions WHERE id = ?`, id).Error
}

func (s *pushStore) Touch(ctx context.Context, id uint64) error {
	db, err := s.conn(ctx)
	if err != nil {
		return err
	}
	return db.Exec(`UPDATE push_subscriptions SET last_used_at = ? WHERE id = ?`, s.app.now(), id).Error
}

func endpointHash(endpoint string) string {
	h := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(h[:])
}

// ---------- Pengiriman dari alur pesanan ----------

// pushOrder dipanggil SETELAH commit (di titik yang sama dengan notifyOrder untuk pesanan baru dan
// pembatalan oleh pelanggan). Hanya membaca nomor pesanan; pengiriman asinkron di paket push.
func (a *App) pushOrder(db *gorm.DB, kind string, orderID uint64) {
	if a.pusher == nil || !a.pusher.Enabled() {
		return
	}
	var no string
	if err := db.Raw(`SELECT order_no FROM orders WHERE id = ?`, orderID).Scan(&no).Error; err != nil || no == "" {
		log.Printf("web push pesanan %d: gagal membaca nomor pesanan: %v", orderID, err)
		return
	}
	a.pusher.OrderEvent(push.Event{Kind: kind, OrderNo: no})
}

// ---------- Handler admin ----------

func (a *App) pushRateOK(w http.ResponseWriter, admin *User) bool {
	if ok, wait := a.pushLimiter.Allow("admin:" + strconv.FormatUint(admin.ID, 10)); !ok {
		retryAfter(w, wait)
		writeError(w, http.StatusTooManyRequests, msgTooManyAdmin)
		return false
	}
	return true
}

func (a *App) AdminPushPublicKey(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	if a.pusher == nil || !a.pusher.Enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "publicKey": "", "subscriptions": 0})
		return
	}
	var n int64
	if err := a.db.Load().WithContext(r.Context()).Raw(`SELECT COUNT(*) FROM push_subscriptions WHERE user_id = ?`, admin.ID).Scan(&n).Error; err != nil {
		log.Printf("admin push: gagal menghitung langganan: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "publicKey": a.pusher.PublicKey(), "subscriptions": n})
}

type pushSubscribeInput struct {
	Endpoint       string   `json:"endpoint"`
	ExpirationTime *float64 `json:"expirationTime"` // dikirim PushSubscription.toJSON(); diabaikan
	Keys           struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (a *App) AdminPushSubscribe(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	if a.pusher == nil || !a.pusher.Enabled() {
		writeError(w, http.StatusServiceUnavailable, msgPushDisabled)
		return
	}
	if !a.pushRateOK(w, admin) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPushBodyBytes)
	var in pushSubscribeInput
	if err := decodeJSON(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	if push.ValidateEndpoint(in.Endpoint) != nil || push.ValidateKeys(in.Keys.P256dh, in.Keys.Auth) != nil {
		writeError(w, http.StatusBadRequest, msgPushInvalidSub)
		return
	}
	hash := endpointHash(in.Endpoint)
	ua := truncateUTF8(strings.Map(func(c rune) rune {
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
	db := a.db.Load().WithContext(r.Context())
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO push_subscriptions (user_id, endpoint, endpoint_hash, p256dh, auth, user_agent, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?) AS new
			ON DUPLICATE KEY UPDATE user_id = new.user_id, endpoint = new.endpoint, p256dh = new.p256dh,
				auth = new.auth, user_agent = new.user_agent`,
			admin.ID, in.Endpoint, hash, in.Keys.P256dh, in.Keys.Auth, uaPtr, now).Error; err != nil {
			return err
		}
		// Batas per admin: simpan maksimal 10 langganan; yang paling lama tidak dipakai dibuang.
		var old []uint64
		if err := tx.Raw(`SELECT id FROM push_subscriptions WHERE user_id = ? AND endpoint_hash <> ?
			ORDER BY COALESCE(last_used_at, created_at) DESC, id DESC LIMIT 1000 OFFSET ?`,
			admin.ID, hash, maxPushSubsPerAdm-1).Scan(&old).Error; err != nil {
			return err
		}
		if len(old) > 0 {
			return tx.Exec(`DELETE FROM push_subscriptions WHERE id IN ?`, old).Error
		}
		return nil
	})
	if err != nil {
		log.Printf("admin push: gagal menyimpan langganan: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "subscribed": true})
}

type pushUnsubscribeInput struct {
	Endpoint string `json:"endpoint"`
}

func (a *App) AdminPushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	if !a.pushRateOK(w, admin) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPushBodyBytes)
	var in pushUnsubscribeInput
	if err := decodeJSON(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	if in.Endpoint == "" || len(in.Endpoint) > push.MaxEndpointLen {
		writeError(w, http.StatusBadRequest, msgPushInvalidSub)
		return
	}
	// Hanya langganan milik admin ini yang bisa dihapus lewat rute ini.
	res := a.db.Load().WithContext(r.Context()).Exec(`DELETE FROM push_subscriptions WHERE endpoint_hash = ? AND user_id = ?`,
		endpointHash(in.Endpoint), admin.ID)
	if res.Error != nil {
		log.Printf("admin push: gagal menghapus langganan: %v", res.Error)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "removed": res.RowsAffected > 0})
}

func (a *App) AdminPushTest(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	if a.pusher == nil || !a.pusher.Enabled() {
		writeError(w, http.StatusServiceUnavailable, msgPushDisabled)
		return
	}
	if !a.pushRateOK(w, admin) {
		return
	}
	// Body kosong / {} saja (CSRF tetap mewajibkan Content-Type JSON).
	r.Body = http.MaxBytesReader(w, r.Body, maxPushBodyBytes)
	var in struct{}
	if err := decodeJSON(w, r, &in, true); err != nil {
		respondDecodeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	sent, total, err := a.pusher.SendTest(ctx, admin.ID)
	if err != nil {
		log.Printf("admin push: tes gagal: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "total": total})
}
