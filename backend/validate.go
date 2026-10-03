package main

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	errEmail    = errors.New("Format email tidak valid")
	errUsername = errors.New("Username harus 3–30 karakter, hanya huruf kecil, angka, atau garis bawah (_)")
	errReserved = errors.New("Username tersebut tidak boleh dipakai, silakan pilih yang lain")
	errName     = errors.New("Nama wajib diisi (maksimal 100 karakter)")
)

var usernameRe = regexp.MustCompile(`^[a-z0-9_]{3,30}$`)

// Nama yang tidak boleh dipakai persis.
var reservedUsernames = map[string]bool{
	"admin": true, "administrator": true, "root": true, "support": true, "system": true,
	"sysadmin": true, "superuser": true, "superadmin": true, "moderator": true, "mod": true,
	"staff": true, "owner": true, "official": true, "help": true, "helpdesk": true,
	"info": true, "cs": true, "customer_service": true, "security": true, "api": true,
	"www": true, "mail": true, "webmaster": true, "postmaster": true, "hostmaster": true,
	"abuse": true, "noreply": true, "no_reply": true, "null": true, "undefined": true,
	"anonymous": true, "guest": true, "me": true, "login": true, "logout": true,
	"register": true, "auth": true, "toko": true, "store": true, "shop": true,
}

// Potongan kata yang tidak boleh muncul di username mana pun (mencegah peniruan).
var reservedSubstrings = []string{"admin", "mihan", "moderator", "official"}

// NormalizeEmail: trim, huruf kecil, validasi net/mail, maksimal 254 karakter.
func NormalizeEmail(s string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(s))
	if e == "" || len(e) > 254 {
		return "", errEmail
	}
	a, err := mail.ParseAddress(e)
	// Tolak bentuk "Nama <a@b>" dan alamat dengan komentar: hasil parse harus identik.
	if err != nil || a.Address != e || a.Name != "" {
		return "", errEmail
	}
	at := strings.LastIndexByte(e, '@')
	if at < 1 {
		return "", errEmail
	}
	domain := e[at+1:]
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") ||
		strings.ContainsAny(e, " \t\r\n\"()<>[]\\,;:") {
		return "", errEmail
	}
	return e, nil
}

// NormalizeUsername: trim, huruf kecil, pola ^[a-z0-9_]{3,30}$, bukan nama terlarang.
func NormalizeUsername(s string) (string, error) {
	u := strings.ToLower(strings.TrimSpace(s))
	if !usernameRe.MatchString(u) {
		return "", errUsername
	}
	if reservedUsernames[u] || reservedUsernames[strings.Trim(u, "_")] {
		return "", errReserved
	}
	for _, sub := range reservedSubstrings {
		if strings.Contains(u, sub) {
			return "", errReserved
		}
	}
	return u, nil
}

// Pesan galat telepon (sama dengan frontend/src/phone.js).
var (
	errPhoneChars  = errors.New("Nomor telepon ada karakter yang tidak valid. Gunakan angka saja (boleh diawali +62)")
	errPhonePrefix = errors.New("Nomor telepon harus diawali 08, 62, atau +62 (nomor HP, mis. 0812...)")
	errPhoneShort  = errors.New("Nomor telepon terlalu pendek, minimal 8 digit")
	errPhoneLong   = errors.New("Nomor telepon terlalu panjang, maksimal 15 digit")
)

// foldPhoneRune menyeragamkan karakter dari keyboard/kontak HP ke ASCII sebelum validasi:
// tanda hubung Unicode -> '-', spasi Unicode (NBSP, U+202F, dll.) -> ' ', angka lebar dan
// angka Arab-Indic -> 0-9, plus/kurung/titik lebar -> ASCII. Karakter format tak terlihat
// (zero-width, BOM, penanda arah/bidi) dibuang (-1). Harus sama dengan frontend/src/phone.js.
func foldPhoneRune(r rune) rune {
	switch {
	case r >= 0x2010 && r <= 0x2015, r == 0x2212, r == 0xFE58, r == 0xFE63, r == 0xFF0D:
		return '-'
	case unicode.IsSpace(r):
		return ' '
	case r >= 0xFF10 && r <= 0xFF19:
		return '0' + (r - 0xFF10)
	case r >= 0x0660 && r <= 0x0669:
		return '0' + (r - 0x0660)
	case r >= 0x06F0 && r <= 0x06F9:
		return '0' + (r - 0x06F0)
	case r == 0xFF0B:
		return '+'
	case r == 0xFF08:
		return '('
	case r == 0xFF09:
		return ')'
	case r == 0xFF0E:
		return '.'
	case (r >= 0x200B && r <= 0x200F) || (r >= 0x2060 && r <= 0x2064) || r == 0xFEFF || isBidiControl(r):
		return -1
	}
	return r
}

// NormalizePhone: opsional. Menerima 08xx, 628xx, +628xx (spasi, titik, tanda hubung,
// dan kurung diabaikan; varian Unicode diseragamkan dulu, lihat foldPhoneRune; "+62 0812"
// dirapikan menjadi "+62812") lalu menormalkan ke +628xx. Jumlah digit (tanpa '+') 8-15.
// String kosong menghasilkan "" tanpa error (artinya tidak diisi).
func NormalizePhone(s string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "") {
		switch r = foldPhoneRune(r); r {
		case -1, ' ', '-', '.', '(', ')':
		default:
			b.WriteRune(r)
		}
	}
	p := b.String()
	if p == "" {
		return "", nil
	}
	for i, c := range p {
		if (c < '0' || c > '9') && !(c == '+' && i == 0) {
			return "", errPhoneChars
		}
	}
	switch {
	case strings.HasPrefix(p, "+62"):
		p = p[1:]
	case strings.HasPrefix(p, "62"):
	case strings.HasPrefix(p, "0"):
		p = "62" + p[1:]
	default:
		return "", errPhonePrefix
	}
	if strings.HasPrefix(p, "620") { // "+62 0812..." -> "62812..."
		p = "62" + p[3:]
	}
	if !strings.HasPrefix(p, "628") {
		return "", errPhonePrefix
	}
	if len(p) < 8 {
		return "", errPhoneShort
	}
	if len(p) > 15 {
		return "", errPhoneLong
	}
	return "+" + p, nil
}

// bidi override/isolate dibuang karena bisa dipakai untuk menyamarkan teks.
func isBidiControl(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) || r == 0x200E || r == 0x200F
}

// NormalizeName: buang karakter kontrol, rapikan spasi, 1–100 karakter.
func NormalizeName(s string) (string, error) {
	s = strings.ToValidUTF8(s, "")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || isBidiControl(r) {
			if r == '\t' || r == '\n' || r == '\r' {
				b.WriteRune(' ')
			}
			continue
		}
		b.WriteRune(r)
	}
	n := strings.Join(strings.Fields(b.String()), " ")
	if n == "" || utf8.RuneCountInString(n) > 100 {
		return "", errName
	}
	return n, nil
}

// normalizeIdentifier untuk login: email bila mengandung '@', selain itu username.
// Tidak memvalidasi ketat (hanya trim + huruf kecil) agar semua kegagalan
// menghasilkan pesan yang sama.
func normalizeIdentifier(s string) (value string, isEmail bool, ok bool) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return "", false, false
	}
	if strings.Contains(v, "@") {
		return v, true, len(v) <= 254
	}
	return v, false, len(v) <= 30
}
