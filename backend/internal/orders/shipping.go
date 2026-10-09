package orders

// Saran ongkir saat admin mengonfirmasi pesanan (status pending_confirmation) dan default ongkir per
// KELURAHAN/DESA (tabel region_shipping_defaults, migrasi 021/022 lalu 025: kunci village_code).
//
// GET /api/admin/orders/{id}/shipping-suggestions — maks. 4 saran, urutan prioritas (default ditetapkan lewat
// POST /api/admin/orders/{id}/shipping-default setelah pesanan dikonfirmasi):
//   1. "default"  : default ongkir kelurahan/desa pesanan (region_shipping_defaults)
//   2. "village"  : ongkir pesanan terakhir ke kelurahan/desa yang sama
//   3. "district" : ongkir pesanan terakhir ke kecamatan yang sama (cadangan)
//   4. "regency"  : ongkir pesanan terakhir ke kab/kota yang sama
//   5. "customer" : ongkir pesanan terakhir pelanggan ini
// Pesanan sumber: bukan pesanan ini, sudah dikonfirmasi (pending_payment/paid/completed; bukan cancelled dan
// bukan pending_confirmation), shipping_fee > 0 (ongkir 0 pada pesanan lama tidak bisa dibedakan dari
// "gratis"; Rp 0 tetap tersedia sebagai opsi manual di UI), belum dihapus. Nominal yang sama digabung:
// sumber prioritas tertinggi dipakai sebagai "source", semua sumber di "sources".
// Pesanan lama tanpa village_code: lewati 1 & 2, tetap 3/4 (bila kodenya ada) dan 5.

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/audit"
	"mihanstore/internal/platform"
)

const maxShippingSuggestions = 4

