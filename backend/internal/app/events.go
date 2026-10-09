package app

// Adaptor perakitan untuk modul pesanan:
// - orderEvents: implementasi orders.Events — notifikasi Discord (notify) dan Web Push admin/pelanggan (push)
//   SETELAH commit. Ringkasan pesanan dibaca ulang dari DB (baca saja); nama pemesan memakai alias internal
//   bila ada (identity.DisplayCustomerName).
// - cartReader (orders.CartReader) dan catalogPricing (orders.Pricing): jembatan ke modul cart dan catalog.

import (
	"errors"
	"log"
	"strconv"

	"gorm.io/gorm"

	"mihanstore/internal/cart"
	"mihanstore/internal/catalog"
	"mihanstore/internal/identity"
	"mihanstore/internal/notify"
	"mihanstore/internal/orders"
	"mihanstore/internal/push"
)

type orderEvents struct{ a *App }

func (e orderEvents) OrderCreated(db *gorm.DB, id uint64) {
	e.a.notifyOrder(db, notify.KindCreated, id)
	e.a.pushOrder(db, push.KindCreated, id)
}

func (e orderEvents) OrderCancelledByCustomer(db *gorm.DB, id uint64) {
	e.a.notifyOrder(db, notify.KindCancelled, id)
	e.a.pushOrder(db, push.KindCancelled, id)
}

func (e orderEvents) PaymentProofUploaded(db *gorm.DB, id uint64) {
	e.a.notifyOrder(db, notify.KindPaymentProof, id)
	e.a.pushOrder(db, push.KindPaymentProof, id)
}

func (e orderEvents) PricingSet(db *gorm.DB, id uint64) {
	e.a.pushCustomer(db, push.KindCustomerPricing, id)
}

func (e orderEvents) OrderPaid(db *gorm.DB, id uint64) {
	e.a.notifyOrder(db, notify.KindPaid, id)
	e.a.pushCustomer(db, push.KindCustomerPaid, id)
}

func (e orderEvents) OrderCompleted(db *gorm.DB, id uint64) {
	e.a.pushCustomer(db, push.KindCustomerCompleted, id)
}

func (e orderEvents) OrderCancelledByAdmin(db *gorm.DB, id uint64) {
	e.a.notifyOrder(db, notify.KindCancelled, id)
	e.a.pushCustomer(db, push.KindCustomerCancelled, id)
}

// ---------- Discord & Web Push ----------

func (a *App) adminOrderURL(id uint64) string {
	return a.cfg.PublicBaseURL + "/admin/orders/" + strconv.FormatUint(id, 10)
}

// notifyOrder dipanggil SETELAH commit. Membaca ringkasan minimal lalu mengirim asinkron.
func (a *App) notifyOrder(db *gorm.DB, kind string, orderID uint64) {
	if _, off := a.notifier.(notify.Noop); off || a.notifier == nil {
		return
	}
	var row struct {
		OrderNo   string  `gorm:"column:order_no"`
		Status    string  `gorm:"column:status"`
		Total     int64   `gorm:"column:total"`
		Name      string  `gorm:"column:name"`
		Alias     *string `gorm:"column:alias"`
		ItemCount int64   `gorm:"column:item_count"`
	}
	err := db.Raw(`SELECT o.order_no, o.status, o.total, u.name, u.alias,
		(SELECT COALESCE(SUM(qty), 0) FROM order_items oi WHERE oi.order_id = o.id) AS item_count
		FROM orders o JOIN users u ON u.id = o.user_id WHERE o.id = ?`, orderID).Scan(&row).Error
	if err != nil || row.OrderNo == "" {
		log.Printf("notifikasi pesanan %d: gagal membaca ringkasan: %v", orderID, err)
		return
	}
	a.notifier.OrderEvent(notify.OrderEvent{
		// Pemesan: alias internal bila ada (kanal Discord hanya untuk pengelola), selain itu nama akun.
		Kind: kind, OrderNo: row.OrderNo, CustomerName: identity.DisplayCustomerName(row.Name, row.Alias), ItemCount: int(row.ItemCount),
		Total: uint64(row.Total), Status: row.Status, AdminURL: a.adminOrderURL(orderID),
	})
}

