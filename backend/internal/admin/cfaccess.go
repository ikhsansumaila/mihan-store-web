package admin

// Verifikasi JWT Cloudflare Access (header Cf-Access-Jwt-Assertion) untuk rute /api/admin/*.
// Fail-closed: konfigurasi kosong/tidak valid -> 503; token tidak ada/tidak valid -> 401;
// email di luar ADMIN_EMAILS -> 403. Tidak ada mode bypass.

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"mihanstore/internal/platform"
)

var (
	teamDomainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.cloudflareaccess\.com$`)
	audRe        = regexp.MustCompile(`^[A-Za-z0-9]{16,128}$`)

	errAccessNotConfigured = errors.New("akses admin belum dikonfigurasi")
	errEmailNotAllowed     = errors.New("email tidak diizinkan")
)

type AccessVerifier struct {
	teamDomain string
	aud        string
	issuer     string
	admins     map[string]bool
	JWKS       *platform.JWKSCache
	now        func() time.Time
}

// NewAccessVerifier mengembalikan errAccessNotConfigured bila konfigurasi belum lengkap.
func NewAccessVerifier(cfg platform.Config) (*AccessVerifier, error) {
	if !teamDomainRe.MatchString(cfg.CFAccessTeamDomain) || !audRe.MatchString(cfg.CFAccessAUD) || len(cfg.AdminEmails) == 0 {
		return nil, errAccessNotConfigured
	}
	admins := map[string]bool{}
	for _, e := range cfg.AdminEmails {
		if !strings.Contains(e, "@") {
			return nil, errAccessNotConfigured
		}
		admins[strings.ToLower(e)] = true
	}
	url := "https://" + cfg.CFAccessTeamDomain + "/cdn-cgi/access/certs"
	if cfg.TestCFAccessJWKSURL != "" && cfg.IsTestDB() {
		url = cfg.TestCFAccessJWKSURL
	}
	return &AccessVerifier{
		teamDomain: cfg.CFAccessTeamDomain, aud: cfg.CFAccessAUD,
		issuer: "https://" + cfg.CFAccessTeamDomain, admins: admins,
		JWKS: platform.NewJWKSCache(platform.HTTPJWKSFetcher(url)), now: time.Now,
	}, nil
}

type accessClaims struct {
	Aud   json.RawMessage      `json:"aud"`
	Iss   string               `json:"iss"`
	Exp   platform.NumericDate `json:"exp"`
	Nbf   platform.NumericDate `json:"nbf"`
	Iat   platform.NumericDate `json:"iat"`
	Email string               `json:"email"`
	Type  string               `json:"type"`
}

// Verify memvalidasi token dan mengembalikan email (huruf kecil).
// Error errEmailNotAllowed berarti token sah tetapi email tidak ada di ADMIN_EMAILS.
func (v *AccessVerifier) Verify(ctx context.Context, token string) (string, error) {
	pb, err := platform.VerifyRS256(ctx, v.JWKS, token)
	if err != nil {
		return "", err
	}
	var c accessClaims
	if err := json.Unmarshal(pb, &c); err != nil {
		return "", errors.New("payload rusak")
	}
	if !platform.AudContains(c.Aud, v.aud) {
		return "", errors.New("aud tidak cocok")
	}
	if c.Iss != v.issuer {
		return "", errors.New("iss tidak cocok")
	}
	if err := platform.CheckTimes(v.now(), c.Exp, c.Nbf, c.Iat); err != nil {
		return "", err
	}
	email := strings.ToLower(strings.TrimSpace(c.Email))
	if email == "" {
		return "", errors.New("token tanpa email")
	}
	if !v.admins[email] {
		return email, errEmailNotAllowed
	}
	return email, nil
}
