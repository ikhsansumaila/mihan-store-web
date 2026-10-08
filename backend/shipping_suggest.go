package main

// Saran ongkir saat admin mengonfirmasi pesanan (status pending_confirmation) dan default ongkir per
// kecamatan (tabel region_shipping_defaults, migrasi 021/022).
//
// GET /api/admin/orders/{id}/shipping-suggestions — maks. 4 saran, urutan prioritas:
//   1. "default"  : default ongkir kecamatan pesanan (region_shipping_defaults)
//   2. "district" : ongkir pesanan terakhir ke kecamatan yang sama
//   3. "regency"  : ongkir pesanan terakhir ke kab/kota yang sama
//   4. "customer" : ongkir pesanan terakhir pelanggan ini
// Pesanan sumber: bukan pesanan ini, sudah dikonfirmasi (pending_payment/paid/completed; bukan cancelled dan
// bukan pending_confirmation), shipping_fee > 0 (ongkir 0 pada pesanan lama tidak bisa dibedakan dari
// "gratis"; Rp 0 tetap tersedia sebagai opsi manual di UI), belum dihapus. Nominal yang sama digabung:
// sumber prioritas tertinggi dipakai sebagai "source", semua sumber di "sources".
// Pesanan lama tanpa district_code: lewati 1 & 2, tetap 3 (bila regency_code ada) dan 4.

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

const maxShippingSuggestions = 4

var (
	errDefaultNoDistrict = errors.New("Pesanan ini tidak memiliki data kecamatan, jadi default ongkir wilayah tidak bisa ditetapkan")
	errDefaultZeroFee    = errors.New("Default ongkir wilayah harus lebih dari Rp 0")
)

type ShippingSuggestion struct {
	Source      string     `json:"source"`
	Sources     []string   `json:"sources"`
	Fee         int64      `json:"fee"`
	OrderNo     *string    `json:"orderNo"`
	Date        *time.Time `json:"date"`
	RegionLabel string     `json:"regionLabel"`
}

// mergeSuggestions: urutan masukan = prioritas; nominal sama digabung; maks. 4; fee <= 0 dibuang.
func mergeSuggestions(in []ShippingSuggestion) []ShippingSuggestion {
	out := []ShippingSuggestion{}
	for _, s := range in {
		if s.Fee <= 0 || s.Fee > maxShippingFee {
			continue
		}
		merged := false
		for i := range out {
			if out[i].Fee == s.Fee {
				out[i].Sources = append(out[i].Sources, s.Source)
				merged = true
				break
			}
		}
		if !merged {
			s.Sources = []string{s.Source}
			out = append(out, s)
		}
		if len(out) >= maxShippingSuggestions {
			break
		}
	}
	return out
}

