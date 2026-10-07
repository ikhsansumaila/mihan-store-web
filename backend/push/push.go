// Package push mengirim Web Push (VAPID, RFC 8030/8291/8292) ke perangkat ADMIN.
//
// Prinsip (sama dengan notifikasi Discord di paket notify):
//   - Dipanggil SETELAH transaksi commit, asinkron (goroutine), timeout per kiriman.
//     Kegagalan tidak pernah menggagalkan pesanan; hanya dicatat di log TANPA endpoint
//     langganan maupun kunci (hanya id langganan dan host layanan push).
//   - Isi MINIMAL: judul + nomor pesanan + path admin. Tanpa nominal, nama, alamat, telepon.
//   - Layanan push menjawab 404/410 -> langganan sudah tidak berlaku dan dihapus.
//   - Endpoint hanya https ke host layanan push yang dikenal (anti-SSRF), koneksi keluar
//     ditolak bila host me-resolve ke alamat privat/loopback, redirect tidak diikuti.
package push

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Jenis kejadian (sama dengan notify.KindCreated / notify.KindCancelled).
const (
	KindCreated   = "created"
	KindCancelled = "cancelled"
	KindTest      = "test"

	// Kejadian untuk PELANGGAN pemilik pesanan (diubah oleh admin).
	KindCustomerPricing   = "customer.pricing"   // diskon/ongkir/total ditetapkan atau diubah admin
	KindCustomerPaid      = "customer.paid"      // pembayaran diterima
	KindCustomerCompleted = "customer.completed" // pesanan selesai
	KindCustomerCancelled = "customer.cancelled" // dibatalkan oleh admin
)

// Batas validasi langganan.
const (
	MaxEndpointLen = 2000
	DefaultTTL     = 3600 // detik: notifikasi pesanan tidak berguna bila tertunda > 1 jam
)

// Event: data yang BOLEH dikirim. Sengaja tanpa field nominal/nama/alamat/telepon.
type Event struct {
	Kind    string
	OrderNo string
}

// Payload JSON yang dibaca service worker (frontend/public/sw.js).
type Payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

// Subscription: satu langganan push (satu browser/perangkat admin).
type Subscription struct {
	ID       uint64
	Endpoint string
	P256dh   string
	Auth     string
}

// Store: penyimpanan langganan (diimplementasikan di paket main dengan GORM; tiruan di tes).
type Store interface {
	// List mengembalikan langganan ADMIN (audiens admin) yang masih berhak; userID 0 = semua admin.
	List(ctx context.Context, userID uint64) ([]Subscription, error)
	// ListCustomer mengembalikan langganan PELANGGAN (audiens customer) milik userID saja.
	ListCustomer(ctx context.Context, userID uint64) ([]Subscription, error)
	Delete(ctx context.Context, id uint64) error
	Touch(ctx context.Context, id uint64) error
}

// Sender: antarmuka kecil seperti notify.Notifier.
type Sender interface {
	Enabled() bool
	PublicKey() string
	// OrderEvent mengirim ke semua langganan admin secara asinkron (tidak pernah memblokir).
	OrderEvent(e Event)
	// CustomerOrderEvent mengirim ke langganan pelanggan milik userID (pemilik pesanan) saja, asinkron.
	CustomerOrderEvent(userID uint64, e Event)
	// SendTest mengirim satu notifikasi tes (sinkron) ke langganan milik userID.
	// Mengembalikan jumlah terkirim dan jumlah langganan yang dicoba.
	SendTest(ctx context.Context, userID uint64) (sent, total int, err error)
}

// Noop: fitur nonaktif (kunci VAPID belum dikonfigurasi).
type Noop struct{}

func (Noop) Enabled() bool                    { return false }
func (Noop) PublicKey() string                { return "" }
func (Noop) OrderEvent(Event)                 {}
func (Noop) CustomerOrderEvent(uint64, Event) {}
func (Noop) SendTest(context.Context, uint64) (int, int, error) {
	return 0, 0, ErrDisabled
}

var ErrDisabled = errors.New("web push nonaktif")

// ---------- Validasi ----------

// knownPushHosts: host (atau akhiran domain bila diawali ".") layanan push browser utama.
var knownPushHosts = []string{
	"fcm.googleapis.com",                // Chrome, Edge (sebagian), Android, Samsung Internet, Opera, Brave
	"android.googleapis.com",            // FCM lama
	"updates.push.services.mozilla.com", // Firefox
	"web.push.apple.com",                // Safari macOS / iOS 16.4+ (aplikasi layar utama)
	".notify.windows.com",               // Edge (WNS), mis. wns2-xxx.notify.windows.com
	".push.apple.com",                   // cadangan subdomain Apple
}

