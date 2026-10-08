package main

// Pesanan pelanggan: checkout dari keranjang (satu transaksi), daftar & detail pesanan milik
// sendiri, pembatalan oleh pemilik, dan info pembayaran toko.
//
// Keamanan:
// - Item, harga, dan total SELALU diambil dari database di server (bukan dari body).
// - Pesanan orang lain dijawab 404 persis seperti pesanan yang tidak ada (anti-IDOR).
// - Notifikasi Discord dikirim setelah commit, asinkron, tanpa data pribadi selain nama.
// - Web Push admin (pesanan baru / dibatalkan pelanggan) juga setelah commit, asinkron, hanya nomor pesanan.

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"gorm.io/gorm"

	"mihanstore/notify"
	"mihanstore/push"
)

const (
	msgOrderMissing     = "Pesanan tidak ditemukan"
	msgTooManyCheckout  = "Terlalu banyak percobaan membuat pesanan. Coba lagi sebentar lagi."
	msgTooManyCancel    = "Terlalu banyak percobaan pembatalan. Coba lagi sebentar lagi."
	msgCartEmpty        = "Keranjang masih kosong"
	msgCartUnavailable  = "Beberapa produk di keranjang sudah tidak tersedia. Hapus produk yang ditandai lalu coba lagi."
	msgStatusChanged    = "Status pesanan sudah berubah. Muat ulang halaman lalu coba lagi."
	msgOrderTooBig      = "Total pesanan terlalu besar. Silakan bagi menjadi beberapa pesanan."
	actorLabelCustomer  = "pelanggan"
	customerOrdersLimit = 50
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

type HistoryDTO struct {
	From      *string   `json:"from"`
	To        string    `json:"to"`
	ToLabel   string    `json:"toLabel"`
	Actor     string    `json:"actor"`
	Note      *string   `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
}

type RecipientDTO struct {
	Name       string  `json:"name"`
	Phone      string  `json:"phone"`
	Address    string  `json:"address"` // pesanan baru: alamat lengkap (jalan, RT/RW, nomor)
	City       string  `json:"city"`    // pesanan baru: nama kab/kota (kompatibel)
	PostalCode *string `json:"postalCode"`
	// Wilayah (kode + nama dari data DB saat checkout); null untuk pesanan lama.
	Region *RegionDTO `json:"region"`
	// Alamat tersusun: "<alamat>, <kel/desa>, Kec. <kecamatan>, <kab/kota>, <provinsi> <kode pos>".
	FullAddress string `json:"fullAddress"`
}

// CustomerOrderDTO: tampilan pesanan untuk pemiliknya (tanpa catatan admin/pembayaran internal).
type CustomerOrderDTO struct {
	OrderNo       string         `json:"orderNo"`
	Status        string         `json:"status"`
	StatusLabel   string         `json:"statusLabel"`
	Subtotal      int64          `json:"subtotal"`
	Discount      int64          `json:"discount"`
	DiscountNote  *string        `json:"discountNote"`
	ShippingFee   int64          `json:"shippingFee"`
	Total         int64          `json:"total"`
	PaymentMethod string         `json:"paymentMethod"`
	Recipient     RecipientDTO   `json:"recipient"`
	Note          *string        `json:"note"`
	CancelReason  *string        `json:"cancelReason"`
	ItemCount     int64          `json:"itemCount"`
	Items         []OrderItemDTO `json:"items"`
	History       []HistoryDTO   `json:"history"`
	CanCancel     bool           `json:"canCancel"`
	CreatedAt     time.Time      `json:"createdAt"`
	PaidAt        *time.Time     `json:"paidAt"`
	CompletedAt   *time.Time     `json:"completedAt"`
	CancelledAt   *time.Time     `json:"cancelledAt"`
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

func sumQty(items []OrderItemDTO) int64 {
	var n int64
	for _, it := range items {
		n += it.Qty
	}
	return n
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// regionOf: wilayah pesanan, nil bila pesanan lama (tanpa kode kelurahan/desa).
func regionOf(o *orderRow) *RegionDTO {
	if o.VillageCode == nil || o.ProvinceCode == nil {
		return nil
	}
	return &RegionDTO{
		Province: RegionRef{Code: deref(o.ProvinceCode), Name: deref(o.ProvinceName)},
		Regency:  RegionRef{Code: deref(o.RegencyCode), Name: deref(o.RegencyName)},
		District: RegionRef{Code: deref(o.DistrictCode), Name: deref(o.DistrictName)},
		Village:  RegionRef{Code: deref(o.VillageCode), Name: deref(o.VillageName)},
	}
}

func recipientOf(o *orderRow) RecipientDTO {
	reg := regionOf(o)
	return RecipientDTO{Name: o.RecipientName, Phone: o.RecipientPhone, Address: o.Address, City: o.City, PostalCode: o.PostalCode,
		Region: reg, FullAddress: composeAddress(o.Address, reg, o.City, o.PostalCode)}
}

// findCustomerOrder: hanya pesanan milik userID. Tidak ada / milik orang lain -> ErrRecordNotFound.
func findCustomerOrder(db *gorm.DB, userID uint64, orderNo string, lock bool) (*orderRow, error) {
	if !validOrderNo(orderNo) {
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

func (a *App) customerOrderDTO(db *gorm.DB, o *orderRow) (*CustomerOrderDTO, error) {
	items, err := loadOrderItems(db, o.ID)
	if err != nil {
		return nil, err
	}
	hist, err := loadHistory(db, o.ID)
	if err != nil {
		return nil, err
	}
	h := make([]HistoryDTO, 0, len(hist))
	for _, x := range hist {
		actor := "Admin toko"
		if x.ActorUserID != nil && *x.ActorUserID == o.UserID {
			actor = "Anda"
		}
		var note *string
		if x.ToStatus == StatusCancelled {
			note = x.Note // alasan pembatalan boleh dilihat pelanggan; catatan lain tidak
		}
		h = append(h, HistoryDTO{From: x.FromStatus, To: x.ToStatus, ToLabel: statusLabel(x.ToStatus), Actor: actor, Note: note, CreatedAt: x.CreatedAt})
	}
	return &CustomerOrderDTO{
		OrderNo: o.OrderNo, Status: o.Status, StatusLabel: statusLabel(o.Status),
		Subtotal: o.Subtotal, Discount: o.Discount, DiscountNote: o.DiscountNote, ShippingFee: o.ShippingFee, Total: o.Total,
		PaymentMethod: o.PaymentMethod, Recipient: recipientOf(o), Note: o.CustomerNote, CancelReason: o.CancelReason,
		ItemCount: sumQty(items), Items: items, History: h, CanCancel: canOwnerCancel(o.Status),
		CreatedAt: o.CreatedAt, PaidAt: o.PaidAt, CompletedAt: o.CompletedAt, CancelledAt: o.CancelledAt,
	}, nil
}

// ---------- Notifikasi ----------

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
		Kind: kind, OrderNo: row.OrderNo, CustomerName: displayCustomerName(row.Name, row.Alias), ItemCount: int(row.ItemCount),
		Total: uint64(row.Total), Status: row.Status, AdminURL: a.adminOrderURL(orderID),
	})
}

// ---------- Checkout ----------

type cartLine struct {
	ProductID uint64 `gorm:"column:product_id"`
	Name      string `gorm:"column:name"`
	Price     int64  `gorm:"column:price"` // harga dasar
	Unit      string `gorm:"column:unit"`
	Qty       int64  `gorm:"column:qty"`
	Available bool   `gorm:"column:available"`

	unitPrice  int64 // harga efektif (effectiveUnitPrice)
	tierMinQty int64 // 0 = harga eceran
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

// CreateOrder: POST /api/orders. Item & harga dari keranjang di server; satu transaksi.
func (a *App) CreateOrder(w http.ResponseWriter, r *http.Request) {
	u := customerFrom(r.Context())
	var in CheckoutInput
	if err := decodeJSONLenient(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	f, err := validateCheckout(in)
	if err != nil {
		respondFieldError(w, err)
		return
	}
	db := a.db.Load().WithContext(r.Context())

	// Kunci idempotensi yang sudah dipakai -> kembalikan pesanan yang sama (200), tanpa
	// menghabiskan kuota batas laju.
	if ex, err := existingOrderByKey(db, u.ID, f.IdemKey); err != nil {
		log.Printf("checkout: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	} else if ex != nil {
		a.respondCustomerOrder(w, db, ex, http.StatusOK)
		return
	}
	if !limitUser(w, a.checkoutLimiter, u, msgTooManyCheckout) {
		return
	}
	// Wilayah: kode dari klien, NAMA selalu dari data wilayah di database (bukan dari klien).
	region, err := a.resolveRegionChain(r.Context(), db, f.Region)
	if err != nil {
		if errors.Is(err, errRegionUnavailable) {
			writeError(w, http.StatusServiceUnavailable, msgRegionUnavailable+". Silakan coba lagi nanti atau hubungi toko.")
			return
		}
		var fe *fieldError
		if errors.As(err, &fe) {
			respondFieldError(w, fe)
			return
		}
		log.Printf("checkout wilayah: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	city := truncateUTF8(region.Regency.Name, 100)

	var orderID uint64
	var replay *orderRow
	err = db.Transaction(func(tx *gorm.DB) error {
		cartID, err := lockCart(tx, u.ID)
		if err != nil {
			return err
		}
		// Periksa ulang di dalam kunci (permintaan ganda yang berjalan bersamaan).
		if ex, err := existingOrderByKey(tx, u.ID, f.IdemKey); err != nil {
			return err
		} else if ex != nil {
			replay = ex
			return nil
		}
		if cartID == 0 {
			return &httpError{http.StatusBadRequest, msgCartEmpty}
		}
		var lines []cartLine
		if err := tx.Raw(`SELECT ci.product_id, p.name, p.price, p.unit, ci.qty,
			(p.is_active = 1 AND p.deleted_at IS NULL AND c.deleted_at IS NULL) AS available
			FROM cart_items ci JOIN products p ON p.id = ci.product_id JOIN categories c ON c.id = p.category_id
			WHERE ci.cart_id = ? AND ci.deleted_at IS NULL ORDER BY ci.id FOR SHARE`, cartID).Scan(&lines).Error; err != nil {
			return err
		}
		if len(lines) == 0 {
			return &httpError{http.StatusBadRequest, msgCartEmpty}
		}
		if len(lines) > maxCartLines {
			return &httpError{http.StatusBadRequest, "Keranjang maksimal berisi 50 produk berbeda"}
		}
		var subtotal, itemCount, tierLines int64
		productIDs := make([]uint64, 0, len(lines))
		for _, l := range lines {
			productIDs = append(productIDs, l.ProductID)
		}
		tiers, err := loadTiersFor(tx, productIDs)
		if err != nil {
			return err
		}
		for i := range lines {
			l := &lines[i]
			if !l.Available {
				return &httpError{http.StatusConflict, msgCartUnavailable}
			}
			if l.Qty < 1 || l.Qty > maxItemQty {
				return &httpError{http.StatusBadRequest, "Jumlah per produk harus 1 sampai 999"}
			}
			l.unitPrice, l.tierMinQty = effectiveUnitPrice(l.Price, tiers[l.ProductID], l.Qty)
			if l.tierMinQty > 0 {
				tierLines++
			}
			subtotal += l.unitPrice * l.Qty
			itemCount += l.Qty
		}
		if subtotal <= 0 {
			return &httpError{http.StatusBadRequest, "Total pesanan harus lebih dari 0"}
		}
		if subtotal > maxOrderSubtotal {
			return &httpError{http.StatusBadRequest, msgOrderTooBig}
		}
		total, err := computeTotal(subtotal, 0, 0)
		if err != nil {
			return &httpError{http.StatusBadRequest, err.Error()}
		}
		// Total yang dilihat pelanggan harus sama dengan hitungan server (harga bisa berubah
		// setelah halaman dibuka). Berbeda -> batal tanpa menulis apa pun, keranjang tetap.
		if err := checkExpectedTotal(in.ExpectedTotal, total); err != nil {
			return err
		}
		now := a.now()
		if err := tx.Exec(`INSERT INTO orders (user_id, status, subtotal, discount, shipping_fee, total, payment_method,
			recipient_name, recipient_phone, address, city, postal_code, customer_note, idempotency_key,
			province_code, province_name, regency_code, regency_name, district_code, district_name, village_code, village_name,
			created_at, updated_at)
			VALUES (?, ?, ?, 0, 0, ?, 'bank_transfer', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			u.ID, StatusPendingConfirmation, subtotal, total, f.Name, f.Phone, f.Address, city, f.PostalCode, f.Note, f.IdemKey,
			region.Province.Code, truncateUTF8(region.Province.Name, 100), region.Regency.Code, truncateUTF8(region.Regency.Name, 100),
			region.District.Code, truncateUTF8(region.District.Name, 100), region.Village.Code, truncateUTF8(region.Village.Name, 100),
			now, now).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT LAST_INSERT_ID()`).Scan(&orderID).Error; err != nil || orderID == 0 {
			return errors.New("gagal membaca id pesanan")
		}
		orderNo := orderNumber(orderID, now)
		if err := tx.Exec(`UPDATE orders SET order_no = ? WHERE id = ?`, orderNo, orderID).Error; err != nil {
			return err
		}
		for _, l := range lines {
			pid := l.ProductID
			var tierMin *int64
			if l.tierMinQty > 0 {
				m := l.tierMinQty
				tierMin = &m
			}
			if err := tx.Exec(`INSERT INTO order_items (order_id, product_id, product_name, unit_price, base_unit_price, tier_min_qty, unit,
				qty, line_total, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				orderID, pid, l.Name, l.unitPrice, l.Price, tierMin, l.Unit, l.Qty, l.unitPrice*l.Qty, now, now).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`INSERT INTO order_status_history (order_id, from_status, to_status, actor_user_id, actor_label, note, created_at)
			VALUES (?, NULL, ?, ?, ?, NULL, ?)`, orderID, StatusPendingConfirmation, u.ID, truncateUTF8(actorLabelCustomer+":"+u.Username, 100), now).Error; err != nil {
			return err
		}
		if err := a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(u), ActorLabel: actorOf(u), Action: "order.create",
			EntityType: "order", EntityID: orderNo,
			Summary: "Pesanan dibuat: " + orderNo,
			Details: map[string]any{"orderNo": orderNo, "jumlahItem": itemCount, "jumlahProduk": len(lines),
				"subtotal": subtotal, "total": total, "produkId": productIDs, "barisHargaGrosir": tierLines},
		})); err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM cart_items WHERE cart_id = ?`, cartID).Error
	})
	if err != nil {
		if isDuplicateKey(err) {
			// Permintaan kembar yang lolos bersamaan: kembalikan pesanan pertama.
			if ex, e2 := existingOrderByKey(db, u.ID, f.IdemKey); e2 == nil && ex != nil {
				a.respondCustomerOrder(w, db, ex, http.StatusOK)
				return
			}
		}
		if errors.Is(err, errPriceChanged) {
			a.respondPriceChanged(w, r, db, u.ID)
			return
		}
		a.respondTxError(w, err, "checkout")
		return
	}
	if replay != nil {
		a.respondCustomerOrder(w, db, replay, http.StatusOK)
		return
	}
	a.notifyOrder(db, notify.KindCreated, orderID)
	a.pushOrder(db, push.KindCreated, orderID)
	o, err := findOrderByID(db, orderID)
	if err != nil {
		writeJSON(w, http.StatusCreated, map[string]any{"success": true})
		return
	}
	a.respondCustomerOrder(w, db, o, http.StatusCreated)
}

// respondFieldError: *fieldError -> 422 {error, field}; galat lain -> 422 {error}.
func respondFieldError(w http.ResponseWriter, err error) {
	var fe *fieldError
	if errors.As(err, &fe) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": fe.Msg, "field": fe.Field})
		return
	}
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
}

// respondPriceChanged: 409 {error:"price_changed", message, cart} — pesanan TIDAK dibuat dan
// keranjang tidak diubah; pelanggan memeriksa keranjang terkini lalu mengonfirmasi ulang.
func (a *App) respondPriceChanged(w http.ResponseWriter, r *http.Request, db *gorm.DB, userID uint64) {
	c, err := loadCart(db, userID)
	if err != nil {
		log.Printf("checkout (keranjang terkini): %v", err)
		writeJSON(w, http.StatusConflict, map[string]any{"error": "price_changed", "message": msgPriceChanged})
		return
	}
	fillUnseenPrices(db, c)
	a.attachCartImages(c)
	writeJSON(w, http.StatusConflict, map[string]any{"error": "price_changed", "message": msgPriceChanged, "cart": c})
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

func (a *App) respondCustomerOrder(w http.ResponseWriter, db *gorm.DB, o *orderRow, status int) {
	dto, err := a.customerOrderDTO(db, o)
	if err != nil {
		log.Printf("pesanan: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, status, dto)
}

// ---------- Daftar & detail milik sendiri ----------

type CustomerOrderSummary struct {
	OrderNo     string    `json:"orderNo" gorm:"column:order_no"`
	Status      string    `json:"status" gorm:"column:status"`
	StatusLabel string    `json:"statusLabel" gorm:"-"`
	Total       int64     `json:"total" gorm:"column:total"`
	ItemCount   int64     `json:"itemCount" gorm:"column:item_count"`
	FirstItem   *string   `json:"firstItem" gorm:"column:first_item"`
	CreatedAt   time.Time `json:"createdAt" gorm:"column:created_at"`
}

func (a *App) ListMyOrders(w http.ResponseWriter, r *http.Request) {
	u := customerFrom(r.Context())
	page, perPage := pageParams(r)
	if perPage > customerOrdersLimit {
		perPage = customerOrdersLimit
	}
	db := a.db.Load().WithContext(r.Context())
	var total int64
	if err := db.Raw(`SELECT COUNT(*) FROM orders WHERE user_id = ? AND deleted_at IS NULL AND order_no IS NOT NULL`, u.ID).Scan(&total).Error; err != nil {
		log.Printf("pesanan saya: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	items := []CustomerOrderSummary{}
	if err := db.Raw(`SELECT o.order_no, o.status, o.total, o.created_at,
		(SELECT COALESCE(SUM(qty), 0) FROM order_items oi WHERE oi.order_id = o.id) AS item_count,
		(SELECT product_name FROM order_items oi WHERE oi.order_id = o.id ORDER BY oi.id LIMIT 1) AS first_item
		FROM orders o WHERE o.user_id = ? AND o.deleted_at IS NULL AND o.order_no IS NOT NULL
		ORDER BY o.created_at DESC, o.id DESC LIMIT ? OFFSET ?`, u.ID, perPage, (page-1)*perPage).Scan(&items).Error; err != nil {
		log.Printf("pesanan saya: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	for i := range items {
		items[i].StatusLabel = statusLabel(items[i].Status)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "perPage": perPage})
}

func (a *App) GetMyOrder(w http.ResponseWriter, r *http.Request) {
	u := customerFrom(r.Context())
	db := a.db.Load().WithContext(r.Context())
	o, err := findCustomerOrder(db, u.ID, mux.Vars(r)["orderNo"], false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	if err != nil {
		log.Printf("detail pesanan: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	a.respondCustomerOrder(w, db, o, http.StatusOK)
}

type cancelInput struct {
	Reason string `json:"reason"`
}

// CancelMyOrder: POST /api/orders/{orderNo}/cancel — hanya pemilik, dari pending_confirmation atau
// pending_payment (UPDATE bersyarat pada status asal; riwayat from_status = status asal).
func (a *App) CancelMyOrder(w http.ResponseWriter, r *http.Request) {
	u := customerFrom(r.Context())
	if !limitUser(w, a.cancelLimiter, u, msgTooManyCancel) {
		return
	}
	var in cancelInput
	if err := decodeJSONLenient(w, r, &in, true); err != nil {
		respondDecodeError(w, err)
		return
	}
	reason, err := optionalText(in.Reason, maxNote255, false, "Alasan")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	db := a.db.Load().WithContext(r.Context())
	var orderID uint64
	err = db.Transaction(func(tx *gorm.DB) error {
		o, err := findCustomerOrder(tx, u.ID, mux.Vars(r)["orderNo"], true)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &httpError{http.StatusNotFound, msgOrderMissing}
		}
		if err != nil {
			return err
		}
		orderID = o.ID
		if err := checkTransition(o.Status, StatusCancelled, actorOwner, ""); err != nil {
			return &httpError{http.StatusConflict, "Pesanan tidak bisa dibatalkan karena statusnya sudah " + strings.ToLower(statusLabel(o.Status))}
		}
		now := a.now()
		res := tx.Exec(`UPDATE orders SET status = ?, cancelled_at = ?, cancelled_by = ?, cancel_reason = ? WHERE id = ? AND status = ?`,
			StatusCancelled, now, u.ID, reason, o.ID, o.Status)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return &httpError{http.StatusConflict, msgStatusChanged}
		}
		if err := tx.Exec(`INSERT INTO order_status_history (order_id, from_status, to_status, actor_user_id, actor_label, note, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, o.ID, o.Status, StatusCancelled, u.ID,
			truncateUTF8(actorLabelCustomer+":"+u.Username, 100), reason, now).Error; err != nil {
			return err
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(u), ActorLabel: actorOf(u), Action: "order.cancel",
			EntityType: "order", EntityID: o.OrderNo,
			Summary: "Pesanan dibatalkan oleh pelanggan: " + o.OrderNo,
			Details: map[string]any{"orderNo": o.OrderNo, "status": map[string]any{"dari": o.Status, "menjadi": StatusCancelled},
				"oleh": "pelanggan", "alasan": reason},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "batal pesanan")
		return
	}
	a.notifyOrder(db, notify.KindCancelled, orderID)
	a.pushOrder(db, push.KindCancelled, orderID)
	o, err := findOrderByID(db, orderID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}
	a.respondCustomerOrder(w, db, o, http.StatusOK)
}

// ---------- Info toko ----------

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

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// StoreInfo: GET /api/store-info (wajib login). Nilai yang belum diisi -> null.
func (a *App) StoreInfo(w http.ResponseWriter, r *http.Request) {
	s, err := loadSettings(a.db.Load().WithContext(r.Context()))
	if err != nil {
		log.Printf("store-info: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	wa := s["store_whatsapp"]
	if p, err := NormalizePhone(wa); err != nil || p == "" {
		wa = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"storeWhatsapp":     nullable(wa),
		"bankName":          nullable(s["bank_name"]),
		"bankAccountNumber": nullable(s["bank_account_number"]),
		"bankAccountHolder": nullable(s["bank_account_holder"]),
		"paymentNote":       nullable(s["payment_note"]),
		"paymentConfigured": s["bank_name"] != "" && s["bank_account_number"] != "" && s["bank_account_holder"] != "",
	})
}
