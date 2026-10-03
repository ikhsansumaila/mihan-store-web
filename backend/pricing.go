package main

// Harga berjenjang (grosir) dan satuan jual — logika MURNI (tanpa database).
//
// SATU-SATUNYA tempat perhitungan harga efektif: effectiveUnitPrice. Keranjang, checkout,
// pesanan, API publik, dan admin semuanya memanggil fungsi ini (jangan menduplikasi logika).
//
// Aturan:
//   - Jenjang dihitung PER PRODUK dari jumlah produk itu saja; harga jenjang berlaku untuk SEMUA
//     unit di baris itu (bukan marginal).
//   - Jenjang yang dipakai: min_qty terbesar yang <= qty (hanya bila qty >= 2).
//   - fixed  : harga per unit = value (rupiah).
//   - percent: harga per unit = round half up(base * (100 - percent) / 100) ke rupiah.
//   - Harga efektif = min(harga jenjang, harga dasar), minimal 1 rupiah. Tanpa jenjang cocok: harga dasar.
//
// Nilai jenjang disimpan sebagai DECIMAL(12,2); di Go dipakai bilangan bulat "x100"
// (seperseratus) agar tidak ada galat pembulatan float: fixed 42000 -> 4_200_000,
// percent 12,5% -> 1250.

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	TierFixed   = "fixed"
	TierPercent = "percent"

	maxTiersPerProduct = 20
	minTierQty         = 2
	maxTierQty         = 1_000_000
	minPercentX100     = 1    // 0,01%
	maxPercentX100     = 9999 // 99,99%

	defaultUnit = "pcs"
	maxUnitLen  = 20
)

// PriceTier adalah satu jenjang harga yang sudah tervalidasi.
type PriceTier struct {
	MinQty    int64
	Type      string
	ValueX100 int64 // fixed: rupiah*100; percent: persen*100
}

// tierRawPrice: harga per unit menurut jenjang SEBELUM dibatasi harga dasar / minimal 1.
func tierRawPrice(base int64, t PriceTier) int64 {
	if t.Type == TierPercent {
		// round half up dari base*(10000-p)/10000 (semua bilangan bulat, tidak negatif)
		return (base*(10000-t.ValueX100) + 5000) / 10000
	}
	return t.ValueX100 / 100
}

// tierPrice: harga efektif bila jenjang t dipakai = min(harga jenjang, harga dasar), minimal 1.
func tierPrice(base int64, t PriceTier) int64 {
	p := tierRawPrice(base, t)
	if p > base {
		p = base
	}
	if p < 1 {
		p = 1
	}
	return p
}

// matchTier: jenjang dengan min_qty terbesar yang <= qty (qty >= 2). tiers boleh tidak urut.
func matchTier(tiers []PriceTier, qty int64) (PriceTier, bool) {
	var best PriceTier
	found := false
	if qty < minTierQty {
		return best, false
	}
	for _, t := range tiers {
		if t.MinQty <= qty && (!found || t.MinQty > best.MinQty) {
			best, found = t, true
		}
	}
	return best, found
}

// effectiveUnitPrice mengembalikan harga satuan efektif untuk qty dan min_qty jenjang yang
// dipakai (0 bila memakai harga dasar).
func effectiveUnitPrice(base int64, tiers []PriceTier, qty int64) (price int64, tierMinQty int64) {
	t, ok := matchTier(tiers, qty)
	if !ok {
		return base, 0
	}
	p := tierPrice(base, t)
	if p >= base {
		return base, 0 // jenjang tidak lebih murah (tidak lolos validasi; jaga-jaga)
	}
	return p, t.MinQty
}

// NextTierDTO: jenjang berikutnya yang lebih murah (petunjuk "tambah N lagi").
type NextTierDTO struct {
	MinQty    int64 `json:"minQty"`
	UnitPrice int64 `json:"unitPrice"`
	MoreQty   int64 `json:"moreQty"`
}

// nextTier: jenjang terkecil dengan min_qty > qty yang memberi harga lebih murah dari harga
// sekarang dan masih bisa dicapai di keranjang (min_qty <= maxQty). nil bila tidak ada.
func nextTier(base int64, tiers []PriceTier, qty, maxQty int64) *NextTierDTO {
	cur, _ := effectiveUnitPrice(base, tiers, qty)
	sorted := sortedTiers(tiers)
	for _, t := range sorted {
		if t.MinQty <= qty || t.MinQty > maxQty {
			continue
		}
		p, _ := effectiveUnitPrice(base, tiers, t.MinQty)
		if p < cur {
			return &NextTierDTO{MinQty: t.MinQty, UnitPrice: p, MoreQty: t.MinQty - qty}
		}
	}
	return nil
}

func sortedTiers(tiers []PriceTier) []PriceTier {
	out := append([]PriceTier(nil), tiers...)
	sort.Slice(out, func(i, j int) bool { return out[i].MinQty < out[j].MinQty })
	return out
}

// TierDTO adalah bentuk jenjang di API (publik & admin).
type TierDTO struct {
	MinQty    int64   `json:"minQty"`
	Type      string  `json:"type"`
	Value     float64 `json:"value"`
	UnitPrice int64   `json:"unitPrice"`
}

