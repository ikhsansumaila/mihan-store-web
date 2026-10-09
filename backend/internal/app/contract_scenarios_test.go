//go:build contract

package app

// Perakitan server uji + skenario kontrak. Urutan bagian TETAP (id auto-increment ikut kontrak).
// Seluruh dependensi luar diganti tiruan: Turnstile (server lokal), JWKS Google/Access (kunci uji),
// notifier Discord & pusher Web Push (perekam), jam (melangkah 1 detik per pemanggilan),
// penyimpanan foto & bukti transfer (folder sementara), sumber data wilayah (server tiruan).

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"mihanstore/internal/admin"
	"mihanstore/internal/catalog/media"
	"mihanstore/internal/identity"
	"mihanstore/internal/notify"
	"mihanstore/internal/orders"
	"mihanstore/internal/platform"
	"mihanstore/internal/push"
	"mihanstore/internal/regions"
	"mihanstore/internal/regions/regionstest"
)

// ---------- tiruan ----------

type contractNotifier struct {
	mu sync.Mutex
	ev []string
}

func (n *contractNotifier) OrderEvent(e notify.OrderEvent) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ev = append(n.ev, fmt.Sprintf("discord %s %s status=%s items=%d total=%d customer=%q", e.Kind, e.OrderNo, e.Status, e.ItemCount, e.Total, e.CustomerName))
}

// contractPushRec: pusher tiruan (selalu aktif) yang hanya mencatat kejadian.
type contractPushRec struct {
	mu sync.Mutex
	ev []string
}

func (p *contractPushRec) Enabled() bool { return true }
func (p *contractPushRec) PublicKey() string {
	return "BContractTestPublicKey-000000000000000000000000000000000000000000000000000000000"
}
func (p *contractPushRec) OrderEvent(e push.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ev = append(p.ev, fmt.Sprintf("push admin %s %s customer=%q recipient=%q", e.Kind, e.OrderNo, e.Customer, e.Recipient))
}
func (p *contractPushRec) CustomerOrderEvent(uid uint64, e push.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ev = append(p.ev, fmt.Sprintf("push customer(user %d) %s %s fee=%d total=%d", uid, e.Kind, e.OrderNo, e.ShippingFee, e.Total))
}
func (p *contractPushRec) SendTest(ctx context.Context, uid uint64) (int, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ev = append(p.ev, fmt.Sprintf("push test (user %d)", uid))
	return 1, 1, nil
}

const (
	cAdmin    = "admin.kontrak@uji.test"
	cOther    = "bukan.admin@uji.test"
	cPassword = "passwordku123"
)

func accessToken2(t *testing.T, email string) string {
	return signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "email", email))
}

func multipartBody(buf *bytes.Buffer, data []byte, fname string) string {
	mw := multipart.NewWriter(buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, fname))
	h.Set("Content-Type", "application/octet-stream")
	fw, _ := mw.CreatePart(h)
	fw.Write(data)
	mw.Close()
	return mw.FormDataContentType()
}

// testPNG: gambar kecil deterministik (gradasi warna).
func testPNG(w, h int, seed uint8) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x*255/w) ^ seed, uint8(y*255/h) + seed, seed, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func newContractEnv(t *testing.T) *contractEnv {
	t.Helper()
	cfg := platform.LoadConfig()
	if !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatalf("tes kontrak hanya boleh memakai database *_test, bukan %q", cfg.DBName)
	}
	db, err := platform.OpenDB(cfg)
	if err != nil {
		t.Fatalf("koneksi DB uji: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Write([]byte(`{"success":` + map[bool]string{true: "true", false: "false"}[r.Form.Get("response") == "lulus"] + `}`))
	}))
	t.Cleanup(ts.Close)

	cfg.TurnstileSecret = "secret-uji"
	cfg.AllowNoTurnstile = false
	cfg.AdminEmails = []string{cAdmin}
	cfg.CFAccessTeamDomain, cfg.CFAccessAUD = testTeam, testAUD
	cfg.GoogleClientID, cfg.AuthHMACSecret = testGoogleClient, strings.Repeat("h", 48)
	cfg.UploadsDir = t.TempDir()
	cfg.PaymentProofDir = t.TempDir() + "/private-uploads/payment-proofs"
	identity.InitDummyHash()
	av, err := admin.NewAccessVerifier(cfg)
	if err != nil {
		t.Fatal(err)
	}
	av.JWKS = stubJWKS(map[string]*rsa.PublicKey{"k1": &testKey.PublicKey})
	gv := identity.NewGoogleVerifier(cfg)
	gv.JWKS = stubJWKS(map[string]*rsa.PublicKey{"g1": &testKey.PublicKey})

	// Server tiruan sumber data wilayah (tidak pernah memanggil wilayah.id asli).
	m := regionstest.NewServer()
	rs := httptest.NewServer(m)
	t.Cleanup(rs.Close)
	rcfg := regions.DefaultFetchConfig(rs.URL)
	rcfg.Interval, rcfg.MinProvinces, rcfg.BackoffBase, rcfg.BackoffMax, rcfg.Timeout = 0, 1, time.Millisecond, 5*time.Millisecond, 3*time.Second

	c := &contractEnv{t: t, db: db, norm: newNormalizer(), names: map[string]bool{}, seen: map[string]map[int]int{}}
	c.clock.Store(time.Date(2026, 3, 10, 5, 0, 0, 0, time.UTC).UnixMilli())
	c.pushR = &contractPushRec{}
	// Perakitan sama dengan produksi (Build); dependensi luar ditukar lewat opsi.
	app := Build(cfg,
		WithTurnstileEndpoint(ts.URL),
		WithAccessVerifier(av),
		WithGoogleVerifier(gv),
		WithRateLimit(1000000, time.Minute),
		WithImageStore(media.NewLocalStore(cfg.UploadsDir, "/uploads/"), cfg.MaxUploadsMB),
		WithProofStore(orders.NewProofStore(cfg.PaymentProofDir)),
		WithClock(func() time.Time { return time.UnixMilli(c.clock.Add(1000)).UTC() }),
		WithPusher(c.pushR),
		WithNotifier(&contractNotifier{}),
		WithRegionFetch(rcfg, 0),
	)
	c.app = app
	app.db.Store(db)
	seedRegionsContract(t, c)
	app.regions.InvalidateCache()
	c.app.afterConnect = nil
	c.h = app.Handler()
	c.db = db
	return c
}

func seedRegionsContract(t *testing.T, c *contractEnv) {
	t.Helper()
	keys := make([]string, 0)
	tree := regionstest.Tree()
	for k := range tree {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		items := tree[key]
		level, parent, _ := regions.ParseDatasetKey(key)
		hash, data := regions.ItemsHash(regionItems(items))
		var p any
		if parent != "" {
			p = parent
		}
		if err := c.db.Exec(`INSERT INTO region_datasets (dataset_key, level, parent_code, data, item_count, content_hash, source_updated_at)
			VALUES (?, ?, ?, ?, ?, ?, '2025-07-04')`, key, level, p, string(data), len(items), hash).Error; err != nil {
			t.Fatalf("seed wilayah %s: %v", key, err)
		}
	}
}

func checkoutPayload(key string) map[string]any {
	return map[string]any{"recipientName": "Budi Penerima", "recipientPhone": "0813-1111-2222", "address": "Jl. Melati No. 9, RT 3",
		"city": "Tangerang", "postalCode": "15111", "note": "Titip di pos satpam", "idempotencyKey": key,
		"provinceCode": "36", "regencyCode": "36.71", "districtCode": "36.71.01", "villageCode": "36.71.01.1001"}
}

func (c *contractEnv) must(r cres, code int, what string) cres {
	c.t.Helper()
	if r.Code != code {
		c.t.Fatalf("%s: status %d (mau %d): %s", what, r.Code, code, string(r.Raw))
	}
	return r
}

func str(v any) string { s, _ := v.(string); return s }
func numf(v any) int64 { f, _ := v.(float64); return int64(f) }

func (c *contractEnv) orderID(no string) uint64 {
	var id uint64
	c.db.Raw("SELECT id FROM orders WHERE order_no = ?", no).Scan(&id)
	return id
}

func regBody(u, e, p, name, pw string) map[string]any {
	return map[string]any{"username": u, "email": e, "phone": p, "name": name, "password": pw, "turnstileToken": "lulus"}
}

// idem menghasilkan kunci idempotensi UUID yang deterministik per urutan.
var idemSeq int

func idem() string {
	idemSeq++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", idemSeq)
}

// ---------- tes utama ----------

