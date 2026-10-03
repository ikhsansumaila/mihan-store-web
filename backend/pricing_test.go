package main

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func fx(minQty, rupiah int64) PriceTier {
	return PriceTier{MinQty: minQty, Type: TierFixed, ValueX100: rupiah * 100}
}
func pc(minQty, pctX100 int64) PriceTier {
	return PriceTier{MinQty: minQty, Type: TierPercent, ValueX100: pctX100}
}

func TestEffectiveUnitPriceBoundaries(t *testing.T) {
	tiers := []PriceTier{pc(50, 1000), fx(10, 42000)} // sengaja tidak urut
	cases := []struct {
		qty, want, tier int64
	}{
		{1, 45000, 0},   // di bawah semua jenjang
		{9, 45000, 0},   // tepat sebelum jenjang
		{10, 42000, 10}, // tepat di min_qty
		{49, 42000, 10},
		{50, 40500, 50}, // 45000 * 90% = 40500
		{999, 40500, 50},
	}
	for _, c := range cases {
		got, tier := effectiveUnitPrice(45000, tiers, c.qty)
		if got != c.want || tier != c.tier {
			t.Errorf("qty %d: dapat %d (jenjang %d), mau %d (jenjang %d)", c.qty, got, tier, c.want, c.tier)
		}
	}
	// Tanpa jenjang: harga dasar.
	if got, tier := effectiveUnitPrice(45000, nil, 500); got != 45000 || tier != 0 {
		t.Fatalf("tanpa jenjang: %d %d", got, tier)
	}
	// qty < 2 tidak pernah memakai jenjang (walau min_qty 2).
	if got, _ := effectiveUnitPrice(1000, []PriceTier{fx(2, 900)}, 1); got != 1000 {
		t.Fatalf("qty 1: %d", got)
	}
	if got, tier := effectiveUnitPrice(1000, []PriceTier{fx(2, 900)}, 2); got != 900 || tier != 2 {
		t.Fatalf("qty 2: %d %d", got, tier)
	}
}

func TestEffectiveUnitPricePercentRoundingHalfUp(t *testing.T) {
	cases := []struct {
		base, pctX100, want int64
	}{
		{1001, 5000, 501},    // 500,5 -> 501 (half up)
		{999, 1500, 849},     // 849,15 -> 849
		{3, 5000, 2},         // 1,5 -> 2
		{45000, 1250, 39375}, // 12,5%
		{10, 3333, 7},        // 6,667 -> 7
		{10, 3500, 7},        // 6,5 -> 7 (half up, bukan banker's rounding)
		{10, 4500, 6},        // 5,5 -> 6
		{3, 9999, 1},         // 0,0003 -> 0 -> minimal 1
		{1, 5000, 1},         // 0,5 -> 1, tetapi tidak lebih murah dari dasar -> harga dasar
	}
	for _, c := range cases {
		got, _ := effectiveUnitPrice(c.base, []PriceTier{pc(2, c.pctX100)}, 5)
		if got != c.want {
			t.Errorf("dasar %d diskon %d/100%%: dapat %d, mau %d", c.base, c.pctX100, got, c.want)
		}
	}
	if _, tier := effectiveUnitPrice(1, []PriceTier{pc(2, 5000)}, 5); tier != 0 {
		t.Fatalf("jenjang yang tidak lebih murah tidak boleh dilaporkan dipakai: %d", tier)
	}
}

func TestEffectiveUnitPriceMinWithBaseAndFloor(t *testing.T) {
	// fixed lebih mahal dari harga dasar -> harga dasar (min), jenjang tidak dianggap dipakai.
	if got, tier := effectiveUnitPrice(40000, []PriceTier{fx(10, 42000)}, 10); got != 40000 || tier != 0 {
		t.Fatalf("min(harga jenjang, dasar): %d %d", got, tier)
	}
	// Minimal 1 rupiah.
	if p := tierPrice(5, PriceTier{MinQty: 2, Type: TierFixed, ValueX100: 0}); p != 1 {
		t.Fatalf("minimal 1: %d", p)
	}
}

