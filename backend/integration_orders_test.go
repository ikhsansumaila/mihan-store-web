//go:build integration

// Tes integrasi fitur pesanan (keranjang, checkout, pesanan, admin, pengaturan toko)
// terhadap database UJI yang baru dibuat dari migrasi 001–009.
package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"mihanstore/notify"
)

type recNotifier struct {
	mu sync.Mutex
	ev []notify.OrderEvent
}

func (n *recNotifier) OrderEvent(e notify.OrderEvent) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ev = append(n.ev, e)
}

func (n *recNotifier) kinds() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var k []string
	for _, e := range n.ev {
		k = append(k, e.Kind+":"+e.OrderNo)
	}
	return strings.Join(k, ",")
}

func setupOrders(t *testing.T) (*App, http.Handler, *gorm.DB, *recNotifier) {
	app, _, db := setupAdminIntegration(t)
	rec := &recNotifier{}
	app.notifier = rec
	app.cartLimiter = NewRateLimiter(100000, time.Minute)
	app.checkoutLimiter = NewRateLimiter(100000, time.Minute)
	app.cancelLimiter = NewRateLimiter(100000, time.Minute)
	seedRegionsInteg(t, db)
	return app, newRouter(app), db, rec
}

var userSeq int

func newCustomer(t *testing.T, h http.Handler, name string) string {
	t.Helper()
	userSeq++
	u := fmt.Sprintf("pembeli_%d_%d", time.Now().UnixNano()%1000000, userSeq)
	r := call(t, h, "POST", "/api/auth/register", "", reg(u, u+"@uji.test", "081200000000", name, "passwordku123"))
	if r.Code != 201 {
		t.Fatalf("register %s: %d %v", u, r.Code, r.Body)
	}
	return r.Body["token"].(string)
}

// checkoutBody: wilayah Kota Tangerang dari pohon tiruan (seedRegionsInteg). "city" dari klien diabaikan server.
func checkoutBody(key string) map[string]any {
	return map[string]any{"recipientName": "Budi Penerima", "recipientPhone": "0813-1111-2222", "address": "Jl. Melati No. 9, RT 3",
		"city": "Tangerang", "postalCode": "15111", "note": "Titip di pos satpam", "idempotencyKey": key,
		"provinceCode": "36", "regencyCode": "36.71", "districtCode": "36.71.01", "villageCode": "36.71.01.1001"}
}

