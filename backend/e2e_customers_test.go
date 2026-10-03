//go:build e2e

// E2E menu Pelanggan & alias terhadap container backend (dan frontend) UJI. Webhook Discord =
// server tiruan di proses tes ini.
package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestE2ECustomerAlias(t *testing.T) {
	discordMode.Store("ok")
	admin := os.Getenv("E2E_ADMIN_EMAIL")
	const name = "Pelanggan Asli E2E"
	const alias = "Bu Siti *Toko Maju*"
	// Akun lewat Google (tiruan) agar tidak menambah registrasi password (batas 5/jam per IP).
	g := e2e(t, "POST", e2eBackend+"/api/auth/google", nil, map[string]any{"credential": googleJWT(t, "sub-e2e-plg-alias", "e2e_plg_alias@uji.test", name)})
	if g.Code != 200 || g.Body["needsProfile"] != true {
		t.Fatalf("google: %d %s", g.Code, g.Raw)
	}
	g = e2e(t, "POST", e2eBackend+"/api/auth/google/complete", nil, map[string]any{"profileToken": g.Body["profileToken"], "username": "e2e_plg_alias"})
	if g.Code != 201 || g.Body["token"] == nil {
		t.Fatalf("google complete: %d %s", g.Code, g.Raw)
	}
	tok := g.Body["token"].(string)

	// Tanpa token Access / dengan sesi pelanggan -> ditolak.
	for _, p := range []string{"/api/admin/customers", "/api/admin/customers/1"} {
		if r := e2e(t, "GET", e2eBackend+p, nil, nil); r.Code != 401 {
			t.Errorf("%s tanpa Access: %d", p, r.Code)
		}
		if r := e2e(t, "GET", e2eBackend+p, bearer(tok), nil); r.Code != 401 && r.Code != 403 {
			t.Errorf("%s dengan sesi pelanggan: %d", p, r.Code)
		}
	}

	// Pesanan 1 (tanpa alias) -> notifikasi memakai nama akun.
	r := e2eCheckout(t, tok, 1, 1)
	if r.Code != 201 {
		t.Fatalf("checkout 1: %d %s", r.Code, r.Raw)
	}
	o1 := r.Body["orderNo"].(string)
	hits := waitHits(o1, 1, 10*time.Second)
	if len(hits) != 1 || !strings.Contains(hits[0], name) {
		t.Fatalf("notifikasi tanpa alias: %v", hits)
	}
	assertMinimalPayload(t, hits[0])

	// Admin mencari pelanggan lalu memberi alias (CSRF wajib).
	r = e2e(t, "GET", e2eBackend+"/api/admin/customers?q=e2e_plg_alias", adminHdr(t, admin, false), nil)
	if r.Code != 200 || r.Body["total"].(float64) != 1 {
		t.Fatalf("daftar pelanggan: %d %s", r.Code, r.Raw)
	}
	id := fmt.Sprint(r.Body["items"].([]any)[0].(map[string]any)["id"].(float64))
	if r := e2e(t, "PATCH", e2eBackend+"/api/admin/customers/"+id+"/alias", map[string]string{"Cf-Access-Jwt-Assertion": accessJWT(t, admin)},
		map[string]any{"alias": alias}); r.Code != 403 {
		t.Fatalf("PATCH alias tanpa CSRF harus 403: %d", r.Code)
	}
	if r := e2e(t, "PATCH", e2eBackend+"/api/admin/customers/"+id+"/alias", bearer(tok), map[string]any{"alias": alias}); r.Code != 401 && r.Code != 403 {
		t.Fatalf("PATCH alias oleh pelanggan: %d", r.Code)
	}
	r = e2e(t, "PATCH", e2eBackend+"/api/admin/customers/"+id+"/alias", adminHdr(t, admin, true), map[string]any{"alias": "  " + alias + "  "})
	if r.Code != 200 || r.Body["alias"] != alias {
		t.Fatalf("ubah alias: %d %s", r.Code, r.Raw)
	}

	// Pesanan 2 (dengan alias) -> notifikasi memakai alias (markdown dinetralkan), tanpa nama akun.
	r = e2eCheckout(t, tok, 2, 1)
	if r.Code != 201 {
		t.Fatalf("checkout 2: %d %s", r.Code, r.Raw)
	}
	o2 := r.Body["orderNo"].(string)
	hits = waitHits(o2, 1, 10*time.Second)
	if len(hits) != 1 || !strings.Contains(hits[0], `Bu Siti \\*Toko Maju\\*`) || strings.Contains(hits[0], name) {
		t.Fatalf("notifikasi dengan alias: %v", hits)
	}
	assertMinimalPayload(t, hits[0])
	t.Logf("PAYLOAD_DISCORD_ALIAS %s", hits[0])

	// Alias tampak di daftar pesanan admin (dan bisa dicari) serta di daftar/detail pelanggan.
	r = e2e(t, "GET", e2eBackend+"/api/admin/orders?q="+url.QueryEscape("Toko Maju"), adminHdr(t, admin, false), nil)
	if r.Code != 200 || r.Body["total"].(float64) < 2 {
		t.Fatalf("cari pesanan lewat alias: %d %s", r.Code, r.Raw)
	}
	for _, it := range r.Body["items"].([]any) {
		c := it.(map[string]any)["customer"].(map[string]any)
		if c["alias"] != alias || fmt.Sprint(c["id"].(float64)) != id {
			t.Fatalf("customer di daftar pesanan: %v", c)
		}
	}
	r = e2e(t, "GET", e2eBackend+"/api/admin/customers?sort=orders&q="+url.QueryEscape("Toko Maju"), adminHdr(t, admin, false), nil)
	if r.Code != 200 || r.Body["total"].(float64) != 1 {
		t.Fatalf("cari pelanggan lewat alias: %d %s", r.Code, r.Raw)
	}
	c := r.Body["items"].([]any)[0].(map[string]any)
	if c["alias"] != alias || c["orderCount"].(float64) != 2 || c["totalSpent"].(float64) != 0 {
		t.Fatalf("statistik pelanggan (belum ada yang dibayar): %v", c)
	}
	r = e2e(t, "GET", e2eBackend+"/api/admin/customers/"+id, adminHdr(t, admin, false), nil)
	if r.Code != 200 || len(r.Body["recentOrders"].([]any)) != 2 {
		t.Fatalf("detail pelanggan: %d %s", r.Code, r.Raw)
	}

	// Alias tidak pernah muncul di API pelanggan.
	for _, p := range []string{"/api/auth/me", "/api/orders", "/api/orders/" + o2, "/api/cart"} {
		r := e2e(t, "GET", e2eBackend+p, bearer(tok), nil)
		if r.Code != 200 || strings.Contains(string(r.Raw), "Toko Maju") || strings.Contains(string(r.Raw), `"alias"`) {
			t.Errorf("alias bocor / gagal di %s: %d %s", p, r.Code, r.Raw)
		}
	}

	// Alias dikosongkan -> notifikasi kembali memakai nama akun.
	if r := e2e(t, "PATCH", e2eBackend+"/api/admin/customers/"+id+"/alias", adminHdr(t, admin, true), map[string]any{"alias": ""}); r.Code != 200 || r.Body["alias"] != nil {
		t.Fatalf("hapus alias: %d %s", r.Code, r.Raw)
	}
	r = e2eCheckout(t, tok, 3, 1)
	o3 := r.Body["orderNo"].(string)
	hits = waitHits(o3, 1, 10*time.Second)
	if len(hits) != 1 || !strings.Contains(hits[0], name) || strings.Contains(hits[0], "Toko Maju") {
		t.Fatalf("notifikasi setelah alias dihapus: %v", hits)
	}
	t.Logf("PAYLOAD_DISCORD_TANPA_ALIAS %s", hits[0])

	// Frontend uji: rute /admin/customers tersaji (SPA) dan bundel memuat menu Pelanggan & toggle invoice.
	if e2eFrontend != "" {
		res, err := http.Get(e2eFrontend + "/admin/customers")
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("frontend /admin/customers: %v %v", err, res)
		}
		html, _ := io.ReadAll(res.Body)
		res.Body.Close()
		m := regexp.MustCompile(`/static/js/main\.[0-9a-f]+\.js`).Find(html)
		if m == nil {
			t.Fatalf("bundel main tidak ditemukan: %s", html)
		}
		res, err = http.Get(e2eFrontend + string(m))
		if err != nil {
			t.Fatal(err)
		}
		js, _ := io.ReadAll(res.Body)
		res.Body.Close()
		for _, s := range []string{"Pelanggan", "/admin/customers", "Pakai nama alias", "/alias"} {
			if !strings.Contains(string(js), s) {
				t.Errorf("bundel tidak memuat %q", s)
			}
		}
	}
}