func tierValueNumber(t PriceTier) float64 {
	if t.Type == TierFixed {
		return float64(t.ValueX100 / 100)
	}
	return float64(t.ValueX100) / 100
}

// tierDTOs: jenjang urut min_qty naik, unitPrice = harga efektif tiap jenjang. Selalu non-nil.
func tierDTOs(base int64, tiers []PriceTier) []TierDTO {
	out := make([]TierDTO, 0, len(tiers))
	for _, t := range sortedTiers(tiers) {
		p, _ := effectiveUnitPrice(base, tiers, t.MinQty)
		out = append(out, TierDTO{MinQty: t.MinQty, Type: t.Type, Value: tierValueNumber(t), UnitPrice: p})
	}
	return out
}

// tierValueSQL: nilai DECIMAL(12,2) untuk disimpan, mis. "42000.00" atau "12.50".
func tierValueSQL(t PriceTier) string {
	return fmt.Sprintf("%d.%02d", t.ValueX100/100, t.ValueX100%100)
}

// ---------- Format teks (log, pesan) ----------

func formatRupiah(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-Rp " + b.String()
	}
	return "Rp " + b.String()
}

func formatPercentX100(v int64) string {
	s := strconv.FormatInt(v/100, 10)
	if frac := v % 100; frac != 0 {
		f := fmt.Sprintf("%02d", frac)
		s += "," + strings.TrimRight(f, "0")
	}
	return s + "%"
}

// tierSummary: ringkasan satu jenjang untuk log aktivitas, mis. "min 10: Rp 42.000" / "min 50: 5%".
func tierSummary(t PriceTier) string {
	if t.Type == TierPercent {
		return fmt.Sprintf("min %d: %s", t.MinQty, formatPercentX100(t.ValueX100))
	}
	return fmt.Sprintf("min %d: %s", t.MinQty, formatRupiah(t.ValueX100/100))
}

func tierSummaries(tiers []PriceTier) []string {
	out := make([]string, 0, len(tiers))
	for _, t := range sortedTiers(tiers) {
		out = append(out, tierSummary(t))
	}
	return out
}

// ---------- Satuan jual ----------

var unitRe = regexp.MustCompile(`^[a-z0-9 ./]{1,20}$`)

var errUnit = errors.New("Satuan hanya boleh huruf, angka, spasi, titik, atau garis miring (maksimal 20 karakter), mis. pcs, pak, dus")

// normalizeUnit: trim, huruf kecil, spasi ganda dirapikan. Kosong -> "pcs".
func normalizeUnit(s string) (string, error) {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	if s == "" {
		return defaultUnit, nil
	}
	if !unitRe.MatchString(s) {
		return "", errUnit
	}
	return s, nil
}

// ---------- Validasi jenjang (admin) ----------

// TierInput adalah satu jenjang dari body admin. Value memakai json.Number agar desimal
// tidak melewati float.
type TierInput struct {
	MinQty json.Number `json:"minQty"`
	Type   string      `json:"type"`
	Value  json.Number `json:"value"`
}

// TierError: kesalahan satu jenjang. Index = posisi di daftar yang dikirim (0-based);
// MinQty = jumlah minimal (0 bila tidak terbaca).
type TierError struct {
	Index   int    `json:"index"`
	MinQty  int64  `json:"minQty"`
	Message string `json:"message"`
}

// TierValidationError dikembalikan validateTiers (HTTP 422).
type TierValidationError struct {
	Lead   string // konteks opsional, mis. perubahan harga dasar
	Errors []TierError
}

func (e *TierValidationError) Error() string {
	msg := "Jenjang harga grosir tidak valid"
	switch {
	case len(e.Errors) == 1:
		msg = e.Errors[0].Message
	case len(e.Errors) > 1:
		msg = fmt.Sprintf("%s (dan %d kesalahan jenjang lain)", e.Errors[0].Message, len(e.Errors)-1)
	}
	if e.Lead != "" {
		return e.Lead + " " + msg
	}
	return msg
}

