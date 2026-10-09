//go:build integration

// Tes integrasi alur konfirmasi pesanan (status pending_confirmation, migrasi 020).
package app

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"mihanstore/internal/orders"
	"mihanstore/internal/push"
)

// confirmCall: POST /api/admin/orders/{id}/confirm sebagai admin uji; wajib 200.
func confirmCall(t *testing.T, h http.Handler, id uint64, discount, ship int64) resp {
	t.Helper()
	r := adminCall(t, h, "POST", fmt.Sprintf("/api/admin/orders/%d/confirm", id), integAdmin,
		map[string]any{"discount": discount, "shippingFee": ship})
	if r.Code != 200 {
		t.Fatalf("konfirmasi pesanan %d: %d %v", id, r.Code, r.Body)
	}
	return r
}

func TestIntegrationConfirmSchema(t *testing.T) {
	_, _, db, _ := setupOrders(t)
	var types []string
	db.Raw(`SELECT COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND
		((TABLE_NAME = 'orders' AND COLUMN_NAME = 'status') OR (TABLE_NAME = 'order_status_history' AND COLUMN_NAME IN ('from_status','to_status')))`).Scan(&types)
	if len(types) != 3 {
		t.Fatalf("kolom: %v", types)
	}
	for _, ty := range types {
		if !strings.HasSuffix(ty, ",'pending_confirmation')") || !strings.Contains(ty, "'pending_payment'") {
			t.Errorf("enum harus menambah pending_confirmation di akhir: %s", ty)
		}
	}
}

