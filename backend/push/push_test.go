package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func vapidKeys(t *testing.T) (pub, priv string) {
	t.Helper()
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// clientKeys membuat kunci langganan browser tiruan (p256dh + auth).
func clientKeys(t *testing.T) (string, string) {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a := make([]byte, 16)
	rand.Read(a)
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(a)
}

type memStore struct {
	mu      sync.Mutex
	subs    []Subscription
	owner   map[uint64]uint64
	deleted []uint64
	touched []uint64
	cust    map[uint64][]Subscription // langganan pelanggan per user
}

func (m *memStore) ListCustomer(_ context.Context, userID uint64) ([]Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Subscription(nil), m.cust[userID]...), nil
}

func (m *memStore) List(_ context.Context, userID uint64) ([]Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Subscription
	for _, s := range m.subs {
		if userID == 0 || m.owner[s.ID] == userID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *memStore) Delete(_ context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted = append(m.deleted, id)
	for i, s := range m.subs {
		if s.ID == id {
			m.subs = append(m.subs[:i], m.subs[i+1:]...)
			break
		}
	}
	return nil
}

func (m *memStore) Touch(_ context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.touched = append(m.touched, id)
	return nil
}

// fakeClient menjawab berdasarkan endpoint dan merekam permintaan (tanpa jaringan).
type fakeClient struct {
	mu     sync.Mutex
	status map[string]int
	reqs   []*http.Request
	bodies [][]byte
}

func (f *fakeClient) Do(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, _ := io.ReadAll(r.Body)
	f.reqs = append(f.reqs, r)
	f.bodies = append(f.bodies, b)
	code := f.status[r.URL.String()]
	if code == 0 {
		code = 201
	}
	if code < 0 {
		return nil, fmt.Errorf("Post %q: dial tcp: connection refused", r.URL.String())
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
}

func TestValidateEndpointSSRF(t *testing.T) {
	ok := []string{
		"https://fcm.googleapis.com/fcm/send/abc:def",
		"https://updates.push.services.mozilla.com/wpush/v2/gAAAA",
		"https://web.push.apple.com/QGx7abc",
		"https://wns2-par02p.notify.windows.com/w/?token=BQYAAAB",
		"https://fcm.googleapis.com:443/fcm/send/x",
	}
	for _, e := range ok {
		if err := ValidateEndpoint(e); err != nil {
			t.Errorf("%s harus diterima: %v", e, err)
		}
	}
	bad := []string{
		"",
		"http://fcm.googleapis.com/fcm/send/x", // bukan https
		"https://127.0.0.1/x",                  // IP
		"https://[::1]/x",                      // IP v6
		"https://169.254.169.254/latest/meta-data",                          // metadata cloud
		"https://localhost/x",                                               // localhost
		"https://mysql_db:3306/x",                                           // host internal
		"https://172.22.0.7:8080/api/admin/me",                              // backend sendiri
		"https://fcm.googleapis.com.evil.test/x",                            // akhiran palsu
		"https://evilfcm.googleapis.com/x",                                  // host lain
		"https://notify.windows.com/x",                                      // akar tanpa subdomain
		"https://user:pw@fcm.googleapis.com/x",                              // userinfo
		"https://fcm.googleapis.com:8443/x",                                 // port lain
		"https://fcm.googleapis.com/x#frag",                                 // fragmen
		"https://fcm.googleapis.com/x y",                                    // spasi
		"https://fcm.googleapis.com/\x00",                                   // kontrol
		"javascript:alert(1)",                                               // skema lain
		"https://fcm.googleapis.com/" + strings.Repeat("a", MaxEndpointLen), // terlalu panjang
	}
	for _, e := range bad {
		if err := ValidateEndpoint(e); err == nil {
			t.Errorf("%q harus ditolak", e)
		}
	}
}

func TestPublicIP(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "172.22.0.7", "192.168.1.1", "169.254.169.254", "::1", "fc00::1",
		"fe80::1", "0.0.0.0", "100.64.0.1", "224.0.0.1"} {
		if publicIP(net.ParseIP(s)) {
			t.Errorf("%s bukan IP publik", s)
		}
	}
	for _, s := range []string{"142.250.4.95", "17.253.144.10", "2607:f8b0:4004::5f"} {
		if !publicIP(net.ParseIP(s)) {
			t.Errorf("%s harus publik", s)
		}
	}
}

func TestSafeHTTPClientRefusesPrivate(t *testing.T) {
	c := SafeHTTPClient(2 * time.Second)
	_, err := c.Get("http://127.0.0.1:1/")
	if err == nil || !strings.Contains(err.Error(), "bukan IP publik") {
		t.Fatalf("koneksi ke loopback harus ditolak dialer: %v", err)
	}
}

func TestValidateKeysAndVAPID(t *testing.T) {
	p, a := clientKeys(t)
	if err := ValidateKeys(p, a); err != nil {
		t.Fatal(err)
	}
	// Padding '=' dan base64 standar juga diterima (beberapa browser lama).
	if err := ValidateKeys(p+"=", a+"=="); err != nil {
		t.Fatal(err)
	}
	if ValidateKeys(a, a) == nil || ValidateKeys(p, p) == nil || ValidateKeys("", a) == nil || ValidateKeys(p, "!!") == nil {
		t.Fatal("kunci rusak harus ditolak")
	}
	bad := append([]byte{4}, make([]byte, 64)...) // bukan titik di kurva
	if ValidateKeys(base64.RawURLEncoding.EncodeToString(bad), a) == nil {
		t.Fatal("titik di luar kurva harus ditolak")
	}
	pub, priv := vapidKeys(t)
	if err := ValidateVAPID(pub, priv); err != nil {
		t.Fatal(err)
	}
	pub2, _ := vapidKeys(t)
	err := ValidateVAPID(pub2, priv)
	if err == nil {
		t.Fatal("pasangan tidak cocok harus ditolak")
	}
	if strings.Contains(err.Error(), priv) || strings.Contains(err.Error(), pub2) {
		t.Fatal("pesan galat memuat kunci")
	}
	if ValidateVAPID("x", "y") == nil {
		t.Fatal("kunci rusak harus ditolak")
	}
}

func TestNewNoopWhenUnconfigured(t *testing.T) {
	st := &memStore{}
	if s := New("", "", "https://store.mihan.web.id", st); s.Enabled() {
		t.Fatal("tanpa kunci harus Noop")
	}
	pub, priv := vapidKeys(t)
	if s := New(pub, priv, "ftp://x", st); s.Enabled() {
		t.Fatal("subject tidak valid harus Noop")
	}
	if s := New(pub, "AAAA", "https://store.mihan.web.id", st); s.Enabled() {
		t.Fatal("kunci rusak harus Noop")
	}
	s := New(pub, priv, "mailto:toko@mihan.web.id", st, WithHTTPClient(&fakeClient{}))
	if !s.Enabled() || s.PublicKey() != pub {
		t.Fatal("harus aktif dengan kunci publik yang sama")
	}
	if _, _, err := (Noop{}).SendTest(context.Background(), 1); err != ErrDisabled {
		t.Fatal("Noop.SendTest harus ErrDisabled")
	}
}

func TestBuildPayloadMinimal(t *testing.T) {
	b, err := BuildPayload(Event{Kind: KindCreated, OrderNo: "MS-261002-0001", Customer: "Bu Siti Toko Maju", Recipient: "Siti Aminah"})
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]string
	json.Unmarshal(b, &p)
	if p["title"] != "Pesanan Baru" || p["body"] != "dari Bu Siti Toko Maju\npenerima Siti Aminah\nMS-261002-0001" || p["url"] != "/admin/orders/MS-261002-0001" || p["tag"] != "pesanan-MS-261002-0001" || len(p) != 4 {
		t.Fatalf("payload: %v", p)
	}
	b, _ = BuildPayload(Event{Kind: KindCancelled, OrderNo: "MS-261002-0002", Customer: "Budi"})
	json.Unmarshal(b, &p)
	if p["title"] != "Pesanan Dibatalkan" || p["tag"] != "pesanan-MS-261002-0002" || p["body"] != "dari Budi\npenerima -\nMS-261002-0002" {
		t.Fatalf("payload batal: %v", p)
	}
	for _, e := range []Event{{Kind: "paid", OrderNo: "MS-1"}, {Kind: KindCreated}, {Kind: KindCreated, OrderNo: "../x"}} {
		if _, err := BuildPayload(e); err == nil {
			t.Errorf("%+v harus ditolak", e)
		}
	}
}

