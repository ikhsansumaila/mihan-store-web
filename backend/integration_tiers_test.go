//go:build integration

// Tes integrasi harga berjenjang (grosir) + satuan jual terhadap database UJI yang dibuat dari
// migrasi 001–012. Produk uji dibuat sendiri lewat API admin (tidak mengubah produk seed 1–20).
package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"
)

func tierBody(name string, price int64, unit string, tiers ...map[string]any) map[string]any {
	if tiers == nil {
		tiers = []map[string]any{}
	}
	return map[string]any{"name": name, "categoryId": 1, "price": price, "description": "uji grosir", "unit": unit, "tiers": tiers}
}

func tr(minQty int, typ string, value any) map[string]any {
	return map[string]any{"minQty": minQty, "type": typ, "value": value}
}

func createTierProduct(t *testing.T, h http.Handler, body map[string]any) uint64 {
	t.Helper()
	r := adminCall(t, h, "POST", "/api/admin/products", integAdmin, body)
	if r.Code != 201 {
		t.Fatalf("buat produk: %d %v", r.Code, r.Body)
	}
	return uint64(num(r.Body["id"]))
}

func cartLineOf(t *testing.T, body map[string]any, pid uint64) map[string]any {
	t.Helper()
	for _, it := range body["items"].([]any) {
		m := it.(map[string]any)
		if uint64(num(m["productId"])) == pid {
			return m
		}
	}
	t.Fatalf("produk %d tidak ada di keranjang: %v", pid, body)
	return nil
}

func tierRowsOf(db *gorm.DB, pid uint64) (alive, deleted int64) {
	db.Raw("SELECT COUNT(*) FROM product_price_tiers WHERE product_id = ? AND deleted_at IS NULL", pid).Scan(&alive)
	db.Raw("SELECT COUNT(*) FROM product_price_tiers WHERE product_id = ? AND deleted_at IS NOT NULL", pid).Scan(&deleted)
	return
}

func TestIntegrationTierSchemaAndGrants(t *testing.T) {
	_, _, db, _ := setupOrders(t)
	var n int64
	db.Raw("SELECT COUNT(*) FROM products WHERE unit <> 'pcs'").Scan(&n)
	var total int64
	db.Raw("SELECT COUNT(*) FROM products").Scan(&total)
	if total < 20 {
		t.Fatalf("seed produk: %d", total)
	}
	// Data awal jenjang KOSONG.
	var nt int64
	db.Raw("SELECT COUNT(*) FROM product_price_tiers").Scan(&nt)
	if nt != 0 {
		t.Fatalf("jenjang awal harus kosong: %d", nt)
	}
	// Hak aplikasi: SELECT/INSERT/UPDATE boleh, DELETE/DDL ditolak; order_items tetap append-only.
	if err := db.Exec("INSERT INTO product_price_tiers (product_id, min_qty, discount_type, value) VALUES (20, 7, 'fixed', 100)").Error; err != nil {
		t.Fatalf("INSERT jenjang harus boleh: %v", err)
	}
	// UNIQUE (product_id, min_qty, alive): duplikat aktif ditolak, setelah soft delete boleh lagi.
	if err := db.Exec("INSERT INTO product_price_tiers (product_id, min_qty, discount_type, value) VALUES (20, 7, 'percent', 5)").Error; err == nil {
		t.Fatal("duplikat jenjang aktif harus ditolak")
	}
	if err := db.Exec("UPDATE product_price_tiers SET deleted_at = UTC_TIMESTAMP(3) WHERE product_id = 20 AND min_qty = 7").Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if err := db.Exec("INSERT INTO product_price_tiers (product_id, min_qty, discount_type, value) VALUES (20, 7, 'percent', 5)").Error; err != nil {
		t.Fatalf("insert ulang setelah soft delete harus boleh: %v", err)
	}
	db.Exec("UPDATE product_price_tiers SET deleted_at = UTC_TIMESTAMP(3) WHERE product_id = 20")
	// CHECK min_qty >= 2 dan value > 0.
	for _, q := range []string{
		"INSERT INTO product_price_tiers (product_id, min_qty, discount_type, value) VALUES (20, 1, 'fixed', 100)",
		"INSERT INTO product_price_tiers (product_id, min_qty, discount_type, value) VALUES (20, 9, 'fixed', 0)",
		"INSERT INTO product_price_tiers (product_id, min_qty, discount_type, value) VALUES (20, 9, 'gratis', 1)",
	} {
		if err := db.Exec(q).Error; err == nil {
			t.Errorf("harus ditolak: %s", q)
		}
	}
	for _, q := range []string{
		"DELETE FROM product_price_tiers WHERE id = 0",
		"TRUNCATE TABLE product_price_tiers",
		"ALTER TABLE product_price_tiers ADD COLUMN x INT",
		"UPDATE order_items SET base_unit_price = 1 WHERE id = 0",
		"UPDATE order_items SET unit = 'x' WHERE id = 0",
	} {
		err := db.Exec(q).Error
		if err == nil || mysqlErrNo(err) != 1142 {
			t.Errorf("harus ditolak (1142): %s -> %v", q, err)
		}
	}
}

