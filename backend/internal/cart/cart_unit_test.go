package cart

import (
	"encoding/json"
	"strings"
	"testing"

	"mihanstore/internal/catalog"
)

// Sementara di paket main: cartItemRow/buildCartItem pindah bersama cart (langkah 5).
func fx(minQty, rupiah int64) catalog.PriceTier {
	return catalog.PriceTier{MinQty: minQty, Type: catalog.TierFixed, ValueX100: rupiah * 100}
}

func TestBuildCartItem(t *testing.T) {
	tiers := []catalog.PriceTier{fx(10, 42000), fx(50, 40000)}
	seen := int64(45000)
	row := cartItemRow{ItemID: 1, ProductID: 7, Name: "Kerupuk", Price: 45000, Unit: "pak", Qty: 12, Seen: &seen, Available: true}
	it := buildCartItem(row, tiers)
	if it.UnitPrice != 42000 || it.LineTotal != 12*42000 || it.BaseUnitPrice != 45000 || it.TierMinQty == nil || *it.TierMinQty != 10 {
		t.Fatalf("harga: %+v", it)
	}
	if it.Savings != 12*3000 || it.NextTier == nil || it.NextTier.MoreQty != 38 || it.NextTier.UnitPrice != 40000 {
		t.Fatalf("hemat/next: %+v %+v", it, it.NextTier)
	}
	if !it.PriceChanged || it.PreviousUnitPrice == nil || *it.PreviousUnitPrice != 45000 {
		t.Fatalf("seen 45000 vs sekarang 42000 harus berubah: %+v", it)
	}
	seen = 42000
	if it := buildCartItem(row, tiers); it.PriceChanged || it.PreviousUnitPrice != nil {
		t.Fatalf("harga sama tidak berubah: %+v", it)
	}
	row.Seen = nil
	if it := buildCartItem(row, tiers); it.PriceChanged {
		t.Fatal("seen NULL dianggap tidak berubah")
	}
	row.Available = false
	row.Seen = &seen
	seen = 1
	if it := buildCartItem(row, tiers); it.PriceChanged || it.NextTier != nil {
		t.Fatal("produk tidak tersedia tidak diberi penanda/petunjuk")
	}
	b, _ := json.Marshal(buildCartItem(cartItemRow{Price: 5000, Qty: 1, Unit: "pcs", Available: true}, nil))
	for _, k := range []string{`"unit":"pcs"`, `"baseUnitPrice":5000`, `"unitPrice":5000`, `"tierMinQty":null`, `"nextTier":null`, `"savings":0`, `"priceChanged":false`, `"previousUnitPrice":null`, `"price":5000`, `"lineTotal":5000`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("JSON baris keranjang tidak memuat %s: %s", k, b)
		}
	}
	if strings.Contains(string(b), "itemID") || strings.Contains(string(b), "seen") {
		t.Fatalf("field internal bocor: %s", b)
	}
}