func uuid(t *testing.T) string {
	id, err := newPublicID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func num(v any) int64 {
	f, _ := v.(float64)
	return int64(f)
}

func TestIntegrationOrderSchemaAndGrants(t *testing.T) {
	_, _, db, _ := setupOrders(t)
	for _, tbl := range []string{"carts", "cart_items", "orders", "order_items", "order_status_history", "site_settings"} {
		var n int64
		if err := db.Raw("SELECT COUNT(*) FROM " + tbl).Scan(&n).Error; err != nil {
			t.Fatalf("SELECT %s harus boleh: %v", tbl, err)
		}
	}
	var keys []string
	db.Raw("SELECT setting_key FROM site_settings ORDER BY setting_key").Scan(&keys)
	if strings.Join(keys, ",") != "bank_account_holder,bank_account_number,bank_name,payment_note,store_whatsapp" {
		t.Fatalf("seed site_settings: %v", keys)
	}
	// Ditolak (1142): ubah/hapus item pesanan & riwayat, hapus pesanan/keranjang/pengaturan, DDL.
	for _, q := range []string{
		"UPDATE order_items SET unit_price = 1 WHERE id = 0",
		"DELETE FROM order_items WHERE id = 0",
		"UPDATE order_status_history SET note = 'x' WHERE id = 0",
		"DELETE FROM order_status_history WHERE id = 0",
		"DELETE FROM orders WHERE id = 0",
		"DELETE FROM carts WHERE id = 0",
		"DELETE FROM site_settings WHERE setting_key = 'x'",
		"TRUNCATE TABLE order_items",
		"ALTER TABLE orders ADD COLUMN x INT",
		"DROP TABLE cart_items",
	} {
		err := db.Exec(q).Error
		if err == nil || (mysqlErrNo(err) != 1142 && mysqlErrNo(err) != 1370) {
			t.Errorf("harus ditolak (1142): %s -> %v", q, err)
		}
	}
	// Boleh: DELETE pada cart_items.
	if err := db.Exec("DELETE FROM cart_items WHERE id = 0").Error; err != nil {
		t.Errorf("DELETE cart_items harus boleh: %v", err)
	}
}

func TestIntegrationCartCheckoutFlow(t *testing.T) {
	_, h, db, rec := setupOrders(t)
	tokA := newCustomer(t, h, "Pelanggan *A* @everyone")

	// Tanpa login -> 401.
	if r := call(t, h, "GET", "/api/cart", "", nil); r.Code != 401 {
		t.Fatalf("keranjang tanpa login: %d", r.Code)
	}
	// Keranjang kosong.
	r := call(t, h, "GET", "/api/cart", tokA, nil)
	if r.Code != 200 || len(r.Body["items"].([]any)) != 0 || num(r.Body["subtotal"]) != 0 {
		t.Fatalf("keranjang awal: %d %v", r.Code, r.Body)
	}
	// Tambah: harga dari browser diabaikan (field tak dikenal), qty dijumlahkan.
	r = call(t, h, "POST", "/api/cart/items", tokA, map[string]any{"productId": 1, "qty": 2, "price": 1})
	if r.Code != 200 || num(r.Body["subtotal"]) != 2*45000 {
		t.Fatalf("tambah: %d %v", r.Code, r.Body)
	}
	call(t, h, "POST", "/api/cart/items", tokA, map[string]any{"productId": 1})
	r = call(t, h, "POST", "/api/cart/items", tokA, map[string]any{"productId": 2, "qty": 1})
	if num(r.Body["subtotal"]) != 3*45000+55000 || num(r.Body["itemCount"]) != 4 || num(r.Body["lineCount"]) != 2 {
		t.Fatalf("keranjang: %v", r.Body)
	}
	// Validasi qty & produk.
	for _, b := range []map[string]any{{"productId": 1, "qty": 0}, {"productId": 1, "qty": 1000}, {"productId": 0}, {"productId": 1, "qty": -1}} {
		if r := call(t, h, "POST", "/api/cart/items", tokA, b); r.Code != 400 {
			t.Errorf("tambah %v harus 400: %d", b, r.Code)
		}
	}
	if r := call(t, h, "POST", "/api/cart/items", tokA, map[string]any{"productId": 99999}); r.Code != 404 {
		t.Errorf("produk tidak ada harus 404: %d", r.Code)
	}
	// Maksimal 999 per produk (dibatasi).
	call(t, h, "PUT", "/api/cart/items", tokA, map[string]any{"productId": 3, "qty": 999})
	r = call(t, h, "POST", "/api/cart/items", tokA, map[string]any{"productId": 3, "qty": 5})
	for _, it := range r.Body["items"].([]any) {
		m := it.(map[string]any)
		if num(m["productId"]) == 3 && num(m["qty"]) != 999 {
			t.Fatalf("qty harus dibatasi 999: %v", m)
		}
	}
	// PUT qty 0 menghapus; DELETE item.
	call(t, h, "PUT", "/api/cart/items", tokA, map[string]any{"productId": 3, "qty": 0})
	r = call(t, h, "PUT", "/api/cart/items", tokA, map[string]any{"productId": 1, "qty": 2})
	if num(r.Body["subtotal"]) != 2*45000+55000 || num(r.Body["lineCount"]) != 2 {
		t.Fatalf("PUT: %v", r.Body)
	}

	// Produk dinonaktifkan admin -> ditandai, checkout ditolak (rollback, tidak ada yang ditulis).
	if r := adminCall(t, h, "PATCH", "/api/admin/products/2/active", integAdmin, map[string]any{"active": false}); r.Code != 200 {
		t.Fatalf("nonaktifkan: %d", r.Code)
	}
	r = call(t, h, "GET", "/api/cart", tokA, nil)
	if r.Body["hasUnavailable"] != true || num(r.Body["subtotal"]) != 2*45000 {
		t.Fatalf("produk nonaktif harus ditandai: %v", r.Body)
	}
	var ordersBefore, histBefore, logsBefore int64
	db.Raw("SELECT COUNT(*) FROM orders").Scan(&ordersBefore)
	db.Raw("SELECT COUNT(*) FROM order_status_history").Scan(&histBefore)
	logsBefore = countLogs(db, "action = 'order.create'")
	key1 := uuid(t)
	if r := call(t, h, "POST", "/api/orders", tokA, checkoutBody(key1)); r.Code != 409 {
		t.Fatalf("checkout dengan produk nonaktif harus 409: %d %v", r.Code, r.Body)
	}
	var ordersAfter, histAfter, itemsLeft int64
	db.Raw("SELECT COUNT(*) FROM orders").Scan(&ordersAfter)
	db.Raw("SELECT COUNT(*) FROM order_status_history").Scan(&histAfter)
	if ordersAfter != ordersBefore || histAfter != histBefore || countLogs(db, "action = 'order.create'") != logsBefore {
		t.Fatal("checkout gagal tidak boleh menulis apa pun")
	}
	if r := call(t, h, "GET", "/api/cart", tokA, nil); num(r.Body["lineCount"]) != 2 {
		t.Fatal("keranjang tidak boleh berubah saat checkout gagal")
	}
	adminCall(t, h, "PATCH", "/api/admin/products/2/active", integAdmin, map[string]any{"active": true})

	// Validasi input checkout.
	bad := checkoutBody(uuid(t))
	bad["recipientPhone"] = "123"
	if r := call(t, h, "POST", "/api/orders", tokA, bad); r.Code != 422 || r.Body["field"] != "recipientPhone" {
		t.Fatalf("telepon salah harus 422 (field recipientPhone): %d %v", r.Code, r.Body)
	}

	// Checkout sukses: harga/total dari browser diabaikan.
	body := checkoutBody(key1)
	body["total"], body["items"], body["subtotal"] = 1, []map[string]any{{"productId": 1, "qty": 1, "price": 1}}, 1
	r = call(t, h, "POST", "/api/orders", tokA, body)
	if r.Code != 201 {
		t.Fatalf("checkout: %d %v", r.Code, r.Body)
	}
	orderNo := r.Body["orderNo"].(string)
	if !validOrderNo(orderNo) || r.Body["status"] != StatusPending || num(r.Body["subtotal"]) != 2*45000+55000 ||
		num(r.Body["total"]) != 2*45000+55000 || num(r.Body["itemCount"]) != 3 || len(r.Body["items"].([]any)) != 2 {
		t.Fatalf("pesanan: %v", r.Body)
	}
	rcp := r.Body["recipient"].(map[string]any)
	if rcp["phone"] != "+6281311112222" || rcp["postalCode"] != "15111" {
		t.Fatalf("penerima: %v", rcp)
	}
	db.Raw("SELECT COUNT(*) FROM cart_items ci JOIN carts c ON c.id = ci.cart_id JOIN users u ON u.id = c.user_id WHERE u.name LIKE 'Pelanggan%A%'").Scan(&itemsLeft)
	if itemsLeft != 0 {
		t.Fatal("keranjang harus kosong setelah checkout")
	}
	var o orderRow
	db.Raw("SELECT "+orderCols+" FROM orders o WHERE order_no = ?", orderNo).Scan(&o)
	var nHist int64
	db.Raw("SELECT COUNT(*) FROM order_status_history WHERE order_id = ? AND to_status = 'pending_payment' AND from_status IS NULL", o.ID).Scan(&nHist)
	if nHist != 1 {
		t.Fatal("riwayat status awal harus tertulis")
	}
	l := lastLog(t, db, "order.create")
	if l == nil || *l.EntityID != orderNo || strings.Contains(det(l), "Melati") || strings.Contains(det(l), "8131111") || strings.Contains(det(l), "satpam") {
		t.Fatalf("log order.create (tanpa alamat/telepon/catatan): %v", det(l))
	}
	if !strings.Contains(rec.kinds(), "created:"+orderNo) {
		t.Fatalf("notifikasi created: %s", rec.kinds())
	}
	ev := rec.ev[len(rec.ev)-1]
	if ev.ItemCount != 3 || ev.Total != uint64(2*45000+55000) || ev.CustomerName != "Pelanggan *A* @everyone" ||
		!strings.HasSuffix(ev.AdminURL, fmt.Sprintf("/admin/orders/%d", o.ID)) {
		t.Fatalf("event: %+v", ev)
	}

	// Idempotensi: kunci sama -> pesanan sama (200), tidak ganda.
	call(t, h, "POST", "/api/cart/items", tokA, map[string]any{"productId": 5, "qty": 1})
	r = call(t, h, "POST", "/api/orders", tokA, checkoutBody(key1))
	if r.Code != 200 || r.Body["orderNo"] != orderNo {
		t.Fatalf("idempotensi: %d %v", r.Code, r.Body)
	}
	var nOrders int64
	db.Raw("SELECT COUNT(*) FROM orders WHERE idempotency_key = ?", key1).Scan(&nOrders)
	if nOrders != 1 {
		t.Fatalf("pesanan ganda: %d", nOrders)
	}
	if r := call(t, h, "GET", "/api/cart", tokA, nil); num(r.Body["lineCount"]) != 1 {
		t.Fatal("replay idempotensi tidak boleh mengosongkan keranjang baru")
	}
	// Dua klik bersamaan dengan kunci baru -> tepat satu pesanan.
	key2 := uuid(t)
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = call(t, h, "POST", "/api/orders", tokA, checkoutBody(key2)).Code
		}(i)
	}
	wg.Wait()
	db.Raw("SELECT COUNT(*) FROM orders WHERE idempotency_key = ?", key2).Scan(&nOrders)
	created := 0
	for _, c := range codes {
		if c == 201 {
			created++
		} else if c != 200 {
			t.Errorf("kode tak terduga untuk klik ganda: %v", codes)
		}
	}
	if nOrders != 1 || created != 1 {
		t.Fatalf("klik ganda bersamaan: %d pesanan, kode %v", nOrders, codes)
	}
	// Keranjang kosong -> 400.
	if r := call(t, h, "POST", "/api/orders", tokA, checkoutBody(uuid(t))); r.Code != 400 {
		t.Fatalf("keranjang kosong harus 400: %d", r.Code)
	}

	// Snapshot harga: ubah harga produk 1, item pesanan tetap.
	if r := adminCall(t, h, "PUT", "/api/admin/products/1", integAdmin, map[string]any{"name": "Kerupuk Finna Udang", "description": "x", "price": 99000, "categoryId": 1}); r.Code != 200 {
		t.Fatalf("ubah harga: %d %v", r.Code, r.Body)
	}
	r = call(t, h, "GET", "/api/orders/"+orderNo, tokA, nil)
	if r.Code != 200 || num(r.Body["total"]) != 2*45000+55000 || num(r.Body["items"].([]any)[0].(map[string]any)["unitPrice"]) != 45000 {
		t.Fatalf("snapshot harga berubah: %v", r.Body)
	}
	adminCall(t, h, "PUT", "/api/admin/products/1", integAdmin, map[string]any{"name": "Kerupuk Finna Udang", "description": "Kerupuk udang asli dengan rasa gurih", "price": 45000, "categoryId": 1, "imagePath": "kerupuk1.jpg"})

	// Daftar pesanan sendiri.
	r = call(t, h, "GET", "/api/orders", tokA, nil)
	if r.Code != 200 || num(r.Body["total"]) != 2 {
		t.Fatalf("daftar pesanan: %v", r.Body)
	}
	// Info toko (login): placeholder -> null.
	r = call(t, h, "GET", "/api/store-info", tokA, nil)
	if r.Code != 200 || r.Body["storeWhatsapp"] != nil || r.Body["paymentConfigured"] != false {
		t.Fatalf("store-info awal: %v", r.Body)
	}
}