func TestNextTierHint(t *testing.T) {
	tiers := []PriceTier{fx(10, 42000), fx(50, 40000), fx(1000, 30000)}
	n := nextTier(45000, tiers, 7, maxItemQty)
	if n == nil || n.MinQty != 10 || n.UnitPrice != 42000 || n.MoreQty != 3 {
		t.Fatalf("next tier dari 7: %+v", n)
	}
	n = nextTier(45000, tiers, 10, maxItemQty)
	if n == nil || n.MinQty != 50 || n.MoreQty != 40 {
		t.Fatalf("next tier dari 10: %+v", n)
	}
	// Jenjang 1000 tidak bisa dicapai (maks 999 per produk) -> tidak disarankan.
	if n := nextTier(45000, tiers, 60, maxItemQty); n != nil {
		t.Fatalf("jenjang di atas 999 tidak boleh disarankan: %+v", n)
	}
	if n := nextTier(45000, nil, 1, maxItemQty); n != nil {
		t.Fatalf("tanpa jenjang: %+v", n)
	}
}

func tin(minQty, typ, value string) TierInput {
	return TierInput{MinQty: json.Number(minQty), Type: typ, Value: json.Number(value)}
}

func tierErrs(t *testing.T, err error) []TierError {
	t.Helper()
	var tve *TierValidationError
	if !errors.As(err, &tve) {
		t.Fatalf("harus TierValidationError, dapat %v", err)
	}
	return tve.Errors
}