// pushOrder dipanggil SETELAH commit (di titik yang sama dengan notifyOrder untuk pesanan baru dan
// pembatalan oleh pelanggan). Membaca nomor pesanan, nama pemesan (alias bila ada — logika sama dengan
// notifikasi Discord & daftar admin, displayCustomerName) dan nama penerima; pengiriman asinkron di paket push.
func (a *App) pushOrder(db *gorm.DB, kind string, orderID uint64) {
	if a.pusher == nil || !a.pusher.Enabled() {
		return
	}
	var row struct {
		OrderNo   string  `gorm:"column:order_no"`
		Recipient string  `gorm:"column:recipient_name"`
		Name      string  `gorm:"column:name"`
		Alias     *string `gorm:"column:alias"`
	}
	if err := db.Raw(`SELECT o.order_no, o.recipient_name, u.name, u.alias
		FROM orders o JOIN users u ON u.id = o.user_id WHERE o.id = ?`, orderID).Scan(&row).Error; err != nil || row.OrderNo == "" {
		log.Printf("web push pesanan %d: gagal membaca pesanan: %v", orderID, err)
		return
	}
	a.pusher.OrderEvent(push.Event{Kind: kind, OrderNo: row.OrderNo,
		Customer: identity.DisplayCustomerName(row.Name, row.Alias), Recipient: row.Recipient})
}

// pushCustomer dipanggil SETELAH commit perubahan oleh admin (harga/ongkir, dibayar, selesai, dibatalkan
// admin). Hanya ke langganan pelanggan PEMILIK pesanan (orders.user_id); asinkron di paket push.
func (a *App) pushCustomer(db *gorm.DB, kind string, orderID uint64) {
	if a.pusher == nil || !a.pusher.Enabled() {
		return
	}
	var row struct {
		OrderNo     string `gorm:"column:order_no"`
		UserID      uint64 `gorm:"column:user_id"`
		ShippingFee int64  `gorm:"column:shipping_fee"`
		Total       int64  `gorm:"column:total"`
	}
	// Nominal dibaca dari DB setelah commit (bukan dari input permintaan).
	if err := db.Raw(`SELECT order_no, user_id, shipping_fee, total FROM orders WHERE id = ?`, orderID).Scan(&row).Error; err != nil || row.OrderNo == "" || row.UserID == 0 {
		log.Printf("web push pelanggan, pesanan %d: gagal membaca pesanan: %v", orderID, err)
		return
	}
	e := push.Event{Kind: kind, OrderNo: row.OrderNo}
	if kind == push.KindCustomerPricing {
		e.ShippingFee, e.Total = row.ShippingFee, row.Total
	}
	a.pusher.CustomerOrderEvent(row.UserID, e)
}

// ---------- Keranjang & harga ----------

type cartReader struct{ c *cart.Service }

func (r cartReader) Lock(tx *gorm.DB, userID uint64) (uint64, error) { return cart.Lock(tx, userID) }

// Snapshot: keranjang terkini (Load + harga terlihat + foto), sama seperti respons GET /api/cart.
func (r cartReader) Snapshot(db *gorm.DB, userID uint64) (any, error) {
	c, err := r.c.Load(db, userID)
	if err != nil {
		return nil, err
	}
	cart.FillUnseenPrices(db, c)
	r.c.AttachImages(c)
	return c, nil
}

type catalogPricing struct{}

func (catalogPricing) UnitPrices(tx *gorm.DB, lines []orders.PriceLine) ([]orders.PriceResult, error) {
	ids := make([]uint64, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.ProductID)
	}
	tiers, err := catalog.LoadTiersFor(tx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]orders.PriceResult, len(lines))
	for i, l := range lines {
		out[i].UnitPrice, out[i].TierMinQty = catalog.EffectiveUnitPrice(l.BasePrice, tiers[l.ProductID], l.Qty)
	}
	return out, nil
}

func (catalogPricing) CheckExpectedTotal(expected *int64, total int64) error {
	return catalog.CheckExpectedTotal(expected, total)
}

func (catalogPricing) IsPriceChanged(err error) bool { return errors.Is(err, catalog.ErrPriceChanged) }

func (catalogPricing) PriceChangedMessage() string { return catalog.MsgPriceChanged }