func TestOrderEventSendsAndRemovesGone(t *testing.T) {
	pub, priv := vapidKeys(t)
	p1, a1 := clientKeys(t)
	p2, a2 := clientKeys(t)
	p3, a3 := clientKeys(t)
	st := &memStore{subs: []Subscription{
		{ID: 1, Endpoint: "https://fcm.googleapis.com/fcm/send/tokD", P256dh: p1, Auth: a1},
		{ID: 2, Endpoint: "https://web.push.apple.com/tokA410", P256dh: p2, Auth: a2},
		{ID: 3, Endpoint: "https://updates.push.services.mozilla.com/wpush/v2/tokB404", P256dh: p3, Auth: a3},
		{ID: 4, Endpoint: "https://fcm.googleapis.com/fcm/send/error", P256dh: p1, Auth: a1},
		{ID: 5, Endpoint: "https://fcm.googleapis.com/fcm/send/tokC", P256dh: p1, Auth: a1},
		{ID: 6, Endpoint: "http://10.0.0.1/tersusup", P256dh: p1, Auth: a1}, // baris rusak di DB
	}}
	fc := &fakeClient{status: map[string]int{
		"https://web.push.apple.com/tokA410":                         410,
		"https://updates.push.services.mozilla.com/wpush/v2/tokB404": 404,
		"https://fcm.googleapis.com/fcm/send/error":                  500,
		"https://fcm.googleapis.com/fcm/send/tokC":                   -1,
	}}
	var logMu sync.Mutex
	var logs []string
	s := New(pub, priv, "https://store.mihan.web.id", st, WithHTTPClient(fc), WithLogf(func(f string, a ...any) {
		logMu.Lock()
		logs = append(logs, fmt.Sprintf(f, a...))
		logMu.Unlock()
	})).(*WebPush)

	start := time.Now()
	s.OrderEvent(Event{Kind: KindCreated, OrderNo: "MS-261007-0042"})
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("OrderEvent harus asinkron")
	}
	s.Wait()

	if len(fc.reqs) != 5 {
		t.Fatalf("harus 5 permintaan (endpoint tersusup tidak dikirim), dapat %d", len(fc.reqs))
	}
	r := fc.reqs[0]
	if r.Header.Get("TTL") != "3600" || r.Header.Get("Urgency") != "normal" || r.Header.Get("Content-Encoding") != "aes128gcm" ||
		!strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") || !strings.Contains(r.Header.Get("Authorization"), "k="+pub) {
		t.Fatalf("header kiriman salah: %v", r.Header)
	}
	// Isi terenkripsi: nomor pesanan tidak terlihat mentah.
	if strings.Contains(string(fc.bodies[0]), "MS-261007-0042") {
		t.Fatal("payload tidak terenkripsi")
	}
	if fmt.Sprint(st.deleted) != "[2 3 6]" {
		t.Fatalf("langganan 404/410/tersusup harus dihapus: %v", st.deleted)
	}
	if fmt.Sprint(st.touched) != "[1]" {
		t.Fatalf("hanya kiriman sukses yang ditandai: %v", st.touched)
	}
	all := strings.Join(logs, "\n")
	for _, secret := range []string{"tokA410", "tokB404", "tokC", "tokD", "/fcm/send/", priv, p1, a1} {
		if strings.Contains(all, secret) {
			t.Fatalf("log memuat endpoint/kunci (%q):\n%s", secret, all)
		}
	}
	if !strings.Contains(all, "HTTP 500") || !strings.Contains(all, "#5") {
		t.Fatalf("galat harus dicatat: %s", all)
	}
}

