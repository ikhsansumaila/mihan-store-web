//go:build e2e

package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func e2eCartLine(t *testing.T, body map[string]any, pid float64) map[string]any {
	t.Helper()
	items, _ := body["items"].([]any)
	for _, it := range items {
		if m := it.(map[string]any); m["productId"].(float64) == pid {
			return m
		}
	}
	t.Fatalf("produk %v tidak ada di keranjang: %v", pid, body)
	return nil
}

func e2eOrderBody(expected any) map[string]any {
	key, _ := newPublicID()
	b := map[string]any{"recipientName": "Penerima Grosir", "recipientPhone": "0813-5555-4444", "address": "Jl. Grosir No. 1",
		"city": "Tangerang", "postalCode": "15111", "idempotencyKey": key}
	for k, v := range e2eRegionCodes() {
		b[k] = v
	}
	if expected != nil {
		b["expectedTotal"] = expected
	}
	return b
}

// Alur harga grosir lengkap lewat HTTP ke container backend uji: admin membuat produk dengan jenjang
// campuran fixed + percent, pelanggan menambah ke keranjang, harga turun saat qty melewati jenjang,
// admin mengubah harga, pelanggan melihat penanda, checkout dengan total lama -> 409, setelah
// konfirmasi pesanan terbentuk dengan harga baru, dan pesanan lama tidak berubah.
func TestE2ETierFlow(t *testing.T) {
	ensureE2ERegions(t)
	discordMode.Store("ok")
	admin := os.Getenv("E2E_ADMIN_EMAIL")
	B := e2eBackend

	r := e2e(t, "POST", B+"/api/admin/products", adminHdr(t, admin, true), map[string]any{
		"name": "Kerupuk Grosir E2E", "categoryId": 1, "price": 50000, "description": "uji e2e", "unit": "Pak",
		"tiers": []map[string]any{{"minQty": 10, "type": "fixed", "value": 46000}, {"minQty": 50, "type": "percent", "value": 10}}})
	if r.Code != 201 || r.Body["unit"] != "pak" {
		t.Fatalf("admin buat produk: %d %s", r.Code, r.Raw)
	}
	pid := r.Body["id"].(float64)
	path := fmt.Sprintf("%s/api/admin/products/%d", B, int(pid))

	// Validasi server: jenjang tidak monoton ditolak 422 dengan pesan per jenjang.
	if r := e2e(t, "PUT", path, adminHdr(t, admin, true), map[string]any{"name": "Kerupuk Grosir E2E", "categoryId": 1, "price": 50000,
		"tiers": []map[string]any{{"minQty": 10, "type": "fixed", "value": 46000}, {"minQty": 20, "type": "fixed", "value": 47000}}}); r.Code != 422 || r.Body["tierErrors"] == nil {
		t.Fatalf("jenjang tidak monoton: %d %s", r.Code, r.Raw)
	}
	// Tanpa token Access: tidak pernah 200.
	if r := e2e(t, "GET", path, nil, nil); r.Code == 200 {
		t.Fatal("admin tanpa Access tidak boleh 200")
	}

	// Publik: unit + tiers.
	pub := e2e(t, "GET", B+"/api/products/search?q=Grosir%20E2E", nil, nil)
	if !strings.Contains(string(pub.Raw), `"unit":"pak","tiers":[{"minQty":10,"type":"fixed","value":46000,"unitPrice":46000},{"minQty":50,"type":"percent","value":10,"unitPrice":45000}]`) {
		t.Fatalf("publik: %s", pub.Raw)
	}

	// Daftar dari IP klien lain (backend uji mempercayai X-Real-IP dari jaringan Docker) agar tidak
	// terkena batas laju registrasi 5/jam per IP dari tes lain.
	reg := e2e(t, "POST", B+"/api/auth/register", map[string]string{"X-Real-IP": "198.51.100.77"}, map[string]any{
		"username": "e2e_grosir", "email": "e2e_grosir@uji.test", "name": "Pembeli Grosir", "phone": "081277776655",
		"password": "passwordE2E123", "turnstileToken": e2eTurnstileToken})
	if reg.Code != 201 {
		t.Fatalf("register: %d %s", reg.Code, reg.Raw)
	}
	tok := reg.Body["token"].(string)
	if r := e2e(t, "POST", B+"/api/cart/ack-prices", nil, nil); r.Code != 401 {
		t.Fatalf("ack tanpa login: %d", r.Code)
	}

	// Pesanan "lama": 50 pak dengan harga jenjang percent (45.000).
	e2e(t, "POST", B+"/api/cart/items", bearer(tok), map[string]any{"productId": pid, "qty": 50})
	old := e2e(t, "POST", B+"/api/orders", bearer(tok), e2eOrderBody(2250000))
	if old.Code != 201 {
		t.Fatalf("pesanan lama: %d %s", old.Code, old.Raw)
	}
	oldNo := old.Body["orderNo"].(string)

	// Keranjang: 9 -> harga dasar + petunjuk; 10 -> harga grosir (bukan "perubahan harga").
	r = e2e(t, "POST", B+"/api/cart/items", bearer(tok), map[string]any{"productId": pid, "qty": 9})
	l := e2eCartLine(t, r.Body, pid)
	if l["unitPrice"].(float64) != 50000 || l["nextTier"].(map[string]any)["moreQty"].(float64) != 1 {
		t.Fatalf("qty 9: %v", l)
	}
	r = e2e(t, "PUT", B+"/api/cart/items", bearer(tok), map[string]any{"productId": pid, "qty": 10})
	l = e2eCartLine(t, r.Body, pid)
	if l["unitPrice"].(float64) != 46000 || l["tierMinQty"].(float64) != 10 || l["priceChanged"] != false || l["savings"].(float64) != 40000 {
		t.Fatalf("qty 10: %v", l)
	}
	shown := r.Body["subtotal"].(float64) // 460.000 ditampilkan ke pelanggan

	// Admin mengubah harga jenjang 10 -> 47.000.
	if r := e2e(t, "PUT", path, adminHdr(t, admin, true), map[string]any{"name": "Kerupuk Grosir E2E", "categoryId": 1, "price": 50000, "unit": "pak",
		"tiers": []map[string]any{{"minQty": 10, "type": "fixed", "value": 47000}, {"minQty": 50, "type": "percent", "value": 10}}}); r.Code != 200 {
		t.Fatalf("admin ubah jenjang: %d %s", r.Code, r.Raw)
	}
	r = e2e(t, "GET", B+"/api/cart", bearer(tok), nil)
	l = e2eCartLine(t, r.Body, pid)
	if l["priceChanged"] != true || l["previousUnitPrice"].(float64) != 46000 || l["unitPrice"].(float64) != 47000 {
		t.Fatalf("penanda: %v", l)
	}

	// Checkout dengan total lama -> 409 price_changed, tanpa pesanan, keranjang utuh.
	body := e2eOrderBody(shown)
	r = e2e(t, "POST", B+"/api/orders", bearer(tok), body)
	if r.Code != 409 || r.Body["error"] != "price_changed" || r.Body["cart"].(map[string]any)["subtotal"].(float64) != 470000 {
		t.Fatalf("409: %d %s", r.Code, r.Raw)
	}
	if list := e2e(t, "GET", B+"/api/orders", bearer(tok), nil); list.Body["total"].(float64) != 1 {
		t.Fatalf("409 tidak boleh membuat pesanan: %s", list.Raw)
	}
	// Mengerti + konfirmasi ulang dengan total baru (kunci idempotensi yang sama).
	r = e2e(t, "POST", B+"/api/cart/ack-prices", bearer(tok), nil)
	if l := e2eCartLine(t, r.Body, pid); r.Code != 200 || l["priceChanged"] != false {
		t.Fatalf("ack: %d %s", r.Code, r.Raw)
	}
	body["expectedTotal"] = r.Body["subtotal"]
	r = e2e(t, "POST", B+"/api/orders", bearer(tok), body)
	if r.Code != 201 || r.Body["total"].(float64) != 470000 {
		t.Fatalf("konfirmasi: %d %s", r.Code, r.Raw)
	}
	it := r.Body["items"].([]any)[0].(map[string]any)
	if it["unitPrice"].(float64) != 47000 || it["baseUnitPrice"].(float64) != 50000 || it["tierMinQty"].(float64) != 10 || it["unit"] != "pak" {
		t.Fatalf("snapshot: %v", it)
	}

	// Admin mengubah harga dasar & jenjang lagi: pesanan lama tetap.
	e2e(t, "PUT", path, adminHdr(t, admin, true), map[string]any{"name": "Kerupuk Grosir E2E", "categoryId": 1, "price": 60000, "unit": "dus",
		"tiers": []map[string]any{}})
	o := e2e(t, "GET", B+"/api/orders/"+oldNo, bearer(tok), nil)
	oi := o.Body["items"].([]any)[0].(map[string]any)
	if o.Code != 200 || o.Body["total"].(float64) != 2250000 || oi["unitPrice"].(float64) != 45000 || oi["tierMinQty"].(float64) != 50 || oi["unit"] != "pak" {
		t.Fatalf("pesanan lama berubah: %d %s", o.Code, o.Raw)
	}
}
