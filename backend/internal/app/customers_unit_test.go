package app

import (
	"net/http/httptest"
	"testing"

	"mihanstore/internal/platform"
)

func TestCustomerRoutesRequireAdmin(t *testing.T) {
	// Belum dikonfigurasi -> 503 (fail-closed).
	h := newRouter(NewApp(platform.Config{CORSAllowedOrigins: platform.DefaultCORSOrigins}))
	for _, p := range []string{"/api/admin/customers", "/api/admin/customers/1"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, adminReq("GET", p, "x.y.z", nil))
		if w.Code != 503 {
			t.Errorf("%s belum dikonfigurasi: %d", p, w.Code)
		}
	}
	app := NewApp(accessCfg())
	app.admin.Access = newTestAccess(t)
	h = newRouter(app)
	good := signJWT(t, testKey, "k1", "RS256", accessClaimsOK())
	cases := []struct {
		name, method, path, token string
		hdr                       map[string]string
		status                    int
	}{
		{"daftar tanpa token", "GET", "/api/admin/customers", "", nil, 401},
		{"detail tanpa token", "GET", "/api/admin/customers/1", "", nil, 401},
		{"alias tanpa token", "PATCH", "/api/admin/customers/1/alias", "", nil, 401},
		{"token sesi pelanggan (Bearer) bukan Access", "GET", "/api/admin/customers", "", map[string]string{"Authorization": "Bearer abc"}, 401},
		{"email bukan admin", "GET", "/api/admin/customers", signJWT(t, testKey, "k1", "RS256", with(accessClaimsOK(), "email", "x@y.co")), nil, 403},
		{"PATCH tanpa CSRF", "PATCH", "/api/admin/customers/1/alias", good, nil, 403},
		{"PATCH origin asing", "PATCH", "/api/admin/customers/1/alias", good, map[string]string{
			"Content-Type": "application/json", "X-Requested-With": adminRequestedWith, "Origin": "https://evil.example"}, 403},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, adminReq(c.method, c.path, c.token, c.hdr))
		if w.Code != c.status {
			t.Errorf("%s: %d mau %d (%s)", c.name, w.Code, c.status, w.Body.String())
		}
	}
}