func TestSendTestOnlyOwnSubscriptions(t *testing.T) {
	pub, priv := vapidKeys(t)
	p, a := clientKeys(t)
	st := &memStore{
		subs: []Subscription{
			{ID: 1, Endpoint: "https://fcm.googleapis.com/fcm/send/milik-1", P256dh: p, Auth: a},
			{ID: 2, Endpoint: "https://fcm.googleapis.com/fcm/send/milik-2", P256dh: p, Auth: a},
		},
		owner: map[uint64]uint64{1: 10, 2: 20},
	}
	fc := &fakeClient{}
	s := New(pub, priv, "https://store.mihan.web.id", st, WithHTTPClient(fc))
	sent, total, err := s.SendTest(context.Background(), 10)
	if err != nil || sent != 1 || total != 1 || len(fc.reqs) != 1 || !strings.HasSuffix(fc.reqs[0].URL.Path, "milik-1") {
		t.Fatalf("tes harus hanya ke langganan sendiri: %d/%d %v", sent, total, err)
	}
}

// TestPayloadDecryptable memastikan kiriman bisa didekripsi penerima (RFC 8291) dan isinya minimal.
func TestPayloadDecryptable(t *testing.T) {
	pub, priv := vapidKeys(t)
	ck, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	rand.Read(auth)
	st := &memStore{subs: []Subscription{{ID: 1, Endpoint: "https://fcm.googleapis.com/fcm/send/x",
		P256dh: base64.RawURLEncoding.EncodeToString(ck.PublicKey().Bytes()), Auth: base64.RawURLEncoding.EncodeToString(auth)}}}
	fc := &fakeClient{}
	s := New(pub, priv, "https://store.mihan.web.id", st, WithHTTPClient(fc)).(*WebPush)
	s.OrderEvent(Event{Kind: KindCancelled, OrderNo: "MS-261007-0001", Customer: "Ani", Recipient: "Ani"})
	s.Wait()
	plain, err := decryptAES128GCM(fc.bodies[0], ck, auth)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]string
	if err := json.Unmarshal(plain, &p); err != nil {
		t.Fatalf("plaintext bukan JSON: %q", plain)
	}
	if p["title"] != "Pesanan Dibatalkan" || p["body"] != "dari Ani\npenerima Ani\nMS-261007-0001" || p["url"] != "/admin/orders/MS-261007-0001" {
		t.Fatalf("isi: %v", p)
	}
}

