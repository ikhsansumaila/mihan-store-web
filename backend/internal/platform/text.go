package platform

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// TruncateUTF8 memotong s hingga maksimal max BYTE tanpa memenggal karakter UTF-8
// (byte tidak valid dibuang lebih dulu).
func TruncateUTF8(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// StrPtr: nil untuk string kosong, selain itu penunjuk ke salinannya.
func StrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// EscapeLike meloloskan karakter wildcard LIKE (\, %, _).
func EscapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// IsBidiControl: penanda arah/override/isolate bidi; dibuang karena bisa dipakai untuk menyamarkan teks.
func IsBidiControl(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) || r == 0x200E || r == 0x200F
}

// CleanText membuang karakter kontrol & bidi. keepNewlines mempertahankan \n (deskripsi).
func CleanText(s string, keepNewlines bool) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	for _, r := range s {
		if r == '\n' && keepNewlines {
			b.WriteRune(r)
			continue
		}
		if unicode.IsControl(r) || IsBidiControl(r) {
			if r == '\t' || r == '\n' || r == '\r' {
				b.WriteRune(' ')
			}
			continue
		}
		b.WriteRune(r)
	}
	if !keepNewlines {
		return strings.Join(strings.Fields(b.String()), " ")
	}
	return strings.TrimSpace(b.String())
}
