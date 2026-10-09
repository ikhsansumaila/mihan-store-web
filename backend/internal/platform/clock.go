package platform

import (
	"strings"
	"time"
)

// NowUTC: jam standar aplikasi, UTC dengan ketelitian milidetik (sama dengan NowFunc GORM).
func NowUTC() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

// WIB: zona waktu Indonesia Barat (UTC+7). Tanggal filter admin ditafsirkan dalam WIB; data disimpan UTC.
var WIB = time.FixedZone("WIB", 7*3600)

// ParseWIBDate membaca "YYYY-MM-DD" (WIB) menjadi awal hari itu dalam UTC. Kosong -> nil, true.
func ParseWIBDate(s string) (*time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	t, err := time.ParseInLocation("2006-01-02", s, WIB)
	if err != nil {
		return nil, false
	}
	u := t.UTC()
	return &u, true
}
