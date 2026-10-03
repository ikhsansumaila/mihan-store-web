package main

// Penyimpanan jenjang harga (tabel product_price_tiers). Perhitungan harga ada di pricing.go.

import (
	"time"

	"gorm.io/gorm"
)

type tierRow struct {
	ID        uint64 `gorm:"column:id"`
	ProductID uint64 `gorm:"column:product_id"`
	MinQty    int64  `gorm:"column:min_qty"`
	Type      string `gorm:"column:discount_type"`
	ValueX100 int64  `gorm:"column:value_x100"`
}

func (r tierRow) tier() PriceTier {
	return PriceTier{MinQty: r.MinQty, Type: r.Type, ValueX100: r.ValueX100}
}

const tierSelect = `SELECT id, product_id, min_qty, discount_type, CAST(ROUND(value * 100) AS SIGNED) AS value_x100
FROM product_price_tiers WHERE deleted_at IS NULL`

// loadTiersFor membaca jenjang aktif untuk produk-produk ids (urut min_qty naik).
func loadTiersFor(db *gorm.DB, ids []uint64) (map[uint64][]PriceTier, error) {
	out := map[uint64][]PriceTier{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []tierRow
	if err := db.Raw(tierSelect+` AND product_id IN ? ORDER BY product_id, min_qty`, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ProductID] = append(out[r.ProductID], r.tier())
	}
	return out, nil
}

// lockTiers membaca jenjang aktif satu produk dengan kunci (dipakai di dalam transaksi admin,
// setelah baris produk dikunci).
func lockTiers(tx *gorm.DB, productID uint64) ([]tierRow, error) {
	var rows []tierRow
	err := tx.Raw(tierSelect+` AND product_id = ? ORDER BY min_qty FOR UPDATE`, productID).Scan(&rows).Error
	return rows, err
}

func rowsToTiers(rows []tierRow) []PriceTier {
	out := make([]PriceTier, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.tier())
	}
	return out
}

// replaceTiers mengganti seluruh daftar jenjang produk (di dalam transaksi pemanggil):
// yang tidak ada lagi di-soft-delete, yang min_qty-nya sama diperbarui bila berubah, sisanya ditambahkan.
// Mengembalikan true bila ada perubahan.
func replaceTiers(tx *gorm.DB, productID uint64, existing []tierRow, next []PriceTier, now time.Time) (bool, error) {
	byQty := map[int64]tierRow{}
	for _, r := range existing {
		byQty[r.MinQty] = r
	}
	keep := map[int64]bool{}
	for _, t := range next {
		keep[t.MinQty] = true
	}
	changed := false
	for _, r := range existing {
		if !keep[r.MinQty] {
			if err := tx.Exec(`UPDATE product_price_tiers SET deleted_at = ? WHERE id = ?`, now, r.ID).Error; err != nil {
				return false, err
			}
			changed = true
		}
	}
	for _, t := range next {
		if r, ok := byQty[t.MinQty]; ok {
			if r.Type == t.Type && r.ValueX100 == t.ValueX100 {
				continue
			}
			if err := tx.Exec(`UPDATE product_price_tiers SET discount_type = ?, value = ? WHERE id = ?`, t.Type, tierValueSQL(t), r.ID).Error; err != nil {
				return false, err
			}
			changed = true
			continue
		}
		if err := tx.Exec(`INSERT INTO product_price_tiers (product_id, min_qty, discount_type, value, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`, productID, t.MinQty, t.Type, tierValueSQL(t), now, now).Error; err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}
