package main

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"mihanstore/notify"
)

func TestNormalizeAlias(t *testing.T) {
	long := strings.Repeat("é", 100)
	cases := []struct {
		in   string
		want any // string, nil (dihapus), atau error
	}{
		{"Bu Siti Toko Maju", "Bu Siti Toko Maju"},
		{"  Bu   Siti \t Toko\nMaju  ", "Bu Siti Toko Maju"},
		{"Bu\u00a0Siti", "Bu Siti"},                      // NBSP dirapikan
		{"\u202eBu Siti\u202c", "Bu Siti"},               // bidi override dibuang
		{"Bu\u200b Siti\u2066\u2069\ufeff", "Bu Siti"},   // zero-width, isolate, BOM
		{"Bu\x00Si\x07ti", "BuSiti"},                     // kontrol dibuang
		{"", nil},                                        // kosong = hapus alias
		{"   \u200b\t ", nil},                            // hanya spasi/tak terlihat = hapus
		{long, long},                                     // tepat 100 karakter (rune) boleh
		{long + "x", errAliasLong},                       // 101 ditolak
		{"  " + long + "  ", long},                       // panjang dihitung SETELAH dirapikan
		{"Pak Budi 🙂 *VIP* <b>", "Pak Budi 🙂 *VIP* <b>"}, // teks bebas disimpan apa adanya (ditampilkan sebagai teks)
		{"\xffBu", "Bu"},                                 // UTF-8 rusak dibuang
	}
	for _, c := range cases {
		got, err := NormalizeAlias(c.in)
		switch w := c.want.(type) {
		case error:
			if err != w {
				t.Errorf("%q: err %v, mau %v", c.in, err, w)
			}
		case nil:
			if err != nil || got != nil {
				t.Errorf("%q: %v %v, mau nil", c.in, got, err)
			}
		case string:
			if err != nil || got == nil || *got != w {
				t.Errorf("%q: %v %v, mau %q", c.in, got, err, w)
			}
		}
	}
}

func TestParseAliasInput(t *testing.T) {
	dec := func(s string) aliasInput {
		var in aliasInput
		if err := json.Unmarshal([]byte(s), &in); err != nil {
			t.Fatal(err)
		}
		return in
	}
	if _, err := parseAliasInput(dec(`{}`)); err == nil {
		t.Error("tanpa field alias harus galat")
	}
	if _, err := parseAliasInput(dec(`{"alias": 5}`)); err == nil {
		t.Error("alias angka harus galat")
	}
	if _, err := parseAliasInput(dec(`{"alias": ["a"]}`)); err == nil {
		t.Error("alias array harus galat")
	}
	if a, err := parseAliasInput(dec(`{"alias": null}`)); err != nil || a != nil {
		t.Errorf("null = hapus: %v %v", a, err)
	}
	if a, err := parseAliasInput(dec(`{"alias": "  "}`)); err != nil || a != nil {
		t.Errorf("kosong = hapus: %v %v", a, err)
	}
	if a, err := parseAliasInput(dec(`{"alias": " Bu  Siti "}`)); err != nil || a == nil || *a != "Bu Siti" {
		t.Errorf("normalisasi: %v %v", a, err)
	}
}

func TestCustomerSortWhitelist(t *testing.T) {
	for _, k := range []string{"", "newest", "orders", "spent"} {
		if s, ok := customerOrderBy(k); !ok || s == "" {
			t.Errorf("%q harus diterima", k)
		}
	}
	if s, _ := customerOrderBy(""); s != customerSorts["newest"] {
		t.Error("bawaan harus newest")
	}
	for _, k := range []string{"u.id", "name", "created_at DESC", "orders; DROP TABLE users", "SPENT", "total_spent"} {
		if _, ok := customerOrderBy(k); ok {
			t.Errorf("%q harus ditolak", k)
		}
	}
}