func TestBuildPayloadCustomer(t *testing.T) {
	cases := map[string][2]string{
		KindCustomerPricing:   {"Ongkir sudah dikonfirmasi", "Pesanan MS-261007-0009: silakan cek total dan lanjut pembayaran"},
		KindCustomerPaid:      {"Pembayaran diterima", "Pesanan MS-261007-0009: pembayaran sudah kami terima"},
		KindCustomerCompleted: {"Pesanan selesai", "Pesanan MS-261007-0009 telah selesai. Terima kasih!"},
		KindCustomerCancelled: {"Pesanan dibatalkan", "Pesanan MS-261007-0009 dibatalkan oleh toko"},
	}
	for k, want := range cases {
		b, err := BuildPayload(Event{Kind: k, OrderNo: "MS-261007-0009"})
		if err != nil {
			t.Fatal(err)
		}
		var p map[string]string
		json.Unmarshal(b, &p)
		if p["title"] != want[0] || p["body"] != want[1] || p["url"] != "/pesanan/MS-261007-0009" || p["tag"] != "pesanan-MS-261007-0009" || len(p) != 4 {
			t.Errorf("%s: %v", k, p)
		}
		if strings.Contains(string(b), "Rp") || strings.Contains(string(b), "/admin") {
			t.Errorf("%s: isi tidak minimal: %s", k, b)
		}
	}
}

