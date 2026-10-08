//go:build integration

// Tes integrasi saran ongkir & default ongkir kecamatan (migrasi 021/022).
package main

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"
)

func suggestions(t *testing.T, r resp) []map[string]any {
	t.Helper()
	raw, _ := r.Body["suggestions"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		out = append(out, x.(map[string]any))
	}
	return out
}

func sugSummary(list []map[string]any) string {
	var parts []string
	for _, s := range list {
		parts = append(parts, fmt.Sprintf("%s:%d:%v", s["source"], num(s["fee"]), s["sources"]))
	}
	return strings.Join(parts, " ")
}

func TestIntegrationShippingSuggestionsAndDefault(t *testing.T) {
	_, h, db, _ := setupOrders(t)
	db.Exec("DELETE FROM region_shipping_defaults WHERE village_code IN ('36.71.01.1001','36.71.01.1002','36.71.99.1001')")
	tokX, _ := aliasCustomer(t, h, db, "Pelanggan X", "081277773333")
	tokY, _ := aliasCustomer(t, h, db, "Pelanggan Y", "081277774444")
	idOf := func(no string) uint64 { return orderIDByNo(db, no) }

	// Semua pesanan checkoutBody: Kel. Sukarasa 36.71.01.1001, Kec. Tangerang 36.71.01, Kota Tangerang 36.71.
	s1 := createOrderFor(t, h, tokX, 4) // kelurahan sama
	confirmCall(t, h, idOf(s1), 0, 11111)
	s1b := createOrderFor(t, h, tokY, 4) // kelurahan lain, kecamatan sama
	db.Exec("UPDATE orders SET village_code = '36.71.01.1002', village_name = 'Kel Lain' WHERE id = ?", idOf(s1b))
	confirmCall(t, h, idOf(s1b), 0, 13131)
	s2 := createOrderFor(t, h, tokY, 4) // kecamatan lain, kota sama
	db.Exec("UPDATE orders SET village_code = '36.71.99.1001', village_name = 'Kel Jauh', district_code = '36.71.99', district_name = 'Kec Lain' WHERE id = ?", idOf(s2))
	confirmCall(t, h, idOf(s2), 0, 22222)
	s3 := createOrderFor(t, h, tokY, 4) // dibatalkan -> dikecualikan
	confirmCall(t, h, idOf(s3), 0, 33333)
	adminSetStatus(t, h, idOf(s3), StatusPending, StatusCancelled)
	s4 := createOrderFor(t, h, tokY, 4) // masih menunggu konfirmasi -> dikecualikan
	db.Exec("UPDATE orders SET shipping_fee = 44444, total = subtotal + 44444 WHERE id = ?", idOf(s4))
	s5 := createOrderFor(t, h, tokX, 4) // pesanan terakhir pelanggan X, wilayah lain
	db.Exec("UPDATE orders SET village_code = '99.99.99.9999', village_name = 'Desa Lain', district_code = '99.99.99', regency_code = '99.99', regency_name = 'Kota Lain' WHERE id = ?", idOf(s5))
	confirmCall(t, h, idOf(s5), 0, 55555)
	s6 := createOrderFor(t, h, tokY, 4) // ongkir 0 -> dikecualikan
	confirmCall(t, h, idOf(s6), 0, 0)

	tgt := createOrderFor(t, h, tokX, 4)
	path := fmt.Sprintf("/api/admin/orders/%d/shipping-suggestions", idOf(tgt))
	r := adminCall(t, h, "GET", path, integAdmin, nil)
	if r.Code != 200 {
		t.Fatalf("saran: %d %v", r.Code, r.Body)
	}
	if got := sugSummary(suggestions(t, r)); got != "village:11111:[village] district:13131:[district] regency:22222:[regency] customer:55555:[customer]" {
		t.Fatalf("urutan/isi saran: %s", got)
	}
	reg := r.Body["region"].(map[string]any)
	if r.Body["canSetDefault"] != true || reg["villageCode"] != "36.71.01.1001" || reg["villageName"] != "Sukarasa" || reg["districtName"] != "Tangerang" ||
		reg["regencyName"] != "Kota Tangerang" || r.Body["currentDefault"] != nil {
		t.Fatalf("info wilayah: %v", r.Body)
	}
	first := suggestions(t, r)[0]
	if first["orderNo"] != s1 || first["date"] == nil || first["regionLabel"] != "Sukarasa" {
		t.Fatalf("detail saran: %v", first)
	}
	if lbl := suggestions(t, r)[1]["regionLabel"]; lbl != "Tangerang" {
		t.Fatalf("label kecamatan: %v", lbl)
	}

	// Non-admin.
	if r := call(t, h, "GET", path, tokX, nil); r.Code != 401 {
		t.Fatalf("pelanggan -> saran: %d", r.Code)
	}
	if r := adminCall(t, h, "GET", path, "bukan.admin@uji.test", nil); r.Code != 403 {
		t.Fatalf("bukan admin -> saran: %d", r.Code)
	}
	if r := adminCall(t, h, "GET", "/api/admin/orders/99999999/shipping-suggestions", integAdmin, nil); r.Code != 404 {
		t.Fatalf("pesanan tidak ada: %d", r.Code)
	}

	// Field lama setRegionDefault ditolak.
	cpath := fmt.Sprintf("/api/admin/orders/%d/confirm", idOf(tgt))
	if r := adminCall(t, h, "POST", cpath, integAdmin, map[string]any{"discount": 0, "shippingFee": 11111, "setRegionDefault": true}); r.Code != 400 {
		t.Fatalf("confirm + setRegionDefault harus 400: %d", r.Code)
	}

	// POST /shipping-default: belum dikonfirmasi 409; non-admin; tidak ada 404; nominal klien ditolak; batal 409; ongkir 0 400.
	dpath := func(no string) string { return fmt.Sprintf("/api/admin/orders/%d/shipping-default", idOf(no)) }
	if r := adminCall(t, h, "POST", dpath(tgt), integAdmin, map[string]any{}); r.Code != 409 || !strings.Contains(fmt.Sprint(r.Body["error"]), "belum dikonfirmasi") {
		t.Fatalf("default sebelum konfirmasi: %d %v", r.Code, r.Body)
	}
	if r := call(t, h, "POST", dpath(s1), tokX, map[string]any{}); r.Code != 401 {
		t.Fatalf("pelanggan -> default: %d", r.Code)
	}
	if r := adminCall(t, h, "POST", dpath(s1), "bukan.admin@uji.test", map[string]any{}); r.Code != 403 {
		t.Fatalf("bukan admin -> default: %d", r.Code)
	}
	if r := adminCall(t, h, "POST", "/api/admin/orders/99999999/shipping-default", integAdmin, map[string]any{}); r.Code != 404 {
		t.Fatalf("pesanan tidak ada: %d", r.Code)
	}
	if r := adminCall(t, h, "POST", dpath(s1), integAdmin, map[string]any{"shippingFee": 99999}); r.Code != 400 {
		t.Fatalf("nominal dari klien harus ditolak: %d", r.Code)
	}
	if r := adminCall(t, h, "POST", dpath(s3), integAdmin, nil); r.Code != 409 {
		t.Fatalf("default dari pesanan batal: %d", r.Code)
	}
	if r := adminCall(t, h, "POST", dpath(s6), integAdmin, nil); r.Code != 400 || !strings.Contains(fmt.Sprint(r.Body["error"]), "lebih dari Rp 0") {
		t.Fatalf("default ongkir 0: %d %v", r.Code, r.Body)
	}

	// Konfirmasi tgt (11111) lalu jadikan default kelurahan; rollback bila log gagal.
	confirmCall(t, h, idOf(tgt), 0, 11111)
	var nDef int64
	countDef := func() int64 {
		db.Raw("SELECT COUNT(*) FROM region_shipping_defaults WHERE village_code = '36.71.01.1001'").Scan(&nDef)
		return nDef
	}
	if countDef() != 0 {
		t.Fatal("konfirmasi saja tidak boleh membuat default")
	}
	var fail atomic.Bool
	if err := db.Callback().Create().Before("gorm:create").Register("uji_gagal_log_default", func(tx *gorm.DB) {
		if fail.Load() && tx.Statement.Table == "activity_logs" {
			tx.AddError(errors.New("uji: log gagal"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	if r := adminCall(t, h, "POST", dpath(tgt), integAdmin, nil); r.Code != 500 {
		t.Fatalf("log gagal harus 500: %d", r.Code)
	}
	fail.Store(false)
	db.Callback().Create().Remove("uji_gagal_log_default")
	if countDef() != 0 {
		t.Fatal("default harus di-rollback bila log gagal")
	}
	r = adminCall(t, h, "POST", dpath(tgt), integAdmin, nil)
	if r.Code != 200 || r.Body["villageCode"] != "36.71.01.1001" || r.Body["villageName"] != "Sukarasa" || r.Body["districtName"] != "Tangerang" ||
		num(r.Body["shippingFee"]) != 11111 || r.Body["previousFee"] != nil {
		t.Fatalf("default insert: %d %v", r.Code, r.Body)
	}
	var def struct {
		Fee  int64   `gorm:"column:shipping_fee"`
		Name string  `gorm:"column:village_name"`
		Dist *string `gorm:"column:district_code"`
		Reg  *string `gorm:"column:regency_code"`
		By   *uint64 `gorm:"column:updated_by"`
	}
	db.Raw("SELECT shipping_fee, village_name, district_code, regency_code, updated_by FROM region_shipping_defaults WHERE village_code = '36.71.01.1001'").Scan(&def)
	if def.Fee != 11111 || def.Name != "Sukarasa" || def.Dist == nil || *def.Dist != "36.71.01" || def.Reg == nil || *def.Reg != "36.71" || def.By == nil {
		t.Fatalf("default tersimpan: %+v", def)
	}
	l := lastLog(t, db, "region.shipping_default_set")
	if l == nil || !strings.Contains(det(l), `"ongkir":{"dari":null,"menjadi":11111}`) || !strings.Contains(det(l), `"kelurahan":"Sukarasa"`) ||
		!strings.Contains(det(l), `"kecamatan":"Tangerang"`) || !strings.Contains(l.Summary, "Kec. Tangerang") {
		t.Fatalf("log default: %v %v", l, det(l))
	}
	nLog := countLogs(db, "action = ?", "region.shipping_default_set")
	if r := adminCall(t, h, "POST", dpath(tgt), integAdmin, nil); r.Code != 200 || num(r.Body["previousFee"]) != 11111 {
		t.Fatalf("klik ganda: %d %v", r.Code, r.Body)
	}
	if countLogs(db, "action = ?", "region.shipping_default_set") != nLog {
		t.Fatal("klik ganda tidak boleh menulis log lagi")
	}

	// Pesanan baru di kelurahan sama: default + terakhir (kelurahan, kecamatan, kota = tgt) digabung.
	t2 := createOrderFor(t, h, tokY, 4)
	r = adminCall(t, h, "GET", fmt.Sprintf("/api/admin/orders/%d/shipping-suggestions", idOf(t2)), integAdmin, nil)
	if got := sugSummary(suggestions(t, r)); got != "default:11111:[default village district regency] customer:22222:[customer]" {
		t.Fatalf("saran dengan default: %s", got)
	}
	if num(r.Body["currentDefault"].(map[string]any)["fee"]) != 11111 {
		t.Fatalf("currentDefault: %v", r.Body["currentDefault"])
	}
	// Default kelurahan TIDAK berlaku untuk kelurahan lain di kecamatan yang sama.
	t3x := createOrderFor(t, h, tokY, 4)
	db.Exec("UPDATE orders SET village_code = '36.71.01.1002', village_name = 'Kel Lain' WHERE id = ?", idOf(t3x))
	r = adminCall(t, h, "GET", fmt.Sprintf("/api/admin/orders/%d/shipping-suggestions", idOf(t3x)), integAdmin, nil)
	if r.Body["currentDefault"] != nil || strings.Contains(sugSummary(suggestions(t, r)), "default:") {
		t.Fatalf("default kelurahan lain tidak boleh dipakai: %v", r.Body)
	}
	// Update default (12121): previousFee 11111.
	confirmCall(t, h, idOf(t2), 0, 12121)
	r = adminCall(t, h, "POST", dpath(t2), integAdmin, nil)
	if r.Code != 200 || num(r.Body["previousFee"]) != 11111 || num(r.Body["shippingFee"]) != 12121 {
		t.Fatalf("update default: %d %v", r.Code, r.Body)
	}
	var nRows int64
	db.Raw("SELECT shipping_fee FROM region_shipping_defaults WHERE village_code = '36.71.01.1001'").Scan(&def.Fee)
	db.Raw("SELECT COUNT(*) FROM region_shipping_defaults WHERE village_code = '36.71.01.1001'").Scan(&nRows)
	if def.Fee != 12121 || nRows != 1 {
		t.Fatalf("upsert: fee=%d rows=%d", def.Fee, nRows)
	}
	if l := lastLog(t, db, "region.shipping_default_set"); l == nil || !strings.Contains(det(l), `"ongkir":{"dari":11111,"menjadi":12121}`) {
		t.Fatalf("log update default: %v", det(l))
	}
	adminSetStatus(t, h, idOf(t2), StatusPending, StatusPaid)
	if r := adminCall(t, h, "POST", dpath(t2), integAdmin, nil); r.Code != 200 {
		t.Fatalf("default dari pesanan dibayar: %d", r.Code)
	}

	// Pesanan lama tanpa kelurahan: tanpa default/village, cadangan kecamatan/kota/pelanggan tetap; default -> 400.
	old := createOrderFor(t, h, tokX, 4)
	db.Exec("UPDATE orders SET village_code = NULL, village_name = NULL WHERE id = ?", idOf(old))
	r = adminCall(t, h, "GET", fmt.Sprintf("/api/admin/orders/%d/shipping-suggestions", idOf(old)), integAdmin, nil)
	if r.Body["canSetDefault"] != false || r.Body["region"].(map[string]any)["villageCode"] != nil {
		t.Fatalf("pesanan lama: %v", r.Body)
	}
	for _, s := range suggestions(t, r) {
		if s["source"] == "default" || s["source"] == "village" {
			t.Fatalf("pesanan tanpa kelurahan tidak boleh punya saran %v", s["source"])
		}
	}
	if got := sugSummary(suggestions(t, r)); !strings.Contains(got, "district:") || !strings.Contains(got, "customer:") {
		t.Fatalf("pesanan lama tetap dapat saran cadangan: %s", got)
	}
	confirmCall(t, h, idOf(old), 0, 9000)
	if r := adminCall(t, h, "POST", dpath(old), integAdmin, nil); r.Code != 400 || !strings.Contains(fmt.Sprint(r.Body["error"]), "kelurahan/desa") {
		t.Fatalf("default tanpa kelurahan: %d %v", r.Code, r.Body)
	}
	// Pesanan sendiri tidak pernah menjadi saran untuk dirinya.
	r = adminCall(t, h, "GET", fmt.Sprintf("/api/admin/orders/%d/shipping-suggestions", idOf(s1)), integAdmin, nil)
	for _, s := range suggestions(t, r) {
		if s["orderNo"] == s1 {
			t.Fatal("pesanan sendiri muncul sebagai saran")
		}
	}
	db.Exec("DELETE FROM region_shipping_defaults WHERE village_code IN ('36.71.01.1001','36.71.01.1002','36.71.99.1001')")
}

func TestIntegrationShippingDefaultsGrants(t *testing.T) {
	_, _, db, _ := setupOrders(t)
	for _, q := range []string{"SELECT COUNT(*) FROM region_shipping_defaults", "DELETE FROM region_shipping_defaults WHERE village_code = 'x'"} {
		if err := db.Exec(q).Error; err != nil {
			t.Errorf("harus boleh: %s -> %v", q, err)
		}
	}
	if err := db.Exec("DROP TABLE region_shipping_defaults").Error; mysqlErrNo(err) != 1142 {
		t.Errorf("DDL harus ditolak: %v", err)
	}
	if err := db.Exec("INSERT INTO region_shipping_defaults (village_code, village_name, shipping_fee) VALUES ('zz', 'x', 0)").Error; err == nil {
		t.Error("CHECK shipping_fee > 0 harus menolak")
	}
}
