package main

import (
	"sync"
	"time"
)

// RateLimiter: batas laju fixed-window per kunci (IP), disimpan di memori.
// Data hilang saat restart; ini disengaja (pelindung tambahan selain Turnstile
// dan penguncian akun di database).
type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]*rlEntry
	maxKeys int
	now     func() time.Time
}

type rlEntry struct {
	start time.Time
	count int
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, entries: map[string]*rlEntry{}, maxKeys: 100000, now: time.Now}
}

// Allow mencatat satu percobaan dan mengembalikan false bila batas terlampaui,
// beserta sisa waktu sampai jendela berikutnya.
func (rl *RateLimiter) Allow(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()

	if len(rl.entries) >= rl.maxKeys {
		rl.sweepLocked(now)
	}

	e, ok := rl.entries[key]
	if !ok || now.Sub(e.start) >= rl.window {
		if !ok && len(rl.entries) >= rl.maxKeys {
			// Peta penuh oleh kunci aktif: tolak demi keamanan memori.
			return false, rl.window
		}
		rl.entries[key] = &rlEntry{start: now, count: 1}
		return true, 0
	}
	if e.count >= rl.limit {
		return false, rl.window - now.Sub(e.start)
	}
	e.count++
	return true, 0
}

func (rl *RateLimiter) sweepLocked(now time.Time) {
	for k, e := range rl.entries {
		if now.Sub(e.start) >= rl.window {
			delete(rl.entries, k)
		}
	}
}
