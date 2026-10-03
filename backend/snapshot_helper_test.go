//go:build integration || e2e

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// compareProductsSnapshot memeriksa /api/products terhadap snapshot produksi SEBELUM fitur harga
// grosir: jumlah produk sama, field lama (id..image) identik byte-per-byte dan urutannya tetap, dan
// field baru hanya ditambahkan di belakang dengan nilai awal `"unit":"pcs","tiers":[]`.
func compareProductsSnapshot(want, got []byte) error {
	var w, g []json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(want), &w); err != nil {
		return fmt.Errorf("snapshot: %v", err)
	}
	if err := json.Unmarshal(bytes.TrimSpace(got), &g); err != nil {
		return fmt.Errorf("respons: %v", err)
	}
	if len(w) != len(g) {
		return fmt.Errorf("jumlah produk %d, snapshot %d", len(g), len(w))
	}
	for i := range w {
		old := bytes.TrimSuffix(bytes.TrimSpace(w[i]), []byte("}"))
		wantItem := append(append([]byte{}, old...), []byte(`,"unit":"pcs","tiers":[]}`)...)
		if !bytes.Equal(wantItem, bytes.TrimSpace(g[i])) {
			return fmt.Errorf("produk ke-%d berbeda:\nmau  %s\ndapat %s", i, wantItem, g[i])
		}
	}
	return nil
}
