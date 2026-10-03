package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestComputeTotal(t *testing.T) {
	cases := []struct {
		sub, disc, ship, want int64
		err                   error
	}{
		{100000, 0, 0, 100000, nil},
		{100000, 15000, 20000, 105000, nil},
		{100000, 100000, 0, 0, nil},               // diskon = subtotal boleh
		{100000, 100000, 12000, 12000, nil},       // hanya ongkir
		{100000, 100001, 0, 0, errDiscountTooBig}, // diskon > subtotal
		{100000, -1, 0, 0, errAmountNegative},
		{100000, 0, -5, 0, errAmountNegative},
		{100000, 0, 10_000_000, 10_100_000, nil},
		{100000, 0, 10_000_001, 0, errShippingRange},
		{1_999_999_999, 1, 10_000_000, 2_009_999_998, nil},
	}
	for _, c := range cases {
		got, err := computeTotal(c.sub, c.disc, c.ship)
		if err != c.err || (err == nil && got != c.want) {
			t.Errorf("computeTotal(%d,%d,%d) = %d,%v; mau %d,%v", c.sub, c.disc, c.ship, got, err, c.want, c.err)
		}
		if err == nil && got < 0 {
			t.Errorf("total negatif: %d", got)
		}
	}
}

func TestStatusTransitions(t *testing.T) {
	type tc struct {
		from, to, actor, reason string
		ok                      bool
	}
	cases := []tc{
		{StatusPending, StatusPaid, actorAdmin, "", true},
		{StatusPending, StatusPaid, actorOwner, "", false},
		{StatusPending, StatusCancelled, actorAdmin, "", true},
		{StatusPending, StatusCancelled, actorOwner, "", true},
		{StatusPending, StatusCompleted, actorAdmin, "", false},
		{StatusPaid, StatusCompleted, actorAdmin, "", true},
		{StatusPaid, StatusCompleted, actorOwner, "", false},
		{StatusPaid, StatusCancelled, actorAdmin, "", false}, // alasan wajib
		{StatusPaid, StatusCancelled, actorAdmin, "   ", false},
		{StatusPaid, StatusCancelled, actorAdmin, "Stok habis", true},
		{StatusPaid, StatusCancelled, actorOwner, "x", false},
		{StatusPaid, StatusPending, actorAdmin, "", false},
		{StatusPaid, StatusPaid, actorAdmin, "", false},
		{StatusCompleted, StatusCancelled, actorAdmin, "x", false},
		{StatusCompleted, StatusPaid, actorAdmin, "", false},
		{StatusCancelled, StatusPending, actorAdmin, "", false},
		{StatusCancelled, StatusPaid, actorAdmin, "", false},
		{StatusPending, "shipped", actorAdmin, "", false},
	}
	for _, c := range cases {
		err := checkTransition(c.from, c.to, c.actor, c.reason)
		if (err == nil) != c.ok {
			t.Errorf("%s -> %s oleh %s (alasan %q): err=%v, mau ok=%v", c.from, c.to, c.actor, c.reason, err, c.ok)
		}
	}
	if err := checkTransition(StatusPaid, StatusCancelled, actorAdmin, ""); err != errReasonNeeded {
		t.Errorf("paid->cancelled tanpa alasan harus errReasonNeeded: %v", err)
	}
	if err := checkTransition(StatusCompleted, StatusCancelled, actorAdmin, "x"); err != errFinalStatus {
		t.Errorf("status final: %v", err)
	}
	if got := strings.Join(allowedNext(StatusPending, actorAdmin), ","); got != "paid,cancelled" {
		t.Errorf("allowedNext pending admin: %s", got)
	}
	if got := strings.Join(allowedNext(StatusPaid, actorAdmin), ","); got != "completed,cancelled" {
		t.Errorf("allowedNext paid admin: %s", got)
	}
	if got := allowedNext(StatusCompleted, actorAdmin); len(got) != 0 {
		t.Errorf("completed final: %v", got)
	}
	if got := strings.Join(allowedNext(StatusPending, actorOwner), ","); got != "cancelled" {
		t.Errorf("allowedNext pending owner: %s", got)
	}
}

