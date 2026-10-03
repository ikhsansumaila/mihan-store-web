package main

// Logika murni pesanan (tanpa database): status & perpindahannya, perhitungan total,
// nomor pesanan, validasi input checkout / harga / pengaturan toko.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	StatusPending   = "pending_payment"
	StatusPaid      = "paid"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"

	maxCartLines     = 50
	maxItemQty       = 999
	maxShippingFee   = 10_000_000
	maxOrderSubtotal = 2_000_000_000 // agar subtotal + ongkir tetap muat di INT UNSIGNED
	maxNote255       = 255
	maxNote500       = 500

	placeholderSetting = "BELUM DIISI"
)

var statusLabelID = map[string]string{
	StatusPending:   "Menunggu pembayaran",
	StatusPaid:      "Dibayar",
	StatusCompleted: "Selesai",
	StatusCancelled: "Dibatalkan",
}

func statusLabel(s string) string {
	if l, ok := statusLabelID[s]; ok {
		return l
	}
	return s
}

func validStatus(s string) bool { _, ok := statusLabelID[s]; return ok }

// Pelaku perpindahan status.
const (
	actorAdmin = "admin"
	actorOwner = "owner"
)

// statusTransitions: from -> to -> pelaku yang boleh. completed & cancelled final.
var statusTransitions = map[string]map[string][]string{
	StatusPending: {
		StatusPaid:      {actorAdmin},
		StatusCancelled: {actorAdmin, actorOwner},
	},
	StatusPaid: {
		StatusCompleted: {actorAdmin},
		StatusCancelled: {actorAdmin}, // alasan wajib
	},
}

var (
	errTransition    = errors.New("Perpindahan status tidak diizinkan")
	errReasonNeeded  = errors.New("Alasan pembatalan wajib diisi untuk pesanan yang sudah dibayar")
	errFinalStatus   = errors.New("Pesanan sudah selesai atau dibatalkan dan tidak bisa diubah lagi")
	errPricingLocked = errors.New("Diskon dan ongkir hanya bisa diubah saat pesanan menunggu pembayaran")
)

// checkTransition memeriksa apakah actor boleh memindahkan status from -> to.
// reason dipakai untuk aturan paid -> cancelled (alasan wajib).
func checkTransition(from, to, actor, reason string) error {
	if from == StatusCompleted || from == StatusCancelled {
		return errFinalStatus
	}
	allowed, ok := statusTransitions[from][to]
	if !ok {
		return errTransition
	}
	permitted := false
	for _, a := range allowed {
		if a == actor {
			permitted = true
		}
	}
	if !permitted {
		return errTransition
	}
	if from == StatusPaid && to == StatusCancelled && strings.TrimSpace(reason) == "" {
		return errReasonNeeded
	}
	return nil
}

// allowedNext: status tujuan yang boleh dipilih actor dari status sekarang (untuk UI).
func allowedNext(from, actor string) []string {
	out := []string{}
	for _, to := range []string{StatusPaid, StatusCompleted, StatusCancelled} {
		for _, a := range statusTransitions[from][to] {
			if a == actor {
				out = append(out, to)
			}
		}
	}
	return out
}

var (
	errDiscountTooBig = errors.New("Diskon tidak boleh melebihi subtotal")
	errShippingRange  = fmt.Errorf("Ongkir harus bilangan bulat 0 sampai %s", "10.000.000")
	errAmountNegative = errors.New("Nominal tidak boleh negatif")
)

// computeTotal: total = subtotal - discount + shipping, dengan diskon <= subtotal dan
// ongkir 0..10.000.000. Semua bilangan bulat rupiah, tidak pernah negatif.
func computeTotal(subtotal, discount, shipping int64) (int64, error) {
	if subtotal < 0 || discount < 0 || shipping < 0 {
		return 0, errAmountNegative
	}
	if discount > subtotal {
		return 0, errDiscountTooBig
	}
	if shipping > maxShippingFee {
		return 0, errShippingRange
	}
	return subtotal - discount + shipping, nil
}

