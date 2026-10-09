package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFormatRupiah(t *testing.T) {
	for n, want := range map[uint64]string{0: "Rp 0", 5: "Rp 5", 999: "Rp 999", 1000: "Rp 1.000", 45000: "Rp 45.000",
		1234567: "Rp 1.234.567", 100000000: "Rp 100.000.000"} {
		if got := FormatRupiah(n); got != want {
			t.Errorf("FormatRupiah(%d) = %q, mau %q", n, got, want)
		}
	}
}

func TestEscapeMarkdown(t *testing.T) {
	cases := map[string]string{
		"Budi Santoso":            "Budi Santoso",
		"**tebal** _miring_":      `\*\*tebal\*\* \_miring\_`,
		"@everyone halo":          "@​everyone halo",
		"<@123456> [x](http://a)": `\<@` + "​" + `123456\> \[x\]\(http\://a\)`,
		"baris1\nbaris2\t\x00":    "baris1 baris2",
		"`kode` ~~coret~~ ||sp||": "\\`kode\\` \\~\\~coret\\~\\~ \\|\\|sp\\|\\|",
		"# judul > kutip":         `\# judul \> kutip`,
		"a‮b":                     "ab",
	}
	for in, want := range cases {
		if got := EscapeMarkdown(in, 0); got != want {
			t.Errorf("EscapeMarkdown(%q) = %q, mau %q", in, got, want)
		}
	}
	long := strings.Repeat("x", 100)
	if got := EscapeMarkdown(long, 64); got != strings.Repeat("x", 64)+"…" {
		t.Errorf("pemotongan salah: %q", got)
	}
}

func TestBuildPayloadMinimalNoMentions(t *testing.T) {
	b, err := BuildPayload(OrderEvent{Kind: KindCreated, OrderNo: "MS-261002-0001", CustomerName: "@everyone *Budi*",
		ItemCount: 3, Total: 135000, Status: "pending_payment", AdminURL: "https://store.mihan.web.id/admin/orders/1"})
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	am := p["allowed_mentions"].(map[string]any)
	if parse, ok := am["parse"].([]any); !ok || len(parse) != 0 {
		t.Fatalf("allowed_mentions.parse harus [] : %v", am)
	}
	s := string(b)
	for _, want := range []string{"Pesanan baru", "MS-261002-0001", "Rp 135.000", "Menunggu pembayaran", `"value":"3"`,
		"https://store.mihan.web.id/admin/orders/1", `\\*Budi\\*`} {
		if !strings.Contains(s, want) {
			t.Errorf("payload tidak memuat %q: %s", want, s)
		}
	}
	if strings.Contains(s, "@everyone") {
		t.Errorf("mention @everyone tidak dinetralkan: %s", s)
	}
	// Tidak ada field selain yang diizinkan (struktur OrderEvent tidak punya alamat/telepon).
	fields := p["embeds"].([]any)[0].(map[string]any)["fields"].([]any)
	var names []string
	for _, f := range fields {
		names = append(names, f.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "Nomor pesanan,Pemesan,Jumlah item,Total,Status" {
		t.Errorf("field embed: %v", names)
	}
	if _, err := BuildPayload(OrderEvent{Kind: "lain"}); err == nil {
		t.Error("jenis tidak dikenal harus error")
	}
	// URL admin aneh (javascript:) tidak dipakai.
	b, _ = BuildPayload(OrderEvent{Kind: KindPaid, OrderNo: "MS-1", AdminURL: "javascript:alert(1)"})
	if strings.Contains(string(b), "javascript") {
		t.Error("URL non-http tidak boleh dipakai")
	}
}

func TestValidateWebhookURL(t *testing.T) {
	ok := []string{"https://discord.com/api/webhooks/1/abc", "https://discordapp.com/api/webhooks/1/abc"}
	bad := []string{"http://discord.com/api/webhooks/1/abc", "https://evil.com/api/webhooks/1", "https://discord.com/other",
		"https://user:pw@discord.com/api/webhooks/1/a", "ftp://discord.com/api/webhooks/1", "bukan url"}
	for _, u := range ok {
		if err := ValidateWebhookURL(u, false); err != nil {
			t.Errorf("%s harus valid: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := ValidateWebhookURL(u, false); err == nil {
			t.Errorf("%s harus ditolak", u)
		}
	}
	if err := ValidateWebhookURL("http://e2e-runner:9000/discord", true); err != nil {
		t.Errorf("mode uji harus menerima server tiruan: %v", err)
	}
	if _, ok := New("", false).(Noop); !ok {
		t.Error("URL kosong harus Noop")
	}
	if _, ok := New("https://evil.example/x", false).(Noop); !ok {
		t.Error("URL tidak valid harus Noop")
	}
}

type logSink struct {
	mu   sync.Mutex
	msgs []string
}

func (l *logSink) logf(f string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, fmt.Sprintf(f, a...))
}

func (l *logSink) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.msgs, "\n")
}

func TestDiscordSendRetryAndNoURLInLogs(t *testing.T) {
	var hits atomic.Int32
	var lastBody atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		b, _ := io.ReadAll(r.Body)
		lastBody.Store(string(b))
		if n == 1 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	sink := &logSink{}
	n := New(srv.URL+"/api/webhooks/1/RAHASIA", true, WithRetryDelay(10*time.Millisecond), WithLogf(sink.logf)).(*Discord)
	start := time.Now()
	n.OrderEvent(OrderEvent{Kind: KindCreated, OrderNo: "MS-261002-0007", CustomerName: "Ani", ItemCount: 1, Total: 1000, Status: "pending_payment"})
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("OrderEvent harus asinkron")
	}
	n.Wait()
	if hits.Load() != 2 {
		t.Fatalf("harus 1x ulang setelah 500: %d", hits.Load())
	}
	if !strings.Contains(lastBody.Load().(string), "MS-261002-0007") {
		t.Fatal("body tidak sampai")
	}
	if strings.Contains(sink.all(), "RAHASIA") || strings.Contains(sink.all(), srv.URL) {
		t.Fatalf("URL webhook bocor ke log: %s", sink.all())
	}
}

func TestDiscordTimeoutGivesUp(t *testing.T) {
	var hits atomic.Int32
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-block:
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	defer close(block)
	sink := &logSink{}
	n := New(srv.URL+"/api/webhooks/1/RAHASIA", true, WithTimeout(100*time.Millisecond),
		WithRetryDelay(10*time.Millisecond), WithLogf(sink.logf)).(*Discord)
	n.OrderEvent(OrderEvent{Kind: KindCancelled, OrderNo: "MS-261002-0008", Status: "cancelled"})
	n.Wait()
	if hits.Load() != 2 {
		t.Fatalf("timeout harus dicoba ulang 1x: %d", hits.Load())
	}
	logs := sink.all()
	if !strings.Contains(logs, "menyerah") || !strings.Contains(logs, "timeout") {
		t.Fatalf("log kegagalan: %s", logs)
	}
	if strings.Contains(logs, "RAHASIA") || strings.Contains(logs, "127.0.0.1") {
		t.Fatalf("URL bocor ke log: %s", logs)
	}
}