func TestIntegrationConfirmFlow(t *testing.T) {
	app, h, db, rec := setupOrders(t)
	rs := &recSender{}
	app.pusher = rs
	h = newRouter(app)
	tok, uid := aliasCustomer(t, h, db, "Pembeli Konfirmasi", "081277772222")
	no := createOrderFor(t, h, tok, 4)
	id := orderIDByNo(db, no)
	base := fmt.Sprintf("/api/admin/orders/%d", id)
	if !strings.Contains(rec.kinds(), "created:"+no) {
		t.Fatalf("notifikasi Discord pesanan baru tetap: %s", rec.kinds())
	}
	rs.mu.Lock()
	if len(rs.ev) != 1 || rs.ev[0].Kind != push.KindCreated {
		t.Fatalf("push admin pesanan baru tetap: %+v", rs.ev)
	}
	rs.mu.Unlock()

	// Pelanggan: menunggu konfirmasi, boleh batal; detail admin: bisa dikonfirmasi, Dibayar tidak ditawarkan.
	if r := call(t, h, "GET", "/api/orders/"+no, tok, nil); r.Body["status"] != orders.StatusPendingConfirmation || r.Body["canCancel"] != true {
		t.Fatalf("pelanggan: %v", r.Body)
	}
	r := adminCall(t, h, "GET", base, integAdmin, nil)
	if r.Body["canConfirm"] != true || r.Body["pricingLocked"] != false || fmt.Sprint(r.Body["allowedNext"]) != "[cancelled]" {
		t.Fatalf("detail admin: canConfirm=%v locked=%v next=%v", r.Body["canConfirm"], r.Body["pricingLocked"], r.Body["allowedNext"])
	}
	// Ringkasan & filter.
	var pc int64
	db.Raw("SELECT COUNT(*) FROM orders WHERE status = 'pending_confirmation' AND deleted_at IS NULL AND order_no IS NOT NULL").Scan(&pc)
	if r := adminCall(t, h, "GET", "/api/admin/summary", integAdmin, nil); num(r.Body["orders"].(map[string]any)["pendingConfirmation"]) != pc {
		t.Fatalf("summary pendingConfirmation: %v (db %d)", r.Body["orders"], pc)
	} else if _, ok := r.Body["orders"].(map[string]any)["pendingPayment"]; !ok {
		t.Fatal("field lama pendingPayment harus tetap ada")
	}
	if r := adminCall(t, h, "GET", "/api/admin/orders?status=pending_confirmation&q="+no, integAdmin, nil); r.Code != 200 || num(r.Body["total"]) != 1 {
		t.Fatalf("filter pending_confirmation: %d %v", r.Code, r.Body)
	}

	// Belum dikonfirmasi: /pricing dan status -> paid / pending_payment ditolak.
	if r := adminCall(t, h, "PATCH", base+"/pricing", integAdmin, map[string]any{"discount": 0, "shippingFee": 1000}); r.Code != 409 {
		t.Fatalf("pricing sebelum konfirmasi harus 409: %d", r.Code)
	}
	for _, to := range []string{"paid", "pending_payment"} {
		if r := adminCall(t, h, "PATCH", base+"/status", integAdmin, map[string]any{"from": "pending_confirmation", "to": to}); r.Code != 400 {
			t.Fatalf("status %s lewat endpoint status harus 400: %d %v", to, r.Code, r.Body)
		}
	}

	// Validasi confirm (tidak mengubah status).
	var sub int64
	db.Raw("SELECT subtotal FROM orders WHERE id = ?", id).Scan(&sub)
	for _, b := range []map[string]any{
		{"discount": sub + 1, "shippingFee": 0},
		{"discount": 0, "shippingFee": 10_000_001},
		{"discount": -1, "shippingFee": 0},
		{"discount": 0},
		{"discount": 0, "shippingFee": 0, "discountNote": strings.Repeat("x", 256)},
	} {
		if r := adminCall(t, h, "POST", base+"/confirm", integAdmin, b); r.Code != 400 {
			t.Fatalf("confirm tidak sah %v harus 400: %d", b, r.Code)
		}
	}
	// Non-admin / tanpa token / tanpa CSRF.
	if r := call(t, h, "POST", base+"/confirm", tok, map[string]any{"discount": 0, "shippingFee": 0}); r.Code != 401 {
		t.Fatalf("pelanggan -> confirm: %d", r.Code)
	}
	if r := adminCall(t, h, "POST", base+"/confirm", "bukan.admin@uji.test", map[string]any{"discount": 0, "shippingFee": 0}); r.Code != 403 {
		t.Fatalf("bukan admin -> confirm: %d", r.Code)
	}

	// Sukses: ongkir 0 boleh.
	r = confirmCall(t, h, id, 1000, 0)
	if r.Body["status"] != orders.StatusPending || num(r.Body["shippingFee"]) != 0 || num(r.Body["total"]) != sub-1000 || r.Body["canConfirm"] != false ||
		fmt.Sprint(r.Body["allowedNext"]) != "[paid cancelled]" {
		t.Fatalf("hasil konfirmasi: %v", r.Body)
	}
	var hist []struct {
		From  *string `gorm:"column:from_status"`
		To    string  `gorm:"column:to_status"`
		Actor *uint64 `gorm:"column:actor_user_id"`
	}
	db.Raw("SELECT from_status, to_status, actor_user_id FROM order_status_history WHERE order_id = ? ORDER BY id", id).Scan(&hist)
	if len(hist) != 2 || hist[0].From != nil || hist[0].To != orders.StatusPendingConfirmation || hist[1].From == nil ||
		*hist[1].From != orders.StatusPendingConfirmation || hist[1].To != orders.StatusPending || hist[1].Actor == nil || *hist[1].Actor == uid {
		t.Fatalf("riwayat: %+v", hist)
	}
	l := lastLog(t, db, "order.confirm")
	if l == nil || !strings.Contains(det(l), `"orderNo":"`+no+`"`) || !strings.Contains(det(l), `"shippingFee":0`) {
		t.Fatalf("log order.confirm: %v", det(l))
	}
	rs.mu.Lock()
	if len(rs.cust) != 1 || rs.cust[0].Kind != push.KindCustomerPricing || rs.cust[0].ShippingFee != 0 || rs.cust[0].Total != sub-1000 || rs.uids[0] != uid {
		t.Fatalf("push pelanggan saat konfirmasi (walau ongkir 0): %+v", rs.cust)
	}
	rs.mu.Unlock()

	// Klik ganda / sudah dikonfirmasi -> 409; tidak ada riwayat/push tambahan.
	if r := adminCall(t, h, "POST", base+"/confirm", integAdmin, map[string]any{"discount": 0, "shippingFee": 5000}); r.Code != 409 {
		t.Fatalf("konfirmasi kedua harus 409: %d", r.Code)
	}
	rs.mu.Lock()
	if len(rs.cust) != 1 {
		t.Fatal("push tidak boleh terkirim untuk konfirmasi yang ditolak")
	}
	rs.mu.Unlock()
	// Setelah konfirmasi: /pricing boleh, lalu dibayar.
	if r := adminCall(t, h, "PATCH", base+"/pricing", integAdmin, map[string]any{"discount": 0, "shippingFee": 9000}); r.Code != 200 {
		t.Fatalf("ubah ongkir setelah konfirmasi: %d", r.Code)
	}
	adminSetStatus(t, h, id, orders.StatusPending, orders.StatusPaid)
	if r := adminCall(t, h, "POST", base+"/confirm", integAdmin, map[string]any{"discount": 0, "shippingFee": 0}); r.Code != 409 {
		t.Fatalf("konfirmasi pesanan dibayar harus 409: %d", r.Code)
	}
}

