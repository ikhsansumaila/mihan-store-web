package orders

// Domain murni pesanan (TANPA gorm / net/http): status & perpindahannya, perhitungan total,
// nomor pesanan. Validasi masukan ada di validate.go.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// Alur: pending_confirmation (checkout) -> [admin konfirmasi ongkir] -> pending_payment -> paid -> completed;
	// cancelled dari pending_confirmation / pending_payment (admin atau pemilik) dan dari paid (admin, alasan wajib).
	StatusPendingConfirmation = "pending_confirmation"
	StatusPending             = "pending_payment"
	StatusPaid                = "paid"
	StatusCompleted           = "completed"
	StatusCancelled           = "cancelled"

	MaxShippingFee   = 10_000_000
	MaxOrderSubtotal = 2_000_000_000 // agar subtotal + ongkir tetap muat di INT UNSIGNED
	MaxNote255       = 255
	MaxNote500       = 500
)

// wib: zona waktu tanggal pada nomor pesanan.
var wib = time.FixedZone("WIB", 7*3600)

var statusLabelID = map[string]string{
	StatusPendingConfirmation: "Menunggu konfirmasi",
	StatusPending:             "Menunggu pembayaran",
	StatusPaid:                "Dibayar",
	StatusCompleted:           "Selesai",
	StatusCancelled:           "Dibatalkan",
}

func StatusLabel(s string) string {
	if l, ok := statusLabelID[s]; ok {
		return l
	}
	return s
}

func ValidStatus(s string) bool { _, ok := statusLabelID[s]; return ok }

// Pelaku perpindahan status.
const (
	ActorAdmin = "admin"
	ActorOwner = "owner"
)

// statusTransitions: from -> to -> pelaku yang boleh. completed & cancelled final.
// pending_confirmation -> pending_payment SENGAJA tidak ada di sini: hanya lewat endpoint konfirmasi
// (POST /api/admin/orders/{id}/confirm, CheckConfirm) yang sekaligus menyimpan diskon & ongkir.
var statusTransitions = map[string]map[string][]string{
	StatusPendingConfirmation: {
		StatusCancelled: {ActorAdmin, ActorOwner},
	},
	StatusPending: {
		StatusPaid:      {ActorAdmin},
		StatusCancelled: {ActorAdmin, ActorOwner},
	},
	StatusPaid: {
		StatusCompleted: {ActorAdmin},
		StatusCancelled: {ActorAdmin}, // alasan wajib
	},
}

var (
	ErrTransition         = errors.New("Perpindahan status tidak diizinkan")
	ErrReasonNeeded       = errors.New("Alasan pembatalan wajib diisi untuk pesanan yang sudah dibayar")
	ErrFinalStatus        = errors.New("Pesanan sudah selesai atau dibatalkan dan tidak bisa diubah lagi")
	ErrPricingLocked      = errors.New("Diskon dan ongkir hanya bisa diubah saat pesanan menunggu pembayaran")
	ErrNotAwaitingConfirm = errors.New("Pesanan ini tidak sedang menunggu konfirmasi. Muat ulang halaman.")
	ErrConfirmFirst       = errors.New("Pesanan belum dikonfirmasi. Konfirmasi pesanan (isi ongkir) terlebih dahulu.")
)

// CheckConfirm: konfirmasi admin hanya dari pending_confirmation.
func CheckConfirm(from string) error {
	if from != StatusPendingConfirmation {
		return ErrNotAwaitingConfirm
	}
	return nil
}

// CanOwnerCancel: pelanggan boleh membatalkan sendiri pesanan yang belum dibayar.
func CanOwnerCancel(status string) bool {
	return CheckTransition(status, StatusCancelled, ActorOwner, "") == nil
}

// CheckTransition memeriksa apakah actor boleh memindahkan status from -> to.
// reason dipakai untuk aturan paid -> cancelled (alasan wajib).
func CheckTransition(from, to, actor, reason string) error {
	if from == StatusCompleted || from == StatusCancelled {
		return ErrFinalStatus
	}
	allowed, ok := statusTransitions[from][to]
	if !ok {
		return ErrTransition
	}
	permitted := false
	for _, a := range allowed {
		if a == actor {
			permitted = true
		}
	}
	if !permitted {
		return ErrTransition
	}
	if from == StatusPaid && to == StatusCancelled && strings.TrimSpace(reason) == "" {
		return ErrReasonNeeded
	}
	return nil
}

// AllowedNext: status tujuan yang boleh dipilih actor dari status sekarang (untuk UI).
func AllowedNext(from, actor string) []string {
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
	ErrDiscountTooBig = errors.New("Diskon tidak boleh melebihi subtotal")
	ErrShippingRange  = fmt.Errorf("Ongkir harus bilangan bulat 0 sampai %s", "10.000.000")
	ErrAmountNegative = errors.New("Nominal tidak boleh negatif")
)

// ComputeTotal: total = subtotal - discount + shipping, dengan diskon <= subtotal dan
// ongkir 0..10.000.000. Semua bilangan bulat rupiah, tidak pernah negatif.
func ComputeTotal(subtotal, discount, shipping int64) (int64, error) {
	if subtotal < 0 || discount < 0 || shipping < 0 {
		return 0, ErrAmountNegative
	}
	if discount > subtotal {
		return 0, ErrDiscountTooBig
	}
	if shipping > MaxShippingFee {
		return 0, ErrShippingRange
	}
	return subtotal - discount + shipping, nil
}

// OrderNumber: MS-YYMMDD-<id min. 4 digit>, tanggal menurut WIB.
func OrderNumber(id uint64, created time.Time) string {
	return fmt.Sprintf("MS-%s-%04d", created.In(wib).Format("060102"), id)
}

var orderNoRe = regexp.MustCompile(`^MS-[0-9]{6}-[0-9]{4,15}$`)

func ValidOrderNo(s string) bool { return len(s) <= 24 && orderNoRe.MatchString(s) }
