package orders

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Matriks lengkap perpindahan status (from x to x pelaku), termasuk status baru pending_confirmation.
func TestStatusTransitionMatrix(t *testing.T) {
	all := []string{StatusPendingConfirmation, StatusPending, StatusPaid, StatusCompleted, StatusCancelled}
	allowed := map[string]bool{
		"pending_confirmation>cancelled>admin": true,
		"pending_confirmation>cancelled>owner": true,
		"pending_payment>paid>admin":           true,
		"pending_payment>cancelled>admin":      true,
		"pending_payment>cancelled>owner":      true,
		"paid>completed>admin":                 true,
		"paid>cancelled>admin":                 true, // alasan wajib
	}
	for _, from := range all {
		for _, to := range all {
			for _, actor := range []string{ActorAdmin, ActorOwner} {
				key := fmt.Sprintf("%s>%s>%s", from, to, actor)
				err := CheckTransition(from, to, actor, "alasan")
				if allowed[key] != (err == nil) {
					t.Errorf("%s: err=%v, mau diizinkan=%v", key, err, allowed[key])
				}
			}
		}
	}
	// pending_confirmation -> pending_payment TIDAK lewat endpoint status (hanya /confirm).
	if err := CheckTransition(StatusPendingConfirmation, StatusPending, ActorAdmin, ""); err != ErrTransition {
		t.Fatalf("konfirmasi lewat endpoint status harus ErrTransition: %v", err)
	}
	if got := strings.Join(AllowedNext(StatusPendingConfirmation, ActorAdmin), ","); got != "cancelled" {
		t.Errorf("allowedNext pending_confirmation admin: %q (Dibayar tidak boleh tampil)", got)
	}
	if got := strings.Join(AllowedNext(StatusPendingConfirmation, ActorOwner), ","); got != "cancelled" {
		t.Errorf("allowedNext pending_confirmation owner: %q", got)
	}
	for _, s := range all {
		if (CheckConfirm(s) == nil) != (s == StatusPendingConfirmation) {
			t.Errorf("checkConfirm(%s)", s)
		}
		want := s == StatusPendingConfirmation || s == StatusPending
		if CanOwnerCancel(s) != want {
			t.Errorf("canOwnerCancel(%s) != %v", s, want)
		}
	}
	if !ValidStatus(StatusPendingConfirmation) || StatusLabel(StatusPendingConfirmation) != "Menunggu konfirmasi" {
		t.Fatal("status baru belum terdaftar")
	}
}

func TestComputeTotal(t *testing.T) {
	cases := []struct {
		sub, disc, ship, want int64
		err                   error
	}{
		{100000, 0, 0, 100000, nil},
		{100000, 15000, 20000, 105000, nil},
		{100000, 100000, 0, 0, nil},               // diskon = subtotal boleh
		{100000, 100000, 12000, 12000, nil},       // hanya ongkir
		{100000, 100001, 0, 0, ErrDiscountTooBig}, // diskon > subtotal
		{100000, -1, 0, 0, ErrAmountNegative},
		{100000, 0, -5, 0, ErrAmountNegative},
		{100000, 0, 10_000_000, 10_100_000, nil},
		{100000, 0, 10_000_001, 0, ErrShippingRange},
		{1_999_999_999, 1, 10_000_000, 2_009_999_998, nil},
	}
	for _, c := range cases {
		got, err := ComputeTotal(c.sub, c.disc, c.ship)
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
		{StatusPending, StatusPaid, ActorAdmin, "", true},
		{StatusPending, StatusPaid, ActorOwner, "", false},
		{StatusPending, StatusCancelled, ActorAdmin, "", true},
		{StatusPending, StatusCancelled, ActorOwner, "", true},
		{StatusPending, StatusCompleted, ActorAdmin, "", false},
		{StatusPaid, StatusCompleted, ActorAdmin, "", true},
		{StatusPaid, StatusCompleted, ActorOwner, "", false},
		{StatusPaid, StatusCancelled, ActorAdmin, "", false}, // alasan wajib
		{StatusPaid, StatusCancelled, ActorAdmin, "   ", false},
		{StatusPaid, StatusCancelled, ActorAdmin, "Stok habis", true},
		{StatusPaid, StatusCancelled, ActorOwner, "x", false},
		{StatusPaid, StatusPending, ActorAdmin, "", false},
		{StatusPaid, StatusPaid, ActorAdmin, "", false},
		{StatusCompleted, StatusCancelled, ActorAdmin, "x", false},
		{StatusCompleted, StatusPaid, ActorAdmin, "", false},
		{StatusCancelled, StatusPending, ActorAdmin, "", false},
		{StatusCancelled, StatusPaid, ActorAdmin, "", false},
		{StatusPending, "shipped", ActorAdmin, "", false},
	}
	for _, c := range cases {
		err := CheckTransition(c.from, c.to, c.actor, c.reason)
		if (err == nil) != c.ok {
			t.Errorf("%s -> %s oleh %s (alasan %q): err=%v, mau ok=%v", c.from, c.to, c.actor, c.reason, err, c.ok)
		}
	}
	if err := CheckTransition(StatusPaid, StatusCancelled, ActorAdmin, ""); err != ErrReasonNeeded {
		t.Errorf("paid->cancelled tanpa alasan harus ErrReasonNeeded: %v", err)
	}
	if err := CheckTransition(StatusCompleted, StatusCancelled, ActorAdmin, "x"); err != ErrFinalStatus {
		t.Errorf("status final: %v", err)
	}
	if got := strings.Join(AllowedNext(StatusPending, ActorAdmin), ","); got != "paid,cancelled" {
		t.Errorf("allowedNext pending admin: %s", got)
	}
	if got := strings.Join(AllowedNext(StatusPaid, ActorAdmin), ","); got != "completed,cancelled" {
		t.Errorf("allowedNext paid admin: %s", got)
	}
	if got := AllowedNext(StatusCompleted, ActorAdmin); len(got) != 0 {
		t.Errorf("completed final: %v", got)
	}
	if got := strings.Join(AllowedNext(StatusPending, ActorOwner), ","); got != "cancelled" {
		t.Errorf("allowedNext pending owner: %s", got)
	}
}

func TestOrderNumber(t *testing.T) {
	// 2026-10-02 18:30 UTC = 3 Oktober 01:30 WIB -> tanggal WIB dipakai.
	ts := time.Date(2026, 10, 2, 18, 30, 0, 0, time.UTC)
	if got := OrderNumber(1, ts); got != "MS-261003-0001" {
		t.Errorf("orderNumber = %s", got)
	}
	if got := OrderNumber(42, time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)); got != "MS-261002-0042" {
		t.Errorf("orderNumber = %s", got)
	}
	if got := OrderNumber(123456, ts); got != "MS-261003-123456" {
		t.Errorf("orderNumber > 4 digit = %s", got)
	}
	for s, ok := range map[string]bool{"MS-261002-0001": true, "MS-261002-123456": true, "MS-261002-001": false,
		"ms-261002-0001": false, "MS-2610-0001": false, "MS-261002-0001' OR 1=1": false, "": false} {
		if ValidOrderNo(s) != ok {
			t.Errorf("validOrderNo(%q) != %v", s, ok)
		}
	}
	if len(OrderNumber(18446744073709551615, ts)) > 30 {
		t.Error("nomor terlalu panjang")
	}
}