func TestOrderNumber(t *testing.T) {
	// 2026-10-02 18:30 UTC = 3 Oktober 01:30 WIB -> tanggal WIB dipakai.
	ts := time.Date(2026, 10, 2, 18, 30, 0, 0, time.UTC)
	if got := orderNumber(1, ts); got != "MS-261003-0001" {
		t.Errorf("orderNumber = %s", got)
	}
	if got := orderNumber(42, time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)); got != "MS-261002-0042" {
		t.Errorf("orderNumber = %s", got)
	}
	if got := orderNumber(123456, ts); got != "MS-261003-123456" {
		t.Errorf("orderNumber > 4 digit = %s", got)
	}
	for s, ok := range map[string]bool{"MS-261002-0001": true, "MS-261002-123456": true, "MS-261002-001": false,
		"ms-261002-0001": false, "MS-2610-0001": false, "MS-261002-0001' OR 1=1": false, "": false} {
		if validOrderNo(s) != ok {
			t.Errorf("validOrderNo(%q) != %v", s, ok)
		}
	}
	if len(orderNumber(18446744073709551615, ts)) > 30 {
		t.Error("nomor terlalu panjang")
	}
}

func validCheckout() CheckoutInput {
	return CheckoutInput{RecipientName: " Budi  Santoso ", RecipientPhone: "0812-3456-7890", Address: "Jl. Mawar No. 5\nRT 01/02",
		City: "diabaikan", PostalCode: "15111", Note: "Tolong cepat", IdempotencyKey: "3F2504E0-4F89-41D3-9A0C-0305E82C3301",
		ProvinceCode: "31", RegencyCode: "31.74", DistrictCode: "31.74.06", VillageCode: "31.74.06.1001"}
}

func TestValidateCheckout(t *testing.T) {
	f, err := validateCheckout(validCheckout())
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "Budi Santoso" || f.Phone != "+6281234567890" || *f.PostalCode != "15111" ||
		f.IdemKey != "3f2504e0-4f89-41d3-9a0c-0305e82c3301" || f.Address != "Jl. Mawar No. 5\nRT 01/02" ||
		f.Region.Village != "31.74.06.1001" {
		t.Fatalf("hasil normalisasi salah: %+v", f)
	}
	bad := map[string]struct {
		mut   func(*CheckoutInput)
		field string
	}{
		"nama kosong":        {func(c *CheckoutInput) { c.RecipientName = " \x00 " }, "recipientName"},
		"nama panjang":       {func(c *CheckoutInput) { c.RecipientName = strings.Repeat("a", 101) }, "recipientName"},
		"telepon kosong":     {func(c *CheckoutInput) { c.RecipientPhone = "" }, "recipientPhone"},
		"telepon salah":      {func(c *CheckoutInput) { c.RecipientPhone = "12345" }, "recipientPhone"},
		"provinsi kosong":    {func(c *CheckoutInput) { c.ProvinceCode = "" }, "provinceCode"},
		"kab/kota kosong":    {func(c *CheckoutInput) { c.RegencyCode = " " }, "regencyCode"},
		"kecamatan kosong":   {func(c *CheckoutInput) { c.DistrictCode = "" }, "districtCode"},
		"desa kosong":        {func(c *CheckoutInput) { c.VillageCode = "" }, "villageCode"},
		"alamat pendek":      {func(c *CheckoutInput) { c.Address = "abc" }, "address"},
		"alamat panjang":     {func(c *CheckoutInput) { c.Address = strings.Repeat("a", 501) }, "address"},
		"kodepos kosong":     {func(c *CheckoutInput) { c.PostalCode = "" }, "postalCode"},
		"kodepos huruf":      {func(c *CheckoutInput) { c.PostalCode = "1511A" }, "postalCode"},
		"kodepos 4 digit":    {func(c *CheckoutInput) { c.PostalCode = "1511" }, "postalCode"},
		"catatan panjang":    {func(c *CheckoutInput) { c.Note = strings.Repeat("a", 501) }, "note"},
		"idempotency kosong": {func(c *CheckoutInput) { c.IdempotencyKey = "" }, "idempotencyKey"},
		"idempotency salah":  {func(c *CheckoutInput) { c.IdempotencyKey = "bukan-uuid" }, "idempotencyKey"},
		"klien lama":         {func(c *CheckoutInput) { c.ProvinceCode, c.RegencyCode, c.DistrictCode, c.VillageCode = "", "", "", "" }, "region"},
	}
	for name, tc := range bad {
		in := validCheckout()
		tc.mut(&in)
		_, err := validateCheckout(in)
		var fe *fieldError
		if !errors.As(err, &fe) || fe.Field != tc.field {
			t.Errorf("%s harus ditolak dengan field %s: %v", name, tc.field, err)
		}
	}
	// Klien lama: pesan meminta memperbarui halaman.
	in := validCheckout()
	in.ProvinceCode, in.RegencyCode, in.DistrictCode, in.VillageCode = "", "", "", ""
	if _, err := validateCheckout(in); err == nil || !strings.Contains(err.Error(), "Perbarui halaman lalu coba lagi") {
		t.Errorf("pesan klien lama: %v", err)
	}
	// Karakter kontrol & bidi dibuang, catatan opsional.
	in = validCheckout()
	in.RecipientName = "Ani\u202e\x07 Putri"
	in.Note = "  "
	f, err = validateCheckout(in)
	if err != nil || f.Name != "Ani Putri" || f.Note != nil {
		t.Fatalf("pembersihan: %+v %v", f, err)
	}
}

