package orders

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

	"mihanstore/internal/audit"
	"mihanstore/internal/platform"
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
	// Pesanan punya bukti transfer (penanda "Bukti" di daftar admin).
	HasPaymentProof bool `json:"hasPaymentProof" gorm:"column:has_payment_proof"`
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

func (a *Service) AdminListOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where := ` WHERE o.deleted_at IS NULL AND o.order_no IS NOT NULL`
	var args []any
	if st := strings.TrimSpace(q.Get("status")); st != "" {
		if !ValidStatus(st) {
			platform.WriteError(w, http.StatusBadRequest, "Status tidak dikenal")
			return
		}
		where += ` AND o.status = ?`
		args = append(args, st)
	}
	from, ok1 := platform.ParseWIBDate(q.Get("from"))
	to, ok2 := platform.ParseWIBDate(q.Get("to"))
	if !ok1 || !ok2 {
		platform.WriteError(w, http.StatusBadRequest, "Format tanggal harus YYYY-MM-DD")
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
	if s := strings.TrimSpace(platform.TruncateUTF8(q.Get("q"), 100)); s != "" {
		like := "%" + platform.EscapeLike(s) + "%"
		cond := `o.order_no LIKE ? OR o.recipient_name LIKE ? OR u.name LIKE ? OR u.username LIKE ? OR o.recipient_phone LIKE ? OR u.alias LIKE ?`
		args = append(args, like, like, like, like, like, like)
		if p, err := a.normalizePhone(s); err == nil && p != "" {
			cond += ` OR o.recipient_phone = ?`
			args = append(args, p)
		}
		where += ` AND (` + cond + `)`
	}
	// ?proof=1: hanya pesanan yang punya bukti transfer.
	if q.Get("proof") == "1" {
		where += ` AND EXISTS (SELECT 1 FROM order_payment_proofs pp WHERE pp.order_id = o.id)`
	}
	page, perPage := platform.PageParams(r)
	db := a.db().WithContext(r.Context())
	from_ := ` FROM orders o JOIN users u ON u.id = o.user_id`
	var total int64
	if err := db.Raw(`SELECT COUNT(*)`+from_+where, args...).Scan(&total).Error; err != nil {
		log.Printf("admin pesanan: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	items := []AdminOrderSummary{}
	qargs := append(append([]any{}, args...), perPage, (page-1)*perPage)
	if err := db.Raw(`SELECT o.id, o.order_no, o.status, o.total, o.recipient_name, o.city, o.created_at,
		u.name AS customer_name, u.username, u.id AS customer_id, u.alias AS customer_alias,
		EXISTS (SELECT 1 FROM order_payment_proofs pp WHERE pp.order_id = o.id) AS has_payment_proof,
		(SELECT COALESCE(SUM(qty), 0) FROM order_items oi WHERE oi.order_id = o.id) AS item_count`+from_+where+
		` ORDER BY o.created_at DESC, o.id DESC LIMIT ? OFFSET ?`, qargs...).Scan(&items).Error; err != nil {
		log.Printf("admin pesanan: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	for i := range items {
		items[i].StatusLabel = StatusLabel(items[i].Status)
		items[i].Customer = AdminOrderCustomer{ID: items[i].CustomerID, Alias: items[i].CustomerAlias}
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "perPage": perPage})
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
	PaymentProof  *PaymentProofDTO  `json:"paymentProof"` // bukti transfer pelanggan (null bila belum ada)
	CanConfirm    bool              `json:"canConfirm"`   // pending_confirmation: tombol "Konfirmasi pesanan" (POST /confirm)
	AllowedNext   []string          `json:"allowedNext"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
	PaidAt        *time.Time        `json:"paidAt"`
	PaidBy        *string           `json:"paidBy"`
	CompletedAt   *time.Time        `json:"completedAt"`
	CancelledAt   *time.Time        `json:"cancelledAt"`
	CancelledBy   *string           `json:"cancelledBy"`
}

func (a *Service) adminOrderDTO(db *gorm.DB, o *orderRow) (*AdminOrderDTO, error) {
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
		h = append(h, AdminHistoryDTO{From: x.FromStatus, To: x.ToStatus, ToLabel: StatusLabel(x.ToStatus), Actor: x.ActorLabel, Note: x.Note, CreatedAt: x.CreatedAt})
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
		ID: o.ID, OrderNo: o.OrderNo, Status: o.Status, StatusLabel: StatusLabel(o.Status),
		Subtotal: o.Subtotal, Discount: o.Discount, DiscountNote: o.DiscountNote, ShippingFee: o.ShippingFee, Total: o.Total,
		PaymentMethod: o.PaymentMethod, Recipient: recipientOf(o), CustomerNote: o.CustomerNote, AdminNote: o.AdminNote,
		PaymentNote: o.PaymentNote, CancelReason: o.CancelReason, Customer: customer,
		ItemCount: sumQty(items), Items: items, History: h, PaymentProof: paymentProofDTOFor(db, o.ID),
		// Diskon/ongkir bisa diisi saat menunggu konfirmasi (disimpan lewat /confirm) dan saat menunggu pembayaran (/pricing).
		PricingLocked: o.Status != StatusPending && o.Status != StatusPendingConfirmation,
		CanConfirm:    o.Status == StatusPendingConfirmation, AllowedNext: AllowedNext(o.Status, ActorAdmin),
		CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt, PaidAt: o.PaidAt, PaidBy: userLabel(db, o.PaidBy),
		CompletedAt: o.CompletedAt, CancelledAt: o.CancelledAt, CancelledBy: userLabel(db, o.CancelledBy),
	}, nil
}

func (a *Service) respondAdminOrder(w http.ResponseWriter, db *gorm.DB, id uint64) {
	o, err := findOrderByID(db, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	if err != nil {
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	dto, err := a.adminOrderDTO(db, o)
	if err != nil {
		log.Printf("admin detail pesanan: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, dto)
}

func (a *Service) AdminGetOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	a.respondAdminOrder(w, a.db().WithContext(r.Context()), id)
}

type pricingInput struct {
	Discount     *int64  `json:"discount"`
	DiscountNote *string `json:"discountNote"`
	ShippingFee  *int64  `json:"shippingFee"`
}

// readPricingInput membaca & memvalidasi {discount, discountNote, shippingFee} (dipakai /pricing dan
// /confirm, pesan galat sama). false = respons galat sudah ditulis.
func readPricingInput(w http.ResponseWriter, r *http.Request) (pricingInput, string, bool) {
	var in pricingInput
	if err := platform.DecodeJSON(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return in, "", false
	}
	if in.Discount == nil || in.ShippingFee == nil {
		platform.WriteError(w, http.StatusBadRequest, "discount dan shippingFee wajib diisi (bilangan bulat rupiah)")
		return in, "", false
	}
	note := ""
	if in.DiscountNote != nil {
		note = platform.CleanText(*in.DiscountNote, false)
		if utf8.RuneCountInString(note) > MaxNote255 {
			platform.WriteError(w, http.StatusBadRequest, "Keterangan diskon maksimal 255 karakter")
			return in, "", false
		}
	}
	if *in.Discount < 0 || *in.ShippingFee < 0 {
		platform.WriteError(w, http.StatusBadRequest, ErrAmountNegative.Error())
		return in, "", false
	}
	if *in.ShippingFee > MaxShippingFee {
		platform.WriteError(w, http.StatusBadRequest, ErrShippingRange.Error())
		return in, "", false
	}
	return in, note, true
}

// AdminConfirmOrder: POST /api/admin/orders/{id}/confirm {discount, discountNote?, shippingFee}.
// Satu transaksi: pending_confirmation -> pending_payment sekaligus menyimpan diskon/ongkir/total,
// riwayat status, dan log aktivitas "order.confirm". Klik ganda: yang kedua 409.
// Setelah commit: push pelanggan "Ongkir sudah dikonfirmasi" (ongkir & total dari DB) SELALU dikirim.
func (a *Service) AdminConfirmOrder(w http.ResponseWriter, r *http.Request) {
	admin := a.admin(r)
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	in, note, ok := readPricingInput(w, r)
	if !ok {
		return
	}
	db := a.db().WithContext(r.Context())
	err := db.Transaction(func(tx *gorm.DB) error {
		o, err := lockOrderByID(tx, id)
		if err != nil {
			return err
		}
		if err := CheckConfirm(o.Status); err != nil {
			return &platform.HTTPError{Status: http.StatusConflict, Msg: err.Error()}
		}
		total, err := ComputeTotal(o.Subtotal, *in.Discount, *in.ShippingFee)
		if err != nil {
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: err.Error()}
		}
		now := a.now()
		res := tx.Exec(`UPDATE orders SET status = ?, discount = ?, discount_note = ?, shipping_fee = ?, total = ? WHERE id = ? AND status = ?`,
			StatusPending, *in.Discount, platform.StrPtr(note), *in.ShippingFee, total, id, StatusPendingConfirmation)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return &platform.HTTPError{Status: http.StatusConflict, Msg: msgStatusChanged}
		}
		if err := tx.Exec(`INSERT INTO order_status_history (order_id, from_status, to_status, actor_user_id, actor_label, note, created_at)
			VALUES (?, ?, ?, ?, ?, NULL, ?)`, id, StatusPendingConfirmation, StatusPending, admin.ID, platform.TruncateUTF8(admin.label(), 100), now).Error; err != nil {
			return err
		}
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: admin.uid(), ActorLabel: admin.label(), Action: "order.confirm",
			EntityType: "order", EntityID: o.OrderNo,
			Summary: "Pesanan dikonfirmasi (ongkir/diskon ditetapkan): " + o.OrderNo,
			Details: map[string]any{"orderNo": o.OrderNo,
				"status":   map[string]any{"dari": StatusPendingConfirmation, "menjadi": StatusPending},
				"subtotal": o.Subtotal, "discount": *in.Discount, "discountNote": note, "shippingFee": *in.ShippingFee, "total": total},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "konfirmasi pesanan")
		return
	}
	a.events.PricingSet(db, id)
	a.respondAdminOrder(w, db, id)
}

// AdminUpdatePricing: PATCH /api/admin/orders/{id}/pricing — hanya saat pending_payment.
func (a *Service) AdminUpdatePricing(w http.ResponseWriter, r *http.Request) {
	admin := a.admin(r)
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	in, note, ok := readPricingInput(w, r)
	if !ok {
		return
	}
	db := a.db().WithContext(r.Context())
	amountsChanged := false // diskon/ongkir/total berubah nilainya (bukan hanya keterangan) -> push pelanggan
	err := db.Transaction(func(tx *gorm.DB) error {
		o, err := lockOrderByID(tx, id)
		if err != nil {
			return err
		}
		if o.Status == StatusPendingConfirmation {
			return &platform.HTTPError{Status: http.StatusConflict, Msg: ErrConfirmFirst.Error()}
		}
		if o.Status != StatusPending {
			return &platform.HTTPError{Status: http.StatusConflict, Msg: ErrPricingLocked.Error()}
		}
		total, err := ComputeTotal(o.Subtotal, *in.Discount, *in.ShippingFee)
		if err != nil {
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: err.Error()}
		}
		oldNote := ""
		if o.DiscountNote != nil {
			oldNote = *o.DiscountNote
		}
		before := map[string]any{"discount": o.Discount, "discountNote": oldNote, "shippingFee": o.ShippingFee, "total": o.Total}
		after := map[string]any{"discount": *in.Discount, "discountNote": note, "shippingFee": *in.ShippingFee, "total": total}
		changes := audit.DiffMaps(before, after)
		if len(changes) == 0 {
			return nil
		}
		res := tx.Exec(`UPDATE orders SET discount = ?, discount_note = ?, shipping_fee = ?, total = ? WHERE id = ? AND status = ?`,
			*in.Discount, platform.StrPtr(note), *in.ShippingFee, total, id, StatusPending)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return &platform.HTTPError{Status: http.StatusConflict, Msg: msgStatusChanged}
		}
		amountsChanged = o.Discount != *in.Discount || o.ShippingFee != *in.ShippingFee || o.Total != total
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: admin.uid(), ActorLabel: admin.label(), Action: "order.pricing_update",
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
		a.events.PricingSet(db, id)
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
func (a *Service) AdminUpdateStatus(w http.ResponseWriter, r *http.Request) {
	admin := a.admin(r)
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	var in statusInput
	if err := platform.DecodeJSON(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	if !ValidStatus(in.From) || !ValidStatus(in.To) {
		platform.WriteError(w, http.StatusBadRequest, "Field from dan to wajib berisi status yang valid")
		return
	}
	note, err := optionalText(in.Note, MaxNote255, false, "Catatan")
	if err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	reason, err := optionalText(in.Reason, MaxNote255, false, "Alasan")
	if err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	payNote, err := optionalText(in.PaymentNote, MaxNote255, false, "Catatan pembayaran")
	if err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.To == StatusCancelled && reason == nil && note != nil {
		reason = note // alasan boleh dikirim sebagai note
	}
	reasonStr := ""
	if reason != nil {
		reasonStr = *reason
	}
	if err := CheckTransition(in.From, in.To, ActorAdmin, reasonStr); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, ErrFinalStatus) {
			code = http.StatusConflict
		}
		platform.WriteError(w, code, err.Error())
		return
	}
	db := a.db().WithContext(r.Context())
	err = db.Transaction(func(tx *gorm.DB) error {
		o, err := lockOrderByID(tx, id)
		if err != nil {
			return err
		}
		if o.Status != in.From {
			return &platform.HTTPError{Status: http.StatusConflict, Msg: "Status pesanan sudah berubah menjadi \"" + StatusLabel(o.Status) + "\". Muat ulang halaman."}
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
			return &platform.HTTPError{Status: http.StatusConflict, Msg: msgStatusChanged}
		}
		if err := tx.Exec(`INSERT INTO order_status_history (order_id, from_status, to_status, actor_user_id, actor_label, note, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, id, in.From, in.To, admin.ID, platform.TruncateUTF8(admin.label(), 100), histNote, now).Error; err != nil {
			return err
		}
		details := map[string]any{"orderNo": o.OrderNo, "status": map[string]any{"dari": in.From, "menjadi": in.To}, "total": o.Total}
		if in.To == StatusCancelled {
			details["alasan"] = reason
		}
		if in.To == StatusPaid && payNote != nil {
			details["catatanPembayaran"] = "diisi"
		}
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: admin.uid(), ActorLabel: admin.label(), Action: statusActions[in.To],
			EntityType: "order", EntityID: o.OrderNo,
			Summary: "Status pesanan " + o.OrderNo + ": " + StatusLabel(in.From) + " → " + StatusLabel(in.To),
			Details: details,
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "ubah status pesanan")
		return
	}
	switch in.To {
	case StatusPaid:
		a.events.OrderPaid(db, id)
	case StatusCompleted:
		a.events.OrderCompleted(db, id)
	case StatusCancelled:
		a.events.OrderCancelledByAdmin(db, id)
	}
	a.respondAdminOrder(w, db, id)
}

