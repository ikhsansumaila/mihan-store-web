package orders

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
	"strings"
	"time"

	"github.com/gorilla/mux"
	"gorm.io/gorm"

	"mihanstore/internal/audit"
	"mihanstore/internal/platform"
	"mihanstore/internal/regions"
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
	Region *regions.DTO `json:"region"`
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
	// Bukti transfer (null bila belum ada); boleh diunggah/diganti/dihapus hanya saat pending_payment.
	PaymentProof   *PaymentProofDTO `json:"paymentProof"`
	CanUploadProof bool             `json:"canUploadProof"`
	CreatedAt      time.Time        `json:"createdAt"`
	PaidAt         *time.Time       `json:"paidAt"`
	CompletedAt    *time.Time       `json:"completedAt"`
	CancelledAt    *time.Time       `json:"cancelledAt"`
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
func regionOf(o *orderRow) *regions.DTO {
	if o.VillageCode == nil || o.ProvinceCode == nil {
		return nil
	}
	return &regions.DTO{
		Province: regions.Ref{Code: deref(o.ProvinceCode), Name: deref(o.ProvinceName)},
		Regency:  regions.Ref{Code: deref(o.RegencyCode), Name: deref(o.RegencyName)},
		District: regions.Ref{Code: deref(o.DistrictCode), Name: deref(o.DistrictName)},
		Village:  regions.Ref{Code: deref(o.VillageCode), Name: deref(o.VillageName)},
	}
}

func recipientOf(o *orderRow) RecipientDTO {
	reg := regionOf(o)
	return RecipientDTO{Name: o.RecipientName, Phone: o.RecipientPhone, Address: o.Address, City: o.City, PostalCode: o.PostalCode,
		Region: reg, FullAddress: regions.ComposeAddress(o.Address, reg, o.City, o.PostalCode)}
}

func (a *Service) customerOrderDTO(db *gorm.DB, o *orderRow) (*CustomerOrderDTO, error) {
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
		h = append(h, HistoryDTO{From: x.FromStatus, To: x.ToStatus, ToLabel: StatusLabel(x.ToStatus), Actor: actor, Note: note, CreatedAt: x.CreatedAt})
	}
	return &CustomerOrderDTO{
		OrderNo: o.OrderNo, Status: o.Status, StatusLabel: StatusLabel(o.Status),
		Subtotal: o.Subtotal, Discount: o.Discount, DiscountNote: o.DiscountNote, ShippingFee: o.ShippingFee, Total: o.Total,
		PaymentMethod: o.PaymentMethod, Recipient: recipientOf(o), Note: o.CustomerNote, CancelReason: o.CancelReason,
		ItemCount: sumQty(items), Items: items, History: h, CanCancel: CanOwnerCancel(o.Status),
		PaymentProof: paymentProofDTOFor(db, o.ID), CanUploadProof: o.Status == StatusPending,
		CreatedAt: o.CreatedAt, PaidAt: o.PaidAt, CompletedAt: o.CompletedAt, CancelledAt: o.CancelledAt,
	}, nil
}

// ---------- Checkout ----------

type cartLine struct {
	ProductID uint64 `gorm:"column:product_id"`
	Name      string `gorm:"column:name"`
	Price     int64  `gorm:"column:price"` // harga dasar
	Unit      string `gorm:"column:unit"`
	Qty       int64  `gorm:"column:qty"`
	Available bool   `gorm:"column:available"`

	unitPrice  int64 // harga efektif (Pricing.UnitPrices)
	tierMinQty int64 // 0 = harga eceran
}