func createOrderFor(t *testing.T, h http.Handler, tok string, productID int) string {
	t.Helper()
	if r := call(t, h, "POST", "/api/cart/items", tok, map[string]any{"productId": productID, "qty": 2}); r.Code != 200 {
		t.Fatalf("tambah keranjang: %d %v", r.Code, r.Body)
	}
	r := call(t, h, "POST", "/api/orders", tok, checkoutBody(uuid(t)))
	if r.Code != 201 {
		t.Fatalf("checkout: %d %v", r.Code, r.Body)
	}
	return r.Body["orderNo"].(string)
}

func TestIntegrationOrderIDORAndCancel(t *testing.T) {
	_, h, db, rec := setupOrders(t)
	tokA := newCustomer(t, h, "Ani")
	tokB := newCustomer(t, h, "Bayu")
	orderA := createOrderFor(t, h, tokA, 4)

	missing := call(t, h, "GET", "/api/orders/MS-200101-9999", tokB, nil)
	other := call(t, h, "GET", "/api/orders/"+orderA, tokB, nil)
	if other.Code != 404 || missing.Code != 404 || fmt.Sprint(other.Body) != fmt.Sprint(missing.Body) {
		t.Fatalf("IDOR: pesanan orang lain harus 404 identik: %d %v / %d %v", other.Code, other.Body, missing.Code, missing.Body)
	}
	if r := call(t, h, "POST", "/api/orders/"+orderA+"/cancel", tokB, map[string]any{}); r.Code != 404 {
		t.Fatalf("IDOR batal: %d", r.Code)
	}
	if r := call(t, h, "GET", "/api/orders", tokB, nil); num(r.Body["total"]) != 0 {
		t.Fatalf("daftar B tidak boleh memuat pesanan A: %v", r.Body)
	}
	if r := call(t, h, "GET", "/api/orders/'%20OR%201=1", tokB, nil); r.Code != 404 {
		t.Fatalf("nomor aneh: %d", r.Code)
	}
	var st string
	db.Raw("SELECT status FROM orders WHERE order_no = ?", orderA).Scan(&st)
	if st != StatusPending {
		t.Fatal("pesanan A tidak boleh berubah")
	}
	// Pemilik membatalkan.
	r := call(t, h, "POST", "/api/orders/"+orderA+"/cancel", tokA, map[string]any{"reason": "Salah pilih"})
	if r.Code != 200 || r.Body["status"] != StatusCancelled || r.Body["canCancel"] != false {
		t.Fatalf("batal pemilik: %d %v", r.Code, r.Body)
	}
	if r := call(t, h, "POST", "/api/orders/"+orderA+"/cancel", tokA, nil); r.Code != 409 {
		t.Fatalf("batal kedua harus 409: %d", r.Code)
	}
	l := lastLog(t, db, "order.cancel")
	if l == nil || !strings.Contains(det(l), `"pelanggan"`) {
		t.Fatalf("log batal: %v", det(l))
	}
	if !strings.Contains(rec.kinds(), "cancelled:"+orderA) {
		t.Fatalf("notifikasi batal: %s", rec.kinds())
	}
	// Admin tidak bisa menandai dibayar pesanan yang sudah dibatalkan.
	var id uint64
	db.Raw("SELECT id FROM orders WHERE order_no = ?", orderA).Scan(&id)
	if r := adminCall(t, h, "PATCH", fmt.Sprintf("/api/admin/orders/%d/status", id), integAdmin, map[string]any{"from": "pending_payment", "to": "paid"}); r.Code != 409 {
		t.Fatalf("bayar pesanan batal harus 409: %d %v", r.Code, r.Body)
	}
	if r := adminCall(t, h, "PATCH", fmt.Sprintf("/api/admin/orders/%d/status", id), integAdmin, map[string]any{"from": "cancelled", "to": "paid"}); r.Code != 409 {
		t.Fatalf("status final harus 409: %d %v", r.Code, r.Body)
	}
}

