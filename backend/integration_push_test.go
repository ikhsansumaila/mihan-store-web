//go:build integration

// Tes integrasi Web Push admin (migrasi 017 + hak 018) terhadap database UJI. Layanan push diganti
// klien HTTP tiruan (tidak ada lalu lintas keluar).
package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"mihanstore/push"
)

type pushRec struct {
	mu     sync.Mutex
	status map[string]int
	urls   []string
}

func (p *pushRec) Do(r *http.Request) (*http.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	io.Copy(io.Discard, r.Body)
	p.urls = append(p.urls, r.URL.String())
	code := p.status[r.URL.String()]
	if code == 0 {
		code = 201
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
}

// hits: jumlah kiriman ke endpoint yang mengandung token tes ini.
func (p *pushRec) hits(tag string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, u := range p.urls {
		if strings.Contains(u, tag) {
			n++
		}
	}
	return n
}

func subBody(t *testing.T, endpoint string) map[string]any {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a := make([]byte, 16)
	rand.Read(a)
	return map[string]any{"endpoint": endpoint, "expirationTime": nil, "keys": map[string]any{
		"p256dh": base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), "auth": base64.RawURLEncoding.EncodeToString(a)}}
}

func setupPush(t *testing.T) (*App, http.Handler, *pushRec, *push.WebPush) {
	app, _, _, _ := setupOrders(t)
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	rec := &pushRec{status: map[string]int{}}
	wp, ok := push.New(pub, priv, "https://store.mihan.web.id", &pushStore{app: app}, push.WithHTTPClient(rec)).(*push.WebPush)
	if !ok {
		t.Fatal("pusher harus aktif")
	}
	app.pusher = wp
	app.pushLimiter = NewRateLimiter(100000, 1<<62)
	return app, newRouter(app), rec, wp
}

func TestIntegrationPushSchemaAndGrants(t *testing.T) {
	_, _, db, _ := setupOrders(t)
	for _, q := range []string{
		"SELECT COUNT(*) FROM push_subscriptions",
		"DELETE FROM push_subscriptions WHERE id = 0",
		"UPDATE push_subscriptions SET last_used_at = NULL WHERE id = 0",
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Errorf("harus boleh: %s -> %v", q, err)
		}
	}
	for _, q := range []string{"ALTER TABLE push_subscriptions ADD COLUMN x INT", "DROP TABLE push_subscriptions", "TRUNCATE TABLE push_subscriptions"} {
		if err := db.Exec(q).Error; err == nil || mysqlErrNo(err) != 1142 {
			t.Errorf("DDL harus ditolak (1142): %s -> %v", q, err)
		}
	}
	var cols []string
	db.Raw(`SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'push_subscriptions' ORDER BY ORDINAL_POSITION`).Scan(&cols)
	if strings.Join(cols, ",") != "id,user_id,endpoint,endpoint_hash,p256dh,auth,user_agent,created_at,last_used_at" {
		t.Fatalf("kolom: %v", cols)
	}
}