func TestDisplayCustomerNameAndDiscordPayload(t *testing.T) {
	alias := "Bu Siti *Toko* @everyone"
	blank := "   "
	if got := displayCustomerName("Siti Aminah", &alias); got != alias {
		t.Errorf("alias harus dipakai: %q", got)
	}
	if got := displayCustomerName("Siti Aminah", nil); got != "Siti Aminah" {
		t.Errorf("tanpa alias = nama akun: %q", got)
	}
	if got := displayCustomerName("Siti Aminah", &blank); got != "Siti Aminah" {
		t.Errorf("alias kosong = nama akun: %q", got)
	}

	field := func(body []byte, name string) string {
		var p struct {
			Embeds []struct {
				Fields []struct{ Name, Value string } `json:"fields"`
			} `json:"embeds"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			t.Fatal(err)
		}
		for _, f := range p.Embeds[0].Fields {
			if f.Name == name {
				return f.Value
			}
		}
		return ""
	}
	ev := notify.OrderEvent{Kind: notify.KindCreated, OrderNo: "MS-261003-0001", ItemCount: 2, Total: 90000,
		Status: "pending_payment", AdminURL: "https://store.mihan.web.id/admin/orders/1"}

	ev.CustomerName = displayCustomerName("Siti Aminah", &alias)
	b, err := notify.BuildPayload(ev)
	if err != nil {
		t.Fatal(err)
	}
	got := field(b, "Pemesan")
	// Markdown & mention dinetralkan seperti nama biasa.
	if got != `Bu Siti \*Toko\* @`+"\u200b"+`everyone` || strings.Contains(string(b), "Siti Aminah") {
		t.Errorf("Pemesan dengan alias: %q / %s", got, b)
	}
	// Alias panjang dipotong seperti nama (64 karakter + elipsis).
	longAlias := strings.Repeat("a", 100)
	ev.CustomerName = displayCustomerName("Siti", &longAlias)
	b, _ = notify.BuildPayload(ev)
	if got := field(b, "Pemesan"); got != strings.Repeat("a", 64)+"…" {
		t.Errorf("alias dipotong: %q", got)
	}
	ev.CustomerName = displayCustomerName("Siti Aminah", nil)
	b, _ = notify.BuildPayload(ev)
	if got := field(b, "Pemesan"); got != "Siti Aminah" {
		t.Errorf("tanpa alias: %q", got)
	}
}

// Alias tidak boleh mungkin ikut ke DTO pelanggan: struct User/UserDTO tidak memetakan kolom alias.
func TestAliasNotInCustomerStructs(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(User{}), reflect.TypeOf(UserDTO{})} {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if strings.Contains(strings.ToLower(f.Name+" "+string(f.Tag)), "alias") {
				t.Errorf("%s.%s memetakan alias", typ.Name(), f.Name)
			}
		}
	}
	b, _ := json.Marshal(toDTO(&User{Name: "x"}))
	if strings.Contains(string(b), "alias") {
		t.Errorf("UserDTO memuat alias: %s", b)
	}
}

func TestCustomerRoutesRequireAdmin(t *testing.T) {
	// Belum dikonfigurasi -> 503 (fail-closed).
	h := newRouter(NewApp(Config{CORSAllowedOrigins: defaultCORSOrigins}))
	for _, p := range []string{"/api/admin/customers", "/api/admin/customers/1"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, adminReq("GET", p, "x.y.z", nil))
		if w.Code != 503 {
			t.Errorf("%s belum dikonfigurasi: %d", p, w.Code)
		}
	}
	app := NewApp(accessCfg())
	app.access = newTestAccess(t)
	h = newRouter(app)
	good := signJWT(t, testKey, "k1", "RS256", accessClaimsOK())
	cases := []struct {
		name, method, path, token string
		hdr                       map[string]string
		status                    int
	}{
		{"daftar tanpa token", "GET", "/api/admin/customers", "", nil, 401},
		{"detail tanpa token", "GET", "/api/admin/customers/1", "", nil, 401},
		{"alias tanpa token", "PATCH", "/api/admin/customers/1/alias", "", nil, 401},
		{"token sesi pelanggan (Bearer) bukan Access", "GET", "/api/admin/customers", "", map[string]string{"Authorization": "Bearer abc"}, 401},
		{"email bukan admin", "GET", "/api/admin/customers", signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "email", "x@y.co")), nil, 403},
		{"PATCH tanpa CSRF", "PATCH", "/api/admin/customers/1/alias", good, nil, 403},
		{"PATCH origin asing", "PATCH", "/api/admin/customers/1/alias", good, map[string]string{
			"Content-Type": "application/json", "X-Requested-With": adminRequestedWith, "Origin": "https://evil.example"}, 403},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, adminReq(c.method, c.path, c.token, c.hdr))
		if w.Code != c.status {
			t.Errorf("%s: %d mau %d (%s)", c.name, w.Code, c.status, w.Body.String())
		}
	}
}