func TestIntegrationAdminOrders(t *testing.T) {
	_, h, db, rec := setupOrders(t)
	tok := newCustomer(t, h, "Citra")
	orderNo := createOrderFor(t, h, tok, 6) // 2 x harga produk 6
	var o orderRow
	db.Raw("SELECT "+orderCols+" FROM orders o WHERE order_no = ?", orderNo).Scan(&o)
	base := fmt.Sprintf("/api/admin/orders/%d", o.ID)

	// Pelanggan biasa tidak bisa memakai API admin.
	if r := call(t, h, "GET", "/api/admin/orders", tok, nil); r.Code != 401 {
		t.Fatalf("pelanggan -> admin: %d", r.Code)
	}
	// Daftar + filter + cari.
	r := adminCall(t, h, "GET", "/api/admin/orders?status=pending_payment&q="+orderNo, integAdmin, nil)
	if r.Code != 200 || num(r.Body["total"]) != 1 {
		t.Fatalf("daftar admin: %d %v", r.Code, r.Body)
	}
	if r := adminCall(t, h, "GET", "/api/admin/orders?q=0813-1111-2222", integAdmin, nil); num(r.Body["total"]) < 1 {
		t.Fatalf("cari telepon: %v", r.Body)
	}
	if r := adminCall(t, h, "GET", "/api/admin/orders?status=paid&q="+orderNo, integAdmin, nil); num(r.Body["total"]) != 0 {
		t.Fatal("filter status")
	}
	today := time.Now().In(wib).Format("2006-01-02")
	if r := adminCall(t, h, "GET", "/api/admin/orders?from="+today+"&to="+today+"&q="+orderNo, integAdmin, nil); num(r.Body["total"]) != 1 {
		t.Fatalf("filter tanggal: %v", r.Body)
	}
	if r := adminCall(t, h, "GET", "/api/admin/orders?status=shipped", integAdmin, nil); r.Code != 400 {
		t.Fatal("status tidak dikenal harus 400")
	}
	// Detail.
	r = adminCall(t, h, "GET", base, integAdmin, nil)
	if r.Code != 200 || r.Body["recipient"].(map[string]any)["address"] == nil || len(r.Body["history"].([]any)) != 1 ||
		r.Body["customer"].(map[string]any)["name"] != "Citra" || r.Body["pricingLocked"] != false {
		t.Fatalf("detail admin: %v", r.Body)
	}
	if r := adminCall(t, h, "GET", "/api/admin/orders/999999", integAdmin, nil); r.Code != 404 {
		t.Fatal("pesanan tidak ada harus 404")
	}

	// Diskon & ongkir.
	sub := o.Subtotal
	if r := adminCall(t, h, "PATCH", base+"/pricing", integAdmin, map[string]any{"discount": sub + 1, "shippingFee": 0}); r.Code != 400 {
		t.Fatalf("diskon > subtotal harus 400: %d", r.Code)
	}
	if r := adminCall(t, h, "PATCH", base+"/pricing", integAdmin, map[string]any{"discount": 0, "shippingFee": 10_000_001}); r.Code != 400 {
		t.Fatalf("ongkir > 10jt harus 400: %d", r.Code)
	}
	if r := adminCall(t, h, "PATCH", base+"/pricing", integAdmin, map[string]any{"discount": -5, "shippingFee": 0}); r.Code != 400 {
		t.Fatalf("diskon negatif harus 400: %d", r.Code)
	}
	r = adminCall(t, h, "PATCH", base+"/pricing", integAdmin, map[string]any{"discount": 5000, "discountNote": "Pelanggan setia", "shippingFee": 12000})
	if r.Code != 200 || num(r.Body["total"]) != sub-5000+12000 || num(r.Body["discount"]) != 5000 || r.Body["discountNote"] != "Pelanggan setia" {
		t.Fatalf("pricing: %d %v", r.Code, r.Body)
	}
	l := lastLog(t, db, "order.pricing_update")
	if l == nil || !strings.Contains(det(l), `"discount":{"dari":0,"menjadi":5000}`) || !strings.Contains(det(l), `"shippingFee":{"dari":0,"menjadi":12000}`) {
		t.Fatalf("log pricing: %v", det(l))
	}
	// Pelanggan melihat total baru.
	if r := call(t, h, "GET", "/api/orders/"+orderNo, tok, nil); num(r.Body["total"]) != sub+7000 || r.Body["adminNote"] != nil {
		t.Fatalf("pelanggan lihat total: %v", r.Body)
	}

	// Catatan admin (isi tidak masuk log).
	r = adminCall(t, h, "PATCH", base+"/note", integAdmin, map[string]any{"adminNote": "Kirim pakai JNE rahasia-internal"})
	if r.Code != 200 || r.Body["adminNote"] != "Kirim pakai JNE rahasia-internal" {
		t.Fatalf("catatan admin: %v", r.Body)
	}
	if l := lastLog(t, db, "order.note_update"); l == nil || strings.Contains(det(l), "rahasia-internal") {
		t.Fatalf("log catatan: %v", det(l))
	}
	if r := call(t, h, "GET", "/api/orders/"+orderNo, tok, nil); strings.Contains(fmt.Sprint(r.Body), "rahasia-internal") {
		t.Fatal("catatan admin tidak boleh terlihat pelanggan")
	}

	// Perpindahan tidak sah.
	if r := adminCall(t, h, "PATCH", base+"/status", integAdmin, map[string]any{"from": "pending_payment", "to": "completed"}); r.Code != 400 {
		t.Fatalf("pending->completed harus 400: %d", r.Code)
	}
	if r := adminCall(t, h, "PATCH", base+"/status", integAdmin, map[string]any{"to": "paid"}); r.Code != 400 {
		t.Fatalf("tanpa from harus 400: %d", r.Code)
	}
	// Tandai dibayar.
	r = adminCall(t, h, "PATCH", base+"/status", integAdmin, map[string]any{"from": "pending_payment", "to": "paid", "paymentNote": "BCA 2 Okt"})
	if r.Code != 200 || r.Body["status"] != StatusPaid || r.Body["paidAt"] == nil || r.Body["paidBy"] != integAdmin || r.Body["pricingLocked"] != true {
		t.Fatalf("tandai dibayar: %d %v", r.Code, r.Body)
	}
	// Admin kedua dengan tampilan lama -> 409 (tidak menimpa).
	if r := adminCall(t, h, "PATCH", base+"/status", "promosi@uji.test", map[string]any{"from": "pending_payment", "to": "cancelled", "reason": "x"}); r.Code != 409 {
		t.Fatalf("konflik dua admin harus 409: %d %v", r.Code, r.Body)
	}
	if r := adminCall(t, h, "PATCH", base+"/status", "promosi@uji.test", map[string]any{"from": "pending_payment", "to": "paid"}); r.Code != 409 {
		t.Fatalf("bayar ganda harus 409: %d", r.Code)
	}
	// Diskon terkunci setelah dibayar.
	if r := adminCall(t, h, "PATCH", base+"/pricing", integAdmin, map[string]any{"discount": 0, "shippingFee": 0}); r.Code != 409 {
		t.Fatalf("diskon setelah dibayar harus 409: %d", r.Code)
	}
	// paid -> cancelled wajib alasan.
	if r := adminCall(t, h, "PATCH", base+"/status", integAdmin, map[string]any{"from": "paid", "to": "cancelled"}); r.Code != 400 {
		t.Fatalf("batal dari paid tanpa alasan harus 400: %d", r.Code)
	}
	// Selesai.
	r = adminCall(t, h, "PATCH", base+"/status", integAdmin, map[string]any{"from": "paid", "to": "completed"})
	if r.Code != 200 || r.Body["status"] != StatusCompleted || len(r.Body["history"].([]any)) != 3 {
		t.Fatalf("selesai: %d %v", r.Code, r.Body)
	}
	if r := adminCall(t, h, "PATCH", base+"/status", integAdmin, map[string]any{"from": "completed", "to": "cancelled", "reason": "x"}); r.Code != 409 {
		t.Fatalf("completed final: %d", r.Code)
	}
	for _, a := range []string{"order.pay", "order.complete"} {
		if l := lastLog(t, db, a); l == nil || *l.EntityID != orderNo {
			t.Errorf("log %s tidak tertulis", a)
		}
	}
	var nHist int64
	db.Raw("SELECT COUNT(*) FROM order_status_history WHERE order_id = ?", o.ID).Scan(&nHist)
	if nHist != 3 {
		t.Fatalf("riwayat status: %d", nHist)
	}
	if !strings.Contains(rec.kinds(), "paid:"+orderNo) || strings.Contains(rec.kinds(), "completed:") {
		t.Fatalf("notifikasi: %s", rec.kinds())
	}
	// Pembatalan admin dari paid dengan alasan (pesanan lain).
	order2 := createOrderFor(t, h, tok, 7)
	var id2 uint64
	db.Raw("SELECT id FROM orders WHERE order_no = ?", order2).Scan(&id2)
	b2 := fmt.Sprintf("/api/admin/orders/%d", id2)
	adminCall(t, h, "PATCH", b2+"/status", integAdmin, map[string]any{"from": "pending_payment", "to": "paid"})
	r = adminCall(t, h, "PATCH", b2+"/status", integAdmin, map[string]any{"from": "paid", "to": "cancelled", "reason": "Stok habis, dana dikembalikan"})
	if r.Code != 200 || r.Body["cancelReason"] != "Stok habis, dana dikembalikan" || r.Body["cancelledBy"] != integAdmin {
		t.Fatalf("batal admin dari paid: %d %v", r.Code, r.Body)
	}
	if r := call(t, h, "GET", "/api/orders/"+order2, tok, nil); r.Body["cancelReason"] != "Stok habis, dana dikembalikan" {
		t.Fatalf("pelanggan melihat alasan: %v", r.Body)
	}

	// Ringkasan.
	r = adminCall(t, h, "GET", "/api/admin/summary", integAdmin, nil)
	oc := r.Body["orders"].(map[string]any)
	if r.Code != 200 || num(oc["last7Days"]) < 2 {
		t.Fatalf("ringkasan: %v", r.Body["orders"])
	}
}

