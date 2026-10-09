package app

import "time"

// Salinan baris pesanan untuk pemeriksaan tes integrasi (tipe asli internal/orders tidak diekspor).

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

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
