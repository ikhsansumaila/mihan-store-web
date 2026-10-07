package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"

	"mihanstore/notify"
	"mihanstore/push"
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
	app.access = newTestAccess(t)
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
	app := NewApp(Config{CORSAllowedOrigins: defaultCORSOrigins})
	if app.pusher.Enabled() {
		t.Fatal("tanpa VAPID harus Noop")
	}
	if _, ok := app.pusher.(push.Noop); !ok {
		t.Fatal("harus push.Noop")
	}
	// pushOrder pada Noop tidak menyentuh database (db nil) dan tidak panik.
	app.pushOrder(nil, push.KindCreated, 1)
}

func TestPushKindsMatchNotify(t *testing.T) {
	if push.KindCreated != notify.KindCreated || push.KindCancelled != notify.KindCancelled {
		t.Fatal("jenis kejadian push harus sama dengan notify")
	}
}

func TestVAPIDSubjectDefault(t *testing.T) {
	t.Setenv("VAPID_SUBJECT", "")
	t.Setenv("PUBLIC_BASE_URL", "")
	if c := LoadConfig(); c.VAPIDSubject != "https://store.mihan.web.id" {
		t.Fatalf("subject default: %q", c.VAPIDSubject)
	}
	t.Setenv("VAPID_SUBJECT", "mailto:toko@mihan.web.id")
	if c := LoadConfig(); c.VAPIDSubject != "mailto:toko@mihan.web.id" {
		t.Fatalf("subject: %q", c.VAPIDSubject)
	}
}

func TestEndpointHashStable(t *testing.T) {
	a := endpointHash("https://fcm.googleapis.com/fcm/send/x")
	if len(a) != 64 || a != endpointHash("https://fcm.googleapis.com/fcm/send/x") || a == endpointHash("https://fcm.googleapis.com/fcm/send/y") {
		t.Fatal("hash endpoint")
	}
}