func TestIntegrationAdminTiersReplaceAndLog(t *testing.T) {
	_, h, db, _ := setupOrders(t)
	// Buat produk dengan jenjang campuran fixed + percent, satuan dinormalisasi.
	pid := createTierProduct(t, h, tierBody("Kerupuk Grosir Uji", 45000, "  PAK ", tr(50, "percent", 10), tr(10, "fixed", 42000)))
	r := adminCall(t, h, "GET", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin, nil)
	tiers := r.Body["tiers"].([]any)
	if r.Body["unit"] != "pak" || len(tiers) != 2 {
		t.Fatalf("produk: %v", r.Body)
	}
	t0, t1 := tiers[0].(map[string]any), tiers[1].(map[string]any)
	if num(t0["minQty"]) != 10 || t0["type"] != "fixed" || num(t0["unitPrice"]) != 42000 || num(t1["unitPrice"]) != 40500 || t1["value"].(float64) != 10 {
		t.Fatalf("jenjang: %v", tiers)
	}
	if l := lastLog(t, db, "product.create"); !strings.Contains(det(l), `"unit":"pak"`) || !strings.Contains(det(l), "min 10: Rp 42.000") {
		t.Fatalf("log create: %s", det(l))
	}
	// API publik: unit + tiers, tanpa data admin.
	w := callRaw(t, h, "GET", "/api/products/search?q=Kerupuk%20Grosir%20Uji")
	if !strings.Contains(w, `"unit":"pak","tiers":[{"minQty":10,"type":"fixed","value":42000,"unitPrice":42000},{"minQty":50,"type":"percent","value":10,"unitPrice":40500}]`) ||
		strings.Contains(w, "createdBy") || strings.Contains(w, "isActive") {
		t.Fatalf("publik: %s", w)
	}

	// Ganti daftar: hapus 50, ubah 10 (fixed->percent), tambah 100. Satu transaksi + log.
	logsBefore := countLogs(db, "action = 'product.update' AND entity_id = ?", itoa(pid))
	r = adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin,
		tierBody("Kerupuk Grosir Uji", 45000, "dus", tr(10, "percent", 5), tr(100, "fixed", 39000)))
	if r.Code != 200 || r.Body["unit"] != "dus" || len(r.Body["tiers"].([]any)) != 2 {
		t.Fatalf("ganti jenjang: %d %v", r.Code, r.Body)
	}
	alive, deleted := tierRowsOf(db, pid)
	if alive != 2 || deleted != 1 {
		t.Fatalf("baris jenjang: aktif %d, terhapus %d", alive, deleted)
	}
	var typ string
	db.Raw("SELECT discount_type FROM product_price_tiers WHERE product_id = ? AND min_qty = 10 AND deleted_at IS NULL", pid).Scan(&typ)
	if typ != "percent" {
		t.Fatalf("jenjang 10 harus diperbarui (upsert): %s", typ)
	}
	if countLogs(db, "action = 'product.update' AND entity_id = ?", itoa(pid)) != logsBefore+1 {
		t.Fatal("harus ada 1 log product.update")
	}
	d := det(lastLog(t, db, "product.update"))
	if !strings.Contains(d, `"unit":{"dari":"pak","menjadi":"dus"}`) || !strings.Contains(d, `"tiers":{"dari":["min 10: Rp 42.000","min 50: 10%"],"menjadi":["min 10: 5%","min 100: Rp 39.000"]}`) {
		t.Fatalf("log selisih jenjang: %s", d)
	}
	// Simpan ulang tanpa perubahan: tidak menulis, tidak mencatat.
	adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin,
		tierBody("Kerupuk Grosir Uji", 45000, "dus", tr(100, "fixed", 39000), tr(10, "percent", 5)))
	if countLogs(db, "action = 'product.update' AND entity_id = ?", itoa(pid)) != logsBefore+1 {
		t.Fatal("tanpa perubahan tidak boleh dicatat")
	}
	// Klien lama (tanpa field unit/tiers): jenjang & satuan tidak berubah.
	r = adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin,
		map[string]any{"name": "Kerupuk Grosir Uji", "categoryId": 1, "price": 45000, "description": "uji grosir v2"})
	if r.Code != 200 || r.Body["unit"] != "dus" || len(r.Body["tiers"].([]any)) != 2 {
		t.Fatalf("klien lama: %d %v", r.Code, r.Body)
	}

	// Validasi: 422 dengan pesan per jenjang (index sesuai urutan kiriman), tidak ada yang ditulis.
	r = adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin,
		tierBody("Kerupuk Grosir Uji", 45000, "dus", tr(10, "fixed", 43000), tr(20, "fixed", 44000), tr(30, "bonus", 1)))
	if r.Code != 422 {
		t.Fatalf("jenjang tidak valid harus 422: %d %v", r.Code, r.Body)
	}
	te := r.Body["tierErrors"].([]any)
	if len(te) != 1 || num(te[0].(map[string]any)["index"]) != 2 {
		t.Fatalf("tierErrors (jenis dulu): %v", te)
	}
	r = adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin,
		tierBody("Kerupuk Grosir Uji", 45000, "dus", tr(10, "fixed", 43000), tr(20, "fixed", 44000)))
	te = r.Body["tierErrors"].([]any)
	if r.Code != 422 || len(te) != 1 || num(te[0].(map[string]any)["minQty"]) != 20 || !strings.Contains(r.Body["error"].(string), "lebih murah dari jenjang min. 10") {
		t.Fatalf("monoton: %d %v", r.Code, r.Body)
	}
	var many []map[string]any
	for i := 0; i < 21; i++ {
		many = append(many, tr(i+2, "fixed", 44000-i))
	}
	if r := adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin, tierBody("Kerupuk Grosir Uji", 45000, "dus", many...)); r.Code != 422 || !strings.Contains(r.Body["error"].(string), "Maksimal 20") {
		t.Fatalf("21 jenjang: %d %v", r.Code, r.Body)
	}
	if r := adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin, tierBody("Kerupuk Grosir Uji", 45000, "dus<b>")); r.Code != 400 {
		t.Fatalf("satuan tidak valid harus 400: %d", r.Code)
	}
	if alive, _ := tierRowsOf(db, pid); alive != 2 {
		t.Fatal("permintaan ditolak tidak boleh mengubah jenjang")
	}

	// Perubahan harga dasar yang merusak jenjang fixed (39000 untuk min 100) -> 422, tidak ditulis.
	logsBefore = countLogs(db, "action = 'product.update' AND entity_id = ?", itoa(pid))
	r = adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin,
		map[string]any{"name": "Kerupuk Grosir Uji", "categoryId": 1, "price": 39000, "description": "uji grosir v2"})
	if r.Code != 422 || !strings.Contains(r.Body["error"].(string), "Harga dasar baru Rp 39.000") || !strings.Contains(r.Body["error"].(string), "min. 100") {
		t.Fatalf("harga dasar merusak jenjang: %d %v", r.Code, r.Body)
	}
	var price int64
	db.Raw("SELECT price FROM products WHERE id = ?", pid).Scan(&price)
	if price != 45000 || countLogs(db, "action = 'product.update' AND entity_id = ?", itoa(pid)) != logsBefore {
		t.Fatal("harga dasar tidak boleh berubah dan tidak boleh ada log")
	}
	// Harga dasar naik: jenjang percent mengikuti, fixed tetap valid -> boleh.
	r = adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin,
		map[string]any{"name": "Kerupuk Grosir Uji", "categoryId": 1, "price": 46000, "description": "uji grosir v2"})
	if r.Code != 200 || num(r.Body["tiers"].([]any)[0].(map[string]any)["unitPrice"]) != 43700 {
		t.Fatalf("harga naik: %d %v", r.Code, r.Body)
	}
}

