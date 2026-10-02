package main

import "time"

const (
	maxFailedLogins = 5
	lockDuration    = 15 * time.Minute
)

// isLocked: akun terkunci bila locked_until masih di masa depan.
func isLocked(lockedUntil *time.Time, now time.Time) bool {
	return lockedUntil != nil && now.Before(*lockedUntil)
}

// nextFailedState menghitung state baru setelah satu login gagal.
// Pada kegagalan ke-5 berturut-turut akun dikunci 15 menit dan penghitung direset,
// sehingga setelah kunci habis pengguna kembali mendapat 5 kesempatan.
func nextFailedState(failed uint8, now time.Time) (uint8, *time.Time) {
	n := int(failed) + 1
	if n >= maxFailedLogins {
		until := now.Add(lockDuration)
		return 0, &until
	}
	return uint8(n), nil
}
