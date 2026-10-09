//go:build contract

// Tes KONTRAK API (golden). Merakit server di dalam proses (newRouter + NewApp) terhadap database
// UJI sementara, memanggil semua rute dengan permintaan sah / tanpa login / tidak valid / tak lazim,
// lalu membandingkan (atau merekam) status, header, dan badan respons ke testdata/golden/*.json
// setelah menormalkan nilai yang berubah-ubah (waktu, UUID, token, nama berkas acak).
//
// Jalankan lewat skrip: backend/scripts/contract.sh            (pembanding, default)
//
//	UPDATE_GOLDEN=1 backend/scripts/contract.sh   (rekam ulang)
//
// Skrip menyiapkan & menghapus database uji sementara (database harus BARU: id auto-increment
// ikut menjadi bagian kontrak).
package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

// Golden tetap di backend/testdata/golden (tidak dipindah saat refactor; isinya harus identik).
const goldenDir = "../../testdata/golden"

// ---------- normalisasi ----------

var (
	reUUID  = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	reOrder = regexp.MustCompile(`MS-[0-9]{6}-([0-9]+)`)
	reTime  = regexp.MustCompile(`[0-9]{4}-[0-9]{2}-[0-9]{2}[T ][0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-][0-9]{2}:?[0-9]{2})?`)
	reHTTPD = regexp.MustCompile(`(Mon|Tue|Wed|Thu|Fri|Sat|Sun), [0-9]{2} [A-Z][a-z]{2} [0-9]{4} [0-9:]{8} GMT`)
	reJWT   = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)
	reToken = regexp.MustCompile(`[A-Za-z0-9_-]{32,}`)
	// Username otomatis admin (user_ + 6 heksa acak) dan port server tiruan lokal.
	reAutoUser = regexp.MustCompile(`user_[0-9a-f]{6}`)
	reLocalSrv = regexp.MustCompile(`127\.0\.0\.1:[0-9]+`)
)

// volatileKeys: kunci JSON yang nilainya selalu berubah (durasi) -> dinolkan.
var volatileKeys = map[string]bool{"durationMs": true}

// normalizer memberi nomor urut kemunculan pertama (<TOKEN#1>, <TOKEN#2>, ...) sehingga hubungan
// antar nilai tetap terlihat tetapi nilai acaknya tidak.
type normalizer struct{ seen map[string]int }

func newNormalizer() *normalizer { return &normalizer{seen: map[string]int{}} }

func (n *normalizer) tag(kind, v string) string {
	k := kind + "|" + v
	if _, ok := n.seen[k]; !ok {
		c := 0
		for kk := range n.seen {
			if strings.HasPrefix(kk, kind+"|") {
				c++
			}
		}
		n.seen[k] = c + 1
	}
	return fmt.Sprintf("<%s#%d>", kind, n.seen[k])
}

func (n *normalizer) str(s string) string {
	s = reJWT.ReplaceAllStringFunc(s, func(m string) string { return n.tag("JWT", m) })
	s = reUUID.ReplaceAllStringFunc(s, func(m string) string { return n.tag("UUID", strings.ToLower(m)) })
	s = reOrder.ReplaceAllString(s, "MS-<DATE>-$1")
	s = reTime.ReplaceAllString(s, "<TIME>")
	s = reHTTPD.ReplaceAllString(s, "<HTTPDATE>")
	s = reAutoUser.ReplaceAllString(s, "user_<RAND>")
	s = reLocalSrv.ReplaceAllString(s, "127.0.0.1:<PORT>")
	s = reToken.ReplaceAllStringFunc(s, func(m string) string { return n.tag("TOKEN", m) })
	return s
}