func TestIntegrationPushSubscribeFlow(t *testing.T) {
	app, h, rec, _ := setupPush(t)
	db := app.db.Load()
	tag := fmt.Sprintf("flow%d", userSeqNext())
	ep := "https://fcm.googleapis.com/fcm/send/" + tag + "-a"

	r := adminCall(t, h, "GET", "/api/admin/push/public-key", integAdmin, nil)
	if r.Code != 200 || r.Body["enabled"] != true || r.Body["publicKey"] != app.pusher.PublicKey() {
		t.Fatalf("public-key: %d %v", r.Code, r.Body)
	}
	before := num(r.Body["subscriptions"])

	// Validasi: endpoint SSRF / kunci rusak / field asing -> 400; tanpa CSRF -> 403.
	for _, b := range []map[string]any{
		subBody(t, "https://127.0.0.1/x"),
		subBody(t, "http://fcm.googleapis.com/fcm/send/x"),
		subBody(t, "https://evil.example/x"),
		{"endpoint": ep, "keys": map[string]any{"p256dh": "AAAA", "auth": "BBBB"}},
	} {
		if r := adminCall(t, h, "POST", "/api/admin/push/subscribe", integAdmin, b); r.Code != 400 {
			t.Fatalf("langganan tidak sah harus 400: %d %v (%v)", r.Code, r.Body, b["endpoint"])
		}
	}
	extra := subBody(t, ep)
	extra["admin"] = true
	if r := adminCall(t, h, "POST", "/api/admin/push/subscribe", integAdmin, extra); r.Code != 400 {
		t.Fatalf("field asing harus 400: %d", r.Code)
	}
	big := subBody(t, ep)
	big["endpoint"] = ep + strings.Repeat("x", 5000)
	if r := adminCall(t, h, "POST", "/api/admin/push/subscribe", integAdmin, big); r.Code != 413 {
		t.Fatalf("body besar harus 413: %d", r.Code)
	}

	// Simpan, lalu daftar ulang endpoint yang sama = upsert (tetap satu baris).
	if r := adminCall(t, h, "POST", "/api/admin/push/subscribe", integAdmin, subBody(t, ep)); r.Code != 200 {
		t.Fatalf("subscribe: %d %v", r.Code, r.Body)
	}
	if r := adminCall(t, h, "POST", "/api/admin/push/subscribe", integAdmin, subBody(t, ep)); r.Code != 200 {
		t.Fatalf("subscribe ulang: %d %v", r.Code, r.Body)
	}
	var n int64
	db.Raw("SELECT COUNT(*) FROM push_subscriptions WHERE endpoint_hash = ?", endpointHash(ep)).Scan(&n)
	if n != 1 {
		t.Fatalf("upsert harus 1 baris: %d", n)
	}
	var ua string
	db.Raw("SELECT user_agent FROM push_subscriptions WHERE endpoint_hash = ?", endpointHash(ep)).Scan(&ua)
	if ua != "integration-admin" {
		t.Fatalf("user_agent: %q", ua)
	}
	r = adminCall(t, h, "GET", "/api/admin/push/public-key", integAdmin, nil)
	if num(r.Body["subscriptions"]) != before+1 {
		t.Fatalf("jumlah langganan: %v", r.Body)
	}

	// Tes kirim: hanya ke langganan admin ini.
	r = adminCall(t, h, "POST", "/api/admin/push/test", integAdmin, map[string]any{})
	if r.Code != 200 || num(r.Body["sent"]) < 1 || rec.hits(tag) != 1 {
		t.Fatalf("tes kirim: %d %v hits=%d", r.Code, r.Body, rec.hits(tag))
	}
	var used *string
	db.Raw("SELECT DATE_FORMAT(last_used_at, '%Y') FROM push_subscriptions WHERE endpoint_hash = ?", endpointHash(ep)).Scan(&used)
	if used == nil {
		t.Fatal("last_used_at harus terisi setelah kirim sukses")
	}

	// Admin lain tidak bisa menghapus langganan milik admin ini.
	if r := adminCall(t, h, "DELETE", "/api/admin/push/subscribe", "promosi@uji.test", map[string]any{"endpoint": ep}); r.Code != 200 || r.Body["removed"] != false {
		t.Fatalf("hapus milik orang lain: %d %v", r.Code, r.Body)
	}
	if r := adminCall(t, h, "DELETE", "/api/admin/push/subscribe", integAdmin, map[string]any{"endpoint": ep}); r.Code != 200 || r.Body["removed"] != true {
		t.Fatalf("hapus: %d %v", r.Code, r.Body)
	}
	db.Raw("SELECT COUNT(*) FROM push_subscriptions WHERE endpoint_hash = ?", endpointHash(ep)).Scan(&n)
	if n != 0 {
		t.Fatal("langganan harus terhapus")
	}
	// Tidak ada log aktivitas untuk langganan push, dan endpoint tidak pernah masuk log.
	if c := countLogs(db, "details LIKE ? OR summary LIKE ?", "%"+tag+"%", "%"+tag+"%"); c != 0 {
		t.Fatalf("endpoint masuk log aktivitas: %d", c)
	}
}

func TestIntegrationPushLimitPerAdmin(t *testing.T) {
	app, h, _, _ := setupPush(t)
	db := app.db.Load()
	// Admin terpisah (email unik) agar tidak bentrok dengan tes lain pada DB uji bersama.
	who := fmt.Sprintf("push%d@uji.test", userSeqNext())
	app.cfg.AdminEmails = append(append([]string{}, app.cfg.AdminEmails...), who)
	app.access.admins[who] = true
	if r := adminCall(t, h, "GET", "/api/admin/me", who, nil); r.Code != 200 {
		t.Fatalf("admin uji %s: %d %v", who, r.Code, r.Body)
	}
	tag := fmt.Sprintf("lim%d", userSeqNext())
	for i := 0; i < maxPushSubsPerAdm+3; i++ {
		if r := adminCall(t, h, "POST", "/api/admin/push/subscribe", who, subBody(t, fmt.Sprintf("https://web.push.apple.com/%s-%02d", tag, i))); r.Code != 200 {
			t.Fatalf("subscribe %d: %d %v", i, r.Code, r.Body)
		}
	}
	var eps []string
	db.Raw("SELECT ps.endpoint FROM push_subscriptions ps JOIN users u ON u.id = ps.user_id WHERE u.email = ? ORDER BY ps.endpoint", who).Scan(&eps)
	if len(eps) != maxPushSubsPerAdm {
		t.Fatalf("batas %d langganan per admin: %d", maxPushSubsPerAdm, len(eps))
	}
	if !strings.HasSuffix(eps[len(eps)-1], fmt.Sprintf("-%02d", maxPushSubsPerAdm+2)) || strings.HasSuffix(eps[0], "-00") {
		t.Fatalf("yang terlama harus dibuang: %v", eps)
	}
}

