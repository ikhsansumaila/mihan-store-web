package app

import (
	"net/http/httptest"
	"strings"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"

	"mihanstore/internal/platform"
	"mihanstore/internal/push"
)

// Rute push admin selalu di balik requireAdmin: tanpa token / token salah / tanpa CSRF tidak pernah 200.
func TestPushRoutesRequireAdmin(t *testing.T) {
	priv, pub, _ := webpush.GenerateVAPIDKeys()
	cfg := accessCfg()
	cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject = pub, priv, "https://store.mihan.web.id"
	app := NewApp(cfg)
	if !app.pusher.Enabled() {
		t.Fatal("pusher harus aktif dengan kunci sah")
	}
	app.admin.Access = newTestAccess(t)
	h := newRouter(app)
	good := signJWT(t, testKey, "k1", "RS256", accessClaimsOK())
	notAdmin := signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "email", "x@y.co"))
	csrf := map[string]string{"Content-Type": "application/json", "X-Requested-With": adminRequestedWith, "Origin": "https://store.mihan.web.id"}
	routes := []struct{ method, path string }{
		{"GET", "/api/admin/push/public-key"},
		{"POST", "/api/admin/push/subscribe"},
		{"DELETE", "/api/admin/push/subscribe"},
		{"POST", "/api/admin/push/test"},
	}
	for _, rt := range routes {
		for _, c := range []struct {
			name  string
			token string
			hdr   map[string]string
			want  int
		}{
			{"tanpa token", "", csrf, 401},
			{"token palsu", "palsu", csrf, 401},
			{"bukan admin", notAdmin, csrf, 403},
		} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, adminReq(rt.method, rt.path, c.token, c.hdr))
			if w.Code != c.want || strings.Contains(w.Body.String(), pub) {
				t.Errorf("%s %s %s: %d mau %d", rt.method, rt.path, c.name, w.Code, c.want)
			}
		}
		if rt.method != "GET" {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, adminReq(rt.method, rt.path, good, nil))
			if w.Code != 403 {
				t.Errorf("%s %s tanpa CSRF: %d mau 403", rt.method, rt.path, w.Code)
			}
		}
		// Token sah tanpa DB -> 503, bukan 200.
		w := httptest.NewRecorder()
		h.ServeHTTP(w, adminReq(rt.method, rt.path, good, csrf))
		if w.Code == 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s tanpa DB: %d", rt.method, rt.path, w.Code)
		}
	}
}

func TestPushDisabledWithoutKeys(t *testing.T) {
	app := NewApp(platform.Config{CORSAllowedOrigins: platform.DefaultCORSOrigins})
	if app.pusher.Enabled() {
		t.Fatal("tanpa VAPID harus Noop")
	}
	if _, ok := app.pusher.(push.Noop); !ok {
		t.Fatal("harus push.Noop")
	}
	// pushOrder pada Noop tidak menyentuh database (db nil) dan tidak panik.
	app.pushOrder(nil, push.KindCreated, 1)
}

// Rute push pelanggan selalu di balik sesi Bearer: tanpa token / token palsu -> 401, tidak pernah 200.
func TestCustomerPushRoutesRequireSession(t *testing.T) {
	priv, pub, _ := webpush.GenerateVAPIDKeys()
	cfg := platform.Config{CORSAllowedOrigins: platform.DefaultCORSOrigins, VAPIDPublicKey: pub, VAPIDPrivateKey: priv, VAPIDSubject: "https://store.mihan.web.id"}
	h := newRouter(NewApp(cfg))
	for _, rt := range [][2]string{{"GET", "/api/push/public-key"}, {"POST", "/api/push/subscribe"}, {"DELETE", "/api/push/subscribe"}} {
		for _, tok := range []string{"", "palsu", strings.Repeat("a", 64)} {
			r := httptest.NewRequest(rt[0], rt[1], strings.NewReader(`{}`))
			if tok != "" {
				r.Header.Set("Authorization", "Bearer "+tok)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code == 200 || strings.Contains(w.Body.String(), pub) || w.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s %s token=%q: %d", rt[0], rt[1], tok, w.Code)
			}
			if tok != strings.Repeat("a", 64) && w.Code != 401 {
				t.Errorf("%s %s token=%q: %d mau 401", rt[0], rt[1], tok, w.Code)
			}
		}
	}
}