func callRaw(t *testing.T, h http.Handler, method, path string) string {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Body.String()
}

// Bila log aktivitas gagal, produk + jenjang ikut dibatalkan (satu transaksi).
func TestIntegrationTierReplaceAtomicWithLog(t *testing.T) {
	_, h, db, _ := setupOrders(t)
	pid := createTierProduct(t, h, tierBody("Saos Grosir Atomik", 20000, "botol", tr(12, "fixed", 18000)))
	var fail atomic.Bool
	if err := db.Callback().Create().Before("gorm:create").Register("uji_gagal_log_tier", func(tx *gorm.DB) {
		if fail.Load() && tx.Statement.Table == "activity_logs" {
			tx.AddError(errors.New("uji: log gagal"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	r := adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin,
		tierBody("Saos Grosir Atomik", 21000, "dus", tr(24, "percent", 15)))
	fail.Store(false)
	if r.Code != 500 {
		t.Fatalf("log gagal harus 500: %d", r.Code)
	}
	var unit string
	var price int64
	db.Raw("SELECT unit FROM products WHERE id = ?", pid).Scan(&unit)
	db.Raw("SELECT price FROM products WHERE id = ?", pid).Scan(&price)
	alive, deleted := tierRowsOf(db, pid)
	var mq int64
	db.Raw("SELECT min_qty FROM product_price_tiers WHERE product_id = ? AND deleted_at IS NULL", pid).Scan(&mq)
	if unit != "botol" || price != 20000 || alive != 1 || deleted != 0 || mq != 12 {
		t.Fatalf("harus di-rollback: unit %s harga %d aktif %d terhapus %d min %d", unit, price, alive, deleted, mq)
	}
}

func TestIntegrationTierCartCheckoutSnapshot(t *testing.T) {
	_, h, db, _ := setupOrders(t)
	pid := createTierProduct(t, h, tierBody("Tepung Grosir Uji", 10000, "kg", tr(5, "fixed", 9000), tr(10, "percent", 15)))
	plain := createTierProduct(t, h, tierBody("Tepung Eceran Uji", 45000, ""))
	tok := newCustomer(t, h, "Grosir Satu")

	// Tambah 4: harga dasar, petunjuk "tambah 1 lagi".
	r := call(t, h, "POST", "/api/cart/items", tok, map[string]any{"productId": pid, "qty": 4})
	l := cartLineOf(t, r.Body, pid)
	nt := l["nextTier"].(map[string]any)
	if num(l["unitPrice"]) != 10000 || l["tierMinQty"] != nil || num(nt["moreQty"]) != 1 || num(nt["unitPrice"]) != 9000 || l["unit"] != "kg" || l["priceChanged"] != false {
		t.Fatalf("qty 4: %v", l)
	}
	// Qty melewati jenjang (oleh pelanggan sendiri): harga turun, BUKAN "perubahan harga".
	r = call(t, h, "PUT", "/api/cart/items", tok, map[string]any{"productId": pid, "qty": 10})
	l = cartLineOf(t, r.Body, pid)
	if num(l["unitPrice"]) != 8500 || num(l["tierMinQty"]) != 10 || num(l["savings"]) != 15000 || num(l["lineTotal"]) != 85000 ||
		l["priceChanged"] != false || l["nextTier"] != nil || num(r.Body["subtotal"]) != 85000 || num(r.Body["savings"]) != 15000 {
		t.Fatalf("qty 10: %v / %v", l, r.Body)
	}

	// Admin mengubah harga dasar -> percent ikut berubah -> penanda muncul.
	adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin,
		tierBody("Tepung Grosir Uji", 10500, "kg", tr(5, "fixed", 9000), tr(10, "percent", 15)))
	r = call(t, h, "GET", "/api/cart", tok, nil)
	l = cartLineOf(t, r.Body, pid)
	if l["priceChanged"] != true || num(l["previousUnitPrice"]) != 8500 || num(l["unitPrice"]) != 8925 || r.Body["hasPriceChanges"] != true {
		t.Fatalf("penanda harga berubah: %v", l)
	}
	// GET tidak menghapus penanda.
	if l := cartLineOf(t, call(t, h, "GET", "/api/cart", tok, nil).Body, pid); l["priceChanged"] != true {
		t.Fatal("penanda harus tetap sampai pelanggan menekan Mengerti")
	}

	// Checkout dengan total lama -> 409 price_changed, tanpa pesanan, keranjang utuh, tanpa log.
	var nOrders, nItems int64
	db.Raw("SELECT COUNT(*) FROM orders").Scan(&nOrders)
	db.Raw("SELECT COUNT(*) FROM order_items").Scan(&nItems)
	logs := countLogs(db, "action = 'order.create'")
	key := uuid(t)
	body := checkoutBody(key)
	body["expectedTotal"] = 85000
	r = call(t, h, "POST", "/api/orders", tok, body)
	if r.Code != 409 || r.Body["error"] != "price_changed" || r.Body["cart"] == nil || num(r.Body["cart"].(map[string]any)["subtotal"]) != 89250 {
		t.Fatalf("expectedTotal lama harus 409 price_changed: %d %v", r.Code, r.Body)
	}
	var nOrders2, nItems2 int64
	db.Raw("SELECT COUNT(*) FROM orders").Scan(&nOrders2)
	db.Raw("SELECT COUNT(*) FROM order_items").Scan(&nItems2)
	if nOrders2 != nOrders || nItems2 != nItems || countLogs(db, "action = 'order.create'") != logs {
		t.Fatal("409 tidak boleh menulis pesanan/log")
	}
	if l := cartLineOf(t, call(t, h, "GET", "/api/cart", tok, nil).Body, pid); num(l["qty"]) != 10 {
		t.Fatal("keranjang tidak boleh dikosongkan")
	}

	// Mengerti -> penanda hilang.
	r = call(t, h, "POST", "/api/cart/ack-prices", tok, nil)
	if l := cartLineOf(t, r.Body, pid); r.Code != 200 || l["priceChanged"] != false || l["previousUnitPrice"] != nil || r.Body["hasPriceChanges"] != false {
		t.Fatalf("ack: %d %v", r.Code, r.Body)
	}
	if r := call(t, h, "POST", "/api/cart/ack-prices", "", nil); r.Code != 401 {
		t.Fatalf("ack tanpa login: %d", r.Code)
	}

	// Konfirmasi ulang dengan total baru (kunci idempotensi yang sama) -> 201, snapshot tersimpan.
	call(t, h, "POST", "/api/cart/items", tok, map[string]any{"productId": plain, "qty": 1}) // tanpa jenjang, satuan kosong -> pcs
	body["expectedTotal"] = 89250 + 45000
	r = call(t, h, "POST", "/api/orders", tok, body)
	if r.Code != 201 {
		t.Fatalf("checkout konfirmasi: %d %v", r.Code, r.Body)
	}
	orderNo := r.Body["orderNo"].(string)
	if num(r.Body["subtotal"]) != 134250 {
		t.Fatalf("subtotal: %v", r.Body["subtotal"])
	}
	items := r.Body["items"].([]any)
	it0, it1 := items[0].(map[string]any), items[1].(map[string]any)
	if num(it0["unitPrice"]) != 8925 || num(it0["baseUnitPrice"]) != 10500 || num(it0["tierMinQty"]) != 10 || it0["unit"] != "kg" || num(it0["lineTotal"]) != 89250 {
		t.Fatalf("snapshot item grosir: %v", it0)
	}
	if num(it1["unitPrice"]) != 45000 || num(it1["baseUnitPrice"]) != 45000 || it1["tierMinQty"] != nil || it1["unit"] != "pcs" {
		t.Fatalf("snapshot item eceran: %v", it1)
	}
	var row struct {
		Unit  string `gorm:"column:unit"`
		Base  int64  `gorm:"column:base_unit_price"`
		Tier  *int64 `gorm:"column:tier_min_qty"`
		Price int64  `gorm:"column:unit_price"`
	}
	db.Raw(`SELECT oi.unit, oi.base_unit_price, oi.tier_min_qty, oi.unit_price FROM order_items oi JOIN orders o ON o.id = oi.order_id
		WHERE o.order_no = ? AND oi.product_id = ?`, orderNo, pid).Scan(&row)
	if row.Unit != "kg" || row.Base != 10500 || row.Tier == nil || *row.Tier != 10 || row.Price != 8925 {
		t.Fatalf("DB snapshot: %+v", row)
	}
	if !strings.Contains(det(lastLog(t, db, "order.create")), `"barisHargaGrosir":1`) {
		t.Fatalf("log order.create: %s", det(lastLog(t, db, "order.create")))
	}
	// Idempotensi tetap: kunci sama -> 200 pesanan yang sama (expectedTotal apa pun).
	body["expectedTotal"] = 1
	if r := call(t, h, "POST", "/api/orders", tok, body); r.Code != 200 || r.Body["orderNo"] != orderNo {
		t.Fatalf("idempotensi: %d %v", r.Code, r.Body)
	}

	// Admin mengubah jenjang & harga dasar: pesanan yang sudah ada TIDAK berubah.
	adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin, tierBody("Tepung Grosir Uji", 15000, "dus", tr(3, "fixed", 1000)))
	r = call(t, h, "GET", "/api/orders/"+orderNo, tok, nil)
	it0 = r.Body["items"].([]any)[0].(map[string]any)
	if num(it0["unitPrice"]) != 8925 || num(it0["baseUnitPrice"]) != 10500 || it0["unit"] != "kg" || num(r.Body["subtotal"]) != 134250 {
		t.Fatalf("pesanan berubah setelah admin mengubah harga: %v", r.Body)
	}
	var oid uint64
	db.Raw("SELECT id FROM orders WHERE order_no = ?", orderNo).Scan(&oid)
	ra := adminCall(t, h, "GET", fmt.Sprintf("/api/admin/orders/%d", oid), integAdmin, nil)
	if a0 := ra.Body["items"].([]any)[0].(map[string]any); num(a0["tierMinQty"]) != 10 || a0["unit"] != "kg" {
		t.Fatalf("admin detail: %v", a0)
	}

	// Klien lama tanpa expectedTotal tetap bisa checkout (tidak diperiksa).
	call(t, h, "POST", "/api/cart/items", tok, map[string]any{"productId": pid, "qty": 3})
	if r := call(t, h, "POST", "/api/orders", tok, checkoutBody(uuid(t))); r.Code != 201 || num(r.Body["subtotal"]) != 3000 {
		t.Fatalf("tanpa expectedTotal: %d %v", r.Code, r.Body)
	}
}

func TestIntegrationTierSeenNullAndOldOrder(t *testing.T) {
	_, h, db, _ := setupOrders(t)
	pid := createTierProduct(t, h, tierBody("Sambal Grosir Uji", 30000, "botol", tr(6, "fixed", 27000)))
	tok := newCustomer(t, h, "Grosir Dua")
	call(t, h, "POST", "/api/cart/items", tok, map[string]any{"productId": pid, "qty": 6})
	// Tiru baris keranjang lama (sebelum fitur): seen NULL -> tidak dianggap berubah, lalu diisi.
	db.Exec("UPDATE cart_items SET seen_unit_price = NULL WHERE product_id = ?", pid)
	r := call(t, h, "GET", "/api/cart", tok, nil)
	if l := cartLineOf(t, r.Body, pid); l["priceChanged"] != false {
		t.Fatalf("seen NULL: %v", l)
	}
	var seen *int64
	db.Raw("SELECT seen_unit_price FROM cart_items WHERE product_id = ?", pid).Scan(&seen)
	if seen == nil || *seen != 27000 {
		t.Fatalf("seen harus diisi saat keranjang dibuka: %v", seen)
	}
	// Admin menurunkan harga jenjang -> berubah; pelanggan menambah qty -> penanda hilang (aksi sendiri).
	adminCall(t, h, "PUT", fmt.Sprintf("/api/admin/products/%d", pid), integAdmin, tierBody("Sambal Grosir Uji", 30000, "botol", tr(6, "fixed", 26000)))
	if l := cartLineOf(t, call(t, h, "GET", "/api/cart", tok, nil).Body, pid); l["priceChanged"] != true || num(l["previousUnitPrice"]) != 27000 {
		t.Fatalf("berubah: %v", l)
	}
	r = call(t, h, "PUT", "/api/cart/items", tok, map[string]any{"productId": pid, "qty": 7})
	if l := cartLineOf(t, r.Body, pid); l["priceChanged"] != false || num(l["unitPrice"]) != 26000 {
		t.Fatalf("ubah qty oleh pelanggan: %v", l)
	}

	// Pesanan lama (sebelum 010/011, kolom baru NULL) tetap tampil benar di admin.
	var oid uint64
	db.Raw("SELECT id FROM orders WHERE order_no = 'MS-261001-0900'").Scan(&oid)
	if oid == 0 {
		t.Skip("fixture pesanan lama tidak ada")
	}
	ra := adminCall(t, h, "GET", fmt.Sprintf("/api/admin/orders/%d", oid), integAdmin, nil)
	it := ra.Body["items"].([]any)[0].(map[string]any)
	if ra.Code != 200 || num(it["unitPrice"]) != 45000 || num(it["lineTotal"]) != 90000 || it["unit"] != nil || it["baseUnitPrice"] != nil || it["tierMinQty"] != nil || num(ra.Body["total"]) != 100000 {
		t.Fatalf("pesanan lama: %d %v", ra.Code, ra.Body)
	}
}