func TestIntegrationPushOnOrderEvents(t *testing.T) {
	app, h, rec, wp := setupPush(t)
	db := app.db.Load()
	tag := fmt.Sprintf("ord%d", userSeqNext())
	epOK := "https://fcm.googleapis.com/fcm/send/" + tag + "-ok"
	epGone := "https://web.push.apple.com/" + tag + "-gone"
	for _, ep := range []string{epOK, epGone} {
		if r := adminCall(t, h, "POST", "/api/admin/push/subscribe", integAdmin, subBody(t, ep)); r.Code != 200 {
			t.Fatalf("subscribe: %d %v", r.Code, r.Body)
		}
	}
	rec.mu.Lock()
	rec.status[epGone] = 410
	rec.mu.Unlock()

	// Langganan milik pengguna yang BUKAN admin (baris rekaan) tidak pernah dikirimi.
	tok := newCustomer(t, h, "Pembeli Push")
	var custID uint64
	db.Raw("SELECT user_id FROM sessions WHERE token_hash = ?", HashToken(tok)).Scan(&custID)
	epCust := "https://fcm.googleapis.com/fcm/send/" + tag + "-cust"
	sb := subBody(t, epCust)
	keys := sb["keys"].(map[string]any)
	if err := db.Exec(`INSERT INTO push_subscriptions (user_id, endpoint, endpoint_hash, p256dh, auth) VALUES (?, ?, ?, ?, ?)`,
		custID, epCust, endpointHash(epCust), keys["p256dh"], keys["auth"]).Error; err != nil {
		t.Fatal(err)
	}

	orderNo := createOrderFor(t, h, tok, 4)
	wp.Wait()
	if rec.hits(tag+"-ok") != 1 || rec.hits(tag+"-gone") != 1 || rec.hits(tag+"-cust") != 0 {
		t.Fatalf("pesanan baru: ok=%d gone=%d cust=%d", rec.hits(tag+"-ok"), rec.hits(tag+"-gone"), rec.hits(tag+"-cust"))
	}
	var n int64
	db.Raw("SELECT COUNT(*) FROM push_subscriptions WHERE endpoint_hash = ?", endpointHash(epGone)).Scan(&n)
	if n != 0 {
		t.Fatal("langganan 410 harus dihapus")
	}

	// Batal oleh pelanggan -> push; batal oleh admin -> tidak ada push.
	if r := call(t, h, "POST", "/api/orders/"+orderNo+"/cancel", tok, map[string]any{"reason": "uji"}); r.Code != 200 {
		t.Fatalf("batal: %d %v", r.Code, r.Body)
	}
	wp.Wait()
	if rec.hits(tag+"-ok") != 2 {
		t.Fatalf("batal pelanggan harus mengirim push: %d", rec.hits(tag+"-ok"))
	}
	order2 := createOrderFor(t, h, tok, 4)
	wp.Wait()
	adminSetStatus(t, h, orderIDByNo(db, order2), StatusPending, StatusCancelled)
	wp.Wait()
	if rec.hits(tag+"-ok") != 3 {
		t.Fatalf("batal oleh admin tidak boleh mengirim push (hanya pesanan baru): %d", rec.hits(tag+"-ok"))
	}

	// Admin yang dikeluarkan dari ADMIN_EMAILS tidak dikirimi lagi.
	saved := app.cfg.AdminEmails
	app.cfg.AdminEmails = []string{"lain@uji.test"}
	createOrderFor(t, h, tok, 4)
	wp.Wait()
	app.cfg.AdminEmails = saved
	if rec.hits(tag+"-ok") != 3 {
		t.Fatalf("email di luar ADMIN_EMAILS tidak boleh dikirimi: %d", rec.hits(tag+"-ok"))
	}
	adminCall(t, h, "DELETE", "/api/admin/push/subscribe", integAdmin, map[string]any{"endpoint": epOK})
	db.Exec("DELETE FROM push_subscriptions WHERE endpoint_hash = ?", endpointHash(epCust))
}

var pushSeq int

// userSeqNext: penanda unik per tes (DB uji dipakai bersama semua tes dalam satu putaran).
func userSeqNext() int64 {
	pushSeq++
	return time.Now().UnixNano()%1000000000*100 + int64(pushSeq)
}