type lastFeeRow struct {
	Fee          int64     `gorm:"column:shipping_fee"`
	OrderNo      string    `gorm:"column:order_no"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	DistrictName *string   `gorm:"column:district_name"`
	RegencyName  *string   `gorm:"column:regency_name"`
	City         string    `gorm:"column:city"`
}

// lastConfirmedFee: ongkir pesanan terakhir yang memenuhi kolom = nilai (kolom dari daftar putih).
func lastConfirmedFee(db *gorm.DB, column string, value any, excludeID uint64) (*lastFeeRow, error) {
	switch column {
	case "district_code", "regency_code", "user_id":
	default:
		return nil, errors.New("kolom tidak diizinkan")
	}
	var rows []lastFeeRow
	err := db.Raw(`SELECT shipping_fee, order_no, created_at, district_name, regency_name, city FROM orders
		WHERE `+column+` = ? AND id <> ? AND status IN ('pending_payment','paid','completed') AND shipping_fee > 0
		AND deleted_at IS NULL AND order_no IS NOT NULL
		ORDER BY created_at DESC, id DESC LIMIT 1`, value, excludeID).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func strOr(p *string, def string) string {
	if p != nil && strings.TrimSpace(*p) != "" {
		return *p
	}
	return def
}

type regionDefaultRow struct {
	Fee       int64     `gorm:"column:shipping_fee"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func loadRegionDefault(db *gorm.DB, districtCode string) (*regionDefaultRow, error) {
	var rows []regionDefaultRow
	if err := db.Raw(`SELECT shipping_fee, updated_at FROM region_shipping_defaults WHERE district_code = ?`, districtCode).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// AdminShippingSuggestions: GET /api/admin/orders/{id}/shipping-suggestions.
func (a *App) AdminShippingSuggestions(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	db := a.db.Load().WithContext(r.Context())
	var rows []orderRow
	if err := db.Raw(`SELECT `+orderCols+` FROM orders o WHERE o.id = ? AND o.deleted_at IS NULL`, id).Scan(&rows).Error; err != nil {
		log.Printf("saran ongkir: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	if len(rows) == 0 {
		writeError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	o := rows[0]
	district := strings.TrimSpace(strOr(o.DistrictCode, ""))
	regency := strings.TrimSpace(strOr(o.RegencyCode, ""))
	districtName := strOr(o.DistrictName, "")
	regencyName := strOr(o.RegencyName, o.City)

	var cands []ShippingSuggestion
	var current *regionDefaultRow
	fail := func(err error) {
		log.Printf("saran ongkir pesanan %d: %v", id, err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
	}
	if district != "" {
		def, err := loadRegionDefault(db, district)
		if err != nil {
			fail(err)
			return
		}
		if def != nil {
			current = def
			d := def.UpdatedAt
			cands = append(cands, ShippingSuggestion{Source: "default", Fee: def.Fee, Date: &d, RegionLabel: districtName})
		}
		last, err := lastConfirmedFee(db, "district_code", district, id)
		if err != nil {
			fail(err)
			return
		}
		if last != nil {
			no, d := last.OrderNo, last.CreatedAt
			cands = append(cands, ShippingSuggestion{Source: "district", Fee: last.Fee, OrderNo: &no, Date: &d, RegionLabel: strOr(last.DistrictName, districtName)})
		}
	}
	if regency != "" {
		last, err := lastConfirmedFee(db, "regency_code", regency, id)
		if err != nil {
			fail(err)
			return
		}
		if last != nil {
			no, d := last.OrderNo, last.CreatedAt
			cands = append(cands, ShippingSuggestion{Source: "regency", Fee: last.Fee, OrderNo: &no, Date: &d, RegionLabel: strOr(last.RegencyName, regencyName)})
		}
	}
	if last, err := lastConfirmedFee(db, "user_id", o.UserID, id); err != nil {
		fail(err)
		return
	} else if last != nil {
		no, d := last.OrderNo, last.CreatedAt
		cands = append(cands, ShippingSuggestion{Source: "customer", Fee: last.Fee, OrderNo: &no, Date: &d,
			RegionLabel: strOr(last.DistrictName, strOr(last.RegencyName, last.City))})
	}

	var currentDefault any
	if current != nil {
		currentDefault = map[string]any{"fee": current.Fee, "updatedAt": current.UpdatedAt}
	}
	var dc, dn any
	if district != "" {
		dc, dn = district, districtName
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"region":         map[string]any{"districtCode": dc, "districtName": dn, "regencyName": regencyName},
		"canSetDefault":  district != "",
		"currentDefault": currentDefault,
		"suggestions":    mergeSuggestions(cands),
	})
}

// setRegionDefaultTx: upsert default ongkir kecamatan pesanan DALAM transaksi konfirmasi + log aktivitas.
func (a *App) setRegionDefaultTx(tx *gorm.DB, r *http.Request, admin *User, o *orderRow, fee int64) error {
	district := strings.TrimSpace(strOr(o.DistrictCode, ""))
	if district == "" {
		return &httpError{http.StatusBadRequest, errDefaultNoDistrict.Error()}
	}
	if fee <= 0 {
		return &httpError{http.StatusBadRequest, errDefaultZeroFee.Error()}
	}
	var old []int64
	if err := tx.Raw(`SELECT shipping_fee FROM region_shipping_defaults WHERE district_code = ? FOR UPDATE`, district).Scan(&old).Error; err != nil {
		return err
	}
	districtName := strOr(o.DistrictName, district)
	if err := tx.Exec(`INSERT INTO region_shipping_defaults (district_code, district_name, regency_code, regency_name, shipping_fee, updated_by)
		VALUES (?, ?, ?, ?, ?, ?) AS new
		ON DUPLICATE KEY UPDATE district_name = new.district_name, regency_code = new.regency_code,
			regency_name = new.regency_name, shipping_fee = new.shipping_fee, updated_by = new.updated_by`,
		district, truncateUTF8(districtName, 100), o.RegencyCode, o.RegencyName, fee, admin.ID).Error; err != nil {
		return err
	}
	var from any
	if len(old) > 0 {
		from = old[0]
	}
	return a.logActivity(tx, a.reqMeta(r, LogEntry{
		UserID: uid(admin), ActorLabel: actorOf(admin), Action: "region.shipping_default_set",
		EntityType: "region", EntityID: district,
		Summary: "Default ongkir kecamatan " + districtName + " ditetapkan (pesanan " + o.OrderNo + ")",
		Details: map[string]any{"kecamatan": districtName, "kodeKecamatan": district, "kabupatenKota": strOr(o.RegencyName, ""),
			"ongkir": map[string]any{"dari": from, "menjadi": fee}, "orderNo": o.OrderNo},
	}))
}