// Audiens terpisah: kejadian admin hanya ke List (admin), kejadian pelanggan hanya ke ListCustomer(pemilik).
func TestAudienceSeparation(t *testing.T) {
	pub, priv := vapidKeys(t)
	p, a := clientKeys(t)
	st := &memStore{
		subs: []Subscription{{ID: 1, Endpoint: "https://fcm.googleapis.com/fcm/send/admin1", P256dh: p, Auth: a}},
		cust: map[uint64][]Subscription{
			7: {{ID: 2, Endpoint: "https://fcm.googleapis.com/fcm/send/cust7", P256dh: p, Auth: a}},
			8: {{ID: 3, Endpoint: "https://fcm.googleapis.com/fcm/send/cust8", P256dh: p, Auth: a}},
		},
	}
	fc := &fakeClient{}
	s := New(pub, priv, "https://store.mihan.web.id", st, WithHTTPClient(fc), WithLogf(func(string, ...any) {})).(*WebPush)
	s.CustomerOrderEvent(7, Event{Kind: KindCustomerPaid, OrderNo: "MS-1"})
	s.Wait()
	if len(fc.reqs) != 1 || !strings.HasSuffix(fc.reqs[0].URL.Path, "cust7") {
		t.Fatalf("pelanggan: hanya pemilik pesanan: %d", len(fc.reqs))
	}
	s.OrderEvent(Event{Kind: KindCreated, OrderNo: "MS-1"})
	s.Wait()
	if len(fc.reqs) != 2 || !strings.HasSuffix(fc.reqs[1].URL.Path, "admin1") {
		t.Fatal("admin: hanya langganan admin")
	}
	// Jenis yang salah alamat ditolak.
	s.OrderEvent(Event{Kind: KindCustomerPaid, OrderNo: "MS-1"})
	s.CustomerOrderEvent(7, Event{Kind: KindCreated, OrderNo: "MS-1"})
	s.CustomerOrderEvent(0, Event{Kind: KindCustomerPaid, OrderNo: "MS-1"})
	s.Wait()
	if len(fc.reqs) != 2 {
		t.Fatalf("kejadian salah audiens tidak boleh terkirim: %d", len(fc.reqs))
	}
}

func TestCleanName(t *testing.T) {
	long := strings.Repeat("a", 60)
	cases := map[string]string{
		"Budi Santoso":                "Budi Santoso",
		"  Bu   Siti\tToko  ":         "Bu Siti Toko",
		"Ani\n\npenerima palsu\nMS-1": "Ani penerima palsu MS-1",
		"Andi\x00\x07\x1b[31m":        "Andi [31m",
		"a\u202eb\u200bc":             "abc",
		"":                            "-",
		"   \n\t ":                    "-",
		long:                          strings.Repeat("a", MaxNameRunes) + "…",
		strings.Repeat("é", 45):       strings.Repeat("é", MaxNameRunes) + "…",
		"Nama Panjang Sekali Dengan Spasi Di Ujung X": "Nama Panjang Sekali Dengan Spasi Di Ujun…",
		"bad\xffutf8": "badutf8",
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, mau %q", in, got, want)
		}
	}
}

func TestAdminBodyNamesSafe(t *testing.T) {
	// Alias kosong -> pemanggil sudah memilih nama akun; penerima kosong -> "-"; newline hanya dari format.
	b, err := BuildPayload(Event{Kind: KindCreated, OrderNo: "MS-261007-0003", Customer: "Eve\nPesanan Dibatalkan", Recipient: strings.Repeat("R", 80)})
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]string
	json.Unmarshal(b, &p)
	lines := strings.Split(p["body"], "\n")
	if len(lines) != 3 || lines[0] != "dari Eve Pesanan Dibatalkan" || lines[1] != "penerima "+strings.Repeat("R", MaxNameRunes)+"…" || lines[2] != "MS-261007-0003" {
		t.Fatalf("body: %q", p["body"])
	}
	if len(p["body"]) > 200 {
		t.Fatalf("body terlalu panjang untuk sw.js (dipotong 200): %d", len(p["body"]))
	}
}
