package main

import (
	"fmt"
	"strings"
	"testing"
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
			for _, actor := range []string{actorAdmin, actorOwner} {
				key := fmt.Sprintf("%s>%s>%s", from, to, actor)
				err := checkTransition(from, to, actor, "alasan")
				if allowed[key] != (err == nil) {
					t.Errorf("%s: err=%v, mau diizinkan=%v", key, err, allowed[key])
				}
			}
		}
	}
	// pending_confirmation -> pending_payment TIDAK lewat endpoint status (hanya /confirm).
	if err := checkTransition(StatusPendingConfirmation, StatusPending, actorAdmin, ""); err != errTransition {
		t.Fatalf("konfirmasi lewat endpoint status harus errTransition: %v", err)
	}
	if got := strings.Join(allowedNext(StatusPendingConfirmation, actorAdmin), ","); got != "cancelled" {
		t.Errorf("allowedNext pending_confirmation admin: %q (Dibayar tidak boleh tampil)", got)
	}
	if got := strings.Join(allowedNext(StatusPendingConfirmation, actorOwner), ","); got != "cancelled" {
		t.Errorf("allowedNext pending_confirmation owner: %q", got)
	}
	for _, s := range all {
		if (checkConfirm(s) == nil) != (s == StatusPendingConfirmation) {
			t.Errorf("checkConfirm(%s)", s)
		}
		want := s == StatusPendingConfirmation || s == StatusPending
		if canOwnerCancel(s) != want {
			t.Errorf("canOwnerCancel(%s) != %v", s, want)
		}
	}
	if !validStatus(StatusPendingConfirmation) || statusLabel(StatusPendingConfirmation) != "Menunggu konfirmasi" {
		t.Fatal("status baru belum terdaftar")
	}
}
