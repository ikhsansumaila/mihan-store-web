package main

// Pengelolaan pesanan dan pengaturan toko oleh admin. Setiap perubahan ditulis bersama
// activity log (dan riwayat status bila status berubah) dalam SATU transaksi, memakai
// UPDATE bersyarat (WHERE status = status_lama) agar dua admin tidak saling menimpa.

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"mihanstore/notify"
	"mihanstore/push"
)

type AdminOrderSummary struct {
	ID            uint64    `json:"id" gorm:"column:id"`
	OrderNo       string    `json:"orderNo" gorm:"column:order_no"`
	Status        string    `json:"status" gorm:"column:status"`
	StatusLabel   string    `json:"statusLabel" gorm:"-"`
	Total         int64     `json:"total" gorm:"column:total"`
	ItemCount     int64     `json:"itemCount" gorm:"column:item_count"`
	RecipientName string    `json:"recipientName" gorm:"column:recipient_name"`
	City          string    `json:"city" gorm:"column:city"`
	CustomerName  string    `json:"customerName" gorm:"column:customer_name"`
	Username      string    `json:"username" gorm:"column:username"`
	CreatedAt     time.Time `json:"createdAt" gorm:"column:created_at"`
	// Akun pemesan (khusus admin): id users dan alias internal.
	CustomerID    uint64             `json:"-" gorm:"column:customer_id"`
	CustomerAlias *string            `json:"-" gorm:"column:customer_alias"`
	Customer      AdminOrderCustomer `json:"customer" gorm:"-"`
}

// AdminOrderCustomer: ringkasan akun pemesan di daftar pesanan admin.
type AdminOrderCustomer struct {
	ID    uint64  `json:"id"`
	Alias *string `json:"alias"`
}