type adminNoteInput struct {
	AdminNote *string `json:"adminNote"`
}

// AdminUpdateNote: PATCH /api/admin/orders/{id}/note. Isi catatan TIDAK masuk activity log.
func (a *Service) AdminUpdateNote(w http.ResponseWriter, r *http.Request) {
	admin := a.admin(r)
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgOrderMissing)
		return
	}
	var in adminNoteInput
	if err := platform.DecodeJSON(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	if in.AdminNote == nil {
		platform.WriteError(w, http.StatusBadRequest, "adminNote wajib diisi (boleh string kosong)")
		return
	}
	note, err := optionalText(*in.AdminNote, MaxNote500, true, "Catatan admin")
	if err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	db := a.db().WithContext(r.Context())
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
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: admin.uid(), ActorLabel: admin.label(), Action: "order.note_update",
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

func (a *Service) AdminGetSettings(w http.ResponseWriter, r *http.Request) {
	s, err := loadSettings(a.db().WithContext(r.Context()))
	if err != nil {
		log.Printf("admin settings: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"settings": s, "keys": settingKeys})
}

func (a *Service) AdminUpdateSettings(w http.ResponseWriter, r *http.Request) {
	admin := a.admin(r)
	var in SettingsInput
	if err := platform.DecodeJSON(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	vals, err := validateSettings(in, a.normalizePhone)
	if err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	db := a.db().WithContext(r.Context())
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
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: admin.uid(), ActorLabel: admin.label(), Action: "settings.update",
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