func TestContract(t *testing.T) {
	c := newContractEnv(t)
	sections := []struct {
		file string
		fn   func(c *contractEnv)
	}{
		{"01_public.json", secPublic},
		{"02_auth.json", secAuth},
		{"03_admin_access.json", secAdminAccess},
		{"04_admin_catalog.json", secAdminCatalog},
		{"05_cart.json", secCart},
		{"06_orders_checkout.json", secCheckout},
		{"07_orders_admin_flow.json", secOrderFlow},
		{"08_orders_proof.json", secProof},
		{"09_orders_status_cancel.json", secStatusCancel},
		{"10_admin_misc.json", secAdminMisc},
		{"11_push.json", secPush},
		{"12_regions_admin.json", secRegionsAdmin},
	}
	for _, s := range sections {
		s := s
		t.Run(strings.TrimSuffix(s.file, ".json"), func(st *testing.T) {
			c.t = st
			s.fn(c)
			c.flush(st, s.file)
		})
	}
	c.t = t
	time.Sleep(200 * time.Millisecond)
	// Kejadian notifikasi (Discord + push), diurutkan agar deterministik.
	c.app.notifier.(*contractNotifier).mu.Lock()
	ev := append([]string{}, c.app.notifier.(*contractNotifier).ev...)
	c.app.notifier.(*contractNotifier).mu.Unlock()
	c.pushR.mu.Lock()
	ev = append(ev, c.pushR.ev...)
	c.pushR.mu.Unlock()
	for i := range ev {
		ev[i] = c.norm.str(ev[i])
	}
	sort.Strings(ev)
	checkGolden(t, "13_notification_events.json", ev)
	checkGolden(t, "00_coverage.json", c.coverageDoc())
	if miss := c.uncovered(); len(miss) > 0 {
		t.Errorf("rute tidak tercakup skenario kontrak: %v", miss)
	}
}

// ---------- 01 publik ----------

func secPublic(c *contractEnv) {
	c.do("health", "GET", "/health", rq{})
	c.do("health ready", "GET", "/health/ready", rq{})
	c.do("products list", "GET", "/api/products", rq{})
	c.do("products by category", "GET", "/api/products?category=snack", rq{})
	c.do("products search ok", "GET", "/api/products/search?q=kerupuk", rq{})
	c.do("products search empty q", "GET", "/api/products/search", rq{})
	c.do("products search no result", "GET", "/api/products/search?q=zzzzzz-tidak-ada", rq{})
	c.do("unusual products search wildcard/unicode", "GET", "/api/products/search?q="+urlEsc("%_\\' OR 1=1 -- 🍤\u202e"), rq{})
	c.do("unusual products search very long q", "GET", "/api/products/search?q="+strings.Repeat("a", 400), rq{})
	c.do("categories list", "GET", "/api/categories", rq{})
	c.do("regions provinces", "GET", "/api/regions/provinces", rq{})
	c.do("regions regencies ok", "GET", "/api/regions/regencies/36", rq{})
	c.do("regions regencies unknown", "GET", "/api/regions/regencies/99", rq{})
	c.do("regions regencies invalid code", "GET", "/api/regions/regencies/abc", rq{})
	c.do("regions districts ok", "GET", "/api/regions/districts/36.71", rq{})
	c.do("regions districts invalid code", "GET", "/api/regions/districts/36", rq{})
	c.do("regions villages ok", "GET", "/api/regions/villages/36.71.01", rq{})
	c.do("regions villages unknown", "GET", "/api/regions/villages/36.71.99", rq{})
	c.do("unusual regions code injection", "GET", "/api/regions/villages/"+urlEsc("36.71.01' OR '1'='1"), rq{})
	c.do("regions provinces conditional (If-None-Match)", "GET", "/api/regions/provinces", rq{hdr: map[string]string{"If-None-Match": `"x"`}})
	c.do("cors preflight allowed origin", "OPTIONS", "/api/products", rq{hdr: map[string]string{"Origin": "https://store.mihan.web.id", "Access-Control-Request-Method": "GET"}})
	c.do("cors get disallowed origin", "GET", "/api/products", rq{hdr: map[string]string{"Origin": "https://evil.example"}})
	c.do("unknown route", "GET", "/api/tidak-ada", rq{})
	c.do("wrong method", "DELETE", "/api/products", rq{})
}

// ---------- 02 auth ----------

func secAuth(c *contractEnv) {
	r := c.must(c.do("register ok", "POST", "/api/auth/register", rq{body: regBody("Budi_Uji", " Budi@Uji.Test ", "0812-3456-7890", " Budi  Uji ", cPassword)}), 201, "register")
	tokBudi := str(r.Body["token"])
	c.do("register ok without phone", "POST", "/api/auth/register", rq{body: regBody("siti", "siti@uji.test", "", "Siti", cPassword)})
	c.do("register ok (akun untuk uji tautan Google)", "POST", "/api/auth/register", rq{body: regBody("tautan", "tautan@uji.test", "", "Tautan", cPassword)})
	c.do("register invalid phone", "POST", "/api/auth/register", rq{body: regBody("andi", "andi@uji.test", "12345", "Andi", cPassword)})
	c.do("register invalid email", "POST", "/api/auth/register", rq{body: regBody("andi2", "bukan-email", "", "Andi", cPassword)})
	c.do("register short password", "POST", "/api/auth/register", rq{body: regBody("andi3", "andi3@uji.test", "", "Andi", "pendek")})
	c.do("register without turnstile", "POST", "/api/auth/register", rq{body: map[string]any{"username": "andi4", "email": "andi4@uji.test", "name": "A", "password": cPassword}})
	c.do("register bad turnstile", "POST", "/api/auth/register", rq{body: map[string]any{"username": "andi5", "email": "andi5@uji.test", "name": "A", "password": cPassword, "turnstileToken": "gagal"}})
	c.do("register duplicate email", "POST", "/api/auth/register", rq{body: regBody("lain", "BUDI@uji.test", "", "X", cPassword)})
	c.do("register duplicate username", "POST", "/api/auth/register", rq{body: regBody("BUDI_UJI", "lain@uji.test", "", "X", cPassword)})
	c.do("register not json", "POST", "/api/auth/register", rq{raw: "bukan json", ct: "application/json"})
	c.do("register wrong content type", "POST", "/api/auth/register", rq{raw: `{"a":1}`, ct: "text/plain"})
	c.do("unusual register unknown field", "POST", "/api/auth/register", rq{body: map[string]any{"username": "x1", "email": "x1@uji.test", "name": "A", "password": cPassword, "turnstileToken": "lulus", "role": "admin"}})
	c.do("unusual register unicode name & huge username", "POST", "/api/auth/register", rq{body: regBody(strings.Repeat("u", 80), "huge@uji.test", "", "Nama \u202e🍤 \x00 Aneh", cPassword)})
	c.do("unusual register unicode name ok", "POST", "/api/auth/register", rq{body: regBody("unik_uji", "unik@uji.test", "", "Nama 🍤  Aneh", cPassword)})

	c.do("login by identifier username", "POST", "/api/auth/login", rq{body: map[string]any{"identifier": "Budi_Uji", "password": cPassword, "turnstileToken": "lulus"}})
	r = c.must(c.do("login by identifier email", "POST", "/api/auth/login", rq{body: map[string]any{"identifier": "BUDI@uji.test", "password": cPassword, "turnstileToken": "lulus"}}), 200, "login")
	tokLogin := str(r.Body["token"])
	c.do("login legacy field email", "POST", "/api/auth/login", rq{body: map[string]any{"email": "budi@uji.test", "password": cPassword, "turnstileToken": "lulus"}})
	c.do("login wrong password", "POST", "/api/auth/login", rq{body: map[string]any{"identifier": "siti", "password": "salahsalah", "turnstileToken": "lulus"}})
	c.do("login unknown user", "POST", "/api/auth/login", rq{body: map[string]any{"identifier": "tidakada", "password": cPassword, "turnstileToken": "lulus"}})
	c.do("login bad turnstile", "POST", "/api/auth/login", rq{body: map[string]any{"identifier": "budi_uji", "password": cPassword, "turnstileToken": "gagal"}})
	c.do("login without turnstile", "POST", "/api/auth/login", rq{body: map[string]any{"identifier": "budi_uji", "password": cPassword}})
	c.do("login empty body", "POST", "/api/auth/login", rq{raw: "{}", ct: "application/json"})
	c.do("unusual login huge identifier", "POST", "/api/auth/login", rq{body: map[string]any{"identifier": strings.Repeat("x", 5000), "password": cPassword, "turnstileToken": "lulus"}})
	c.do("unusual login sql-ish identifier", "POST", "/api/auth/login", rq{body: map[string]any{"identifier": "' OR 1=1 --", "password": "x' OR '1'='1", "turnstileToken": "lulus"}})

	c.do("me ok", "GET", "/api/auth/me", rq{tok: tokLogin})
	c.do("me no token", "GET", "/api/auth/me", rq{})
	c.do("me bad token", "GET", "/api/auth/me", rq{tok: "token-palsu"})
	c.do("verify header", "POST", "/api/auth/verify", rq{tok: tokBudi})
	c.do("verify body", "POST", "/api/auth/verify", rq{body: map[string]any{"token": tokBudi}})
	c.do("verify no token", "POST", "/api/auth/verify", rq{})
	c.do("unusual verify old-style fake token", "POST", "/api/auth/verify", rq{body: map[string]any{"token": "token_budi_uji.test_1"}})
	c.do("logout ok", "POST", "/api/auth/logout", rq{tok: tokLogin})
	c.do("logout no token", "POST", "/api/auth/logout", rq{})
	c.do("verify after logout", "POST", "/api/auth/verify", rq{tok: tokLogin})
	c.do("me after logout", "GET", "/api/auth/me", rq{tok: tokLogin})

	// Google (token ID ditandatangani kunci uji; tidak ada panggilan ke Google).
	gtok := func(sub, email string) string {
		cl := googleClaimsOK()
		cl["sub"], cl["email"], cl["name"] = sub, email, "Pengguna Google"
		return signJWT(c.t, testKey, "g1", "RS256", cl)
	}
	c.do("google no credential", "POST", "/api/auth/google", rq{body: map[string]any{}})
	c.do("google invalid credential", "POST", "/api/auth/google", rq{body: map[string]any{"credential": "bukan.jwt.valid"}})
	c.do("google wrong signature", "POST", "/api/auth/google", rq{body: map[string]any{"credential": signJWT(c.t, otherKey, "g1", "RS256", googleClaimsOK())}})
	r = c.must(c.do("google new user needs profile", "POST", "/api/auth/google", rq{body: map[string]any{"credential": gtok("g-sub-1", "guser@gmail.com")}}), 200, "google")
	pt := str(r.Body["profileToken"])
	c.do("google complete invalid token", "POST", "/api/auth/google/complete", rq{body: map[string]any{"profileToken": "salah", "username": "guser"}})
	c.do("google complete invalid username", "POST", "/api/auth/google/complete", rq{body: map[string]any{"profileToken": pt, "username": "x"}})
	c.do("google complete ok", "POST", "/api/auth/google/complete", rq{body: map[string]any{"profileToken": pt, "username": "guser", "phone": "081200001111"}})
	c.do("google existing user logs in", "POST", "/api/auth/google", rq{body: map[string]any{"credential": gtok("g-sub-1", "guser@gmail.com")}})
	c.do("google link existing password account", "POST", "/api/auth/google", rq{body: map[string]any{"credential": gtok("g-sub-2", "tautan@uji.test")}})
	c.do("unusual google complete unknown fields", "POST", "/api/auth/google/complete", rq{body: map[string]any{"profileToken": pt, "username": "guser2", "role": "admin"}})
}