func (a *App) AdminListOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where := ` WHERE o.deleted_at IS NULL AND o.order_no IS NOT NULL`
	var args []any
	if st := strings.TrimSpace(q.Get("status")); st != "" {
		if !validStatus(st) {
			writeError(w, http.StatusBadRequest, "Status tidak dikenal")
			return
		}
		where += ` AND o.status = ?`
		args = append(args, st)
	}
	from, ok1 := parseWIBDate(q.Get("from"))
	to, ok2 := parseWIBDate(q.Get("to"))
	if !ok1 || !ok2 {
		writeError(w, http.StatusBadRequest, "Format tanggal harus YYYY-MM-DD")
		return
	}
	if from != nil {
		where += ` AND o.created_at >= ?`
		args = append(args, *from)
	}
	if to != nil {
		where += ` AND o.created_at < ?`
		args = append(args, to.Add(24*time.Hour))
	}
	if s := strings.TrimSpace(truncateUTF8(q.Get("q"), 100)); s != "" {
		like := "%" + escapeLike(s) + "%"
		cond := `o.order_no LIKE ? OR o.recipient_name LIKE ? OR u.name LIKE ? OR u.username LIKE ? OR o.recipient_phone LIKE ? OR u.alias LIKE ?`
		args = append(args, like, like, like, like, like, like)
		if p, err := NormalizePhone(s); err == nil && p != "" {
			cond += ` OR o.recipient_phone = ?`
			args = append(args, p)
		}
		where += ` AND (` + cond + `)`
	}
	page, perPage := pageParams(r)
	db := a.db.Load().WithContext(r.Context())
	from_ := ` FROM orders o JOIN users u ON u.id = o.user_id`
	var total int64
	if err := db.Raw(`SELECT COUNT(*)`+from_+where, args...).Scan(&total).Error; err != nil {
		log.Printf("admin pesanan: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	items := []AdminOrderSummary{}
	qargs := append(append([]any{}, args...), perPage, (page-1)*perPage)
	if err := db.Raw(`SELECT o.id, o.order_no, o.status, o.total, o.recipient_name, o.city, o.created_at,
		u.name AS customer_name, u.username, u.id AS customer_id, u.alias AS customer_alias,
		(SELECT COALESCE(SUM(qty), 0) FROM order_items oi WHERE oi.order_id = o.id) AS item_count`+from_+where+
		` ORDER BY o.created_at DESC, o.id DESC LIMIT ? OFFSET ?`, qargs...).Scan(&items).Error; err != nil {
		log.Printf("admin pesanan: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	for i := range items {
		items[i].StatusLabel = statusLabel(items[i].Status)
		items[i].Customer = AdminOrderCustomer{ID: items[i].CustomerID, Alias: items[i].CustomerAlias}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "perPage": perPage})
}

type AdminHistoryDTO struct {
	From      *string   `json:"from"`
	To        string    `json:"to"`
	ToLabel   string    `json:"toLabel"`
	Actor     string    `json:"actor"`
	Note      *string   `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
}

type AdminOrderDTO struct {
	ID            uint64            `json:"id"`
	OrderNo       string            `json:"orderNo"`
	Status        string            `json:"status"`
	StatusLabel   string            `json:"statusLabel"`
	Subtotal      int64             `json:"subtotal"`
	Discount      int64             `json:"discount"`
	DiscountNote  *string           `json:"discountNote"`
	ShippingFee   int64             `json:"shippingFee"`
	Total         int64             `json:"total"`
	PaymentMethod string            `json:"paymentMethod"`
	Recipient     RecipientDTO      `json:"recipient"`
	CustomerNote  *string           `json:"customerNote"`
	AdminNote     *string           `json:"adminNote"`
	PaymentNote   *string           `json:"paymentNote"`
	CancelReason  *string           `json:"cancelReason"`
	Customer      map[string]any    `json:"customer"`
	ItemCount     int64             `json:"itemCount"`
	Items         []OrderItemDTO    `json:"items"`
	History       []AdminHistoryDTO `json:"history"`
	PricingLocked bool              `json:"pricingLocked"`
	AllowedNext   []string          `json:"allowedNext"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
	PaidAt        *time.Time        `json:"paidAt"`
	PaidBy        *string           `json:"paidBy"`
	CompletedAt   *time.Time        `json:"completedAt"`
	CancelledAt   *time.Time        `json:"cancelledAt"`
	CancelledBy   *string           `json:"cancelledBy"`
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

func (a *App) adminOrderDTO(db *gorm.DB, o *orderRow) (*AdminOrderDTO, error) {
	items, err := loadOrderItems(db, o.ID)
	if err != nil {
		return nil, err
	}
	hist, err := loadHistory(db, o.ID)
	if err != nil {
		return nil, err
	}
	h := make([]AdminHistoryDTO, 0, len(hist))
	for _, x := range hist {
		h = append(h, AdminHistoryDTO{From: x.FromStatus, To: x.ToStatus, ToLabel: statusLabel(x.ToStatus), Actor: x.ActorLabel, Note: x.Note, CreatedAt: x.CreatedAt})
	}
	var cust []struct {
		Username string  `gorm:"column:username"`
		Name     string  `gorm:"column:name"`
		Email    string  `gorm:"column:email"`
		Phone    *string `gorm:"column:phone"`
		Alias    *string `gorm:"column:alias"`
	}
	if err := db.Raw(`SELECT username, name, email, phone, alias FROM users WHERE id = ?`, o.UserID).Scan(&cust).Error; err != nil {
		return nil, err
	}
	customer := map[string]any{}
	if len(cust) > 0 {
		customer = map[string]any{"id": o.UserID, "username": cust[0].Username, "name": cust[0].Name, "email": cust[0].Email,
			"phone": cust[0].Phone, "alias": cust[0].Alias}
	}
	return &AdminOrderDTO{
		ID: o.ID, OrderNo: o.OrderNo, Status: o.Status, StatusLabel: statusLabel(o.Status),
		Subtotal: o.Subtotal, Discount: o.Discount, DiscountNote: o.DiscountNote, ShippingFee: o.ShippingFee, Total: o.Total,
		PaymentMethod: o.PaymentMethod, Recipient: recipientOf(o), CustomerNote: o.CustomerNote, AdminNote: o.AdminNote,
		PaymentNote: o.PaymentNote, CancelReason: o.CancelReason, Customer: customer,
		ItemCount: sumQty(items), Items: items, History: h,
		PricingLocked: o.Status != StatusPending, AllowedNext: allowedNext(o.Status, actorAdmin),
		CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt, PaidAt: o.PaidAt, PaidBy: userLabel(db, o.PaidBy),
		CompletedAt: o.CompletedAt, CancelledAt: o.CancelledAt, CancelledBy: userLabel(db, o.CancelledBy),
	}, nil
}

func lockOrderByID(tx *gorm.DB, id uint64) (*orderRow, error) {
	var rows []orderRow
	if err := tx.Raw(`SELECT `+orderCols+` FROM orders o WHERE o.id = ? AND o.deleted_at IS NULL FOR UPDATE`, id).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, &httpError{http.StatusNotFound, msgOrderMissing}
	}
	return &rows[0], nil
}

func (a *App) respondAdminOrder(w http.ResponseWriter, db *gorm.DB, id uint64) {
	o, err := findOrderByID(db, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	dto, err := a.adminOrderDTO(db, o)
	if err != nil {
		log.Printf("admin detail pesanan: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (a *App) AdminGetOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	a.respondAdminOrder(w, a.db.Load().WithContext(r.Context()), id)
}

type pricingInput struct {
	Discount     *int64  `json:"discount"`
	DiscountNote *string `json:"discountNote"`
	ShippingFee  *int64  `json:"shippingFee"`
}

// AdminUpdatePricing: PATCH /api/admin/orders/{id}/pricing — hanya saat pending_payment.
func (a *App) AdminUpdatePricing(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	var in pricingInput
	if err := decodeJSON(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	if in.Discount == nil || in.ShippingFee == nil {
		writeError(w, http.StatusBadRequest, "discount dan shippingFee wajib diisi (bilangan bulat rupiah)")
		return
	}
	note := ""
	if in.DiscountNote != nil {
		note = cleanText(*in.DiscountNote, false)
		if utf8.RuneCountInString(note) > maxNote255 {
			writeError(w, http.StatusBadRequest, "Keterangan diskon maksimal 255 karakter")
			return
		}
	}
	if *in.Discount < 0 || *in.ShippingFee < 0 {
		writeError(w, http.StatusBadRequest, errAmountNegative.Error())
		return
	}
	if *in.ShippingFee > maxShippingFee {
		writeError(w, http.StatusBadRequest, errShippingRange.Error())
		return
	}
	db := a.db.Load().WithContext(r.Context())
	amountsChanged := false // diskon/ongkir/total berubah nilainya (bukan hanya keterangan) -> push pelanggan
	err := db.Transaction(func(tx *gorm.DB) error {
		o, err := lockOrderByID(tx, id)
		if err != nil {
			return err
		}
		if o.Status != StatusPending {
			return &httpError{http.StatusConflict, errPricingLocked.Error()}
		}
		total, err := computeTotal(o.Subtotal, *in.Discount, *in.ShippingFee)
		if err != nil {
			return &httpError{http.StatusBadRequest, err.Error()}
		}
		oldNote := ""
		if o.DiscountNote != nil {
			oldNote = *o.DiscountNote
		}
		before := map[string]any{"discount": o.Discount, "discountNote": oldNote, "shippingFee": o.ShippingFee, "total": o.Total}
		after := map[string]any{"discount": *in.Discount, "discountNote": note, "shippingFee": *in.ShippingFee, "total": total}
		changes := diffMaps(before, after)
		if len(changes) == 0 {
			return nil
		}
		res := tx.Exec(`UPDATE orders SET discount = ?, discount_note = ?, shipping_fee = ?, total = ? WHERE id = ? AND status = ?`,
			*in.Discount, strPtr(note), *in.ShippingFee, total, id, StatusPending)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return &httpError{http.StatusConflict, msgStatusChanged}
		}
		amountsChanged = o.Discount != *in.Discount || o.ShippingFee != *in.ShippingFee || o.Total != total
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "order.pricing_update",
			EntityType: "order", EntityID: o.OrderNo,
			Summary: "Diskon/ongkir pesanan diubah: " + o.OrderNo,
			Details: map[string]any{"orderNo": o.OrderNo, "perubahan": changes, "subtotal": o.Subtotal},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "ubah harga pesanan")
		return
	}
	if amountsChanged {
		a.pushCustomer(db, push.KindCustomerPricing, id)
	}
	a.respondAdminOrder(w, db, id)
}

type statusInput struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Note        string `json:"note"`
	Reason      string `json:"reason"`
	PaymentNote string `json:"paymentNote"`
}

var statusActions = map[string]string{StatusPaid: "order.pay", StatusCompleted: "order.complete", StatusCancelled: "order.cancel"}

// AdminUpdateStatus: PATCH /api/admin/orders/{id}/status {from, to, note?, reason?, paymentNote?}.
// "from" wajib (status yang dilihat admin); bila sudah berubah -> 409.
func (a *App) AdminUpdateStatus(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	var in statusInput
	if err := decodeJSON(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	if !validStatus(in.From) || !validStatus(in.To) {
		writeError(w, http.StatusBadRequest, "Field from dan to wajib berisi status yang valid")
		return
	}
	note, err := optionalText(in.Note, maxNote255, false, "Catatan")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	reason, err := optionalText(in.Reason, maxNote255, false, "Alasan")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	payNote, err := optionalText(in.PaymentNote, maxNote255, false, "Catatan pembayaran")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.To == StatusCancelled && reason == nil && note != nil {
		reason = note // alasan boleh dikirim sebagai note
	}
	reasonStr := ""
	if reason != nil {
		reasonStr = *reason
	}
	if err := checkTransition(in.From, in.To, actorAdmin, reasonStr); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, errFinalStatus) {
			code = http.StatusConflict
		}
		writeError(w, code, err.Error())
		return
	}
	db := a.db.Load().WithContext(r.Context())
	err = db.Transaction(func(tx *gorm.DB) error {
		o, err := lockOrderByID(tx, id)
		if err != nil {
			return err
		}
		if o.Status != in.From {
			return &httpError{http.StatusConflict, "Status pesanan sudah berubah menjadi \"" + statusLabel(o.Status) + "\". Muat ulang halaman."}
		}
		now := a.now()
		var res *gorm.DB
		histNote := note
		switch in.To {
		case StatusPaid:
			if payNote == nil {
				payNote = note
			}
			histNote = payNote
			res = tx.Exec(`UPDATE orders SET status = ?, paid_at = ?, paid_by = ?, payment_note = ? WHERE id = ? AND status = ?`,
				StatusPaid, now, admin.ID, payNote, id, in.From)
		case StatusCompleted:
			res = tx.Exec(`UPDATE orders SET status = ?, completed_at = ? WHERE id = ? AND status = ?`,
				StatusCompleted, now, id, in.From)
		case StatusCancelled:
			histNote = reason
			res = tx.Exec(`UPDATE orders SET status = ?, cancelled_at = ?, cancelled_by = ?, cancel_reason = ? WHERE id = ? AND status = ?`,
				StatusCancelled, now, admin.ID, reason, id, in.From)
		}
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return &httpError{http.StatusConflict, msgStatusChanged}
		}
		if err := tx.Exec(`INSERT INTO order_status_history (order_id, from_status, to_status, actor_user_id, actor_label, note, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, id, in.From, in.To, admin.ID, truncateUTF8(actorOf(admin), 100), histNote, now).Error; err != nil {
			return err
		}
		details := map[string]any{"orderNo": o.OrderNo, "status": map[string]any{"dari": in.From, "menjadi": in.To}, "total": o.Total}
		if in.To == StatusCancelled {
			details["alasan"] = reason
		}
		if in.To == StatusPaid && payNote != nil {
			details["catatanPembayaran"] = "diisi"
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: statusActions[in.To],
			EntityType: "order", EntityID: o.OrderNo,
			Summary: "Status pesanan " + o.OrderNo + ": " + statusLabel(in.From) + " → " + statusLabel(in.To),
			Details: details,
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "ubah status pesanan")
		return
	}
	switch in.To {
	case StatusPaid:
		a.notifyOrder(db, notify.KindPaid, id)
		a.pushCustomer(db, push.KindCustomerPaid, id)
	case StatusCompleted:
		a.pushCustomer(db, push.KindCustomerCompleted, id)
	case StatusCancelled:
		a.notifyOrder(db, notify.KindCancelled, id)
		a.pushCustomer(db, push.KindCustomerCancelled, id)
	}
	a.respondAdminOrder(w, db, id)
}

type adminNoteInput struct {
	AdminNote *string `json:"adminNote"`
}

// AdminUpdateNote: PATCH /api/admin/orders/{id}/note. Isi catatan TIDAK masuk activity log.
func (a *App) AdminUpdateNote(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	var in adminNoteInput
	if err := decodeJSON(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	if in.AdminNote == nil {
		writeError(w, http.StatusBadRequest, "adminNote wajib diisi (boleh string kosong)")
		return
	}
	note, err := optionalText(*in.AdminNote, maxNote500, true, "Catatan admin")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	db := a.db.Load().WithContext(r.Context())
	err = db.Transaction(func(tx *gorm.DB) error {
		o, err := lockOrderByID(tx, id)
		if err != nil {
			return err
		}
		oldLen, newLen := 0, 0
		if o.AdminNote != nil {
			oldLen = utf8.RuneCountInString(*o.AdminNote)
		}
		if note != nil {
			newLen = utf8.RuneCountInString(*note)
		}
		if (o.AdminNote == nil && note == nil) || (o.AdminNote != nil && note != nil && *o.AdminNote == *note) {
			return nil
		}
		if err := tx.Exec(`UPDATE orders SET admin_note = ? WHERE id = ?`, note, id).Error; err != nil {
			return err
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "order.note_update",
			EntityType: "order", EntityID: o.OrderNo,
			Summary: "Catatan admin pesanan diubah: " + o.OrderNo,
			Details: map[string]any{"orderNo": o.OrderNo, "panjang": map[string]any{"dari": oldLen, "menjadi": newLen}},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "catatan admin")
		return
	}
	a.respondAdminOrder(w, db, id)
}

// ---------- Pengaturan toko ----------

func (a *App) AdminGetSettings(w http.ResponseWriter, r *http.Request) {
	s, err := loadSettings(a.db.Load().WithContext(r.Context()))
	if err != nil {
		log.Printf("admin settings: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": s, "keys": settingKeys})
}

func (a *App) AdminUpdateSettings(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	var in SettingsInput
	if err := decodeJSON(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	vals, err := validateSettings(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	db := a.db.Load().WithContext(r.Context())
	err = db.Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			Key   string `gorm:"column:setting_key"`
			Value string `gorm:"column:setting_value"`
		}
		if err := tx.Raw(`SELECT setting_key, setting_value FROM site_settings WHERE deleted_at IS NULL FOR UPDATE`).Scan(&rows).Error; err != nil {
			return err
		}
		cur := map[string]string{}
		for _, r := range rows {
			cur[r.Key] = settingValue(r.Value)
		}
		changed := map[string]any{}
		for _, k := range settingKeys {
			v, sent := vals[k]
			if !sent || cur[k] == v {
				continue
			}
			if err := tx.Exec(`INSERT INTO site_settings (setting_key, setting_value, updated_by) VALUES (?, ?, ?) AS n
				ON DUPLICATE KEY UPDATE setting_value = n.setting_value, updated_by = n.updated_by, deleted_at = NULL`,
				k, v, admin.ID).Error; err != nil {
				return err
			}
			changed[k] = "diubah"
		}
		if len(changed) == 0 {
			return nil
		}
		keys := make([]string, 0, len(changed))
		for _, k := range settingKeys {
			if _, ok := changed[k]; ok {
				keys = append(keys, k)
			}
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "settings.update",
			EntityType: "site_settings",
			Summary:    "Pengaturan toko diubah: " + strings.Join(keys, ", "),
			Details:    map[string]any{"kunci": changed},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "pengaturan toko")
		return
	}
	a.AdminGetSettings(w, r)
}

// orderCounts untuk ringkasan admin.
type orderCounts struct {
	PendingPayment int64 `json:"pendingPayment" gorm:"column:pending_payment"`
	Paid           int64 `json:"paid" gorm:"column:paid"`
	Last7Days      int64 `json:"last7Days" gorm:"column:last7"`
}

func (a *App) loadOrderCounts(db *gorm.DB) (orderCounts, error) {
	var c orderCounts
	err := db.Raw(`SELECT COALESCE(SUM(status = 'pending_payment'), 0) AS pending_payment,
		COALESCE(SUM(status = 'paid'), 0) AS paid,
		COALESCE(SUM(created_at >= ?), 0) AS last7
		FROM orders WHERE deleted_at IS NULL AND order_no IS NOT NULL`, a.now().Add(-7*24*time.Hour)).Scan(&c).Error
	return c, err
}
