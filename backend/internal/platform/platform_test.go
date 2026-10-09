package platform

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rl := NewRateLimiter(3, time.Minute)
	rl.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if ok, _ := rl.Allow("1.2.3.4"); !ok {
			t.Fatalf("percobaan %d seharusnya diizinkan", i+1)
		}
	}
	if ok, wait := rl.Allow("1.2.3.4"); ok || wait <= 0 {
		t.Fatal("percobaan ke-4 seharusnya ditolak dengan waktu tunggu")
	}
	if ok, _ := rl.Allow("5.6.7.8"); !ok {
		t.Fatal("IP lain tidak boleh terpengaruh")
	}
	now = now.Add(time.Minute)
	if ok, _ := rl.Allow("1.2.3.4"); !ok {
		t.Fatal("setelah jendela habis harus diizinkan lagi")
	}
	// Peta penuh: kunci kedaluwarsa dibersihkan.
	rl2 := NewRateLimiter(1, time.Minute)
	rl2.maxKeys = 2
	rl2.now = func() time.Time { return now }
	rl2.Allow("a")
	rl2.Allow("b")
	if ok, _ := rl2.Allow("c"); ok {
		t.Fatal("peta penuh oleh kunci aktif harus menolak")
	}
	now = now.Add(2 * time.Minute)
	if ok, _ := rl2.Allow("c"); !ok || len(rl2.entries) != 1 {
		t.Fatal("kunci kedaluwarsa harus disapu")
	}
}

func TestClientIP(t *testing.T) {
	res := NewIPResolver([]string{"172.16.0.0/12"})
	mk := func(remote string, hdr map[string]string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return r
	}
	cases := []struct {
		name   string
		remote string
		hdr    map[string]string
		want   string
	}{
		{"langsung tanpa proxy: header diabaikan", "8.8.8.8:1234", map[string]string{"X-Real-IP": "1.1.1.1", "CF-Connecting-IP": "2.2.2.2"}, "8.8.8.8"},
		{"lewat NPM tanpa cloudflare", "172.18.0.5:1234", map[string]string{"X-Real-IP": "36.1.2.3"}, "36.1.2.3"},
		{"CF-Connecting-IP palsu dari non-cloudflare", "172.18.0.5:1234", map[string]string{"X-Real-IP": "36.1.2.3", "CF-Connecting-IP": "9.9.9.9"}, "36.1.2.3"},
		{"lewat cloudflare + NPM", "172.18.0.5:1234", map[string]string{"X-Real-IP": "162.158.1.1", "CF-Connecting-IP": "36.9.9.9"}, "36.9.9.9"},
		{"X-Forwarded-For tanpa X-Real-IP", "172.18.0.5:1234", map[string]string{"X-Forwarded-For": "5.5.5.5, 36.1.2.3"}, "36.1.2.3"},
		{"header sampah", "172.18.0.5:1234", map[string]string{"X-Real-IP": "bukan-ip"}, "172.18.0.5"},
	}
	for _, c := range cases {
		if got := res.ClientIP(mk(c.remote, c.hdr)); got.String() != c.want {
			t.Errorf("%s: dapat %v mau %s", c.name, got, c.want)
		}
	}
	if RateKey(net.ParseIP("2001:db8:1:2:3:4:5:6")) != "2001:db8:1:2::/64" {
		t.Error("IPv6 harus dikelompokkan /64")
	}
	if len(IPBytes(net.ParseIP("1.2.3.4"))) != 4 || len(IPBytes(net.ParseIP("2001:db8::1"))) != 16 {
		t.Error("IPBytes salah")
	}
}

func TestDecodeJSON(t *testing.T) {
	type X struct {
		A string `json:"a"`
	}
	run := func(body string) error {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		var x X
		return DecodeJSON(w, r, &x, false)
	}
	if err := run(`{"a":"1"}`); err != nil {
		t.Errorf("valid ditolak: %v", err)
	}
	if err := run(`{"a":"1","b":2}`); err != ErrBadJSON {
		t.Errorf("field tak dikenal harus ditolak: %v", err)
	}
	if err := run(`{"a":"1"}{"a":"2"}`); err != ErrBadJSON {
		t.Errorf("data tambahan harus ditolak: %v", err)
	}
	if err := run(`{"a":"` + strings.Repeat("x", 17<<10) + `"}`); err != ErrBodyTooLarge {
		t.Errorf("body >16KB harus ditolak: %v", err)
	}
	if err := run(``); err != ErrBadJSON {
		t.Errorf("body kosong harus ditolak: %v", err)
	}
}

func TestEscapeLike(t *testing.T) {
	if got := EscapeLike(`50%_a\b`); got != `50\%\_a\\b` {
		t.Fatalf("escapeLike: %q", got)
	}
}