// Dua admin menekan konfirmasi bersamaan: tepat satu sukses.
func TestIntegrationConfirmConcurrent(t *testing.T) {
	_, h, db, _ := setupOrders(t)
	tok := newCustomer(t, h, "Pembeli Paralel")
	no := createOrderFor(t, h, tok, 5)
	id := orderIDByNo(db, no)
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = adminCall(t, h, "POST", fmt.Sprintf("/api/admin/orders/%d/confirm", id), integAdmin, map[string]any{"discount": 0, "shippingFee": int64(1000 * (i + 1))}).Code
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == 200 {
			ok++
		} else if c != 409 {
			t.Fatalf("kode tak terduga: %v", codes)
		}
	}
	var n int64
	db.Raw("SELECT COUNT(*) FROM order_status_history WHERE order_id = ? AND to_status = 'pending_payment'", id).Scan(&n)
	if ok != 1 || n != 1 {
		t.Fatalf("tepat satu konfirmasi: codes=%v riwayat=%d", codes, n)
	}
}

// Pelanggan membatalkan sendiri dari kedua status; riwayat from_status mengikuti status asal.
func TestIntegrationOwnerCancelBothStatuses(t *testing.T) {
	_, h, db, _ := setupOrders(t)
	tok := newCustomer(t, h, "Pembeli Batal")
	a := createOrderFor(t, h, tok, 4)
	b := createOrderFor(t, h, tok, 4)
	confirmCall(t, h, orderIDByNo(db, b), 0, 10000)
	for no, from := range map[string]string{a: orders.StatusPendingConfirmation, b: orders.StatusPending} {
		r := call(t, h, "POST", "/api/orders/"+no+"/cancel", tok, map[string]any{"reason": "uji"})
		if r.Code != 200 || r.Body["status"] != orders.StatusCancelled || r.Body["canCancel"] != false {
			t.Fatalf("batal %s: %d %v", from, r.Code, r.Body)
		}
		var hf string
		db.Raw("SELECT from_status FROM order_status_history WHERE order_id = ? AND to_status = 'cancelled'", orderIDByNo(db, no)).Scan(&hf)
		if hf != from {
			t.Fatalf("riwayat from_status: %s mau %s", hf, from)
		}
	}
	// Dibayar: pelanggan tidak bisa membatalkan.
	c := createOrderFor(t, h, tok, 4)
	confirmCall(t, h, orderIDByNo(db, c), 0, 0)
	adminSetStatus(t, h, orderIDByNo(db, c), orders.StatusPending, orders.StatusPaid)
	if r := call(t, h, "POST", "/api/orders/"+c+"/cancel", tok, nil); r.Code != 409 || r.Body["error"] != "Pesanan tidak bisa dibatalkan karena statusnya sudah dibayar" {
		t.Fatalf("batal dibayar: %d %v", r.Code, r.Body)
	}
}