// orderNumber: MS-YYMMDD-<id min. 4 digit>, tanggal menurut WIB.
func orderNumber(id uint64, created time.Time) string {
	return fmt.Sprintf("MS-%s-%04d", created.In(wib).Format("060102"), id)
}

var orderNoRe = regexp.MustCompile(`^MS-[0-9]{6}-[0-9]{4,15}$`)

func validOrderNo(s string) bool { return len(s) <= 24 && orderNoRe.MatchString(s) }

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ---------- Validasi checkout ----------

type CheckoutInput struct {
	RecipientName  string `json:"recipientName"`
	RecipientPhone string `json:"recipientPhone"`
	Address        string `json:"address"`
	City           string `json:"city"`
	PostalCode     string `json:"postalCode"`
	Note           string `json:"note"`
	IdempotencyKey string `json:"idempotencyKey"`
	// ExpectedTotal: total yang DITAMPILKAN ke pelanggan. Bila berbeda dari hitungan server ->
	// 409 price_changed tanpa membuat pesanan. nil (klien lama) tidak diperiksa.
	ExpectedTotal *int64 `json:"expectedTotal"`
}

type checkoutFields struct {
	Name, Phone, Address, City string
	PostalCode, Note           *string
	IdemKey                    string
}

var postalRe = regexp.MustCompile(`^[0-9]{5}$`)

func validateCheckout(in CheckoutInput) (checkoutFields, error) {
	var f checkoutFields
	f.Name = cleanText(in.RecipientName, false)
	if f.Name == "" || utf8.RuneCountInString(f.Name) > 100 {
		return f, errors.New("Nama penerima wajib diisi (maksimal 100 karakter)")
	}
	phone, err := NormalizePhone(in.RecipientPhone)
	if err != nil {
		return f, err
	}
	if phone == "" {
		return f, errors.New("Nomor telepon penerima wajib diisi")
	}
	f.Phone = phone
	f.Address = cleanText(in.Address, true)
	if n := utf8.RuneCountInString(f.Address); n < 5 || n > 500 {
		return f, errors.New("Alamat wajib diisi (5–500 karakter)")
	}
	f.City = cleanText(in.City, false)
	if n := utf8.RuneCountInString(f.City); n < 2 || n > 100 {
		return f, errors.New("Kota/kabupaten wajib diisi (2–100 karakter)")
	}
	if pc := strings.TrimSpace(in.PostalCode); pc != "" {
		if !postalRe.MatchString(pc) {
			return f, errors.New("Kode pos harus 5 digit angka")
		}
		f.PostalCode = &pc
	}
	if note := cleanText(in.Note, true); note != "" {
		if utf8.RuneCountInString(note) > maxNote500 {
			return f, errors.New("Catatan maksimal 500 karakter")
		}
		f.Note = &note
	}
	key := strings.ToLower(strings.TrimSpace(in.IdempotencyKey))
	if !uuidRe.MatchString(key) {
		return f, errors.New("idempotencyKey wajib berupa UUID")
	}
	f.IdemKey = key
	return f, nil
}

// optionalText membersihkan teks opsional dan memeriksa panjang maksimal (nil = kosong).
func optionalText(s string, max int, keepNewlines bool, label string) (*string, error) {
	t := cleanText(s, keepNewlines)
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
func validateSettings(in SettingsInput) (map[string]string, error) {
	out := map[string]string{}
	if in.StoreWhatsapp != nil {
		p, err := NormalizePhone(*in.StoreWhatsapp)
		if err != nil {
			return nil, errors.New("Nomor WhatsApp toko tidak valid. Gunakan format 08xx, 628xx, atau +628xx")
		}
		out["store_whatsapp"] = p
	}
	text := func(key string, v *string, max int, keepNL bool, label string) error {
		if v == nil {
			return nil
		}
		t := cleanText(*v, keepNL)
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
