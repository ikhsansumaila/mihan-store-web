//go:build integration

// Tes integrasi checkout: variasi penulisan nomor telepon (termasuk karakter Unicode dari
// kontak/WhatsApp di HP) harus diterima dan disimpan dalam bentuk +62.
package app

import (
	"strings"
	"testing"
)

func TestIntegrationCheckoutPhoneVariants(t *testing.T) {
	_, h, db, _ := setupOrders(t)
	tok := newCustomer(t, h, "Pembeli Telepon")
	ok := []string{
		"081234567890",
		"+6281234567890",
		"6281234567890",
		"0812 3456 7890",
		"0812-3456-7890",
		"+62 812-3456-7890",
		" 081234567890 ",
		"+62 0812 3456 7890",               // "+62 0" dirapikan
		"0812\u00a03456\u00a07890",         // NBSP (format kontak HP)
		"+62\u202f812\u20113456\u20117890", // narrow NBSP + non-breaking hyphen
		"\u202a+62 812-3456-7890\u202c",    // pembungkus bidi dari WhatsApp
		"0812\u20133456\u20147890",         // en dash, em dash
		"\uff10\uff18\uff11\uff12\uff13\uff14\uff15\uff16\uff17\uff18\uff19\uff10", // angka lebar
		"\u0660\u0668\u0661\u0662\u0663\u0664\u0665\u0666\u0667\u0668\u0669\u0660", // angka Arab-Indic
		"\u200b081234567890", // zero-width space
	}
	for _, in := range ok {
		if r := call(t, h, "POST", "/api/cart/items", tok, map[string]any{"productId": 1, "qty": 1}); r.Code != 200 {
			t.Fatalf("tambah keranjang: %d %v", r.Code, r.Body)
		}
		body := checkoutBody(uuid(t))
		body["recipientPhone"] = in
		r := call(t, h, "POST", "/api/orders", tok, body)
		if r.Code != 201 {
			t.Errorf("checkout telepon %q: %d %v", in, r.Code, r.Body)
			call(t, h, "DELETE", "/api/cart/items/1", tok, nil)
			continue
		}
		var phone string
		db.Raw("SELECT recipient_phone FROM orders WHERE order_no = ?", r.Body["orderNo"]).Scan(&phone)
		if phone != "+6281234567890" {
			t.Errorf("telepon %q disimpan %q, mau +6281234567890", in, phone)
		}
	}
	// Ditolak: 422 field recipientPhone dengan pesan spesifik.
	bad := map[string]string{
		"08123":                "terlalu pendek",
		"+6281111111111081234": "terlalu panjang",
		"0812abc4567":          "karakter yang tidak valid",
		"0212345678":           "harus diawali",
		"12345678":             "harus diawali",
	}
	call(t, h, "POST", "/api/cart/items", tok, map[string]any{"productId": 1, "qty": 1})
	for in, want := range bad {
		body := checkoutBody(uuid(t))
		body["recipientPhone"] = in
		r := call(t, h, "POST", "/api/orders", tok, body)
		msg, _ := r.Body["error"].(string)
		if r.Code != 422 || r.Body["field"] != "recipientPhone" || !strings.Contains(msg, want) {
			t.Errorf("telepon %q: mau 422 recipientPhone %q, dapat %d %v", in, want, r.Code, r.Body)
		}
	}
}