// ---------- 03 akses admin (tanpa login / bukan admin) untuk SEMUA rute admin ----------

var adminRouteSamples = [][2]string{
	{"GET", "/api/admin/me"}, {"GET", "/api/admin/summary"}, {"GET", "/api/admin/settings"}, {"PUT", "/api/admin/settings"},
	{"GET", "/api/admin/products"}, {"POST", "/api/admin/products"}, {"GET", "/api/admin/products/1"}, {"PUT", "/api/admin/products/1"},
	{"DELETE", "/api/admin/products/1"}, {"PATCH", "/api/admin/products/1/active"}, {"POST", "/api/admin/products/1/image"}, {"DELETE", "/api/admin/products/1/image"},
	{"GET", "/api/admin/categories"}, {"POST", "/api/admin/categories"}, {"PUT", "/api/admin/categories/1"}, {"DELETE", "/api/admin/categories/1"},
	{"GET", "/api/admin/orders"}, {"GET", "/api/admin/orders/1"}, {"PATCH", "/api/admin/orders/1/pricing"}, {"POST", "/api/admin/orders/1/confirm"},
	{"GET", "/api/admin/orders/1/shipping-suggestions"}, {"POST", "/api/admin/orders/1/shipping-default"}, {"GET", "/api/admin/orders/1/payment-proof"},
	{"PATCH", "/api/admin/orders/1/status"}, {"PATCH", "/api/admin/orders/1/note"},
	{"GET", "/api/admin/customers"}, {"GET", "/api/admin/customers/1"}, {"PATCH", "/api/admin/customers/1/alias"},
	{"GET", "/api/admin/activity-logs"}, {"POST", "/api/admin/activity-logs/purge"},
	{"GET", "/api/admin/push/public-key"}, {"POST", "/api/admin/push/subscribe"}, {"DELETE", "/api/admin/push/subscribe"}, {"POST", "/api/admin/push/test"},
	{"GET", "/api/admin/regions/status"}, {"POST", "/api/admin/regions/fetch"}, {"GET", "/api/admin/regions/runs/1"},
	{"POST", "/api/admin/regions/runs/1/save"}, {"POST", "/api/admin/regions/runs/1/discard"},
}

func secAdminAccess(c *contractEnv) {
	// Pelanggan biasa (Bearer sesi) tidak boleh masuk rute admin.
	r := c.must(c.do("customer for admin tests: login", "POST", "/api/auth/login", rq{body: map[string]any{"identifier": "siti", "password": cPassword, "turnstileToken": "lulus"}}), 200, "login siti")
	tokSiti := str(r.Body["token"])
	for _, rt := range adminRouteSamples {
		c.do("noauth "+rt[0]+" "+rt[1], rt[0], rt[1], rq{})
	}
	for _, rt := range adminRouteSamples {
		c.do("non-admin email "+rt[0]+" "+rt[1], rt[0], rt[1], rq{adm: cOther})
	}
	c.do("customer bearer token on admin route", "GET", "/api/admin/me", rq{tok: tokSiti})
	c.do("garbage access jwt", "GET", "/api/admin/me", rq{hdr: map[string]string{"Cf-Access-Jwt-Assertion": "bukan.jwt"}})
	c.do("admin me ok", "GET", "/api/admin/me", rq{adm: cAdmin})
	c.do("admin write without csrf header", "POST", "/api/admin/categories", rq{adm: cAdmin, noCSRF: true, body: map[string]any{"slug": "csrf_uji", "name": "X"}})
	c.do("admin write wrong origin", "POST", "/api/admin/categories", rq{adm: cAdmin, body: map[string]any{"slug": "csrf_uji", "name": "X"}, hdr: map[string]string{"Origin": "https://evil.example"}})
	c.do("unusual admin email case", "GET", "/api/admin/me", rq{adm: "ADMIN.Kontrak@UJI.test"})
}

// ---------- 04 admin: katalog ----------

