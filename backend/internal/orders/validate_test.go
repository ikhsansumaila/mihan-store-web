package orders

import (
	"errors"
	"strings"
	"testing"

	"mihanstore/internal/identity"
	"mihanstore/internal/platform"
)

func validCheckout() CheckoutInput {
	return CheckoutInput{RecipientName: " Budi  Santoso ", RecipientPhone: "0812-3456-7890", Address: "Jl. Mawar No. 5\nRT 01/02",
		City: "diabaikan", PostalCode: "15111", Note: "Tolong cepat", IdempotencyKey: "3F2504E0-4F89-41D3-9A0C-0305E82C3301",
		ProvinceCode: "31", RegencyCode: "31.74", DistrictCode: "31.74.06", VillageCode: "31.74.06.1001"}
}

func TestValidateCheckout(t *testing.T) {
	f, err := validateCheckout(validCheckout(), identity.NormalizePhone)
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
		_, err := validateCheckout(in, identity.NormalizePhone)
		var fe *platform.FieldError
		if !errors.As(err, &fe) || fe.Field != tc.field {
			t.Errorf("%s harus ditolak dengan field %s: %v", name, tc.field, err)
		}
	}
	// Klien lama: pesan meminta memperbarui halaman.
	in := validCheckout()
	in.ProvinceCode, in.RegencyCode, in.DistrictCode, in.VillageCode = "", "", "", ""
	if _, err := validateCheckout(in, identity.NormalizePhone); err == nil || !strings.Contains(err.Error(), "Perbarui halaman lalu coba lagi") {
		t.Errorf("pesan klien lama: %v", err)
	}
	// Karakter kontrol & bidi dibuang, catatan opsional.
	in = validCheckout()
	in.RecipientName = "Ani\u202e\x07 Putri"
	in.Note = "  "
	f, err = validateCheckout(in, identity.NormalizePhone)
	if err != nil || f.Name != "Ani Putri" || f.Note != nil {
		t.Fatalf("pembersihan: %+v %v", f, err)
	}
}

func TestValidateSettings(t *testing.T) {
	s := func(v string) *string { return &v }
	out, err := validateSettings(SettingsInput{StoreWhatsapp: s("0812 3456 7890"), BankName: s(" BCA "),
		BankAccountNumber: s("345-227-1335"), BankAccountHolder: s("Qomariah\nAkmala"), PaymentNote: s("Transfer\nsesuai total")}, identity.NormalizePhone)
	if err != nil {
		t.Fatal(err)
	}
	if out["store_whatsapp"] != "+6281234567890" || out["bank_name"] != "BCA" || out["bank_account_holder"] != "Qomariah Akmala" ||
		out["payment_note"] != "Transfer\nsesuai total" || out["bank_account_number"] != "345-227-1335" {
		t.Fatalf("hasil: %v", out)
	}
	if out, _ := validateSettings(SettingsInput{BankName: s("BCA")}, identity.NormalizePhone); len(out) != 1 {
		t.Fatalf("hanya kunci yang dikirim: %v", out)
	}
	if out, err := validateSettings(SettingsInput{StoreWhatsapp: s("")}, identity.NormalizePhone); err != nil || out["store_whatsapp"] != "" {
		t.Fatalf("WA kosong = belum diisi: %v %v", out, err)
	}
	for name, in := range map[string]SettingsInput{
		"wa salah":       {StoreWhatsapp: s("12345")},
		"wa huruf":       {StoreWhatsapp: s("0812abc")},
		"rekening huruf": {BankAccountNumber: s("abc123")},
		"bank panjang":   {BankName: s(strings.Repeat("a", 101))},
		"catatan":        {PaymentNote: s(strings.Repeat("a", 501))},
	} {
		if _, err := validateSettings(in, identity.NormalizePhone); err == nil {
			t.Errorf("%s harus ditolak", name)
		}
	}
	if settingValue("BELUM DIISI") != "" || settingValue(" BCA ") != "BCA" {
		t.Error("settingValue")
	}
}
