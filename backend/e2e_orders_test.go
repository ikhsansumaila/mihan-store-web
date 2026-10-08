//go:build e2e

// E2E fitur pesanan terhadap container backend UJI. Webhook Discord diarahkan ke server
// tiruan di proses tes ini (http://<runner>:9000/discord/...), BUKAN Discord asli.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- Server tiruan webhook Discord ----------

var (
	discordMode atomic.Value // "ok" | "500" | "timeout"
	discordMu   sync.Mutex
	discordHits []string // body setiap permintaan (termasuk yang gagal)
)

func discordMockHandler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	discordMu.Lock()
	discordHits = append(discordHits, string(b))
	discordMu.Unlock()
	mode, _ := discordMode.Load().(string)
	switch mode {
	case "500":
		w.WriteHeader(500)
	case "timeout":
		time.Sleep(7 * time.Second)
		w.WriteHeader(204)
	default:
		w.WriteHeader(204)
	}
}

// waitHits menunggu sampai ada n permintaan yang memuat substr (atau batas waktu).
func waitHits(substr string, n int, timeout time.Duration) []string {
	deadline := time.Now().Add(timeout)
	for {
		discordMu.Lock()
		var got []string
		for _, h := range discordHits {
			if strings.Contains(h, substr) {
				got = append(got, h)
			}
		}
		discordMu.Unlock()
		if len(got) >= n || time.Now().After(deadline) {
			return got
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func e2eRegister(t *testing.T, username, name string) string {
	r := e2e(t, "POST", e2eBackend+"/api/auth/register", nil, map[string]any{
		"username": username, "email": username + "@uji.test", "name": name, "phone": "081277776666",
		"password": "passwordE2E123", "turnstileToken": e2eTurnstileToken})
	if r.Code != 201 {
		t.Fatalf("register %s: %d %s", username, r.Code, r.Raw)
	}
	return r.Body["token"].(string)
}

const (
	e2eAddress = "Jl. Rahasia Alamat No. 77"
	e2eNote    = "catatan-pribadi-e2e"
)

func e2eCheckout(t *testing.T, tok string, productID, qty int) e2eResp {
	t.Helper()
	ensureE2ERegions(t)
	if r := e2e(t, "POST", e2eBackend+"/api/cart/items", bearer(tok), map[string]any{"productId": productID, "qty": qty, "price": 1}); r.Code != 200 {
		t.Fatalf("tambah keranjang: %d %s", r.Code, r.Raw)
	}
	key, _ := newPublicID()
	b := map[string]any{
		"recipientName": "Penerima E2E", "recipientPhone": "0813-5555-4444", "address": e2eAddress, "city": "Tangerang",
		"postalCode": "15111", "note": e2eNote, "idempotencyKey": key,
		// Harga/total dari browser harus diabaikan server.
		"total": 1, "subtotal": 1, "items": []map[string]any{{"productId": productID, "qty": 1, "price": 1}},
	}
	for k, v := range e2eRegionCodes() {
		b[k] = v
	}
	return e2e(t, "POST", e2eBackend+"/api/orders", bearer(tok), b)
}

func assertMinimalPayload(t *testing.T, body string) {
	t.Helper()
	for _, bad := range []string{e2eAddress, "Rahasia", "135555", "8135555", "@uji.test", e2eNote, "Tangerang", "15111", "@everyone",
		"Sukarasa", "Banten", "36.71"} {
		if strings.Contains(body, bad) {
			t.Errorf("payload Discord memuat data terlarang %q: %s", bad, body)
		}
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("payload bukan JSON: %v", err)
	}
	am, _ := p["allowed_mentions"].(map[string]any)
	if parse, ok := am["parse"].([]any); !ok || len(parse) != 0 {
		t.Errorf("allowed_mentions.parse harus kosong: %v", p["allowed_mentions"])
	}
}

func TestE2EOrderFlow(t *testing.T) {
	discordMode.Store("ok")
	admin := os.Getenv("E2E_ADMIN_EMAIL")
	tokA := e2eRegister(t, "e2e_pembeli_a", "Pembeli *A* @everyone")
	tokB := e2eRegister(t, "e2e_pembeli_b", "Pembeli B")

	// Tanpa login -> 401; pelanggan ke admin -> 401.
	for _, p := range []string{"/api/cart", "/api/orders", "/api/store-info"} {
		if r := e2e(t, "GET", e2eBackend+p, nil, nil); r.Code != 401 {
			t.Errorf("%s tanpa login: %d", p, r.Code)
		}
	}
	if r := e2e(t, "GET", e2eBackend+"/api/admin/orders", bearer(tokA), nil); r.Code != 401 && r.Code != 403 {
		t.Fatalf("pelanggan -> /api/admin/orders: %d", r.Code)
	}

	// Checkout: harga dari browser diabaikan (produk 1 = Rp 45.000).
	r := e2eCheckout(t, tokA, 1, 2)
	if r.Code != 201 || r.Body["total"].(float64) != 90000 || r.Body["subtotal"].(float64) != 90000 {
		t.Fatalf("checkout: %d %s", r.Code, r.Raw)
	}
	orderNo := r.Body["orderNo"].(string)
	if cart := e2e(t, "GET", e2eBackend+"/api/cart", bearer(tokA), nil); cart.Body["lineCount"].(float64) != 0 {
		t.Fatal("keranjang harus kosong setelah checkout")
	}
	hits := waitHits(orderNo, 1, 10*time.Second)
	if len(hits) != 1 || !strings.Contains(hits[0], "Pesanan baru") || !strings.Contains(hits[0], "Rp 90.000") ||
		!strings.Contains(hits[0], "/admin/orders/") {
		t.Fatalf("notifikasi pesanan baru: %v", hits)
	}
	assertMinimalPayload(t, hits[0])

	// Pesanan saya + IDOR.
	if r := e2e(t, "GET", e2eBackend+"/api/orders", bearer(tokA), nil); r.Body["total"].(float64) != 1 {
		t.Fatalf("daftar A: %s", r.Raw)
	}
	other := e2e(t, "GET", e2eBackend+"/api/orders/"+orderNo, bearer(tokB), nil)
	missing := e2e(t, "GET", e2eBackend+"/api/orders/MS-200101-9999", bearer(tokB), nil)
	if other.Code != 404 || string(other.Raw) != string(missing.Raw) {
		t.Fatalf("IDOR: %d %s vs %s", other.Code, other.Raw, missing.Raw)
	}
	if r := e2e(t, "POST", e2eBackend+"/api/orders/"+orderNo+"/cancel", bearer(tokB), map[string]any{}); r.Code != 404 {
		t.Fatalf("IDOR batal: %d", r.Code)
	}

	// Admin: daftar -> diskon & ongkir -> Tandai Dibayar -> Selesai.
	r = e2e(t, "GET", e2eBackend+"/api/admin/orders?q="+orderNo, adminHdr(t, admin, false), nil)
	if r.Code != 200 || r.Body["total"].(float64) != 1 {
		t.Fatalf("admin daftar: %d %s", r.Code, r.Raw)
	}
	id := fmt.Sprint(r.Body["items"].([]any)[0].(map[string]any)["id"].(float64))
	base := e2eBackend + "/api/admin/orders/" + id
	if r := e2e(t, "PATCH", base+"/pricing", map[string]string{"Cf-Access-Jwt-Assertion": accessJWT(t, admin)},
		map[string]any{"discount": 5000, "shippingFee": 12000}); r.Code != 403 {
		t.Fatalf("CSRF pricing tanpa header harus 403: %d", r.Code)
	}
	// Status awal "menunggu konfirmasi": admin mengonfirmasi (ongkir/diskon) -> menunggu pembayaran.
	if r := e2e(t, "POST", base+"/confirm", map[string]string{"Cf-Access-Jwt-Assertion": accessJWT(t, admin)},
		map[string]any{"discount": 5000, "shippingFee": 12000}); r.Code != 403 {
		t.Fatalf("CSRF confirm tanpa header harus 403: %d", r.Code)
	}
	r = e2e(t, "POST", base+"/confirm", adminHdr(t, admin, true), map[string]any{"discount": 5000, "discountNote": "Promo", "shippingFee": 12000})
	if r.Code != 200 || r.Body["total"].(float64) != 97000 || r.Body["status"] != "pending_payment" {
		t.Fatalf("confirm: %d %s", r.Code, r.Raw)
	}
	r = e2e(t, "PATCH", base+"/status", adminHdr(t, admin, true), map[string]any{"from": "pending_payment", "to": "paid", "paymentNote": "BCA"})
	if r.Code != 200 || r.Body["status"] != "paid" {
		t.Fatalf("tandai dibayar: %d %s", r.Code, r.Raw)
	}
	if r := e2e(t, "PATCH", base+"/status", adminHdr(t, admin, true), map[string]any{"from": "pending_payment", "to": "paid"}); r.Code != 409 {
		t.Fatalf("bayar ganda harus 409: %d", r.Code)
	}
	if r := e2e(t, "PATCH", base+"/pricing", adminHdr(t, admin, true), map[string]any{"discount": 0, "shippingFee": 0}); r.Code != 409 {
		t.Fatalf("diskon terkunci setelah dibayar: %d", r.Code)
	}
	paidHits := waitHits("Pesanan dibayar", 1, 10*time.Second)
	if len(paidHits) != 1 || !strings.Contains(paidHits[0], "Rp 97.000") {
		t.Fatalf("notifikasi dibayar: %v", paidHits)
	}
	assertMinimalPayload(t, paidHits[0])
	r = e2e(t, "PATCH", base+"/status", adminHdr(t, admin, true), map[string]any{"from": "paid", "to": "completed"})
	if r.Code != 200 || r.Body["status"] != "completed" || len(r.Body["history"].([]any)) != 4 {
		t.Fatalf("selesai: %d %s", r.Code, r.Raw)
	}
	if r := e2e(t, "GET", e2eBackend+"/api/orders/"+orderNo, bearer(tokA), nil); r.Body["status"] != "completed" || r.Body["total"].(float64) != 97000 {
		t.Fatalf("pelanggan melihat status: %s", r.Raw)
	}

	// Pelanggan membatalkan pesanan lain.
	r = e2eCheckout(t, tokA, 2, 1)
	order2 := r.Body["orderNo"].(string)
	if r := e2e(t, "POST", e2eBackend+"/api/orders/"+order2+"/cancel", bearer(tokA), map[string]any{"reason": "Salah pilih"}); r.Code != 200 || r.Body["status"] != "cancelled" {
		t.Fatalf("batal: %d %s", r.Code, r.Raw)
	}
	if h := waitHits("Pesanan dibatalkan", 1, 10*time.Second); len(h) != 1 || !strings.Contains(h[0], order2) {
		t.Fatalf("notifikasi batal: %v", h)
	}

	// Webhook gagal (500): checkout tetap berhasil, 1x percobaan ulang.
	discordMode.Store("500")
	r = e2eCheckout(t, tokB, 3, 1)
	if r.Code != 201 {
		t.Fatalf("checkout saat webhook 500: %d %s", r.Code, r.Raw)
	}
	if h := waitHits(r.Body["orderNo"].(string), 2, 15*time.Second); len(h) != 2 {
		t.Fatalf("webhook 500 harus dicoba 2x: %d", len(h))
	}
	// Webhook timeout: checkout tetap cepat & berhasil.
	discordMode.Store("timeout")
	start := time.Now()
	r = e2eCheckout(t, tokB, 4, 1)
	if r.Code != 201 || time.Since(start) > 3*time.Second {
		t.Fatalf("checkout saat webhook timeout: %d dalam %v", r.Code, time.Since(start))
	}
	if h := waitHits(r.Body["orderNo"].(string), 2, 20*time.Second); len(h) != 2 {
		t.Fatalf("webhook timeout harus dicoba 2x: %d", len(h))
	}
	discordMode.Store("ok")

	// Pengaturan toko -> store-info pelanggan.
	r = e2e(t, "PUT", e2eBackend+"/api/admin/settings", adminHdr(t, admin, true), map[string]any{"store_whatsapp": "081299998888",
		"bank_name": "BCA", "bank_account_number": "1234567890", "bank_account_holder": "Toko E2E", "payment_note": "Transfer sesuai total"})
	if r.Code != 200 {
		t.Fatalf("settings: %d %s", r.Code, r.Raw)
	}
	if r := e2e(t, "GET", e2eBackend+"/api/store-info", bearer(tokA), nil); r.Body["storeWhatsapp"] != "+6281299998888" || r.Body["paymentConfigured"] != true {
		t.Fatalf("store-info: %s", r.Raw)
	}
	// Ringkasan admin memuat hitungan pesanan.
	if r := e2e(t, "GET", e2eBackend+"/api/admin/summary", adminHdr(t, admin, false), nil); r.Body["orders"] == nil {
		t.Fatalf("summary: %s", r.Raw)
	}
	// Log aktivitas pesanan tercatat.
	r = e2e(t, "GET", e2eBackend+"/api/admin/activity-logs?entity_type=order&per_page=100", adminHdr(t, admin, false), nil)
	seen := map[string]bool{}
	for _, it := range r.Body["items"].([]any) {
		m := it.(map[string]any)
		seen[m["action"].(string)] = true
		b, _ := json.Marshal(m["details"])
		if strings.Contains(string(b), e2eAddress) || strings.Contains(string(b), "5555") {
			t.Errorf("log memuat data pribadi: %s", b)
		}
	}
	for _, a := range []string{"order.create", "order.pricing_update", "order.pay", "order.complete", "order.cancel"} {
		if !seen[a] {
			t.Errorf("aksi %s tidak tercatat", a)
		}
	}
}