func secAdminCatalog(c *contractEnv) {
	c.do("admin categories list", "GET", "/api/admin/categories", rq{adm: cAdmin})
	r := c.must(c.do("admin category create ok", "POST", "/api/admin/categories", rq{adm: cAdmin, body: map[string]any{"slug": "bumbu_uji", "name": "Bumbu Uji", "sortOrder": 70}}), 201, "kategori")
	catID := numf(r.Body["id"])
	c.do("admin category create duplicate slug", "POST", "/api/admin/categories", rq{adm: cAdmin, body: map[string]any{"slug": "bumbu_uji", "name": "Lagi"}})
	c.do("admin category create invalid slug", "POST", "/api/admin/categories", rq{adm: cAdmin, body: map[string]any{"slug": "Bumbu Uji", "name": "X"}})
	c.do("admin category create empty body", "POST", "/api/admin/categories", rq{adm: cAdmin, raw: "{}", ct: "application/json"})
	ur := c.must(c.do("unusual admin category create unicode name", "POST", "/api/admin/categories", rq{adm: cAdmin, body: map[string]any{"slug": "unik_uji", "name": "Kategori \u202e🍤 \x00 aneh", "sortOrder": -5}}), 201, "kategori unik")
	catUnik := numf(ur.Body["id"])
	cp := fmt.Sprintf("/api/admin/categories/%d", catID)
	c.do("admin category update ok", "PUT", cp, rq{adm: cAdmin, body: map[string]any{"slug": "bumbu_uji", "name": "Bumbu Uji Baru"}})
	c.do("admin category update not found", "PUT", "/api/admin/categories/99999", rq{adm: cAdmin, body: map[string]any{"slug": "x", "name": "X"}})
	c.do("admin category update invalid", "PUT", cp, rq{adm: cAdmin, body: map[string]any{"slug": "", "name": ""}})

	c.do("admin products list", "GET", "/api/admin/products", rq{adm: cAdmin})
	c.do("admin products list filtered", "GET", "/api/admin/products?q=kerupuk&category=1&status=aktif&page=1&per_page=3", rq{adm: cAdmin})
	c.do("unusual admin products list bad paging", "GET", "/api/admin/products?page=-1&per_page=100000&q="+urlEsc("%_"), rq{adm: cAdmin})
	pr := c.must(c.do("admin product create ok", "POST", "/api/admin/products", rq{adm: cAdmin, body: map[string]any{
		"name": "Bumbu Kontrak", "price": 12000, "description": "uji", "categoryId": catID, "unit": "pak",
		"tiers": []map[string]any{{"minQty": 10, "type": "fixed", "value": 10000}, {"minQty": 50, "type": "percent", "value": 20}}}}), 201, "produk")
	pid := numf(pr.Body["id"])
	c.do("admin product create simple", "POST", "/api/admin/products", rq{adm: cAdmin, body: map[string]any{"name": "Produk Polos Kontrak", "price": 5000, "categoryId": 1}})
	c.do("admin product create invalid price", "POST", "/api/admin/products", rq{adm: cAdmin, body: map[string]any{"name": "X", "price": -1, "categoryId": 1}})
	c.do("admin product create missing name", "POST", "/api/admin/products", rq{adm: cAdmin, body: map[string]any{"price": 1, "categoryId": 1}})
	c.do("admin product create bad category", "POST", "/api/admin/products", rq{adm: cAdmin, body: map[string]any{"name": "X", "price": 1, "categoryId": 99999}})
	c.do("admin product create bad tiers", "POST", "/api/admin/products", rq{adm: cAdmin, body: map[string]any{"name": "X", "price": 1000, "categoryId": 1, "tiers": []map[string]any{{"minQty": 1, "type": "fixed", "value": 5000}}}})
	c.do("unusual admin product create huge price/unicode", "POST", "/api/admin/products", rq{adm: cAdmin, body: map[string]any{"name": "Produk \u202e🍤 aneh", "price": int64(99999999999), "categoryId": 1}})
	c.do("unusual admin product create not json", "POST", "/api/admin/products", rq{adm: cAdmin, raw: "[1,2,3]", ct: "application/json"})
	pp := fmt.Sprintf("/api/admin/products/%d", int64(pid))
	c.do("admin product get ok", "GET", pp, rq{adm: cAdmin})
	c.do("admin product get not found", "GET", "/api/admin/products/99999", rq{adm: cAdmin})
	c.do("admin product update ok", "PUT", pp, rq{adm: cAdmin, body: map[string]any{"name": "Bumbu Kontrak 2", "price": 13000, "categoryId": catID, "unit": "dus", "tiers": []map[string]any{{"minQty": 5, "type": "percent", "value": 10}}}})
	c.do("admin product update invalid", "PUT", pp, rq{adm: cAdmin, body: map[string]any{"name": "", "price": 0}})
	c.do("admin product active off", "PATCH", pp+"/active", rq{adm: cAdmin, body: map[string]any{"active": false}})
	c.do("admin product active on", "PATCH", pp+"/active", rq{adm: cAdmin, body: map[string]any{"active": true}})
	c.do("admin product active invalid", "PATCH", pp+"/active", rq{adm: cAdmin, body: map[string]any{}})
	c.do("admin product active not found", "PATCH", "/api/admin/products/99999/active", rq{adm: cAdmin, body: map[string]any{"active": true}})

	// Foto produk (gambar kecil dibuat di kode).
	c.do("admin product image upload ok", "POST", pp+"/image", rq{adm: cAdmin, file: testPNG(120, 80, 7), fname: "foto.png"})
	c.do("admin product image replace", "POST", pp+"/image", rq{adm: cAdmin, file: testPNG(160, 100, 40), fname: "foto2.png"})
	c.do("admin product image not an image", "POST", pp+"/image", rq{adm: cAdmin, file: []byte("ini bukan gambar"), fname: "x.png"})
	c.do("admin product image not multipart", "POST", pp+"/image", rq{adm: cAdmin, body: map[string]any{"a": 1}})
	c.do("admin product image not found", "POST", "/api/admin/products/99999/image", rq{adm: cAdmin, file: testPNG(40, 40, 1), fname: "x.png"})
	c.do("unusual admin product image empty file", "POST", pp+"/image", rq{adm: cAdmin, file: []byte{}, fname: "kosong.png"})
	c.do("products list shows uploaded image", "GET", "/api/products?category=bumbu_uji", rq{})
	c.do("admin product image delete ok", "DELETE", pp+"/image", rq{adm: cAdmin})
	c.do("admin product image delete again", "DELETE", pp+"/image", rq{adm: cAdmin})
	c.do("admin product delete ok", "DELETE", pp, rq{adm: cAdmin})
	c.do("admin product delete again", "DELETE", pp, rq{adm: cAdmin})
	// Produk berharga grosir tetap ada untuk keranjang/pesanan berikut.
	c.must(c.do("admin product create tiered for cart", "POST", "/api/admin/products", rq{adm: cAdmin, body: map[string]any{
		"name": "Kerupuk Grosir Kontrak", "price": 10000, "categoryId": 1, "unit": "pak",
		"tiers": []map[string]any{{"minQty": 3, "type": "fixed", "value": 9000}, {"minQty": 6, "type": "percent", "value": 20}}}}), 201, "produk grosir")
	c.do("admin category delete with products", "DELETE", cp, rq{adm: cAdmin})
	c.do("admin category delete not found", "DELETE", "/api/admin/categories/99999", rq{adm: cAdmin})
	c.do("admin category delete empty (unik)", "DELETE", fmt.Sprintf("/api/admin/categories/%d", int64(catUnik)), rq{adm: cAdmin})
	c.do("products public after catalog changes", "GET", "/api/products", rq{})
	c.do("categories public after catalog changes", "GET", "/api/categories", rq{})
	c.do("admin categories list after changes", "GET", "/api/admin/categories", rq{adm: cAdmin})
}

// ---------- 05 keranjang ----------

func login(c *contractEnv, ident string) string {
	c.t.Helper()
	b, _ := json.Marshal(map[string]any{"identifier": ident, "password": cPassword, "turnstileToken": "lulus"})
	r := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(b))
	r.RemoteAddr = "203.0.113.7:5555"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m)
	tok := str(m["token"])
	if tok == "" {
		c.t.Fatalf("login %s gagal: %s", ident, w.Body.String())
	}
	return tok
}