func TestValidateSettings(t *testing.T) {
	s := func(v string) *string { return &v }
	out, err := validateSettings(SettingsInput{StoreWhatsapp: s("0812 3456 7890"), BankName: s(" BCA "),
		BankAccountNumber: s("345-227-1335"), BankAccountHolder: s("Qomariah\nAkmala"), PaymentNote: s("Transfer\nsesuai total")})
	if err != nil {
		t.Fatal(err)
	}
	if out["store_whatsapp"] != "+6281234567890" || out["bank_name"] != "BCA" || out["bank_account_holder"] != "Qomariah Akmala" ||
		out["payment_note"] != "Transfer\nsesuai total" || out["bank_account_number"] != "345-227-1335" {
		t.Fatalf("hasil: %v", out)
	}
	if out, _ := validateSettings(SettingsInput{BankName: s("BCA")}); len(out) != 1 {
		t.Fatalf("hanya kunci yang dikirim: %v", out)
	}
	if out, err := validateSettings(SettingsInput{StoreWhatsapp: s("")}); err != nil || out["store_whatsapp"] != "" {
		t.Fatalf("WA kosong = belum diisi: %v %v", out, err)
	}
	for name, in := range map[string]SettingsInput{
		"wa salah":       {StoreWhatsapp: s("12345")},
		"wa huruf":       {StoreWhatsapp: s("0812abc")},
		"rekening huruf": {BankAccountNumber: s("abc123")},
		"bank panjang":   {BankName: s(strings.Repeat("a", 101))},
		"catatan":        {PaymentNote: s(strings.Repeat("a", 501))},
	} {
		if _, err := validateSettings(in); err == nil {
			t.Errorf("%s harus ditolak", name)
		}
	}
	if settingValue("BELUM DIISI") != "" || settingValue(" BCA ") != "BCA" {
		t.Error("settingValue")
	}
}

func TestCustomerRoutesRequireLogin(t *testing.T) {
	app := NewApp(Config{CORSAllowedOrigins: []string{"https://store.mihan.web.id"}, PublicBaseURL: defaultPublicBaseURL})
	h := newRouter(app)
	for _, rt := range []struct{ m, p string }{
		{"GET", "/api/cart"}, {"POST", "/api/cart/items"}, {"PUT", "/api/cart/items"}, {"DELETE", "/api/cart/items/1"},
		{"DELETE", "/api/cart"}, {"GET", "/api/orders"}, {"POST", "/api/orders"}, {"GET", "/api/orders/MS-261002-0001"},
		{"POST", "/api/orders/MS-261002-0001/cancel"}, {"GET", "/api/store-info"},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(rt.m, rt.p, strings.NewReader(`{}`))
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s tanpa login: %d (mau 401)", rt.m, rt.p, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s harus no-store", rt.m, rt.p)
		}
		// Token rusak juga 401.
		w = httptest.NewRecorder()
		r = httptest.NewRequest(rt.m, rt.p, strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer token_lama")
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s token rusak: %d", rt.m, rt.p, w.Code)
		}
	}
	// Admin pesanan tanpa konfigurasi/token Access -> 503/401, tidak pernah 200.
	for _, p := range []string{"/api/admin/orders", "/api/admin/orders/1", "/api/admin/settings"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code == 200 {
			t.Errorf("%s tanpa Access tidak boleh 200", p)
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	for in, want := range map[string]string{"": defaultPublicBaseURL, "https://store.mihan.web.id/": "https://store.mihan.web.id",
		"javascript:alert(1)": defaultPublicBaseURL, "https://a.b/?x=1": defaultPublicBaseURL, "http://localhost:3000": "http://localhost:3000"} {
		if got := normalizeBaseURL(in); got != want {
			t.Errorf("normalizeBaseURL(%q) = %q", in, got)
		}
	}
}
