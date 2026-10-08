package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHashAndVerifyPassword(t *testing.T) {
	h, err := HashPassword("rahasia-123")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("format PHC salah: %q", h[:30])
	}
	parts := strings.Split(h, "$")
	if len(parts) != 6 || len(parts[4]) != 22 || len(parts[5]) != 43 { // 16 B salt, 32 B hash (base64 tanpa padding)
		t.Fatalf("panjang salt/hash salah")
	}
	if ok, err := VerifyPassword("rahasia-123", h); err != nil || !ok {
		t.Fatalf("password benar ditolak: %v", err)
	}
	if ok, _ := VerifyPassword("rahasia-124", h); ok {
		t.Fatal("password salah diterima")
	}
	h2, _ := HashPassword("rahasia-123")
	if h == h2 {
		t.Fatal("salt tidak acak")
	}
}

func TestVerifyPasswordRejectsBadHash(t *testing.T) {
	bad := []string{
		"", "plaintext", "$argon2i$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0$aGFzaA",
		"$argon2id$v=18$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$" + strings.Repeat("A", 43),
		"$argon2id$v=19$m=99999999,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$" + strings.Repeat("A", 43),
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$" + strings.Repeat("A", 43),
	}
	for _, b := range bad {
		if ok, err := VerifyPassword("x", b); ok || err == nil {
			t.Errorf("hash rusak tidak ditolak: %q", b)
		}
	}
}

func TestDummyHashVerifies(t *testing.T) {
	initDummyHash()
	if ok, err := VerifyPassword("apa saja", dummyHash); ok || err != nil {
		t.Fatalf("dummy hash harus valid formatnya dan tidak cocok: ok=%v err=%v", ok, err)
	}
}