var decimalRe = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,2})?$`)

// parseX100 membaca angka desimal tak negatif dengan maksimal 2 angka di belakang koma.
func parseX100(n json.Number) (int64, bool) {
	s := strings.TrimSpace(string(n))
	if !decimalRe.MatchString(s) || len(s) > 16 {
		return 0, false
	}
	parts := strings.SplitN(s, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	frac := int64(0)
	if len(parts) == 2 {
		f := parts[1]
		if len(f) == 1 {
			f += "0"
		}
		frac, _ = strconv.ParseInt(f, 10, 64)
	}
	return whole*100 + frac, true
}

func tierLabel(i int, minQty int64) string {
	if minQty > 0 {
		return fmt.Sprintf("Jenjang %d (min. %d)", i+1, minQty)
	}
	return fmt.Sprintf("Jenjang %d", i+1)
}

// validateTiers memeriksa daftar jenjang terhadap harga dasar. Mengembalikan jenjang urut
// min_qty naik, atau *TierValidationError berisi pesan per jenjang.
func validateTiers(base int64, in []TierInput) ([]PriceTier, error) {
	if len(in) > maxTiersPerProduct {
		return nil, &TierValidationError{Errors: []TierError{{Index: maxTiersPerProduct, Message: fmt.Sprintf("Maksimal %d jenjang harga grosir per produk", maxTiersPerProduct)}}}
	}
	var errs []TierError
	type idxTier struct {
		idx int
		t   PriceTier
	}
	parsed := make([]idxTier, 0, len(in))
	seen := map[int64]int{}
	for i, ti := range in {
		label := tierLabel(i, 0)
		mq, err := strconv.ParseInt(strings.TrimSpace(string(ti.MinQty)), 10, 64)
		if err != nil || mq < minTierQty || mq > maxTierQty {
			errs = append(errs, TierError{Index: i, Message: label + ": jumlah minimal harus bilangan bulat 2 sampai 1.000.000"})
			continue
		}
		label = tierLabel(i, mq)
		if j, dup := seen[mq]; dup {
			errs = append(errs, TierError{Index: i, MinQty: mq, Message: fmt.Sprintf("%s: jumlah minimal %d sudah dipakai jenjang %d", label, mq, j+1)})
			continue
		}
		seen[mq] = i
		t := PriceTier{MinQty: mq, Type: strings.ToLower(strings.TrimSpace(ti.Type))}
		v, ok := parseX100(ti.Value)
		switch t.Type {
		case TierFixed:
			if !ok || v%100 != 0 || v < 100 || v > maxProductPrice*100 {
				errs = append(errs, TierError{Index: i, MinQty: mq, Message: label + ": harga (Rp) harus bilangan bulat minimal 1"})
				continue
			}
		case TierPercent:
			if !ok || v < minPercentX100 || v > maxPercentX100 {
				errs = append(errs, TierError{Index: i, MinQty: mq, Message: label + ": diskon persen harus 0,01 sampai 99,99 (maksimal 2 angka desimal)"})
				continue
			}
		default:
			errs = append(errs, TierError{Index: i, MinQty: mq, Message: label + ": jenis potongan harus 'fixed' (Rp) atau 'percent' (%)"})
			continue
		}
		t.ValueX100 = v
		parsed = append(parsed, idxTier{i, t})
	}
	if len(errs) > 0 {
		return nil, &TierValidationError{Errors: errs}
	}
	sort.Slice(parsed, func(a, b int) bool { return parsed[a].t.MinQty < parsed[b].t.MinQty })
	out := make([]PriceTier, 0, len(parsed))
	prevPrice, prevMin := int64(-1), int64(0)
	for _, p := range parsed {
		price := tierPrice(base, p.t)
		label := tierLabel(p.idx, p.t.MinQty)
		switch {
		case price >= base:
			errs = append(errs, TierError{Index: p.idx, MinQty: p.t.MinQty, Message: fmt.Sprintf(
				"%s: harga efektif %s harus lebih murah dari harga dasar %s", label, formatRupiah(price), formatRupiah(base))})
		case prevPrice >= 0 && price >= prevPrice:
			errs = append(errs, TierError{Index: p.idx, MinQty: p.t.MinQty, Message: fmt.Sprintf(
				"%s: harga efektif %s harus lebih murah dari jenjang min. %d (%s)", label, formatRupiah(price), prevMin, formatRupiah(prevPrice))})
		}
		prevPrice, prevMin = price, p.t.MinQty
		out = append(out, p.t)
	}
	if len(errs) > 0 {
		sort.SliceStable(errs, func(a, b int) bool { return errs[a].Index < errs[b].Index })
		return nil, &TierValidationError{Errors: errs}
	}
	return out, nil
}

// tierInputsFrom mengubah jenjang tersimpan menjadi input (untuk validasi ulang saat harga
// dasar berubah tanpa daftar jenjang baru).
func tierInputsFrom(tiers []PriceTier) []TierInput {
	out := make([]TierInput, 0, len(tiers))
	for _, t := range sortedTiers(tiers) {
		out = append(out, TierInput{MinQty: json.Number(strconv.FormatInt(t.MinQty, 10)), Type: t.Type, Value: json.Number(tierValueSQL(t))})
	}
	return out
}

// ---------- Penanda perubahan harga di keranjang ----------

// priceChanged: harga efektif sekarang berbeda dari harga yang terakhir dilihat pelanggan.
// seen NULL dianggap tidak berubah (diisi saat keranjang dibuka).
func priceChanged(current int64, seen *int64) bool {
	return seen != nil && *seen != current
}

// errPriceChanged: total yang dilihat pelanggan berbeda dari total hitungan server.
var errPriceChanged = errors.New("price_changed")

// checkExpectedTotal: expected nil (klien lama) tidak diperiksa.
func checkExpectedTotal(expected *int64, total int64) error {
	if expected != nil && *expected != total {
		return errPriceChanged
	}
	return nil
}

const msgPriceChanged = "Harga berubah. Periksa kembali keranjang Anda lalu tekan konfirmasi untuk membuat pesanan dengan harga terbaru."
