package identity

import (
	"regexp"
	"strings"
)

var usernameCleanRe = regexp.MustCompile(`[^a-z0-9_]+`)

// UsernameBase membuat kandidat username dari bagian lokal email.
func UsernameBase(email string) string {
	local := strings.ToLower(email)
	if at := strings.IndexByte(local, '@'); at >= 0 {
		local = local[:at]
	}
	if plus := strings.IndexByte(local, '+'); plus >= 0 {
		local = local[:plus]
	}
	b := strings.Trim(usernameCleanRe.ReplaceAllString(local, "_"), "_")
	for strings.Contains(b, "__") {
		b = strings.ReplaceAll(b, "__", "_")
	}
	if len(b) > 24 {
		b = strings.TrimRight(b[:24], "_")
	}
	if len(b) < 3 {
		b = "user_" + b
	}
	if _, err := NormalizeUsername(b); err != nil {
		b = "user"
	}
	return b
}
