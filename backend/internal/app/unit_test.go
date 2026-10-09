package app

import (
	"net/http/httptest"
	"strings"
	"testing"

	"mihanstore/internal/platform"
)

func TestSecurityHeadersAndHealth(t *testing.T) {
	app := NewApp(platform.Config{CORSAllowedOrigins: platform.DefaultCORSOrigins})
	h := newRouter(app)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("/health: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/health/ready", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "mysql") {
		t.Fatalf("/health/ready tanpa DB harus 503 tanpa detail: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/auth/verify", strings.NewReader(`{"token":"token_x"}`))
	h.ServeHTTP(w, r)
	if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("verify token palsu: %d cache=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	// CORS: origin diizinkan vs tidak.
	for origin, allowed := range map[string]bool{"https://store.mihan.web.id": true, "https://evil.example": false} {
		w = httptest.NewRecorder()
		r = httptest.NewRequest("GET", "/api/products", nil)
		r.Header.Set("Origin", origin)
		h.ServeHTTP(w, r)
		got := w.Header().Get("Access-Control-Allow-Origin")
		if (got == origin) != allowed || got == "*" {
			t.Errorf("CORS %s: ACAO=%q", origin, got)
		}
		if w.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Error("credentials tidak boleh diizinkan")
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/products", nil))
	// Produk kini dari database: tanpa DB -> 503 tanpa detail internal.
	if w.Code != 503 || strings.Contains(w.Body.String(), "mysql") {
		t.Fatalf("/api/products tanpa DB harus 503: %d", w.Code)
	}
}