func TestIntegrationSettings(t *testing.T) {
	_, h, db, _ := setupOrders(t)
	tok := newCustomer(t, h, "Dewi")
	r := adminCall(t, h, "GET", "/api/admin/settings", integAdmin, nil)
	s := r.Body["settings"].(map[string]any)
	if r.Code != 200 || s["bank_name"] != "" || s["store_whatsapp"] != "" {
		t.Fatalf("settings awal (placeholder = kosong): %v", r.Body)
	}
	if r := adminCall(t, h, "PUT", "/api/admin/settings", integAdmin, map[string]any{"store_whatsapp": "12345"}); r.Code != 400 {
		t.Fatalf("WA salah harus 400: %d", r.Code)
	}
	if r := adminCall(t, h, "PUT", "/api/admin/settings", integAdmin, map[string]any{"kunci_lain": "x"}); r.Code != 400 {
		t.Fatalf("kunci tak dikenal harus 400: %d", r.Code)
	}
	r = adminCall(t, h, "PUT", "/api/admin/settings", integAdmin, map[string]any{"store_whatsapp": "0812-9999-8888", "bank_name": "BCA",
		"bank_account_number": "1234567890", "bank_account_holder": "Toko Uji", "payment_note": "Transfer sesuai total"})
	if r.Code != 200 || r.Body["settings"].(map[string]any)["store_whatsapp"] != "+6281299998888" {
		t.Fatalf("simpan settings: %d %v", r.Code, r.Body)
	}
	l := lastLog(t, db, "settings.update")
	if l == nil || strings.Contains(det(l), "1234567890") || strings.Contains(det(l), "Toko Uji") || !strings.Contains(det(l), `"bank_account_number":"diubah"`) {
		t.Fatalf("log settings (tanpa nilai): %v", det(l))
	}
	n := countLogs(db, "action = 'settings.update'")
	adminCall(t, h, "PUT", "/api/admin/settings", integAdmin, map[string]any{"bank_name": "BCA"})
	if countLogs(db, "action = 'settings.update'") != n {
		t.Fatal("tanpa perubahan tidak boleh dicatat")
	}
	r = call(t, h, "GET", "/api/store-info", tok, nil)
	if r.Body["storeWhatsapp"] != "+6281299998888" || r.Body["bankAccountNumber"] != "1234567890" || r.Body["paymentConfigured"] != true {
		t.Fatalf("store-info: %v", r.Body)
	}
	if r := call(t, h, "GET", "/api/store-info", "", nil); r.Code != 401 {
		t.Fatalf("store-info tanpa login: %d", r.Code)
	}
	if r := call(t, h, "PUT", "/api/admin/settings", tok, map[string]any{"bank_name": "X"}); r.Code != 401 && r.Code != 405 {
		t.Fatalf("pelanggan ubah settings: %d", r.Code)
	}
	// Kembalikan ke kosong.
	adminCall(t, h, "PUT", "/api/admin/settings", integAdmin, map[string]any{"store_whatsapp": "", "bank_name": "", "bank_account_number": "", "bank_account_holder": "", "payment_note": ""})
	if r := call(t, h, "GET", "/api/store-info", tok, nil); r.Body["storeWhatsapp"] != nil {
		t.Fatalf("WA kosong -> null: %v", r.Body)
	}
}