// CreateOrder: POST /api/orders. Item & harga dari keranjang di server; satu transaksi.
func (a *Service) CreateOrder(w http.ResponseWriter, r *http.Request) {
	u := a.customer(r)
	var in CheckoutInput
	if err := platform.DecodeJSONLenient(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	f, err := validateCheckout(in, a.normalizePhone)
	if err != nil {
		respondFieldError(w, err)
		return
	}
	db := a.db().WithContext(r.Context())

	// Kunci idempotensi yang sudah dipakai -> kembalikan pesanan yang sama (200), tanpa
	// menghabiskan kuota batas laju.
	if ex, err := existingOrderByKey(db, u.ID, f.IdemKey); err != nil {
		log.Printf("checkout: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	} else if ex != nil {
		a.respondCustomerOrder(w, db, ex, http.StatusOK)
		return
	}
	if !platform.LimitUser(w, a.CheckoutLimiter, u.ID, msgTooManyCheckout) {
		return
	}
	// Wilayah: kode dari klien, NAMA selalu dari data wilayah di database (bukan dari klien).
	region, err := a.regions.ResolveChain(r.Context(), db, f.Region)
	if err != nil {
		if errors.Is(err, regions.ErrUnavailable) {
			platform.WriteError(w, http.StatusServiceUnavailable, regions.MsgUnavailable+". Silakan coba lagi nanti atau hubungi toko.")
			return
		}
		var fe *platform.FieldError
		if errors.As(err, &fe) {
			respondFieldError(w, fe)
			return
		}
		log.Printf("checkout wilayah: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	city := platform.TruncateUTF8(region.Regency.Name, 100)

	var orderID uint64
	var replay *orderRow
	err = db.Transaction(func(tx *gorm.DB) error {
		cartID, err := a.cart.Lock(tx, u.ID)
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
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: msgCartEmpty}
		}
		var lines []cartLine
		if err := tx.Raw(`SELECT ci.product_id, p.name, p.price, p.unit, ci.qty,
			(p.is_active = 1 AND p.deleted_at IS NULL AND c.deleted_at IS NULL) AS available
			FROM cart_items ci JOIN products p ON p.id = ci.product_id JOIN categories c ON c.id = p.category_id
			WHERE ci.cart_id = ? AND ci.deleted_at IS NULL ORDER BY ci.id FOR SHARE`, cartID).Scan(&lines).Error; err != nil {
			return err
		}
		if len(lines) == 0 {
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: msgCartEmpty}
		}
		if len(lines) > a.maxCartLines {
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: "Keranjang maksimal berisi 50 produk berbeda"}
		}
		var subtotal, itemCount, tierLines int64
		productIDs := make([]uint64, 0, len(lines))
		for _, l := range lines {
			productIDs = append(productIDs, l.ProductID)
		}
		priceQ := make([]PriceLine, 0, len(lines))
		for _, l := range lines {
			priceQ = append(priceQ, PriceLine{ProductID: l.ProductID, BasePrice: l.Price, Qty: l.Qty})
		}
		prices, err := a.pricing.UnitPrices(tx, priceQ)
		if err != nil {
			return err
		}
		for i := range lines {
			l := &lines[i]
			if !l.Available {
				return &platform.HTTPError{Status: http.StatusConflict, Msg: msgCartUnavailable}
			}
			if l.Qty < 1 || l.Qty > a.maxItemQty {
				return &platform.HTTPError{Status: http.StatusBadRequest, Msg: "Jumlah per produk harus 1 sampai 999"}
			}
			l.unitPrice, l.tierMinQty = prices[i].UnitPrice, prices[i].TierMinQty
			if l.tierMinQty > 0 {
				tierLines++
			}
			subtotal += l.unitPrice * l.Qty
			itemCount += l.Qty
		}
		if subtotal <= 0 {
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: "Total pesanan harus lebih dari 0"}
		}
		if subtotal > MaxOrderSubtotal {
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: msgOrderTooBig}
		}
		total, err := ComputeTotal(subtotal, 0, 0)
		if err != nil {
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: err.Error()}
		}
		// Total yang dilihat pelanggan harus sama dengan hitungan server (harga bisa berubah
		// setelah halaman dibuka). Berbeda -> batal tanpa menulis apa pun, keranjang tetap.
		if err := a.pricing.CheckExpectedTotal(in.ExpectedTotal, total); err != nil {
			return err
		}
		now := a.now()
		if err := tx.Exec(`INSERT INTO orders (user_id, status, subtotal, discount, shipping_fee, total, payment_method,
			recipient_name, recipient_phone, address, city, postal_code, customer_note, idempotency_key,
			province_code, province_name, regency_code, regency_name, district_code, district_name, village_code, village_name,
			created_at, updated_at)
			VALUES (?, ?, ?, 0, 0, ?, 'bank_transfer', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			u.ID, StatusPendingConfirmation, subtotal, total, f.Name, f.Phone, f.Address, city, f.PostalCode, f.Note, f.IdemKey,
			region.Province.Code, platform.TruncateUTF8(region.Province.Name, 100), region.Regency.Code, platform.TruncateUTF8(region.Regency.Name, 100),
			region.District.Code, platform.TruncateUTF8(region.District.Name, 100), region.Village.Code, platform.TruncateUTF8(region.Village.Name, 100),
			now, now).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT LAST_INSERT_ID()`).Scan(&orderID).Error; err != nil || orderID == 0 {
			return errors.New("gagal membaca id pesanan")
		}
		orderNo := OrderNumber(orderID, now)
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
			VALUES (?, NULL, ?, ?, ?, NULL, ?)`, orderID, StatusPendingConfirmation, u.ID, platform.TruncateUTF8(actorLabelCustomer+":"+u.Username, 100), now).Error; err != nil {
			return err
		}
		if err := a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: u.uid(), ActorLabel: u.label(), Action: "order.create",
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
		if platform.IsDuplicateKey(err) {
			// Permintaan kembar yang lolos bersamaan: kembalikan pesanan pertama.
			if ex, e2 := existingOrderByKey(db, u.ID, f.IdemKey); e2 == nil && ex != nil {
				a.respondCustomerOrder(w, db, ex, http.StatusOK)
				return
			}
		}
		if a.pricing.IsPriceChanged(err) {
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
	a.events.OrderCreated(db, orderID)
	o, err := findOrderByID(db, orderID)
	if err != nil {
		platform.WriteJSON(w, http.StatusCreated, map[string]any{"success": true})
		return
	}
	a.respondCustomerOrder(w, db, o, http.StatusCreated)
}

// respondFieldError: *platform.FieldError -> 422 {error, field}; galat lain -> 422 {error}.
func respondFieldError(w http.ResponseWriter, err error) {
	var fe *platform.FieldError
	if errors.As(err, &fe) {
		platform.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": fe.Msg, "field": fe.Field})
		return
	}
	platform.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
}

// respondPriceChanged: 409 {error:"price_changed", message, cart} — pesanan TIDAK dibuat dan
// keranjang tidak diubah; pelanggan memeriksa keranjang terkini lalu mengonfirmasi ulang.
func (a *Service) respondPriceChanged(w http.ResponseWriter, r *http.Request, db *gorm.DB, userID uint64) {
	c, err := a.cart.Snapshot(db, userID)
	if err != nil {
		log.Printf("checkout (keranjang terkini): %v", err)
		platform.WriteJSON(w, http.StatusConflict, map[string]any{"error": "price_changed", "message": a.pricing.PriceChangedMessage()})
		return
	}
	platform.WriteJSON(w, http.StatusConflict, map[string]any{"error": "price_changed", "message": a.pricing.PriceChangedMessage(), "cart": c})
}

func (a *Service) respondCustomerOrder(w http.ResponseWriter, db *gorm.DB, o *orderRow, status int) {
	dto, err := a.customerOrderDTO(db, o)
	if err != nil {
		log.Printf("pesanan: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, status, dto)
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
	// Penanda "Bukti terkirim" di daftar Pesanan Saya.
	HasPaymentProof bool `json:"hasPaymentProof" gorm:"column:has_payment_proof"`
}

func (a *Service) ListMyOrders(w http.ResponseWriter, r *http.Request) {
	u := a.customer(r)
	page, perPage := platform.PageParams(r)
	if perPage > customerOrdersLimit {
		perPage = customerOrdersLimit
	}
	db := a.db().WithContext(r.Context())
	var total int64
	if err := db.Raw(`SELECT COUNT(*) FROM orders WHERE user_id = ? AND deleted_at IS NULL AND order_no IS NOT NULL`, u.ID).Scan(&total).Error; err != nil {
		log.Printf("pesanan saya: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	items := []CustomerOrderSummary{}
	if err := db.Raw(`SELECT o.order_no, o.status, o.total, o.created_at,
		EXISTS (SELECT 1 FROM order_payment_proofs pp WHERE pp.order_id = o.id) AS has_payment_proof,
		(SELECT COALESCE(SUM(qty), 0) FROM order_items oi WHERE oi.order_id = o.id) AS item_count,
		(SELECT product_name FROM order_items oi WHERE oi.order_id = o.id ORDER BY oi.id LIMIT 1) AS first_item
		FROM orders o WHERE o.user_id = ? AND o.deleted_at IS NULL AND o.order_no IS NOT NULL
		ORDER BY o.created_at DESC, o.id DESC LIMIT ? OFFSET ?`, u.ID, perPage, (page-1)*perPage).Scan(&items).Error; err != nil {
		log.Printf("pesanan saya: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	for i := range items {
		items[i].StatusLabel = StatusLabel(items[i].Status)
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "perPage": perPage})
}

func (a *Service) GetMyOrder(w http.ResponseWriter, r *http.Request) {
	u := a.customer(r)
	db := a.db().WithContext(r.Context())
	o, err := findCustomerOrder(db, u.ID, mux.Vars(r)["orderNo"], false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	if err != nil {
		log.Printf("detail pesanan: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	a.respondCustomerOrder(w, db, o, http.StatusOK)
}

type cancelInput struct {
	Reason string `json:"reason"`
}

// CancelMyOrder: POST /api/orders/{orderNo}/cancel — hanya pemilik, dari pending_confirmation atau
// pending_payment (UPDATE bersyarat pada status asal; riwayat from_status = status asal).
func (a *Service) CancelMyOrder(w http.ResponseWriter, r *http.Request) {
	u := a.customer(r)
	if !platform.LimitUser(w, a.CancelLimiter, u.ID, msgTooManyCancel) {
		return
	}
	var in cancelInput
	if err := platform.DecodeJSONLenient(w, r, &in, true); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	reason, err := optionalText(in.Reason, MaxNote255, false, "Alasan")
	if err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	db := a.db().WithContext(r.Context())
	var orderID uint64
	err = db.Transaction(func(tx *gorm.DB) error {
		o, err := findCustomerOrder(tx, u.ID, mux.Vars(r)["orderNo"], true)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &platform.HTTPError{Status: http.StatusNotFound, Msg: msgOrderMissing}
		}
		if err != nil {
			return err
		}
		orderID = o.ID
		if err := CheckTransition(o.Status, StatusCancelled, ActorOwner, ""); err != nil {
			return &platform.HTTPError{Status: http.StatusConflict, Msg: "Pesanan tidak bisa dibatalkan karena statusnya sudah " + strings.ToLower(StatusLabel(o.Status))}
		}
		now := a.now()
		res := tx.Exec(`UPDATE orders SET status = ?, cancelled_at = ?, cancelled_by = ?, cancel_reason = ? WHERE id = ? AND status = ?`,
			StatusCancelled, now, u.ID, reason, o.ID, o.Status)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return &platform.HTTPError{Status: http.StatusConflict, Msg: msgStatusChanged}
		}
		if err := tx.Exec(`INSERT INTO order_status_history (order_id, from_status, to_status, actor_user_id, actor_label, note, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, o.ID, o.Status, StatusCancelled, u.ID,
			platform.TruncateUTF8(actorLabelCustomer+":"+u.Username, 100), reason, now).Error; err != nil {
			return err
		}
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: u.uid(), ActorLabel: u.label(), Action: "order.cancel",
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
	a.events.OrderCancelledByCustomer(db, orderID)
	o, err := findOrderByID(db, orderID)
	if err != nil {
		platform.WriteJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}
	a.respondCustomerOrder(w, db, o, http.StatusOK)
}

// ---------- Info toko ----------

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// StoreInfo: GET /api/store-info (wajib login). Nilai yang belum diisi -> null.
func (a *Service) StoreInfo(w http.ResponseWriter, r *http.Request) {
	s, err := loadSettings(a.db().WithContext(r.Context()))
	if err != nil {
		log.Printf("store-info: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	wa := s["store_whatsapp"]
	if p, err := a.normalizePhone(wa); err != nil || p == "" {
		wa = ""
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{
		"storeWhatsapp":     nullable(wa),
		"bankName":          nullable(s["bank_name"]),
		"bankAccountNumber": nullable(s["bank_account_number"]),
		"bankAccountHolder": nullable(s["bank_account_holder"]),
		"paymentNote":       nullable(s["payment_note"]),
		"paymentConfigured": s["bank_name"] != "" && s["bank_account_number"] != "" && s["bank_account_holder"] != "",
	})
}