func TestValidateTiersOK(t *testing.T) {
	got, err := validateTiers(45000, []TierInput{tin("50", "percent", "10"), tin("10", "fixed", "42000"), tin("100", "PERCENT", "12.5")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].MinQty != 10 || got[1].MinQty != 50 || got[2].ValueX100 != 1250 || got[2].Type != TierPercent {
		t.Fatalf("hasil: %+v", got)
	}
	if got, err := validateTiers(45000, nil); err != nil || len(got) != 0 {
		t.Fatalf("daftar kosong harus boleh: %v %v", got, err)
	}
	// Batas nilai persen.
	for _, v := range []string{"0.01", "99.99", "5", "5.5"} {
		if _, err := validateTiers(1_000_000, []TierInput{tin("2", "percent", v)}); err != nil {
			t.Errorf("persen %s harus boleh: %v", v, err)
		}
	}
}

func TestValidateTiersRejects(t *testing.T) {
	cases := []struct {
		name string
		in   []TierInput
		want string
	}{
		{"min_qty 1", []TierInput{tin("1", "fixed", "100")}, "jumlah minimal harus bilangan bulat 2 sampai 1.000.000"},
		{"min_qty terlalu besar", []TierInput{tin("1000001", "fixed", "100")}, "2 sampai 1.000.000"},
		{"min_qty desimal", []TierInput{tin("2.5", "fixed", "100")}, "bilangan bulat"},
		{"jenis tidak dikenal", []TierInput{tin("5", "gratis", "100")}, "jenis potongan"},
		{"fixed desimal", []TierInput{tin("5", "fixed", "100.5")}, "harga (Rp) harus bilangan bulat"},
		{"fixed nol", []TierInput{tin("5", "fixed", "0")}, "harga (Rp) harus bilangan bulat minimal 1"},
		{"persen nol", []TierInput{tin("5", "percent", "0")}, "0,01 sampai 99,99"},
		{"persen 100", []TierInput{tin("5", "percent", "100")}, "0,01 sampai 99,99"},
		{"persen 3 desimal", []TierInput{tin("5", "percent", "1.234")}, "maksimal 2 angka desimal"},
		{"persen negatif", []TierInput{tin("5", "percent", "-1")}, "0,01 sampai 99,99"},
		{"duplikat", []TierInput{tin("5", "fixed", "900"), tin("5", "fixed", "800")}, "jumlah minimal 5 sudah dipakai jenjang 1"},
		{"tidak lebih murah dari dasar", []TierInput{tin("5", "fixed", "1000")}, "harus lebih murah dari harga dasar Rp 1.000"},
		{"lebih mahal dari dasar", []TierInput{tin("5", "fixed", "1500")}, "harus lebih murah dari harga dasar"},
		{"tidak monoton (sama)", []TierInput{tin("5", "fixed", "900"), tin("10", "percent", "10")}, "harus lebih murah dari jenjang min. 5 (Rp 900)"},
		{"tidak monoton (naik)", []TierInput{tin("10", "fixed", "800"), tin("5", "fixed", "700")}, "Jenjang 1 (min. 10): harga efektif Rp 800 harus lebih murah dari jenjang min. 5 (Rp 700)"},
	}
	for _, c := range cases {
		_, err := validateTiers(1000, c.in)
		errs := tierErrs(t, err)
		if !strings.Contains(errs[0].Message+err.Error(), c.want) {
			t.Errorf("%s: pesan %q tidak memuat %q", c.name, err.Error(), c.want)
		}
	}
	// Maksimal 20 jenjang.
	var many []TierInput
	for i := 0; i < 21; i++ {
		many = append(many, tin(strconv.Itoa(i+2), "fixed", "1"))
	}
	if errs := tierErrs(t, func() error { _, e := validateTiers(1000, many); return e }()); !strings.Contains(errs[0].Message, "Maksimal 20") {
		t.Fatalf("21 jenjang: %v", errs)
	}
	// Index & minQty kesalahan sesuai posisi input (untuk UI per baris).
	_, err := validateTiers(1000, []TierInput{tin("10", "fixed", "900"), tin("20", "fixed", "950")})
	errs := tierErrs(t, err)
	if len(errs) != 1 || errs[0].Index != 1 || errs[0].MinQty != 20 {
		t.Fatalf("index kesalahan: %+v", errs)
	}
}

func TestValidateTiersBasePriceChange(t *testing.T) {
	stored := []PriceTier{fx(10, 42000), pc(50, 1000)}
	// Harga dasar turun ke 42000: jenjang fixed 42000 tidak lagi lebih murah -> ditolak.
	_, err := validateTiers(42000, tierInputsFrom(stored))
	errs := tierErrs(t, err)
	if errs[0].MinQty != 10 || !strings.Contains(errs[0].Message, "Jenjang 1 (min. 10)") {
		t.Fatalf("pesan harus menyebut jenjang bermasalah: %+v", errs)
	}
	tve := err.(*TierValidationError)
	tve.Lead = "Harga dasar baru Rp 42.000 membuat jenjang grosir tidak valid."
	if !strings.HasPrefix(tve.Error(), "Harga dasar baru Rp 42.000") || !strings.Contains(tve.Error(), "min. 10") {
		t.Fatalf("pesan lengkap: %s", tve.Error())
	}
	// Harga dasar naik sedikit: percent mengikuti (46000*90% = 41400 < 42000), tetap valid.
	if _, err := validateTiers(46000, tierInputsFrom(stored)); err != nil {
		t.Fatalf("naik harus tetap valid: %v", err)
	}
	// Naik banyak: percent ikut naik (60000*90% = 54000) melewati jenjang fixed -> tidak monoton, ditolak.
	_, err = validateTiers(60000, tierInputsFrom(stored))
	if errs := tierErrs(t, err); errs[0].MinQty != 50 || !strings.Contains(errs[0].Message, "lebih murah dari jenjang min. 10") {
		t.Fatalf("naik banyak: %+v", errs)
	}
}

func TestTierDTOsAndSummaries(t *testing.T) {
	d := tierDTOs(45000, []PriceTier{pc(50, 1250), fx(10, 42000)})
	if len(d) != 2 || d[0].MinQty != 10 || d[0].UnitPrice != 42000 || d[0].Value != 42000 || d[1].Value != 12.5 || d[1].UnitPrice != 39375 {
		t.Fatalf("dto: %+v", d)
	}
	if b, _ := json.Marshal(tierDTOs(1, nil)); string(b) != "[]" {
		t.Fatalf("tanpa jenjang harus [] : %s", b)
	}
	s := strings.Join(tierSummaries([]PriceTier{pc(50, 1250), fx(10, 42000)}), "; ")
	if s != "min 10: Rp 42.000; min 50: 12,5%" {
		t.Fatalf("ringkasan: %s", s)
	}
	if tierValueSQL(pc(2, 1205)) != "12.05" || tierValueSQL(fx(2, 42000)) != "42000.00" {
		t.Fatal("format DECIMAL")
	}
	if formatRupiah(1234567) != "Rp 1.234.567" || formatRupiah(0) != "Rp 0" || formatRupiah(999) != "Rp 999" {
		t.Fatal("formatRupiah")
	}
}

func TestNormalizeUnit(t *testing.T) {
	ok := map[string]string{"": "pcs", "  PAK ": "pak", "Kg": "kg", "box  isi 12": "box isi 12", "1/2 lusin": "1/2 lusin", "btl.": "btl."}
	for in, want := range ok {
		if got, err := normalizeUnit(in); err != nil || got != want {
			t.Errorf("%q -> %q, %v (mau %q)", in, got, err, want)
		}
	}
	for _, in := range []string{"<b>", "pak;drop", "abcdefghijklmnopqrstu", "dus-besar", "ü"} {
		if _, err := normalizeUnit(in); err == nil {
			t.Errorf("%q harus ditolak", in)
		}
	}
}

func TestPriceChangedAndExpectedTotal(t *testing.T) {
	v := int64(42000)
	if priceChanged(42000, nil) || priceChanged(42000, &v) || !priceChanged(40000, &v) {
		t.Fatal("priceChanged")
	}
	if checkExpectedTotal(nil, 100) != nil {
		t.Fatal("expectedTotal kosong tidak diperiksa (klien lama)")
	}
	exp := int64(100)
	if checkExpectedTotal(&exp, 100) != nil {
		t.Fatal("sama harus lolos")
	}
	if !errors.Is(checkExpectedTotal(&exp, 101), errPriceChanged) {
		t.Fatal("beda harus price_changed")
	}
}

func TestBuildCartItem(t *testing.T) {
	tiers := []PriceTier{fx(10, 42000), fx(50, 40000)}
	seen := int64(45000)
	row := cartItemRow{ItemID: 1, ProductID: 7, Name: "Kerupuk", Price: 45000, Unit: "pak", Qty: 12, Seen: &seen, Available: true}
	it := buildCartItem(row, tiers)
	if it.UnitPrice != 42000 || it.LineTotal != 12*42000 || it.BaseUnitPrice != 45000 || it.TierMinQty == nil || *it.TierMinQty != 10 {
		t.Fatalf("harga: %+v", it)
	}
	if it.Savings != 12*3000 || it.NextTier == nil || it.NextTier.MoreQty != 38 || it.NextTier.UnitPrice != 40000 {
		t.Fatalf("hemat/next: %+v %+v", it, it.NextTier)
	}
	if !it.PriceChanged || it.PreviousUnitPrice == nil || *it.PreviousUnitPrice != 45000 {
		t.Fatalf("seen 45000 vs sekarang 42000 harus berubah: %+v", it)
	}
	seen = 42000
	if it := buildCartItem(row, tiers); it.PriceChanged || it.PreviousUnitPrice != nil {
		t.Fatalf("harga sama tidak berubah: %+v", it)
	}
	row.Seen = nil
	if it := buildCartItem(row, tiers); it.PriceChanged {
		t.Fatal("seen NULL dianggap tidak berubah")
	}
	row.Available = false
	row.Seen = &seen
	seen = 1
	if it := buildCartItem(row, tiers); it.PriceChanged || it.NextTier != nil {
		t.Fatal("produk tidak tersedia tidak diberi penanda/petunjuk")
	}
	b, _ := json.Marshal(buildCartItem(cartItemRow{Price: 5000, Qty: 1, Unit: "pcs", Available: true}, nil))
	for _, k := range []string{`"unit":"pcs"`, `"baseUnitPrice":5000`, `"unitPrice":5000`, `"tierMinQty":null`, `"nextTier":null`, `"savings":0`, `"priceChanged":false`, `"previousUnitPrice":null`, `"price":5000`, `"lineTotal":5000`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("JSON baris keranjang tidak memuat %s: %s", k, b)
		}
	}
	if strings.Contains(string(b), "itemID") || strings.Contains(string(b), "seen") {
		t.Fatalf("field internal bocor: %s", b)
	}
}