var (
	errDefaultNoVillage    = errors.New("Pesanan ini tidak memiliki data kelurahan/desa, jadi default ongkir wilayah tidak bisa ditetapkan")
	errDefaultZeroFee      = errors.New("Default ongkir wilayah harus lebih dari Rp 0")
	errDefaultNotConfirmed = errors.New("Pesanan belum dikonfirmasi. Konfirmasi pesanan terlebih dahulu sebelum menjadikan ongkirnya default wilayah")
	errDefaultCancelled    = errors.New("Ongkir pesanan yang dibatalkan tidak bisa dijadikan default wilayah")
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
		if s.Fee <= 0 || s.Fee > MaxShippingFee {
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
	VillageName  *string   `gorm:"column:village_name"`
	DistrictName *string   `gorm:"column:district_name"`
	RegencyName  *string   `gorm:"column:regency_name"`
	City         string    `gorm:"column:city"`
}

// lastConfirmedFee: ongkir pesanan terakhir yang memenuhi kolom = nilai (kolom dari daftar putih).
func lastConfirmedFee(db *gorm.DB, column string, value any, excludeID uint64) (*lastFeeRow, error) {
	switch column {
	case "village_code", "district_code", "regency_code", "user_id":
	default:
		return nil, errors.New("kolom tidak diizinkan")
	}
	var rows []lastFeeRow
	err := db.Raw(`SELECT shipping_fee, order_no, created_at, village_name, district_name, regency_name, city FROM orders
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

func loadRegionDefault(db *gorm.DB, villageCode string) (*regionDefaultRow, error) {
	var rows []regionDefaultRow
	if err := db.Raw(`SELECT shipping_fee, updated_at FROM region_shipping_defaults WHERE village_code = ?`, villageCode).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// AdminShippingSuggestions: GET /api/admin/orders/{id}/shipping-suggestions.
func (a *Service) AdminShippingSuggestions(w http.ResponseWriter, r *http.Request) {
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	db := a.db().WithContext(r.Context())
	var rows []orderRow
	if err := db.Raw(`SELECT `+orderCols+` FROM orders o WHERE o.id = ? AND o.deleted_at IS NULL`, id).Scan(&rows).Error; err != nil {
		log.Printf("saran ongkir: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	if len(rows) == 0 {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	o := rows[0]
	village := strings.TrimSpace(strOr(o.VillageCode, ""))
	district := strings.TrimSpace(strOr(o.DistrictCode, ""))
	regency := strings.TrimSpace(strOr(o.RegencyCode, ""))
	villageName := strOr(o.VillageName, "")
	districtName := strOr(o.DistrictName, "")
	regencyName := strOr(o.RegencyName, o.City)

	var cands []ShippingSuggestion
	var current *regionDefaultRow
	fail := func(err error) {
		log.Printf("saran ongkir pesanan %d: %v", id, err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
	}
	addLast := func(source, column string, value any, label func(*lastFeeRow) string) bool {
		last, err := lastConfirmedFee(db, column, value, id)
		if err != nil {
			fail(err)
			return false
		}
		if last != nil {
			no, d := last.OrderNo, last.CreatedAt
			cands = append(cands, ShippingSuggestion{Source: source, Fee: last.Fee, OrderNo: &no, Date: &d, RegionLabel: label(last)})
		}
		return true
	}
	if village != "" {
		def, err := loadRegionDefault(db, village)
		if err != nil {
			fail(err)
			return
		}
		if def != nil {
			current = def
			d := def.UpdatedAt
			cands = append(cands, ShippingSuggestion{Source: "default", Fee: def.Fee, Date: &d, RegionLabel: villageName})
		}
		if !addLast("village", "village_code", village, func(l *lastFeeRow) string { return strOr(l.VillageName, villageName) }) {
			return
		}
	}
	if district != "" && !addLast("district", "district_code", district, func(l *lastFeeRow) string { return strOr(l.DistrictName, districtName) }) {
		return
	}
	if regency != "" && !addLast("regency", "regency_code", regency, func(l *lastFeeRow) string { return strOr(l.RegencyName, regencyName) }) {
		return
	}
	if !addLast("customer", "user_id", o.UserID, func(l *lastFeeRow) string {
		return strOr(l.VillageName, strOr(l.DistrictName, strOr(l.RegencyName, l.City)))
	}) {
		return
	}

	var currentDefault any
	if current != nil {
		currentDefault = map[string]any{"fee": current.Fee, "updatedAt": current.UpdatedAt}
	}
	var vc, vn any
	if village != "" {
		vc, vn = village, villageName
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{
		"region":         map[string]any{"villageCode": vc, "villageName": vn, "districtName": districtName, "regencyName": regencyName},
		"canSetDefault":  village != "",
		"currentDefault": currentDefault,
		"suggestions":    mergeSuggestions(cands),
	})
}

// AdminSetShippingDefault: POST /api/admin/orders/{id}/shipping-default (body kosong / {}).
// Menetapkan default ongkir kecamatan dari PESANAN itu: kecamatan & ongkir dibaca dari baris pesanan di DB
// (nominal TIDAK diterima dari klien). Syarat: pesanan ada, punya kecamatan, ongkir > 0, sudah dikonfirmasi
// (pending_payment/paid/completed). Upsert + log region.shipping_default_set dalam satu transaksi.
// Klik ganda idempoten: nominal sama dengan default -> 200 tanpa menulis ulang/log.
func (a *Service) AdminSetShippingDefault(w http.ResponseWriter, r *http.Request) {
	admin := a.admin(r)
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	var in struct{}
	if err := platform.DecodeJSON(w, r, &in, true); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	var out map[string]any
	db := a.db().WithContext(r.Context())
	err := db.Transaction(func(tx *gorm.DB) error {
		o, err := lockOrderByID(tx, id)
		if err != nil {
			return err
		}
		village := strings.TrimSpace(strOr(o.VillageCode, ""))
		if village == "" {
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: errDefaultNoVillage.Error()}
		}
		switch o.Status {
		case StatusPending, StatusPaid, StatusCompleted:
		case StatusPendingConfirmation:
			return &platform.HTTPError{Status: http.StatusConflict, Msg: errDefaultNotConfirmed.Error()}
		default:
			return &platform.HTTPError{Status: http.StatusConflict, Msg: errDefaultCancelled.Error()}
		}
		if o.ShippingFee <= 0 {
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: errDefaultZeroFee.Error()}
		}
		prev, err := a.setRegionDefaultTx(tx, r, admin, o)
		if err != nil {
			return err
		}
		out = map[string]any{"villageCode": village, "villageName": strOr(o.VillageName, village), "districtName": strOr(o.DistrictName, ""),
			"shippingFee": o.ShippingFee, "previousFee": prev}
		return nil
	})
	if err != nil {
		a.respondTxError(w, err, "default ongkir wilayah")
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

// setRegionDefaultTx: upsert default ongkir KELURAHAN pesanan (ongkir dari baris pesanan) + log aktivitas,
// di dalam transaksi pemanggil. Mengembalikan ongkir default sebelumnya (nil bila belum ada).
func (a *Service) setRegionDefaultTx(tx *gorm.DB, r *http.Request, admin *Person, o *orderRow) (any, error) {
	village := strings.TrimSpace(strOr(o.VillageCode, ""))
	fee := o.ShippingFee
	var old []int64
	if err := tx.Raw(`SELECT shipping_fee FROM region_shipping_defaults WHERE village_code = ? FOR UPDATE`, village).Scan(&old).Error; err != nil {
		return nil, err
	}
	var from any
	if len(old) > 0 {
		from = old[0]
		if old[0] == fee {
			return from, nil // sudah menjadi default (mis. klik ganda): tidak ada perubahan
		}
	}
	villageName := strOr(o.VillageName, village)
	districtName := strOr(o.DistrictName, "")
	if err := tx.Exec(`INSERT INTO region_shipping_defaults (village_code, village_name, district_code, district_name, regency_code, regency_name, shipping_fee, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) AS new
		ON DUPLICATE KEY UPDATE village_name = new.village_name, district_code = new.district_code, district_name = new.district_name,
			regency_code = new.regency_code, regency_name = new.regency_name, shipping_fee = new.shipping_fee, updated_by = new.updated_by`,
		village, platform.TruncateUTF8(villageName, 100), o.DistrictCode, o.DistrictName, o.RegencyCode, o.RegencyName, fee, admin.ID).Error; err != nil {
		return nil, err
	}
	summary := "Default ongkir kelurahan/desa " + villageName
	if districtName != "" {
		summary += " (Kec. " + districtName + ")"
	}
	return from, a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
		UserID: admin.uid(), ActorLabel: admin.label(), Action: "region.shipping_default_set",
		EntityType: "region", EntityID: village,
		Summary: platform.TruncateUTF8(summary+" ditetapkan (pesanan "+o.OrderNo+")", 255),
		Details: map[string]any{"kelurahan": villageName, "kodeKelurahan": village, "kecamatan": districtName,
			"kabupatenKota": strOr(o.RegencyName, ""), "ongkir": map[string]any{"dari": from, "menjadi": fee}, "orderNo": o.OrderNo},
	}))
}