// Bila penulisan activity log gagal, perubahan pesanan + riwayat status ikut dibatalkan
// (semua dalam SATU transaksi).
func TestIntegrationOrderTxAtomicWithLog(t *testing.T) {
	_, h, db, rec := setupOrders(t)
	tok := newCustomer(t, h, "Eko")
	orderNo := createOrderFor(t, h, tok, 8)
	var id uint64
	db.Raw("SELECT id FROM orders WHERE order_no = ?", orderNo).Scan(&id)

	var fail atomic.Bool
	if err := db.Callback().Create().Before("gorm:create").Register("uji_gagal_log", func(tx *gorm.DB) {
		if fail.Load() && tx.Statement.Table == "activity_logs" {
			tx.AddError(errors.New("uji: log gagal"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	before := len(rec.kinds())
	if r := adminCall(t, h, "PATCH", fmt.Sprintf("/api/admin/orders/%d/status", id), integAdmin, map[string]any{"from": "pending_payment", "to": "paid"}); r.Code != 500 {
		t.Fatalf("log gagal harus 500: %d", r.Code)
	}
	var st string
	var nHist int64
	db.Raw("SELECT status FROM orders WHERE id = ?", id).Scan(&st)
	db.Raw("SELECT COUNT(*) FROM order_status_history WHERE order_id = ?", id).Scan(&nHist)
	if st != StatusPending || nHist != 1 {
		t.Fatalf("status/riwayat harus di-rollback: %s %d", st, nHist)
	}
	if r := adminCall(t, h, "PATCH", fmt.Sprintf("/api/admin/orders/%d/pricing", id), integAdmin, map[string]any{"discount": 1000, "shippingFee": 0}); r.Code != 500 {
		t.Fatalf("pricing log gagal harus 500: %d", r.Code)
	}
	var disc int64
	db.Raw("SELECT discount FROM orders WHERE id = ?", id).Scan(&disc)
	if disc != 0 {
		t.Fatal("diskon harus di-rollback")
	}
	// Checkout dengan log gagal: tidak ada pesanan, keranjang utuh.
	call(t, h, "POST", "/api/cart/items", tok, map[string]any{"productId": 9, "qty": 1})
	var nOrders int64
	db.Raw("SELECT COUNT(*) FROM orders").Scan(&nOrders)
	if r := call(t, h, "POST", "/api/orders", tok, checkoutBody(uuid(t))); r.Code != 500 {
		t.Fatalf("checkout log gagal harus 500: %d", r.Code)
	}
	var nAfter int64
	db.Raw("SELECT COUNT(*) FROM orders").Scan(&nAfter)
	if nAfter != nOrders {
		t.Fatal("pesanan tidak boleh tertulis tanpa log")
	}
	if r := call(t, h, "GET", "/api/cart", tok, nil); num(r.Body["lineCount"]) != 1 {
		t.Fatal("keranjang harus utuh")
	}
	if len(rec.kinds()) != before {
		t.Fatal("notifikasi tidak boleh terkirim untuk transaksi gagal")
	}
	fail.Store(false)
}
