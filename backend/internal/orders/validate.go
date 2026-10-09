package orders

// Validasi masukan checkout dan pengaturan toko. Normalisasi telepon milik modul identity dan diberikan
// sebagai fungsi (normPhone) oleh pemanggil; kode wilayah memakai tipe data internal/regions.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"mihanstore/internal/platform"
	"mihanstore/internal/regions"
)

const (
	placeholderSetting = "BELUM DIISI"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ---------- Validasi checkout ----------

type CheckoutInput struct {
	RecipientName  string `json:"recipientName"`
	RecipientPhone string `json:"recipientPhone"`
	Address        string `json:"address"` // alamat lengkap: jalan, RT/RW, nomor
	// City dari klien DIABAIKAN (kolom city diisi nama kab/kota dari data wilayah).
	City       string `json:"city"`
	PostalCode string `json:"postalCode"`
	// Kode wilayah (wajib). Nama diambil server dari data wilayah di database.
	ProvinceCode   string `json:"provinceCode"`
	RegencyCode    string `json:"regencyCode"`
	DistrictCode   string `json:"districtCode"`
	VillageCode    string `json:"villageCode"`
	Note           string `json:"note"`
	IdempotencyKey string `json:"idempotencyKey"`
	// ExpectedTotal: total yang DITAMPILKAN ke pelanggan. Bila berbeda dari hitungan server ->
	// 409 price_changed tanpa membuat pesanan. nil (klien lama) tidak diperiksa.
	ExpectedTotal *int64 `json:"expectedTotal"`
}

type checkoutFields struct {
	Name, Phone, Address string
	PostalCode, Note     *string
	IdemKey              string
	Region               regions.CodesInput
}

var postalRe = regexp.MustCompile(`^[0-9]{5}$`)

// validateCheckout: semua field wajib kecuali catatan. Galat berupa *platform.FieldError (422 {error, field}).
// Klien lama (tanpa kode wilayah sama sekali) -> pesan "Perbarui halaman lalu coba lagi".
func validateCheckout(in CheckoutInput, normPhone func(string) (string, error)) (checkoutFields, error) {
	var f checkoutFields
	f.Region = regions.CodesInput{Province: in.ProvinceCode, Regency: in.RegencyCode, District: in.DistrictCode, Village: in.VillageCode}
	if strings.TrimSpace(in.ProvinceCode+in.RegencyCode+in.DistrictCode+in.VillageCode) == "" {
		return f, &platform.FieldError{Field: "region", Msg: regions.MsgRefresh}
	}
	f.Name = platform.CleanText(in.RecipientName, false)
	if f.Name == "" || utf8.RuneCountInString(f.Name) > 100 {
		return f, &platform.FieldError{Field: "recipientName", Msg: "Nama penerima wajib diisi (maksimal 100 karakter)"}
	}
	phone, err := normPhone(in.RecipientPhone)
	if err != nil {
		return f, &platform.FieldError{Field: "recipientPhone", Msg: err.Error()}
	}
	if phone == "" {
		return f, &platform.FieldError{Field: "recipientPhone", Msg: "Nomor telepon penerima wajib diisi"}
	}
	f.Phone = phone
	codes := []struct{ v, field, label string }{
		{in.ProvinceCode, "provinceCode", "provinsi"}, {in.RegencyCode, "regencyCode", "kabupaten/kota"},
		{in.DistrictCode, "districtCode", "kecamatan"}, {in.VillageCode, "villageCode", "kelurahan/desa"},
	}
	for _, c := range codes {
		if strings.TrimSpace(c.v) == "" {
			return f, &platform.FieldError{Field: c.field, Msg: "Pilih " + c.label}
		}
	}
	f.Address = platform.CleanText(in.Address, true)
	if n := utf8.RuneCountInString(f.Address); n < 5 || n > 500 {
		return f, &platform.FieldError{Field: "address", Msg: "Alamat lengkap (jalan, RT/RW, nomor) wajib diisi (5–500 karakter)"}
	}
	pc := strings.TrimSpace(in.PostalCode)
	if pc == "" {
		return f, &platform.FieldError{Field: "postalCode", Msg: "Kode pos wajib diisi"}
	}
	if !postalRe.MatchString(pc) {
		return f, &platform.FieldError{Field: "postalCode", Msg: "Kode pos harus 5 digit angka"}
	}
	f.PostalCode = &pc
	if note := platform.CleanText(in.Note, true); note != "" {
		if utf8.RuneCountInString(note) > MaxNote500 {
			return f, &platform.FieldError{Field: "note", Msg: "Catatan maksimal 500 karakter"}
		}
		f.Note = &note
	}
	key := strings.ToLower(strings.TrimSpace(in.IdempotencyKey))
	if !uuidRe.MatchString(key) {
		return f, &platform.FieldError{Field: "idempotencyKey", Msg: "idempotencyKey wajib berupa UUID"}
	}
	f.IdemKey = key
	return f, nil
}

// optionalText membersihkan teks opsional dan memeriksa panjang maksimal (nil = kosong).
func optionalText(s string, max int, keepNewlines bool, label string) (*string, error) {
	t := platform.CleanText(s, keepNewlines)
	if t == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(t) > max {
		return nil, fmt.Errorf("%s maksimal %d karakter", label, max)
	}
	return &t, nil
}

// ---------- Pengaturan toko ----------

var settingKeys = []string{"store_whatsapp", "bank_name", "bank_account_number", "bank_account_holder", "payment_note"}

// SettingsInput: semua opsional; field yang tidak dikirim tidak diubah.
type SettingsInput struct {
	StoreWhatsapp     *string `json:"store_whatsapp"`
	BankName          *string `json:"bank_name"`
	BankAccountNumber *string `json:"bank_account_number"`
	BankAccountHolder *string `json:"bank_account_holder"`
	PaymentNote       *string `json:"payment_note"`
}

var accountNoRe = regexp.MustCompile(`^[0-9][0-9 .-]{2,39}$`)

// validateSettings mengembalikan nilai bersih per kunci (hanya kunci yang dikirim).
// String kosong berarti "belum diisi".
func validateSettings(in SettingsInput, normPhone func(string) (string, error)) (map[string]string, error) {
	out := map[string]string{}
	if in.StoreWhatsapp != nil {
		p, err := normPhone(*in.StoreWhatsapp)
		if err != nil {
			return nil, errors.New("Nomor WhatsApp toko tidak valid. Gunakan format 08xx, 628xx, atau +628xx")
		}
		out["store_whatsapp"] = p
	}
	text := func(key string, v *string, max int, keepNL bool, label string) error {
		if v == nil {
			return nil
		}
		t := platform.CleanText(*v, keepNL)
		if t == placeholderSetting {
			t = ""
		}
		if utf8.RuneCountInString(t) > max {
			return fmt.Errorf("%s maksimal %d karakter", label, max)
		}
		out[key] = t
		return nil
	}
	if err := text("bank_name", in.BankName, 100, false, "Nama bank"); err != nil {
		return nil, err
	}
	if in.BankAccountNumber != nil {
		n := strings.TrimSpace(*in.BankAccountNumber)
		if n == placeholderSetting {
			n = ""
		}
		if n != "" && !accountNoRe.MatchString(n) {
			return nil, errors.New("Nomor rekening hanya boleh angka, spasi, titik, atau tanda hubung (3–40 karakter)")
		}
		out["bank_account_number"] = n
	}
	if err := text("bank_account_holder", in.BankAccountHolder, 100, false, "Nama pemilik rekening"); err != nil {
		return nil, err
	}
	if err := text("payment_note", in.PaymentNote, 500, true, "Catatan pembayaran"); err != nil {
		return nil, err
	}
	return out, nil
}

// settingValue: nilai yang dianggap "belum diisi" (kosong / placeholder) -> "".
func settingValue(v string) string {
	v = strings.TrimSpace(v)
	if v == placeholderSetting {
		return ""
	}
	return v
}