func TestValidatePassword(t *testing.T) {
	cases := map[string]bool{
		"1234567": false, "12345678": true, strings.Repeat("a", 128): true,
		strings.Repeat("a", 129): false, "pässwörd": true, "\xff\xfe\xfd\xfc\xfb\xfa\xf9\xf8": false,
	}
	for pw, want := range cases {
		if got := ValidatePassword(pw) == nil; got != want {
			t.Errorf("ValidatePassword(len=%d) = %v, mau %v", len(pw), got, want)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	ok := map[string]string{
		"  Budi@Example.COM ": "budi@example.com",
		"a.b+tag@mail.co.id":  "a.b+tag@mail.co.id",
	}
	for in, want := range ok {
		got, err := NormalizeEmail(in)
		if err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q,%v mau %q", in, got, err, want)
		}
	}
	bad := []string{"", "budi", "budi@", "@x.com", "budi@localhost", "Budi <budi@x.com>", "a b@x.com",
		"budi@x.com.", strings.Repeat("a", 250) + "@x.com", "budi@x.com (komentar)"}
	for _, in := range bad {
		if _, err := NormalizeEmail(in); err == nil {
			t.Errorf("NormalizeEmail(%q) seharusnya ditolak", in)
		}
	}
}

func TestNormalizeUsername(t *testing.T) {
	ok := map[string]string{" Budi_99 ": "budi_99", "abc": "abc", strings.Repeat("a", 30): strings.Repeat("a", 30)}
	for in, want := range ok {
		got, err := NormalizeUsername(in)
		if err != nil || got != want {
			t.Errorf("NormalizeUsername(%q) = %q,%v", in, got, err)
		}
	}
	bad := []string{"ab", strings.Repeat("a", 31), "budi-99", "budi.99", "budi 99", "búdi", "",
		"admin", "ROOT", "support", "system", "mihan", "_admin_", "mihan_official", "superadmin1", "toko"}
	for _, in := range bad {
		if _, err := NormalizeUsername(in); err == nil {
			t.Errorf("NormalizeUsername(%q) seharusnya ditolak", in)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	ok := map[string]string{
		"":                  "",
		"   ":               "",
		"081234567890":      "+6281234567890",
		"6281234567890":     "+6281234567890",
		"+6281234567890":    "+6281234567890",
		"0812-3456-7890":    "+6281234567890",
		"+62 812 3456 7890": "+6281234567890",
		"(0812) 3456.7890":  "+6281234567890",
		"08123":             "+628123", // 6 digit -> ditolak di bawah
	}
	delete(ok, "08123")
	for in, want := range ok {
		got, err := NormalizePhone(in)
		if err != nil || got != want {
			t.Errorf("NormalizePhone(%q) = %q,%v mau %q", in, got, err, want)
		}
	}
	bad := []string{"08123", "021555123", "+1555123456", "12345678", "0812abc4567", "+62812345678901234", "62", "+", "++6281234567"}
	for _, in := range bad {
		if _, err := NormalizePhone(in); err == nil {
			t.Errorf("NormalizePhone(%q) seharusnya ditolak", in)
		}
	}
}

// Varian Unicode dari keyboard/kontak HP diseragamkan; galat spesifik per jenis kesalahan.
// Kasus yang sama dipakai frontend (src/__tests__/phone.test.js).
func TestNormalizePhoneUnicodeAndMessages(t *testing.T) {
	ok := map[string]string{
		"0812\u00a03456\u00a07890":         "+6281234567890", // NBSP
		"+62\u202f812\u20113456\u20117890": "+6281234567890", // narrow NBSP + non-breaking hyphen
		"0812\u20103456\u20127890":         "+6281234567890", // hyphen, figure dash
		"0812\u20133456\u20147890":         "+6281234567890", // en/em dash
		"0812\u22123456\u22127890":         "+6281234567890", // minus
		"\u202a+62 812-3456-7890\u202c":    "+6281234567890", // pembungkus bidi (WhatsApp)
		"\u200b081234567890\ufeff":         "+6281234567890", // zero-width, BOM
		"\uff10\uff18\uff11\uff12\uff13\uff14\uff15\uff16\uff17\uff18\uff19\uff10": "+6281234567890",
		"\u0660\u0668\u0661\u0662\u0663\u0664\u0665\u0666\u0667\u0668\u0669\u0660": "+6281234567890",
		"\u06f0\u06f8\u06f1\u06f2\u06f3\u06f4\u06f5\u06f6\u06f7\u06f8\u06f9\u06f0": "+6281234567890",
		"\uff0b62 812 3456 7890": "+6281234567890",
		"+62 0812 3456 7890":     "+6281234567890",
		"+620812-3456-7890":      "+6281234567890",
		"620812 3456 7890":       "+6281234567890",
		"\u00a0\u2003":           "",
	}
	for in, want := range ok {
		got, err := NormalizePhone(in)
		if err != nil || got != want {
			t.Errorf("NormalizePhone(%q) = %q,%v mau %q", in, got, err, want)
		}
	}
	bad := map[string]error{
		"08123":                errPhoneShort,
		"+62812":               errPhoneShort,
		"+6281111111111081234": errPhoneLong,
		"+62812345678901234":   errPhoneLong,
		"0812abc4567":          errPhoneChars,
		"++6281234567":         errPhoneChars,
		"0812/3456/7890":       errPhoneChars,
		"62+81234567890":       errPhoneChars,
		"021555123":            errPhonePrefix,
		"+1555123456":          errPhonePrefix,
		"12345678":             errPhonePrefix,
		"62":                   errPhonePrefix,
		"+":                    errPhonePrefix,
	}
	for in, want := range bad {
		if _, err := NormalizePhone(in); err != want {
			t.Errorf("NormalizePhone(%q) galat %v, mau %v", in, err, want)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	ok := map[string]string{
		"  Budi   Santoso ":        "Budi Santoso",
		"Budi\x00\x07Santoso":      "BudiSantoso",
		"Siti\tAminah\n":           "Siti Aminah",
		"Ana‮evil":                 "Anaevil",
		strings.Repeat("é", 100):   strings.Repeat("é", 100),
		"Ny. Dewi (Toko Berkah) 👍": "Ny. Dewi (Toko Berkah) 👍",
	}
	for in, want := range ok {
		got, err := NormalizeName(in)
		if err != nil || got != want {
			t.Errorf("NormalizeName(%q) = %q,%v mau %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "   ", "\x00\x01", strings.Repeat("a", 101)} {
		if _, err := NormalizeName(in); err == nil {
			t.Errorf("NormalizeName(%q) seharusnya ditolak", in)
		}
	}
}

func TestNormalizeIdentifier(t *testing.T) {
	if v, isEmail, ok := normalizeIdentifier(" Budi@X.com "); v != "budi@x.com" || !isEmail || !ok {
		t.Error("email identifier salah")
	}
	if v, isEmail, ok := normalizeIdentifier("Budi_99"); v != "budi_99" || isEmail || !ok {
		t.Error("username identifier salah")
	}
	if _, _, ok := normalizeIdentifier(strings.Repeat("a", 31)); ok {
		t.Error("username terlalu panjang harus !ok")
	}
}

func TestSessionToken(t *testing.T) {
	tok, hash, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 43 || !wellFormedToken(tok) {
		t.Fatalf("token tidak berbentuk benar (len=%d)", len(tok))
	}
	if len(hash) != 64 || hash != HashToken(tok) || hash == tok {
		t.Fatal("hash token salah")
	}
	// Vektor uji SHA-256 yang diketahui.
	if HashToken("abc") != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal("HashToken bukan SHA-256 hex")
	}
	tok2, _, _ := NewSessionToken()
	if tok == tok2 {
		t.Fatal("token tidak acak")
	}
	for _, bad := range []string{"", "token_demo_mihanstore.com_1", "token_x", strings.Repeat("A", 42), strings.Repeat("A", 43) + "=", strings.Repeat("!", 43)} {
		if wellFormedToken(bad) {
			t.Errorf("token %q seharusnya tidak valid", bad)
		}
	}
}

func TestBearerToken(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer abc")
	if bearerToken(r) != "abc" {
		t.Error("bearer gagal")
	}
	r.Header.Set("Authorization", "bearer   xyz ")
	if bearerToken(r) != "xyz" {
		t.Error("bearer case-insensitive gagal")
	}
	r.Header.Set("Authorization", "Basic abc")
	if bearerToken(r) != "" {
		t.Error("basic harus diabaikan")
	}
}

func TestPublicID(t *testing.T) {
	id, err := newPublicID()
	if err != nil || len(id) != 36 || id[14] != '4' || strings.Count(id, "-") != 4 {
		t.Fatalf("UUID tidak valid: %q", id)
	}
}

func TestLockout(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var failed uint8
	var until *time.Time
	for i := 1; i <= 4; i++ {
		failed, until = nextFailedState(failed, now)
		if until != nil || int(failed) != i {
			t.Fatalf("percobaan %d: failed=%d terkunci=%v", i, failed, until != nil)
		}
	}
	failed, until = nextFailedState(failed, now)
	if until == nil || !until.Equal(now.Add(15*time.Minute)) || failed != 0 {
		t.Fatalf("kegagalan ke-5 harus mengunci 15 menit")
	}
	if !isLocked(until, now.Add(14*time.Minute)) {
		t.Error("harus masih terkunci di menit 14")
	}
	if isLocked(until, now.Add(15*time.Minute)) || isLocked(nil, now) {
		t.Error("harus terbuka setelah 15 menit / bila nil")
	}
	if f, u := nextFailedState(254, now); u == nil || f != 0 {
		t.Error("overflow failed_logins harus tetap mengunci")
	}
}

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
	if rateKey(net.ParseIP("2001:db8:1:2:3:4:5:6")) != "2001:db8:1:2::/64" {
		t.Error("IPv6 harus dikelompokkan /64")
	}
	if len(ipBytes(net.ParseIP("1.2.3.4"))) != 4 || len(ipBytes(net.ParseIP("2001:db8::1"))) != 16 {
		t.Error("ipBytes salah")
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
		return decodeJSON(w, r, &x, false)
	}
	if err := run(`{"a":"1"}`); err != nil {
		t.Errorf("valid ditolak: %v", err)
	}
	if err := run(`{"a":"1","b":2}`); err != errBadJSON {
		t.Errorf("field tak dikenal harus ditolak: %v", err)
	}
	if err := run(`{"a":"1"}{"a":"2"}`); err != errBadJSON {
		t.Errorf("data tambahan harus ditolak: %v", err)
	}
	if err := run(`{"a":"` + strings.Repeat("x", 17<<10) + `"}`); err != errBodyTooLarge {
		t.Errorf("body >16KB harus ditolak: %v", err)
	}
	if err := run(``); err != errBadJSON {
		t.Errorf("body kosong harus ditolak: %v", err)
	}
}

func TestTurnstileFailClosed(t *testing.T) {
	v := NewTurnstileVerifier(Config{TurnstileSecret: ""})
	if ok, err := v.Verify(t.Context(), "apa-saja", ""); ok || err != errTurnstileNotConfigured {
		t.Fatal("tanpa secret harus fail-closed")
	}
	v = NewTurnstileVerifier(Config{TurnstileSecret: "", AllowNoTurnstile: true})
	if ok, _ := v.Verify(t.Context(), "apa-saja", ""); !ok {
		t.Fatal("ALLOW_NO_TURNSTILE=true harus melewati (dev)")
	}
	// Dengan secret: gunakan server palsu, pastikan form terkirim benar dan hasil dihormati.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("secret") != "s3cr&t=x" || r.Form.Get("response") != "tok&en" {
			w.Write([]byte(`{"success":false}`))
			return
		}
		w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()
	v = NewTurnstileVerifier(Config{TurnstileSecret: "s3cr&t=x"})
	v.endpoint = srv.URL
	if ok, err := v.Verify(t.Context(), "tok&en", "1.2.3.4"); !ok || err != nil {
		t.Fatalf("verifikasi harus lulus dengan encoding form benar: %v", err)
	}
	if ok, _ := v.Verify(t.Context(), "", ""); ok {
		t.Fatal("token kosong harus gagal")
	}
}

func TestSecurityHeadersAndHealth(t *testing.T) {
	app := NewApp(Config{CORSAllowedOrigins: defaultCORSOrigins})
	h := newRouter(app)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("/health: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/health/ready", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "mysql") {
		t.Fatalf("/health/ready tanpa DB harus 503 tanpa detail: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/auth/verify", strings.NewReader(`{"token":"token_x"}`))
	h.ServeHTTP(w, r)
	if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("verify token palsu: %d cache=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	// CORS: origin diizinkan vs tidak.
	for origin, allowed := range map[string]bool{"https://store.mihan.web.id": true, "https://evil.example": false} {
		w = httptest.NewRecorder()
		r = httptest.NewRequest("GET", "/api/products", nil)
		r.Header.Set("Origin", origin)
		h.ServeHTTP(w, r)
		got := w.Header().Get("Access-Control-Allow-Origin")
		if (got == origin) != allowed || got == "*" {
			t.Errorf("CORS %s: ACAO=%q", origin, got)
		}
		if w.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Error("credentials tidak boleh diizinkan")
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/products", nil))
	// Produk kini dari database: tanpa DB -> 503 tanpa detail internal.
	if w.Code != 503 || strings.Contains(w.Body.String(), "mysql") {
		t.Fatalf("/api/products tanpa DB harus 503: %d", w.Code)
	}
}
