package orders

// Akses data pesanan: baris orders/order_items/riwayat status/pengaturan toko (SQL eksplisit; transaksi
// dibuka oleh pemanggil, kunci baris FOR UPDATE tetap di sini).

import (
	"net/http"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/platform"
)

// orderRow adalah satu baris tabel orders (dibaca lewat Raw/Scan).
type orderRow struct {
	ID             uint64     `gorm:"column:id"`
	OrderNo        string     `gorm:"column:order_no"`
	UserID         uint64     `gorm:"column:user_id"`
	Status         string     `gorm:"column:status"`
	Subtotal       int64      `gorm:"column:subtotal"`
	Discount       int64      `gorm:"column:discount"`
	DiscountNote   *string    `gorm:"column:discount_note"`
	ShippingFee    int64      `gorm:"column:shipping_fee"`
	Total          int64      `gorm:"column:total"`
	PaymentMethod  string     `gorm:"column:payment_method"`
	RecipientName  string     `gorm:"column:recipient_name"`
	RecipientPhone string     `gorm:"column:recipient_phone"`
	Address        string     `gorm:"column:address"`
	City           string     `gorm:"column:city"`
	PostalCode     *string    `gorm:"column:postal_code"`
	CustomerNote   *string    `gorm:"column:customer_note"`
	AdminNote      *string    `gorm:"column:admin_note"`
	PaidAt         *time.Time `gorm:"column:paid_at"`
	PaidBy         *uint64    `gorm:"column:paid_by"`
	PaymentNote    *string    `gorm:"column:payment_note"`
	CompletedAt    *time.Time `gorm:"column:completed_at"`
	CancelledAt    *time.Time `gorm:"column:cancelled_at"`
	CancelledBy    *uint64    `gorm:"column:cancelled_by"`
	CancelReason   *string    `gorm:"column:cancel_reason"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	// Wilayah (migrations/014). NULL untuk pesanan sebelum fitur data wilayah.
	ProvinceCode *string `gorm:"column:province_code"`
	ProvinceName *string `gorm:"column:province_name"`
	RegencyCode  *string `gorm:"column:regency_code"`
	RegencyName  *string `gorm:"column:regency_name"`
	DistrictCode *string `gorm:"column:district_code"`
	DistrictName *string `gorm:"column:district_name"`
	VillageCode  *string `gorm:"column:village_code"`
	VillageName  *string `gorm:"column:village_name"`
}

const orderCols = `o.id, o.order_no, o.user_id, o.status, o.subtotal, o.discount, o.discount_note, o.shipping_fee,
  o.total, o.payment_method, o.recipient_name, o.recipient_phone, o.address, o.city, o.postal_code,
  o.customer_note, o.admin_note, o.paid_at, o.paid_by, o.payment_note, o.completed_at, o.cancelled_at,
  o.cancelled_by, o.cancel_reason, o.created_at, o.updated_at, o.province_code, o.province_name, o.regency_code,
  o.regency_name, o.district_code, o.district_name, o.village_code, o.village_name`

type OrderItemDTO struct {
	ProductID *uint64 `json:"productId" gorm:"column:product_id"`
	Name      string  `json:"name" gorm:"column:product_name"`
	UnitPrice int64   `json:"unitPrice" gorm:"column:unit_price"` // harga efektif yang dipakai (snapshot)
	Qty       int64   `json:"qty" gorm:"column:qty"`
	LineTotal int64   `json:"lineTotal" gorm:"column:line_total"`
	// Snapshot harga grosir & satuan (NULL untuk pesanan sebelum fitur ini).
	Unit          *string `json:"unit" gorm:"column:unit"`
	BaseUnitPrice *int64  `json:"baseUnitPrice" gorm:"column:base_unit_price"`
	TierMinQty    *int64  `json:"tierMinQty" gorm:"column:tier_min_qty"`
}

type historyRow struct {
	FromStatus  *string   `gorm:"column:from_status"`
	ToStatus    string    `gorm:"column:to_status"`
	ActorUserID *uint64   `gorm:"column:actor_user_id"`
	ActorLabel  string    `gorm:"column:actor_label"`
	Note        *string   `gorm:"column:note"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func loadOrderItems(db *gorm.DB, orderID uint64) ([]OrderItemDTO, error) {
	items := []OrderItemDTO{}
	err := db.Raw(`SELECT product_id, product_name, unit_price, qty, line_total, unit, base_unit_price, tier_min_qty FROM order_items
		WHERE order_id = ? AND deleted_at IS NULL ORDER BY id`, orderID).Scan(&items).Error
	return items, err
}

func loadHistory(db *gorm.DB, orderID uint64) ([]historyRow, error) {
	var rows []historyRow
	err := db.Raw(`SELECT from_status, to_status, actor_user_id, actor_label, note, created_at FROM order_status_history
		WHERE order_id = ? AND deleted_at IS NULL ORDER BY created_at, id`, orderID).Scan(&rows).Error
	return rows, err
}

// findCustomerOrder: hanya pesanan milik userID. Tidak ada / milik orang lain -> ErrRecordNotFound.
func findCustomerOrder(db *gorm.DB, userID uint64, orderNo string, lock bool) (*orderRow, error) {
	if !ValidOrderNo(orderNo) {
		return nil, gorm.ErrRecordNotFound
	}
	q := `SELECT ` + orderCols + ` FROM orders o WHERE o.order_no = ? AND o.user_id = ? AND o.deleted_at IS NULL`
	if lock {
		q += ` FOR UPDATE`
	}
	var rows []orderRow
	if err := db.Raw(q, orderNo, userID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &rows[0], nil
}

// existingOrderByKey mencari pesanan dengan idempotency key yang sama dari pengguna yang sama.
func existingOrderByKey(db *gorm.DB, userID uint64, key string) (*orderRow, error) {
	var rows []orderRow
	if err := db.Raw(`SELECT `+orderCols+` FROM orders o WHERE o.user_id = ? AND o.idempotency_key = ? AND o.deleted_at IS NULL`,
		userID, key).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func findOrderByID(db *gorm.DB, id uint64) (*orderRow, error) {
	var rows []orderRow
	if err := db.Raw(`SELECT `+orderCols+` FROM orders o WHERE o.id = ? AND o.deleted_at IS NULL`, id).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &rows[0], nil
}

func loadSettings(db *gorm.DB) (map[string]string, error) {
	var rows []struct {
		Key   string `gorm:"column:setting_key"`
		Value string `gorm:"column:setting_value"`
	}
	if err := db.Raw(`SELECT setting_key, setting_value FROM site_settings WHERE deleted_at IS NULL`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, k := range settingKeys {
		out[k] = ""
	}
	for _, r := range rows {
		if _, ok := out[r.Key]; ok {
			out[r.Key] = settingValue(r.Value)
		}
	}
	return out, nil
}

func userLabel(db *gorm.DB, id *uint64) *string {
	if id == nil {
		return nil
	}
	var rows []struct {
		Email    string `gorm:"column:email"`
		Username string `gorm:"column:username"`
	}
	if db.Raw(`SELECT email, username FROM users WHERE id = ?`, *id).Scan(&rows).Error != nil || len(rows) == 0 {
		return nil
	}
	s := rows[0].Username
	if rows[0].Email != "" {
		s = rows[0].Email
	}
	return &s
}

func lockOrderByID(tx *gorm.DB, id uint64) (*orderRow, error) {
	var rows []orderRow
	if err := tx.Raw(`SELECT `+orderCols+` FROM orders o WHERE o.id = ? AND o.deleted_at IS NULL FOR UPDATE`, id).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, &platform.HTTPError{Status: http.StatusNotFound, Msg: msgOrderMissing}
	}
	return &rows[0], nil
}