func secCart(c *contractEnv) {
	for _, rt := range [][2]string{{"GET", "/api/cart"}, {"DELETE", "/api/cart"}, {"POST", "/api/cart/items"}, {"PUT", "/api/cart/items"}, {"DELETE", "/api/cart/items/1"}, {"POST", "/api/cart/ack-prices"}} {
		c.do("noauth "+rt[0]+" "+rt[1], rt[0], rt[1], rq{})
	}
	tok := login(c, "budi_uji")
	c.do("cart empty", "GET", "/api/cart", rq{tok: tok})
	c.do("cart add product 1 qty 2", "POST", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": 1, "qty": 2}})
	c.do("cart add product 1 default qty", "POST", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": 1}})
	c.do("cart add product 2", "POST", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": 2, "qty": 1}})
	c.do("cart add invalid qty 0", "POST", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": 1, "qty": 0}})
	c.do("cart add invalid qty too many", "POST", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": 1, "qty": 100000}})
	c.do("cart add missing productId", "POST", "/api/cart/items", rq{tok: tok, body: map[string]any{"qty": 1}})
	c.do("cart add unknown product", "POST", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": 99999, "qty": 1}})
	c.do("unusual cart add price override ignored", "POST", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": 1, "qty": 1, "price": 1}})
	c.do("unusual cart add qty as string", "POST", "/api/cart/items", rq{tok: tok, raw: `{"productId":1,"qty":"2"}`, ct: "application/json"})
	c.do("unusual cart add qty float", "POST", "/api/cart/items", rq{tok: tok, raw: `{"productId":1,"qty":1.5}`, ct: "application/json"})
	c.do("unusual cart add negative productId", "POST", "/api/cart/items", rq{tok: tok, raw: `{"productId":-3,"qty":1}`, ct: "application/json"})
	c.do("cart set qty", "PUT", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": 1, "qty": 4}})
	c.do("cart set qty invalid", "PUT", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": 1}})
	c.do("cart set qty 0 removes", "PUT", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": 2, "qty": 0}})
	c.do("cart delete item", "DELETE", "/api/cart/items/1", rq{tok: tok})
	c.do("cart delete item not in cart", "DELETE", "/api/cart/items/1", rq{tok: tok})
	c.do("cart delete item invalid id", "DELETE", "/api/cart/items/abc", rq{tok: tok})
	// Produk grosir: harga bertingkat tampil di keranjang.
	var tid uint64
	c.db.Raw("SELECT id FROM products WHERE name = 'Kerupuk Grosir Kontrak'").Scan(&tid)
	c.do("cart add tiered below threshold", "POST", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": tid, "qty": 2}})
	c.do("cart add tiered at threshold", "PUT", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": tid, "qty": 3}})
	c.do("cart add tiered percent tier", "PUT", "/api/cart/items", rq{tok: tok, body: map[string]any{"productId": tid, "qty": 7}})
	c.do("cart get with tiered", "GET", "/api/cart", rq{tok: tok})
	c.do("cart ack-prices", "POST", "/api/cart/ack-prices", rq{tok: tok})
	c.do("cart clear", "DELETE", "/api/cart", rq{tok: tok})
	c.do("cart after clear", "GET", "/api/cart", rq{tok: tok})
}

// ---------- 06 checkout (+ sisi pelanggan) ----------

var ordNo = map[string]string{}

func secCheckout(c *contractEnv) {
	for _, rt := range [][2]string{{"GET", "/api/orders"}, {"POST", "/api/orders"}, {"GET", "/api/orders/MS-260310-1"}, {"POST", "/api/orders/MS-260310-1/cancel"},
		{"POST", "/api/orders/MS-260310-1/payment-proof"}, {"GET", "/api/orders/MS-260310-1/payment-proof"}, {"DELETE", "/api/orders/MS-260310-1/payment-proof"}, {"GET", "/api/store-info"}} {
		c.do("noauth "+rt[0]+" "+rt[1], rt[0], rt[1], rq{})
	}
	tokA := login(c, "budi_uji")
	tokB := login(c, "siti")
	c.do("store-info before settings", "GET", "/api/store-info", rq{tok: tokA})
	c.do("orders list empty", "GET", "/api/orders", rq{tok: tokA})
	c.do("checkout empty cart", "POST", "/api/orders", rq{tok: tokA, body: checkoutPayload(idem())})
	c.do("cart add for checkout", "POST", "/api/cart/items", rq{tok: tokA, body: map[string]any{"productId": 4, "qty": 2}})
	c.do("checkout invalid empty body", "POST", "/api/orders", rq{tok: tokA, raw: "{}", ct: "application/json"})
	c.do("checkout invalid idempotency key", "POST", "/api/orders", rq{tok: tokA, body: func() map[string]any { b := checkoutPayload("bukan-uuid"); return b }()})
	c.do("checkout invalid phone", "POST", "/api/orders", rq{tok: tokA, body: func() map[string]any { b := checkoutPayload(idem()); b["recipientPhone"] = "123"; return b }()})
	c.do("checkout missing region", "POST", "/api/orders", rq{tok: tokA, body: func() map[string]any { b := checkoutPayload(idem()); delete(b, "villageCode"); return b }()})
	c.do("checkout region not in data", "POST", "/api/orders", rq{tok: tokA, body: func() map[string]any { b := checkoutPayload(idem()); b["villageCode"] = "36.71.01.9999"; return b }()})
	c.do("checkout region mismatch", "POST", "/api/orders", rq{tok: tokA, body: func() map[string]any { b := checkoutPayload(idem()); b["regencyCode"] = "31.71"; return b }()})
	c.do("checkout expectedTotal mismatch", "POST", "/api/orders", rq{tok: tokA, body: func() map[string]any { b := checkoutPayload(idem()); b["expectedTotal"] = 1; return b }()})
	c.do("unusual checkout html/unicode address", "POST", "/api/orders", rq{tok: tokA, body: func() map[string]any {
		b := checkoutPayload(idem())
		b["address"] = "<script>alert(1)</script> \u202e Jl. 🍤 \x00 No 1"
		b["recipientName"] = strings.Repeat("N", 500)
		return b
	}()})
	c.do("unusual checkout array body", "POST", "/api/orders", rq{tok: tokA, raw: "[1]", ct: "application/json"})
	c.do("unusual checkout huge note", "POST", "/api/orders", rq{tok: tokA, body: func() map[string]any { b := checkoutPayload(idem()); b["note"] = strings.Repeat("c", 5000); return b }()})

	key1 := idem()
	r := c.must(c.do("checkout ok -> pending_confirmation", "POST", "/api/orders", rq{tok: tokA, body: checkoutPayload(key1)}), 201, "checkout")
	ordNo["A1"] = str(r.Body["orderNo"])
	c.do("checkout same idempotency key replays", "POST", "/api/orders", rq{tok: tokA, body: checkoutPayload(key1)})
	c.do("cart after checkout", "GET", "/api/cart", rq{tok: tokA})
	c.do("orders list", "GET", "/api/orders", rq{tok: tokA})
	c.do("order get own", "GET", "/api/orders/"+ordNo["A1"], rq{tok: tokA})
	c.do("order get other customer (IDOR) 404", "GET", "/api/orders/"+ordNo["A1"], rq{tok: tokB})
	c.do("order get unknown number", "GET", "/api/orders/MS-200101-9999", rq{tok: tokA})
	c.do("order get invalid number format", "GET", "/api/orders/bukan-nomor", rq{tok: tokA})
	c.do("unusual order get long number", "GET", "/api/orders/"+strings.Repeat("9", 300), rq{tok: tokA})
	c.do("order cancel other customer 404", "POST", "/api/orders/"+ordNo["A1"]+"/cancel", rq{tok: tokB, body: map[string]any{}})
	c.do("order cancel invalid number", "POST", "/api/orders/bukan-nomor/cancel", rq{tok: tokA, body: map[string]any{}})
	c.do("order cancel reason too long", "POST", "/api/orders/"+ordNo["A1"]+"/cancel", rq{tok: tokA, body: map[string]any{"reason": strings.Repeat("r", 600)}})
	// Pesanan khusus: bidang asing pada body batal tetap membatalkan (perilaku saat ini).
	ordNo["C1"] = newOrder(c, tokA, 1, 1)
	c.do("unusual order cancel with unknown field", "POST", "/api/orders/"+ordNo["C1"]+"/cancel", rq{tok: tokA, body: map[string]any{"status": "paid"}})
}

// ---------- 07 alur admin: konfirmasi, ongkir, saran & default ----------

func admOrd(c *contractEnv, key string) (int64, string) {
	id := c.orderID(ordNo[key])
	if id == 0 {
		c.t.Fatalf("pesanan %s (%s) tidak ditemukan", key, ordNo[key])
	}
	return int64(id), fmt.Sprintf("/api/admin/orders/%d", id)
}

func secOrderFlow(c *contractEnv) {
	_, op := admOrd(c, "A1")
	c.do("admin orders list", "GET", "/api/admin/orders", rq{adm: cAdmin})
	c.do("admin orders list filter status", "GET", "/api/admin/orders?status=pending_confirmation&page=1&per_page=5", rq{adm: cAdmin})
	c.do("admin orders list invalid status", "GET", "/api/admin/orders?status=ngawur", rq{adm: cAdmin})
	c.do("admin orders list invalid date", "GET", "/api/admin/orders?from=kemarin", rq{adm: cAdmin})
	c.do("unusual admin orders list sql-ish q", "GET", "/api/admin/orders?q="+urlEsc("' OR 1=1 --"), rq{adm: cAdmin})
	c.do("admin order get (pending_confirmation, status awal)", "GET", op, rq{adm: cAdmin})
	c.do("admin order get not found", "GET", "/api/admin/orders/99999999", rq{adm: cAdmin})
	c.do("admin order get non numeric id", "GET", "/api/admin/orders/abc", rq{adm: cAdmin})
	c.do("admin summary", "GET", "/api/admin/summary", rq{adm: cAdmin})

	c.do("pricing before confirm is locked", "PATCH", op+"/pricing", rq{adm: cAdmin, body: map[string]any{"discount": 0, "shippingFee": 5000}})
	c.do("status to pending_payment bypassing confirm", "PATCH", op+"/status", rq{adm: cAdmin, body: map[string]any{"from": "pending_confirmation", "to": "pending_payment"}})
	c.do("status to paid before confirm", "PATCH", op+"/status", rq{adm: cAdmin, body: map[string]any{"from": "pending_confirmation", "to": "paid"}})
	c.do("admin payment-proof get none (not yet confirmed)", "GET", op+"/payment-proof", rq{adm: cAdmin})
	c.do("shipping-default before confirm", "POST", op+"/shipping-default", rq{adm: cAdmin, body: map[string]any{}})
	c.do("shipping-suggestions (no default yet)", "GET", op+"/shipping-suggestions", rq{adm: cAdmin})
	c.do("shipping-suggestions not found", "GET", "/api/admin/orders/99999999/shipping-suggestions", rq{adm: cAdmin})

	c.do("confirm invalid: missing fields", "POST", op+"/confirm", rq{adm: cAdmin, body: map[string]any{}})
	c.do("confirm invalid: negative", "POST", op+"/confirm", rq{adm: cAdmin, body: map[string]any{"discount": -1, "shippingFee": 5000}})
	c.do("confirm invalid: shipping too large", "POST", op+"/confirm", rq{adm: cAdmin, body: map[string]any{"discount": 0, "shippingFee": 99999999999}})
	c.do("confirm invalid: discount exceeds subtotal", "POST", op+"/confirm", rq{adm: cAdmin, body: map[string]any{"discount": 99999999, "shippingFee": 0}})
	c.do("confirm invalid: old field setRegionDefault", "POST", op+"/confirm", rq{adm: cAdmin, body: map[string]any{"discount": 0, "shippingFee": 11111, "setRegionDefault": true}})
	c.do("unusual confirm: fractional rupiah", "POST", op+"/confirm", rq{adm: cAdmin, raw: `{"discount":0,"shippingFee":1500.5}`, ct: "application/json"})
	c.do("unusual confirm: string amount", "POST", op+"/confirm", rq{adm: cAdmin, raw: `{"discount":"0","shippingFee":"1000"}`, ct: "application/json"})
	c.do("confirm not found", "POST", "/api/admin/orders/99999999/confirm", rq{adm: cAdmin, body: map[string]any{"discount": 0, "shippingFee": 1000}})
	c.do("confirm ok (diskon + ongkir) -> pending_payment", "POST", op+"/confirm", rq{adm: cAdmin, body: map[string]any{"discount": 1000, "discountNote": "Diskon kontrak", "shippingFee": 15000}})
	c.do("confirm again (409)", "POST", op+"/confirm", rq{adm: cAdmin, body: map[string]any{"discount": 0, "shippingFee": 1000}})
	c.do("admin order get after confirm", "GET", op, rq{adm: cAdmin})
	c.do("customer sees confirmed order", "GET", "/api/orders/"+ordNo["A1"], rq{tok: login(c, "budi_uji")})

	c.do("shipping-default POST client amount rejected", "POST", op+"/shipping-default", rq{adm: cAdmin, body: map[string]any{"shippingFee": 99999}})
	c.do("shipping-default POST ok", "POST", op+"/shipping-default", rq{adm: cAdmin, body: map[string]any{}})
	c.do("shipping-default POST again (idempotent update)", "POST", op+"/shipping-default", rq{adm: cAdmin})
	c.do("shipping-default not found", "POST", "/api/admin/orders/99999999/shipping-default", rq{adm: cAdmin, body: map[string]any{}})
	c.do("shipping-suggestions with default", "GET", op+"/shipping-suggestions", rq{adm: cAdmin})

	c.do("pricing patch ok", "PATCH", op+"/pricing", rq{adm: cAdmin, body: map[string]any{"discount": 2000, "discountNote": "Diskon diubah", "shippingFee": 12000}})
	c.do("pricing patch invalid", "PATCH", op+"/pricing", rq{adm: cAdmin, body: map[string]any{"discount": -5, "shippingFee": 1}})
	c.do("pricing patch missing fields", "PATCH", op+"/pricing", rq{adm: cAdmin, body: map[string]any{"discount": 1}})
	c.do("pricing patch not found", "PATCH", "/api/admin/orders/99999999/pricing", rq{adm: cAdmin, body: map[string]any{"discount": 0, "shippingFee": 0}})
	c.do("note patch ok", "PATCH", op+"/note", rq{adm: cAdmin, body: map[string]any{"adminNote": "Catatan admin kontrak"}})
	c.do("note patch clear", "PATCH", op+"/note", rq{adm: cAdmin, body: map[string]any{"adminNote": nil}})
	c.do("note patch too long", "PATCH", op+"/note", rq{adm: cAdmin, body: map[string]any{"adminNote": strings.Repeat("n", 700)}})
	c.do("note patch invalid body", "PATCH", op+"/note", rq{adm: cAdmin, body: map[string]any{"foo": 1}})
	c.do("note patch not found", "PATCH", "/api/admin/orders/99999999/note", rq{adm: cAdmin, body: map[string]any{"adminNote": "x"}})
}

// ---------- 08 bukti transfer ----------

func secProof(c *contractEnv) {
	tokA := login(c, "budi_uji")
	tokB := login(c, "siti")
	_, op := admOrd(c, "A1")
	pf := "/api/orders/" + ordNo["A1"] + "/payment-proof"
	c.do("proof get none (customer)", "GET", pf, rq{tok: tokA})
	c.do("proof admin get none", "GET", op+"/payment-proof", rq{adm: cAdmin})
	c.do("proof upload no multipart", "POST", pf, rq{tok: tokA, body: map[string]any{"a": 1}})
	c.do("proof upload empty file", "POST", pf, rq{tok: tokA, file: []byte{}, fname: "kosong.png"})
	c.do("proof upload not an image", "POST", pf, rq{tok: tokA, file: []byte("%PDF-1.4 bukan gambar"), fname: "x.pdf"})
	c.do("proof upload other customer 404", "POST", pf, rq{tok: tokB, file: testPNG(60, 40, 3), fname: "bukti.png"})
	c.do("proof upload unknown order", "POST", "/api/orders/MS-200101-9999/payment-proof", rq{tok: tokA, file: testPNG(60, 40, 3), fname: "bukti.png"})
	c.do("unusual proof upload tiny 1x1 png", "POST", pf, rq{tok: tokA, file: testPNG(1, 1, 0), fname: "../../etc/passwd.png"})
	c.do("proof get after tiny upload", "GET", pf, rq{tok: tokA})
	c.do("proof delete tiny", "DELETE", pf, rq{tok: tokA})
	c.do("proof upload ok", "POST", pf, rq{tok: tokA, file: testPNG(300, 200, 11), fname: "bukti.png"})
	c.do("proof get (customer)", "GET", pf, rq{tok: tokA})
	c.do("proof get other customer 404", "GET", pf, rq{tok: tokB})
	c.do("proof admin get", "GET", op+"/payment-proof", rq{adm: cAdmin})
	c.do("proof admin get unknown order", "GET", "/api/admin/orders/99999999/payment-proof", rq{adm: cAdmin})
	c.do("order detail with proof (customer)", "GET", "/api/orders/"+ordNo["A1"], rq{tok: tokA})
	c.do("order detail with proof (admin)", "GET", op, rq{adm: cAdmin})
	c.do("proof replace", "POST", pf, rq{tok: tokA, file: testPNG(320, 240, 90), fname: "bukti2.png"})
	c.do("proof get after replace", "GET", pf, rq{tok: tokA})
	c.do("proof delete other customer 404", "DELETE", pf, rq{tok: tokB})
	c.do("proof delete ok", "DELETE", pf, rq{tok: tokA})
	c.do("proof delete again", "DELETE", pf, rq{tok: tokA})
	c.do("proof get after delete", "GET", pf, rq{tok: tokA})
	c.do("proof re-upload", "POST", pf, rq{tok: tokA, file: testPNG(200, 200, 50), fname: "bukti3.png"})
	// Pesanan dibayar -> bukti terkunci.
	_, _ = admOrd(c, "A1")
	c.do("status paid (with proof)", "PATCH", op+"/status", rq{adm: cAdmin, body: map[string]any{"from": "pending_payment", "to": "paid", "paymentNote": "Transfer BCA"}})
	c.do("proof locked: upload after paid", "POST", pf, rq{tok: tokA, file: testPNG(60, 40, 3), fname: "bukti4.png"})
	c.do("proof locked: delete after paid", "DELETE", pf, rq{tok: tokA})
	c.do("proof still readable after paid", "GET", pf, rq{tok: tokA})
	c.do("pricing locked after paid", "PATCH", op+"/pricing", rq{adm: cAdmin, body: map[string]any{"discount": 0, "shippingFee": 1}})
	c.do("order detail after paid (admin)", "GET", op, rq{adm: cAdmin})
	c.do("order detail after paid (customer)", "GET", "/api/orders/"+ordNo["A1"], rq{tok: tokA})
}

// ---------- 09 status, selesai, batal (kedua status menunggu), ongkir 0 ----------

func newOrder(c *contractEnv, tok string, productID, qty int) string {
	c.t.Helper()
	for _, rt := range []rq{
		{m: "POST", p: "/api/cart/items", tok: tok, body: map[string]any{"productId": productID, "qty": qty}},
	} {
		b, _ := json.Marshal(rt.body)
		r := httptest.NewRequest(rt.m, rt.p, bytes.NewReader(b))
		r.RemoteAddr = "203.0.113.7:5555"
		r.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		c.h.ServeHTTP(w, r)
		if w.Code != 200 {
			c.t.Fatalf("tambah keranjang: %d %s", w.Code, w.Body.String())
		}
	}
	b, _ := json.Marshal(checkoutPayload(idem()))
	r := httptest.NewRequest("POST", "/api/orders", bytes.NewReader(b))
	r.RemoteAddr = "203.0.113.7:5555"
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m)
	if w.Code != 201 {
		c.t.Fatalf("checkout: %d %s", w.Code, w.Body.String())
	}
	return str(m["orderNo"])
}

func secStatusCancel(c *contractEnv) {
	tokA := login(c, "budi_uji")
	tokB := login(c, "siti")
	_, o1 := admOrd(c, "A1")
	// A1 (paid) -> completed.
	c.do("status invalid values", "PATCH", o1+"/status", rq{adm: cAdmin, body: map[string]any{"from": "paid", "to": "ngawur"}})
	c.do("status missing from", "PATCH", o1+"/status", rq{adm: cAdmin, body: map[string]any{"to": "completed"}})
	c.do("status stale from (409)", "PATCH", o1+"/status", rq{adm: cAdmin, body: map[string]any{"from": "pending_payment", "to": "paid"}})
	c.do("status paid -> completed", "PATCH", o1+"/status", rq{adm: cAdmin, body: map[string]any{"from": "paid", "to": "completed", "note": "Terkirim"}})
	c.do("status completed -> cancelled (final)", "PATCH", o1+"/status", rq{adm: cAdmin, body: map[string]any{"from": "completed", "to": "cancelled", "reason": "x"}})
	c.do("customer cancel completed order", "POST", "/api/orders/"+ordNo["A1"]+"/cancel", rq{tok: tokA, body: map[string]any{}})
	c.do("status not found", "PATCH", "/api/admin/orders/99999999/status", rq{adm: cAdmin, body: map[string]any{"from": "paid", "to": "completed"}})
	c.do("admin order after completed", "GET", o1, rq{adm: cAdmin})

	// B1: ongkir 0 -> pending_payment -> dibatalkan pelanggan (dari status menunggu pembayaran).
	ordNo["B1"] = newOrder(c, tokB, 3, 2)
	_, b1 := admOrd(c, "B1")
	c.do("confirm with shipping 0 (ongkir gratis)", "POST", b1+"/confirm", rq{adm: cAdmin, body: map[string]any{"discount": 0, "shippingFee": 0}})
	c.do("shipping-default with shipping 0 rejected", "POST", b1+"/shipping-default", rq{adm: cAdmin, body: map[string]any{}})
	c.do("order with ongkir 0 (customer)", "GET", "/api/orders/"+ordNo["B1"], rq{tok: tokB})
	c.do("customer cancel from pending_payment", "POST", "/api/orders/"+ordNo["B1"]+"/cancel", rq{tok: tokB, body: map[string]any{"reason": "Salah pesan"}})
	c.do("customer cancel again (final)", "POST", "/api/orders/"+ordNo["B1"]+"/cancel", rq{tok: tokB, body: map[string]any{}})
	c.do("confirm cancelled order", "POST", b1+"/confirm", rq{adm: cAdmin, body: map[string]any{"discount": 0, "shippingFee": 100}})
	c.do("shipping-default on cancelled order", "POST", b1+"/shipping-default", rq{adm: cAdmin, body: map[string]any{}})
	c.do("proof upload on cancelled order", "POST", "/api/orders/"+ordNo["B1"]+"/payment-proof", rq{tok: tokB, file: testPNG(40, 40, 2), fname: "x.png"})

	// B2: dibatalkan pelanggan dari menunggu konfirmasi.
	ordNo["B2"] = newOrder(c, tokB, 1, 1)
	c.do("order B2 (customer) pending_confirmation, canCancel", "GET", "/api/orders/"+ordNo["B2"], rq{tok: tokB})
	c.do("customer cancel from pending_confirmation", "POST", "/api/orders/"+ordNo["B2"]+"/cancel", rq{tok: tokB, body: map[string]any{"reason": "Berubah pikiran"}})
	c.do("proof upload on pending_confirmation (409)", "POST", "/api/orders/"+ordNo["A1"]+"/payment-proof", rq{tok: tokA, file: testPNG(40, 40, 2), fname: "x.png"})

	// A2: admin membatalkan dari menunggu konfirmasi; A3: dari menunggu pembayaran; A4: paid -> batal (alasan wajib).
	ordNo["A2"] = newOrder(c, tokA, 2, 1)
	_, a2 := admOrd(c, "A2")
	c.do("admin cancel from pending_confirmation", "PATCH", a2+"/status", rq{adm: cAdmin, body: map[string]any{"from": "pending_confirmation", "to": "cancelled", "reason": "Stok habis"}})
	ordNo["A3"] = newOrder(c, tokA, 5, 1)
	_, a3 := admOrd(c, "A3")
	c.do("confirm A3", "POST", a3+"/confirm", rq{adm: cAdmin, body: map[string]any{"discount": 0, "shippingFee": 8000}})
	c.do("admin cancel from pending_payment (no reason ok)", "PATCH", a3+"/status", rq{adm: cAdmin, body: map[string]any{"from": "pending_payment", "to": "cancelled"}})
	ordNo["A4"] = newOrder(c, tokA, 4, 3)
	_, a4 := admOrd(c, "A4")
	c.do("confirm A4", "POST", a4+"/confirm", rq{adm: cAdmin, body: map[string]any{"discount": 500, "shippingFee": 9000}})
	c.do("A4 pending_payment -> completed (not allowed)", "PATCH", a4+"/status", rq{adm: cAdmin, body: map[string]any{"from": "pending_payment", "to": "completed"}})
	c.do("A4 paid", "PATCH", a4+"/status", rq{adm: cAdmin, body: map[string]any{"from": "pending_payment", "to": "paid"}})
	c.do("A4 paid -> cancelled without reason", "PATCH", a4+"/status", rq{adm: cAdmin, body: map[string]any{"from": "paid", "to": "cancelled"}})
	c.do("A4 paid -> cancelled with reason", "PATCH", a4+"/status", rq{adm: cAdmin, body: map[string]any{"from": "paid", "to": "cancelled", "reason": "Dikembalikan"}})
	c.do("unusual status note too long", "PATCH", a4+"/status", rq{adm: cAdmin, body: map[string]any{"from": "cancelled", "to": "paid", "note": strings.Repeat("n", 600)}})
	c.do("customer orders list (all statuses)", "GET", "/api/orders", rq{tok: tokA})
	c.do("customer orders list B", "GET", "/api/orders", rq{tok: tokB})
	c.do("admin orders list (all)", "GET", "/api/admin/orders", rq{adm: cAdmin})
	c.do("admin orders list status=cancelled", "GET", "/api/admin/orders?status=cancelled", rq{adm: cAdmin})
	c.do("admin summary after flows", "GET", "/api/admin/summary", rq{adm: cAdmin})
}

// ---------- 10 admin lain: pengaturan, pelanggan, log ----------

func secAdminMisc(c *contractEnv) {
	tokA := login(c, "budi_uji")
	c.do("settings get", "GET", "/api/admin/settings", rq{adm: cAdmin})
	c.do("settings put invalid whatsapp", "PUT", "/api/admin/settings", rq{adm: cAdmin, body: map[string]any{"store_whatsapp": "12345"}})
	c.do("settings put unknown key", "PUT", "/api/admin/settings", rq{adm: cAdmin, body: map[string]any{"kunci_lain": "x"}})
	c.do("settings put ok", "PUT", "/api/admin/settings", rq{adm: cAdmin, body: map[string]any{"store_whatsapp": "0812-9999-8888", "bank_name": "BCA", "bank_account_number": "1234567890", "bank_account_holder": "Mihan Store", "payment_note": "Transfer lalu unggah bukti"}})
	c.do("settings put too long", "PUT", "/api/admin/settings", rq{adm: cAdmin, body: map[string]any{"payment_note": strings.Repeat("p", 2000)}})
	c.do("unusual settings put empty body", "PUT", "/api/admin/settings", rq{adm: cAdmin, raw: "{}", ct: "application/json"})
	c.do("settings get after update", "GET", "/api/admin/settings", rq{adm: cAdmin})
	c.do("store-info after settings", "GET", "/api/store-info", rq{tok: tokA})

	c.do("customers list", "GET", "/api/admin/customers", rq{adm: cAdmin})
	c.do("customers list sort orders", "GET", "/api/admin/customers?sort=orders&per_page=2&page=1", rq{adm: cAdmin})
	c.do("customers list sort spent + q", "GET", "/api/admin/customers?sort=spent&q=budi", rq{adm: cAdmin})
	c.do("customers list invalid sort", "GET", "/api/admin/customers?sort=ngawur", rq{adm: cAdmin})
	c.do("unusual customers list wildcard q", "GET", "/api/admin/customers?q="+urlEsc("%_\\"), rq{adm: cAdmin})
	var cid uint64
	c.db.Raw("SELECT id FROM users WHERE username = 'budi_uji'").Scan(&cid)
	cp := fmt.Sprintf("/api/admin/customers/%d", cid)
	c.do("customer get", "GET", cp, rq{adm: cAdmin})
	c.do("customer get not found", "GET", "/api/admin/customers/99999", rq{adm: cAdmin})
	c.do("customer alias set", "PATCH", cp+"/alias", rq{adm: cAdmin, body: map[string]any{"alias": "  Bu\u202e Budi \t Toko   Maju "}})
	c.do("customer alias too long", "PATCH", cp+"/alias", rq{adm: cAdmin, body: map[string]any{"alias": strings.Repeat("a", 101)}})
	c.do("customer alias missing", "PATCH", cp+"/alias", rq{adm: cAdmin, body: map[string]any{}})
	c.do("customer alias unknown field", "PATCH", cp+"/alias", rq{adm: cAdmin, body: map[string]any{"alias": "x", "name": "y"}})
	c.do("customer alias not found", "PATCH", "/api/admin/customers/99999/alias", rq{adm: cAdmin, body: map[string]any{"alias": "x"}})
	c.do("unusual customer alias blank clears", "PATCH", cp+"/alias", rq{adm: cAdmin, body: map[string]any{"alias": "   "}})
	c.do("customer alias set again", "PATCH", cp+"/alias", rq{adm: cAdmin, body: map[string]any{"alias": "Toko Budi"}})
	c.do("customer get after alias", "GET", cp, rq{adm: cAdmin})
	c.do("admin orders list shows alias", "GET", "/api/admin/orders?per_page=2", rq{adm: cAdmin})

	c.do("activity-logs list", "GET", "/api/admin/activity-logs", rq{adm: cAdmin})
	c.do("activity-logs filter action", "GET", "/api/admin/activity-logs?action=order.confirm&per_page=3", rq{adm: cAdmin})
	c.do("activity-logs filter entity_type", "GET", "/api/admin/activity-logs?entity_type=order&per_page=2&page=2", rq{adm: cAdmin})
	c.do("activity-logs invalid date", "GET", "/api/admin/activity-logs?from=kemarin", rq{adm: cAdmin})
	c.do("unusual activity-logs weird filters", "GET", "/api/admin/activity-logs?action="+urlEsc("%' OR 1=1 --")+"&per_page=0&page=-3", rq{adm: cAdmin})
	c.do("activity-logs purge", "POST", "/api/admin/activity-logs/purge", rq{adm: cAdmin, body: map[string]any{}})
	c.do("activity-logs purge invalid body", "POST", "/api/admin/activity-logs/purge", rq{adm: cAdmin, body: map[string]any{"days": -1}})
	c.do("activity-logs list after purge", "GET", "/api/admin/activity-logs?per_page=2", rq{adm: cAdmin})
}

// ---------- 11 push (endpoint saja; pusher tiruan, tanpa kirim nyata) ----------

// pushSubSeq membuat kunci langganan deterministik (kunci privat P-256 tetap per urutan).
var pushSubSeq byte

func pushSub(t *testing.T, endpoint string) map[string]any {
	pushSubSeq++
	seed := bytes.Repeat([]byte{pushSubSeq}, 32)
	k, err := ecdh.P256().NewPrivateKey(seed)
	if err != nil {
		t.Fatal(err)
	}
	a := bytes.Repeat([]byte{pushSubSeq + 100}, 16)
	return map[string]any{"endpoint": endpoint, "expirationTime": nil, "keys": map[string]any{
		"p256dh": base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), "auth": base64.RawURLEncoding.EncodeToString(a)}}
}

func secPush(c *contractEnv) {
	tokA := login(c, "budi_uji")
	for _, rt := range [][2]string{{"GET", "/api/push/public-key"}, {"POST", "/api/push/subscribe"}, {"DELETE", "/api/push/subscribe"}} {
		c.do("noauth "+rt[0]+" "+rt[1], rt[0], rt[1], rq{})
	}
	c.do("admin push public-key", "GET", "/api/admin/push/public-key", rq{adm: cAdmin})
	c.do("admin push subscribe ok", "POST", "/api/admin/push/subscribe", rq{adm: cAdmin, body: pushSub(c.t, "https://fcm.googleapis.com/fcm/send/kontrak-admin-1")})
	c.do("admin push subscribe same endpoint again", "POST", "/api/admin/push/subscribe", rq{adm: cAdmin, body: pushSub(c.t, "https://fcm.googleapis.com/fcm/send/kontrak-admin-1")})
	c.do("admin push subscribe SSRF endpoint", "POST", "/api/admin/push/subscribe", rq{adm: cAdmin, body: pushSub(c.t, "https://127.0.0.1/x")})
	c.do("admin push subscribe http endpoint", "POST", "/api/admin/push/subscribe", rq{adm: cAdmin, body: pushSub(c.t, "http://fcm.googleapis.com/fcm/send/x")})
	c.do("admin push subscribe bad keys", "POST", "/api/admin/push/subscribe", rq{adm: cAdmin, body: map[string]any{"endpoint": "https://fcm.googleapis.com/fcm/send/x", "keys": map[string]any{"p256dh": "AAAA", "auth": "BBBB"}}})
	c.do("unusual admin push subscribe unknown field", "POST", "/api/admin/push/subscribe", rq{adm: cAdmin, body: func() map[string]any {
		b := pushSub(c.t, "https://fcm.googleapis.com/fcm/send/x2")
		b["extra"] = 1
		return b
	}()})
	c.do("admin push test", "POST", "/api/admin/push/test", rq{adm: cAdmin, body: map[string]any{}})
	c.do("admin push test invalid body", "POST", "/api/admin/push/test", rq{adm: cAdmin, body: map[string]any{"a": 1}})
	c.do("admin push public-key after subscribe", "GET", "/api/admin/push/public-key", rq{adm: cAdmin})
	c.do("admin push unsubscribe", "DELETE", "/api/admin/push/subscribe", rq{adm: cAdmin, body: map[string]any{"endpoint": "https://fcm.googleapis.com/fcm/send/kontrak-admin-1"}})
	c.do("admin push unsubscribe again", "DELETE", "/api/admin/push/subscribe", rq{adm: cAdmin, body: map[string]any{"endpoint": "https://fcm.googleapis.com/fcm/send/kontrak-admin-1"}})
	c.do("admin push unsubscribe invalid", "DELETE", "/api/admin/push/subscribe", rq{adm: cAdmin, body: map[string]any{}})

	c.do("customer push public-key", "GET", "/api/push/public-key", rq{tok: tokA})
	c.do("customer push subscribe ok", "POST", "/api/push/subscribe", rq{tok: tokA, body: pushSub(c.t, "https://fcm.googleapis.com/fcm/send/kontrak-pelanggan-1")})
	c.do("customer push subscribe invalid endpoint", "POST", "/api/push/subscribe", rq{tok: tokA, body: pushSub(c.t, "https://evil.example/x")})
	c.do("customer push subscribe not json", "POST", "/api/push/subscribe", rq{tok: tokA, raw: "xx", ct: "application/json"})
	c.do("customer push unsubscribe", "DELETE", "/api/push/subscribe", rq{tok: tokA, body: map[string]any{"endpoint": "https://fcm.googleapis.com/fcm/send/kontrak-pelanggan-1"}})
	c.do("customer push unsubscribe again", "DELETE", "/api/push/subscribe", rq{tok: tokA, body: map[string]any{"endpoint": "https://fcm.googleapis.com/fcm/send/kontrak-pelanggan-1"}})
	c.do("unusual customer push unsubscribe invalid", "DELETE", "/api/push/subscribe", rq{tok: tokA, body: map[string]any{"endpoint": 123}})
	// Subscribe adm + pelanggan dengan endpoint SAMA tidak boleh saling menghapus (audiens terpisah).
	c.do("push same endpoint as admin", "POST", "/api/admin/push/subscribe", rq{adm: cAdmin, body: pushSub(c.t, "https://fcm.googleapis.com/fcm/send/kontrak-bersama")})
	c.do("push same endpoint as customer", "POST", "/api/push/subscribe", rq{tok: tokA, body: pushSub(c.t, "https://fcm.googleapis.com/fcm/send/kontrak-bersama")})
	c.do("push same endpoint: customer unsubscribes", "DELETE", "/api/push/subscribe", rq{tok: tokA, body: map[string]any{"endpoint": "https://fcm.googleapis.com/fcm/send/kontrak-bersama"}})
	c.do("push same endpoint: admin unsubscribes (still there)", "DELETE", "/api/admin/push/subscribe", rq{adm: cAdmin, body: map[string]any{"endpoint": "https://fcm.googleapis.com/fcm/send/kontrak-bersama"}})
}

// ---------- 12 wilayah (admin) ----------

func secRegionsAdmin(c *contractEnv) {
	c.do("regions status", "GET", "/api/admin/regions/status", rq{adm: cAdmin})
	c.do("regions run get not found", "GET", "/api/admin/regions/runs/999999", rq{adm: cAdmin})
	c.do("regions run discard not found", "POST", "/api/admin/regions/runs/999999/discard", rq{adm: cAdmin, body: map[string]any{}})
	c.do("regions run save not found", "POST", "/api/admin/regions/runs/999999/save", rq{adm: cAdmin, body: map[string]any{}})
	r := c.must(c.do("regions fetch (mulai run)", "POST", "/api/admin/regions/fetch", rq{adm: cAdmin, body: map[string]any{}}), 202, "fetch")
	id := numf(r.Body["run"].(map[string]any)["id"])
	waitRun := func() string {
		var st string
		for i := 0; i < 200; i++ {
			c.db.Raw(`SELECT status FROM region_import_runs WHERE id = ?`, id).Scan(&st)
			if st != regions.RunRunning {
				return st
			}
			time.Sleep(25 * time.Millisecond)
		}
		return st
	}
	if st := waitRun(); st != regions.RunStaged {
		c.t.Fatalf("run wilayah harus staged, dapat %q", st)
	}
	rp := fmt.Sprintf("/api/admin/regions/runs/%d", id)
	c.do("regions run get staged", "GET", rp, rq{adm: cAdmin})
	c.do("regions save without csrf", "POST", rp+"/save", rq{adm: cAdmin, noCSRF: true, body: map[string]any{}})
	c.do("regions save ok", "POST", rp+"/save", rq{adm: cAdmin, body: map[string]any{}})
	c.do("regions save again (409)", "POST", rp+"/save", rq{adm: cAdmin, body: map[string]any{}})
	c.do("regions discard saved run", "POST", rp+"/discard", rq{adm: cAdmin, body: map[string]any{}})
	c.do("regions status after save", "GET", "/api/admin/regions/status", rq{adm: cAdmin})
	c.do("regions provinces public after save", "GET", "/api/regions/provinces", rq{})
	r = c.must(c.do("regions fetch second run", "POST", "/api/admin/regions/fetch", rq{adm: cAdmin, body: map[string]any{}}), 202, "fetch 2")
	id = numf(r.Body["run"].(map[string]any)["id"])
	waitRun()
	rp = fmt.Sprintf("/api/admin/regions/runs/%d", id)
	c.do("regions discard staged run", "POST", rp+"/discard", rq{adm: cAdmin, body: map[string]any{}})
	c.do("regions discard again", "POST", rp+"/discard", rq{adm: cAdmin, body: map[string]any{}})
	c.do("regions fetch with unexpected body", "POST", "/api/admin/regions/fetch", rq{adm: cAdmin, body: map[string]any{"x": 1}})
	waitIdle := func() {
		for i := 0; i < 200; i++ {
			var n int64
			c.db.Raw(`SELECT COUNT(*) FROM region_import_runs WHERE status = ?`, regions.RunRunning).Scan(&n)
			if n == 0 {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	waitIdle()
}
