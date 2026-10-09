package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mihanstore/internal/platform"
)

func TestCustomerRoutesRequireLogin(t *testing.T) {
	app := NewApp(platform.Config{CORSAllowedOrigins: []string{"https://store.mihan.web.id"}, PublicBaseURL: platform.DefaultPublicBaseURL})
	h := newRouter(app)
	for _, rt := range []struct{ m, p string }{
		{"GET", "/api/cart"}, {"POST", "/api/cart/items"}, {"PUT", "/api/cart/items"}, {"DELETE", "/api/cart/items/1"},
		{"DELETE", "/api/cart"}, {"GET", "/api/orders"}, {"POST", "/api/orders"}, {"GET", "/api/orders/MS-261002-0001"},
		{"POST", "/api/orders/MS-261002-0001/cancel"}, {"GET", "/api/store-info"},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(rt.m, rt.p, strings.NewReader(`{}`))
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s tanpa login: %d (mau 401)", rt.m, rt.p, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s harus no-store", rt.m, rt.p)
		}
		// Token rusak juga 401.
		w = httptest.NewRecorder()
		r = httptest.NewRequest(rt.m, rt.p, strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer token_lama")
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s token rusak: %d", rt.m, rt.p, w.Code)
		}
	}
	// Admin pesanan tanpa konfigurasi/token Access -> 503/401, tidak pernah 200.
	for _, p := range []string{"/api/admin/orders", "/api/admin/orders/1", "/api/admin/settings"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code == 200 {
			t.Errorf("%s tanpa Access tidak boleh 200", p)
		}
	}
}