// HostAllowed: true bila host termasuk layanan push yang dikenal.
func HostAllowed(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(host), ".")
	for _, k := range knownPushHosts {
		if strings.HasPrefix(k, ".") {
			if strings.HasSuffix(h, k) && len(h) > len(k) {
				return true
			}
		} else if h == k {
			return true
		}
	}
	return false
}

// ValidateEndpoint memeriksa endpoint langganan dari browser (anti-SSRF).
func ValidateEndpoint(raw string) error {
	if raw == "" || len(raw) > MaxEndpointLen {
		return errors.New("panjang endpoint tidak valid")
	}
	for _, c := range raw {
		if c <= 0x20 || c >= 0x7f {
			return errors.New("endpoint berisi karakter tidak sah")
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return errors.New("endpoint tidak valid")
	}
	if u.Scheme != "https" {
		return errors.New("endpoint harus https")
	}
	if p := u.Port(); p != "" && p != "443" {
		return errors.New("port endpoint tidak diizinkan")
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil {
		return errors.New("endpoint tidak boleh berupa alamat IP")
	}
	if !HostAllowed(host) {
		return errors.New("host endpoint bukan layanan push yang dikenal")
	}
	return nil
}

func decodeB64URL(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// ValidateKeys memeriksa kunci langganan: p256dh = titik P-256 tak terkompresi (65 byte),
// auth = 16 byte.
func ValidateKeys(p256dh, auth string) error {
	if len(p256dh) > 128 || len(auth) > 64 {
		return errors.New("kunci langganan terlalu panjang")
	}
	pk, err := decodeB64URL(p256dh)
	if err != nil || len(pk) != 65 || pk[0] != 4 {
		return errors.New("kunci p256dh tidak valid")
	}
	if _, err := ecdh.P256().NewPublicKey(pk); err != nil {
		return errors.New("kunci p256dh tidak valid")
	}
	a, err := decodeB64URL(auth)
	if err != nil || len(a) != 16 {
		return errors.New("kunci auth tidak valid")
	}
	return nil
}

// ValidateVAPID memeriksa pasangan kunci VAPID (base64url): privat 32 byte yang menghasilkan
// kunci publik yang sama. Pesan galat tidak pernah memuat nilai kunci.
func ValidateVAPID(pub, priv string) error {
	pb, err := decodeB64URL(pub)
	if err != nil || len(pb) != 65 || pb[0] != 4 {
		return errors.New("VAPID_PUBLIC_KEY bukan kunci publik P-256 base64url (65 byte)")
	}
	kb, err := decodeB64URL(priv)
	if err != nil || len(kb) != 32 {
		return errors.New("VAPID_PRIVATE_KEY bukan kunci privat P-256 base64url (32 byte)")
	}
	k, err := ecdh.P256().NewPrivateKey(kb)
	if err != nil {
		return errors.New("VAPID_PRIVATE_KEY tidak valid")
	}
	if string(k.PublicKey().Bytes()) != string(pb) {
		return errors.New("VAPID_PUBLIC_KEY tidak cocok dengan VAPID_PRIVATE_KEY")
	}
	return nil
}

// NormalizeSubject: "mailto:..." atau "https://..." ; selain itu kosong (tidak valid).
func NormalizeSubject(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, "mailto:") && strings.Contains(s, "@") && !strings.ContainsAny(s, " \t\r\n"):
		return s
	case strings.HasPrefix(s, "https://"):
		if u, err := url.Parse(s); err == nil && u.Host != "" && u.User == nil {
			return s
		}
	}
	return ""
}

// BuildPayload menyusun isi notifikasi minimal.
func BuildPayload(e Event) ([]byte, error) {
	no := strings.TrimSpace(e.OrderNo)
	if e.Kind == KindTest {
		return json.Marshal(Payload{Title: "Tes notifikasi", Body: "Notifikasi Mihan Store berfungsi di perangkat ini.", URL: "/admin", Tag: "tes-notifikasi"})
	}
	switch e.Kind {
	case KindCreated, KindCancelled, KindCustomerPricing, KindCustomerPaid, KindCustomerCompleted, KindCustomerCancelled:
	default:
		return nil, fmt.Errorf("jenis kejadian tidak dikenal: %q", e.Kind)
	}
	if no == "" || len(no) > 32 || strings.ContainsAny(no, "/?#\\ ") {
		return nil, errors.New("nomor pesanan tidak valid")
	}
	esc := url.PathEscape(no)
	p := Payload{Body: no, URL: "/admin/orders/" + esc, Tag: "pesanan-" + no}
	switch e.Kind {
	case KindCreated:
		p.Title = "Pesanan baru"
	case KindCancelled:
		p.Title = "Pesanan dibatalkan"
	default:
		// Pelanggan: tautan ke halaman pesanan miliknya; isi tanpa nominal/alamat/telepon.
		p.URL = "/pesanan/" + esc
		switch e.Kind {
		case KindCustomerPricing:
			p.Title, p.Body = "Ongkir sudah dikonfirmasi", "Pesanan "+no+": silakan cek total dan lanjut pembayaran"
		case KindCustomerPaid:
			p.Title, p.Body = "Pembayaran diterima", "Pesanan "+no+": pembayaran sudah kami terima"
		case KindCustomerCompleted:
			p.Title, p.Body = "Pesanan selesai", "Pesanan "+no+" telah selesai. Terima kasih!"
		case KindCustomerCancelled:
			p.Title, p.Body = "Pesanan dibatalkan", "Pesanan "+no+" dibatalkan oleh toko"
		}
	}
	return json.Marshal(p)
}

// ---------- Pengirim ----------

// HTTPClient: klien HTTP (bisa diganti tiruan di tes).
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// WebPush mengirim lewat layanan push browser.
type WebPush struct {
	pub, priv, subject string
	store              Store
	client             HTTPClient
	timeout            time.Duration
	ttl                int
	logf               func(format string, args ...any)
	wg                 sync.WaitGroup
}

// Option mengubah perilaku WebPush (dipakai tes).
type Option func(*WebPush)

func WithHTTPClient(c HTTPClient) Option     { return func(w *WebPush) { w.client = c } }
func WithTimeout(d time.Duration) Option     { return func(w *WebPush) { w.timeout = d } }
func WithLogf(f func(string, ...any)) Option { return func(w *WebPush) { w.logf = f } }

// New membuat pengirim. Kunci kosong/tidak valid -> Noop + peringatan (tanpa nilai kunci).
func New(pub, priv, subject string, store Store, opts ...Option) Sender {
	pub, priv = strings.TrimSpace(pub), strings.TrimSpace(priv)
	if pub == "" || priv == "" {
		log.Println("PERINGATAN: Web Push admin dinonaktifkan (VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY kosong)")
		return Noop{}
	}
	if err := ValidateVAPID(pub, priv); err != nil {
		log.Printf("PERINGATAN: Web Push admin dinonaktifkan (%v)", err)
		return Noop{}
	}
	sub := NormalizeSubject(subject)
	if sub == "" {
		log.Println("PERINGATAN: Web Push admin dinonaktifkan (VAPID_SUBJECT harus mailto:... atau https://...)")
		return Noop{}
	}
	if store == nil {
		return Noop{}
	}
	w := &WebPush{pub: pub, priv: priv, subject: sub, store: store, timeout: 10 * time.Second, ttl: DefaultTTL, logf: log.Printf}
	for _, o := range opts {
		o(w)
	}
	if w.client == nil {
		w.client = SafeHTTPClient(w.timeout)
	}
	log.Println("Web Push admin aktif")
	return w
}

func (w *WebPush) Enabled() bool     { return true }
func (w *WebPush) PublicKey() string { return w.pub }

// Wait menunggu semua kiriman asinkron selesai (dipakai tes).
func (w *WebPush) Wait() { w.wg.Wait() }

// OrderEvent: asinkron, tidak pernah memblokir pemanggil.
func (w *WebPush) OrderEvent(e Event) {
	if IsCustomerKind(e.Kind) || e.Kind == KindTest {
		w.logf("web push: kejadian %q bukan untuk admin", e.Kind)
		return
	}
	body, err := BuildPayload(e)
	if err != nil {
		w.logf("web push: gagal menyusun pesan: %v", err)
		return
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer func() {
			if v := recover(); v != nil {
				w.logf("web push: panic: %v", v)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		subs, err := w.store.List(ctx, 0)
		if err != nil {
			w.logf("web push: gagal membaca langganan: %v", err)
			return
		}
		for _, s := range subs {
			w.deliver(ctx, s, body)
		}
	}()
}

// IsCustomerKind: true untuk kejadian yang ditujukan ke pelanggan.
func IsCustomerKind(k string) bool {
	switch k {
	case KindCustomerPricing, KindCustomerPaid, KindCustomerCompleted, KindCustomerCancelled:
		return true
	}
	return false
}

// CustomerOrderEvent: asinkron; hanya ke langganan pelanggan milik userID (pemilik pesanan).
func (w *WebPush) CustomerOrderEvent(userID uint64, e Event) {
	if userID == 0 || !IsCustomerKind(e.Kind) {
		w.logf("web push: kejadian pelanggan tidak sah (%q)", e.Kind)
		return
	}
	body, err := BuildPayload(e)
	if err != nil {
		w.logf("web push: gagal menyusun pesan pelanggan: %v", err)
		return
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer func() {
			if v := recover(); v != nil {
				w.logf("web push: panic: %v", v)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		subs, err := w.store.ListCustomer(ctx, userID)
		if err != nil {
			w.logf("web push: gagal membaca langganan pelanggan: %v", err)
			return
		}
		for _, s := range subs {
			w.deliver(ctx, s, body)
		}
	}()
}

func (w *WebPush) SendTest(ctx context.Context, userID uint64) (int, int, error) {
	body, err := BuildPayload(Event{Kind: KindTest})
	if err != nil {
		return 0, 0, err
	}
	subs, err := w.store.List(ctx, userID)
	if err != nil {
		return 0, 0, err
	}
	sent := 0
	for _, s := range subs {
		if w.deliver(ctx, s, body) {
			sent++
		}
	}
	return sent, len(subs), nil
}

func hostOf(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil {
		return u.Hostname()
	}
	return "?"
}

// deliver mengirim satu notifikasi. true = diterima layanan push (2xx).
func (w *WebPush) deliver(ctx context.Context, s Subscription, body []byte) bool {
	// Endpoint di DB tetap divalidasi ulang sebelum dipakai (pertahanan berlapis).
	if err := ValidateEndpoint(s.Endpoint); err != nil {
		w.logf("web push: langganan #%d dilewati (endpoint tidak sah) dan dihapus", s.ID)
		_ = w.store.Delete(ctx, s.ID)
		return false
	}
	sctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()
	// Pustaka menambah padding ke buffer pesan: beri salinan tersendiri.
	msg := append([]byte(nil), body...)
	subject := strings.TrimPrefix(w.subject, "mailto:") // pustaka menambah "mailto:" sendiri bila bukan https
	resp, err := webpush.SendNotificationWithContext(sctx, msg, &webpush.Subscription{
		Endpoint: s.Endpoint, Keys: webpush.Keys{P256dh: s.P256dh, Auth: s.Auth},
	}, &webpush.Options{
		HTTPClient: w.client, Subscriber: subject, TTL: w.ttl, Urgency: webpush.UrgencyNormal,
		VAPIDPublicKey: w.pub, VAPIDPrivateKey: w.priv,
	})
	if err != nil {
		w.logf("web push: langganan #%d (%s): gagal mengirim: %s", s.ID, hostOf(s.Endpoint), sanitizeErr(err, s.Endpoint))
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		_ = w.store.Touch(ctx, s.ID)
		return true
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		if err := w.store.Delete(ctx, s.ID); err != nil {
			w.logf("web push: langganan #%d kedaluwarsa tetapi gagal dihapus: %v", s.ID, err)
		} else {
			w.logf("web push: langganan #%d (%s) kedaluwarsa (HTTP %d), dihapus", s.ID, hostOf(s.Endpoint), resp.StatusCode)
		}
	default:
		w.logf("web push: langganan #%d (%s): layanan push menjawab HTTP %d", s.ID, hostOf(s.Endpoint), resp.StatusCode)
	}
	return false
}

// sanitizeErr membuang endpoint (URL berisi token perangkat) dari pesan galat.
func sanitizeErr(err error, endpoint string) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	msg := err.Error()
	if endpoint != "" {
		msg = strings.ReplaceAll(msg, endpoint, "<endpoint>")
	}
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

// ---------- Klien HTTP aman ----------

// publicIP: false untuk loopback, privat, link-local, unspecified, multicast, CGNAT, dll.
func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10 (CGNAT), 0.0.0.0/8, 192.0.0.0/24, 198.18.0.0/15, 240.0.0.0/4
		switch {
		case v4[0] == 100 && v4[1]&0xc0 == 64, v4[0] == 0, v4[0] == 192 && v4[1] == 0 && v4[2] == 0,
			v4[0] == 198 && (v4[1] == 18 || v4[1] == 19), v4[0] >= 240:
			return false
		}
	}
	return true
}

// SafeHTTPClient: tanpa proxy dari env, tanpa redirect, koneksi ke IP non-publik ditolak.
func SafeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if !publicIP(net.ParseIP(host)) {
				return errors.New("alamat tujuan bukan IP publik")
			}
			return nil
		},
	}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	return &http.Client{
		Timeout:       timeout,
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