func (n *normalizer) val(v any) any {
	switch x := v.(type) {
	case string:
		return n.str(x)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = n.val(x[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			if volatileKeys[k] {
				out[k] = json.Number("0")
				continue
			}
			out[k] = n.val(vv)
		}
		return out
	}
	return v
}

// ---------- rekaman ----------

type goldenEntry struct {
	Name    string            `json:"name"`
	Request string            `json:"request"`
	ReqBody any               `json:"requestBody,omitempty"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body,omitempty"`
	Text    string            `json:"bodyText,omitempty"`
	Binary  *goldenBinary     `json:"binary,omitempty"`
}

type goldenBinary struct {
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Header respons yang tidak ikut direkam (selalu berubah / turunan).
var skipHeaders = map[string]bool{"Date": true, "Content-Length": true}

type cres struct {
	Code int
	Body map[string]any // badan JSON MENTAH (tanpa normalisasi), untuk mengambil nilai
	Raw  []byte
	Hdr  http.Header
}

// rq: satu permintaan.
type rq struct {
	m, p   string
	tok    string            // Authorization: Bearer
	adm    string            // email admin (Cf-Access-Jwt-Assertion)
	body   any               // JSON
	raw    string            // badan mentah (dengan ct)
	ct     string            // Content-Type untuk raw
	hdr    map[string]string // header tambahan
	noCSRF bool              // jangan kirim header CSRF admin
	file   []byte            // multipart field "file"
	fname  string
}

type contractEnv struct {
	t     *testing.T
	app   *App
	h     http.Handler
	db    *gorm.DB
	norm  *normalizer
	cur   []goldenEntry
	names map[string]bool
	seen  map[string]map[int]int // rute -> status -> jumlah
	clock atomic.Int64
	pushR *contractPushRec
}

func (c *contractEnv) doReq(name string, q rq) cres {
	c.t.Helper()
	if c.names[name] {
		c.t.Fatalf("nama skenario ganda: %s", name)
	}
	c.names[name] = true

	var rd *bytes.Reader
	ct := ""
	var reqJSON any
	switch {
	case q.file != nil:
		var buf bytes.Buffer
		ct = multipartBody(&buf, q.file, q.fname)
		rd = bytes.NewReader(buf.Bytes())
	case q.raw != "":
		rd = bytes.NewReader([]byte(q.raw))
		ct = q.ct
		_ = json.Unmarshal([]byte(q.raw), &reqJSON)
	case q.body != nil:
		b, _ := json.Marshal(q.body)
		rd = bytes.NewReader(b)
		ct = "application/json"
		_ = json.Unmarshal(b, &reqJSON)
	default:
		rd = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(q.m, q.p, rd)
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	r.Header.Set("User-Agent", "contract-test")
	if q.adm != "" || strings.HasPrefix(q.p, "/api/admin") {
		r.RemoteAddr = "172.22.0.2:5555"
		r.Header.Set("X-Real-IP", "198.51.100.20")
		if q.adm != "" {
			r.Header.Set("Cf-Access-Jwt-Assertion", accessToken2(c.t, q.adm))
		}
		if q.m != "GET" && q.m != "HEAD" && q.m != "OPTIONS" && !q.noCSRF {
			if ct == "" {
				r.Header.Set("Content-Type", "application/json")
			}
			r.Header.Set("X-Requested-With", adminRequestedWith)
			r.Header.Set("Origin", "https://store.mihan.web.id")
			r.Header.Set("Sec-Fetch-Site", "same-origin")
		}
	} else {
		r.RemoteAddr = "203.0.113.7:5555"
	}
	if q.tok != "" {
		r.Header.Set("Authorization", "Bearer "+q.tok)
	}
	for k, v := range q.hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	res := w.Result()
	raw := w.Body.Bytes()
	out := cres{Code: res.StatusCode, Raw: raw, Hdr: res.Header}
	_ = json.Unmarshal(raw, &out.Body)

	e := goldenEntry{Name: name, Request: c.norm.str(q.m + " " + q.p), Status: res.StatusCode, Headers: map[string]string{}}
	if reqJSON != nil {
		e.ReqBody = c.norm.val(reqJSON)
	} else if q.file != nil {
		e.ReqBody = fmt.Sprintf("<multipart file %q, %d byte>", q.fname, len(q.file))
	} else if q.raw != "" {
		e.ReqBody = c.norm.str(q.raw)
	}
	for k, vs := range res.Header {
		if skipHeaders[k] {
			continue
		}
		e.Headers[k] = c.norm.str(strings.Join(vs, ", "))
	}
	respCT := res.Header.Get("Content-Type")
	switch {
	case len(raw) == 0:
	case strings.HasPrefix(respCT, "application/json"):
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			e.Text = c.norm.str(string(raw))
		} else {
			e.Body = c.norm.val(v)
		}
	case strings.HasPrefix(respCT, "text/"):
		e.Text = c.norm.str(string(raw))
	default:
		sum := sha256.Sum256(raw)
		e.Binary = &goldenBinary{Bytes: len(raw), SHA256: hex.EncodeToString(sum[:])}
	}
	c.cur = append(c.cur, e)
	c.record(q.m, q.p, res.StatusCode)
	return out
}

// do: pintasan dengan nama skenario.
func (c *contractEnv) do(name, method, path string, q rq) cres {
	c.t.Helper()
	q.m, q.p = method, path
	return c.doReq(name, q)
}

// ---------- cakupan rute ----------

type routeDef struct {
	key string
	re  *regexp.Regexp
}

var contractRoutes = buildRoutes()

func buildRoutes() []routeDef {
	list := []string{
		"GET /health", "GET /health/ready",
		"GET /api/products", "GET /api/products/search", "GET /api/categories",
		"GET /api/regions/provinces", "GET /api/regions/regencies/{code}", "GET /api/regions/districts/{code}", "GET /api/regions/villages/{code}",
		"POST /api/auth/register", "POST /api/auth/login", "POST /api/auth/verify", "POST /api/auth/logout", "GET /api/auth/me",
		"POST /api/auth/google", "POST /api/auth/google/complete",
		"GET /api/cart", "DELETE /api/cart", "POST /api/cart/items", "PUT /api/cart/items", "DELETE /api/cart/items/{id}", "POST /api/cart/ack-prices",
		"GET /api/orders", "POST /api/orders", "GET /api/orders/{no}", "POST /api/orders/{no}/cancel",
		"POST /api/orders/{no}/payment-proof", "GET /api/orders/{no}/payment-proof", "DELETE /api/orders/{no}/payment-proof",
		"GET /api/store-info", "GET /api/push/public-key", "POST /api/push/subscribe", "DELETE /api/push/subscribe",
		"GET /api/admin/me", "GET /api/admin/summary", "GET /api/admin/settings", "PUT /api/admin/settings",
		"GET /api/admin/products", "POST /api/admin/products", "GET /api/admin/products/{id}", "PUT /api/admin/products/{id}",
		"DELETE /api/admin/products/{id}", "PATCH /api/admin/products/{id}/active",
		"POST /api/admin/products/{id}/image", "DELETE /api/admin/products/{id}/image",
		"GET /api/admin/categories", "POST /api/admin/categories", "PUT /api/admin/categories/{id}", "DELETE /api/admin/categories/{id}",
		"GET /api/admin/orders", "GET /api/admin/orders/{id}", "PATCH /api/admin/orders/{id}/pricing", "POST /api/admin/orders/{id}/confirm",
		"GET /api/admin/orders/{id}/shipping-suggestions", "POST /api/admin/orders/{id}/shipping-default",
		"GET /api/admin/orders/{id}/payment-proof", "PATCH /api/admin/orders/{id}/status", "PATCH /api/admin/orders/{id}/note",
		"GET /api/admin/customers", "GET /api/admin/customers/{id}", "PATCH /api/admin/customers/{id}/alias",
		"GET /api/admin/activity-logs", "POST /api/admin/activity-logs/purge",
		"GET /api/admin/push/public-key", "POST /api/admin/push/subscribe", "DELETE /api/admin/push/subscribe", "POST /api/admin/push/test",
		"GET /api/admin/regions/status", "POST /api/admin/regions/fetch", "GET /api/admin/regions/runs/{id}",
		"POST /api/admin/regions/runs/{id}/save", "POST /api/admin/regions/runs/{id}/discard",
	}
	var out []routeDef
	for _, k := range list {
		parts := strings.SplitN(k, " ", 2)
		pat := regexp.QuoteMeta(parts[1])
		pat = strings.NewReplacer(`\{code\}`, `[^/]+`, `\{id\}`, `[^/]+`, `\{no\}`, `[^/]+`).Replace(pat)
		out = append(out, routeDef{k, regexp.MustCompile(`^` + parts[0] + ` ` + pat + `$`)})
	}
	return out
}

func (c *contractEnv) record(method, path string, status int) {
	p := path
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	key := method + " " + p
	for _, rd := range contractRoutes {
		if rd.re.MatchString(key) {
			if c.seen[rd.key] == nil {
				c.seen[rd.key] = map[int]int{}
			}
			c.seen[rd.key][status]++
			return
		}
	}
}

// ---------- berkas golden ----------

func (c *contractEnv) flush(t *testing.T, file string) {
	t.Helper()
	entries := c.cur
	c.cur = nil
	checkGolden(t, file, entries)
}

func marshalGolden(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func checkGolden(t *testing.T, file string, v any) {
	t.Helper()
	got := marshalGolden(v)
	path := filepath.Join(goldenDir, file)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("berkas golden %s belum ada (%v). Rekam dulu: UPDATE_GOLDEN=1 backend/scripts/contract.sh", path, err)
	}
	if bytes.Equal(want, got) {
		return
	}
	t.Errorf("KONTRAK BERUBAH di %s:\n%s", path, diffGolden(want, got))
}

// diffGolden membandingkan dua dokumen golden entri demi entri (berdasarkan "name") dan
// menampilkan selisih baris yang jelas. Untuk dokumen non-daftar: diff baris biasa.
func diffGolden(want, got []byte) string {
	var w, g []map[string]any
	if json.Unmarshal(want, &w) != nil || json.Unmarshal(got, &g) != nil {
		return lineDiff(string(want), string(got), 60)
	}
	idx := func(l []map[string]any) map[string]map[string]any {
		m := map[string]map[string]any{}
		for _, e := range l {
			if n, ok := e["name"].(string); ok {
				m[n] = e
			}
		}
		return m
	}
	wi, gi := idx(w), idx(g)
	var sb strings.Builder
	for _, e := range w {
		n, _ := e["name"].(string)
		ge, ok := gi[n]
		if !ok {
			fmt.Fprintf(&sb, "- skenario HILANG: %s\n", n)
			continue
		}
		a, b := string(marshalGolden(e)), string(marshalGolden(ge))
		if a != b {
			fmt.Fprintf(&sb, "~ skenario BERUBAH: %s\n%s\n", n, lineDiff(a, b, 40))
		}
	}
	for _, e := range g {
		n, _ := e["name"].(string)
		if _, ok := wi[n]; !ok {
			fmt.Fprintf(&sb, "+ skenario BARU: %s\n", n)
		}
	}
	if sb.Len() == 0 {
		return lineDiff(string(want), string(got), 60)
	}
	return sb.String()
}

// lineDiff: diff baris sederhana (LCS), baris "-" = golden, "+" = sekarang.
func lineDiff(a, b string, max int) string {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	n, m := len(x), len(y)
	l := make([][]int, n+1)
	for i := range l {
		l[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				l[i][j] = l[i+1][j+1] + 1
			} else if l[i+1][j] >= l[i][j+1] {
				l[i][j] = l[i+1][j]
			} else {
				l[i][j] = l[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case x[i] == y[j]:
			i++
			j++
		case l[i+1][j] >= l[i][j+1]:
			out = append(out, "    - "+x[i])
			i++
		default:
			out = append(out, "    + "+y[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "    - "+x[i])
	}
	for ; j < m; j++ {
		out = append(out, "    + "+y[j])
	}
	if len(out) > max {
		out = append(out[:max], fmt.Sprintf("    ... (%d baris selisih lagi)", len(out)-max))
	}
	return strings.Join(out, "\n")
}

// coverageDoc: ringkasan rute -> kode status yang teramati (golden tersendiri).
func (c *contractEnv) coverageDoc() map[string]any {
	doc := map[string]any{}
	for _, rd := range contractRoutes {
		var st []string
		for code, n := range c.seen[rd.key] {
			st = append(st, fmt.Sprintf("%d x%d", code, n))
		}
		sort.Strings(st)
		doc[rd.key] = st
	}
	return doc
}

func (c *contractEnv) uncovered() []string {
	var miss []string
	for _, rd := range contractRoutes {
		if len(c.seen[rd.key]) == 0 {
			miss = append(miss, rd.key)
		}
	}
	return miss
}

func urlEsc(s string) string { return url.QueryEscape(s) }

var _ = time.Second
