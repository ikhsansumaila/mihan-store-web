package main

// Menu admin "Pelanggan": daftar & detail akun role customer beserta statistik pesanan, dan
// ALIAS pelanggan (users.alias, migrations/016) = nama panggilan/label internal yang HANYA dilihat
// admin. Alias sengaja TIDAK dipetakan ke struct User/UserDTO, sehingga tidak mungkin ikut terkirim
// lewat API pelanggan (/api/auth/*, /api/cart*, /api/orders*); hanya dibaca dengan SQL eksplisit di
// rute /api/admin/* (customers.go, admin_orders.go) dan untuk notifikasi Discord internal.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
)

const (
	maxAliasLen        = 100
	msgCustomerMissing = "Pelanggan tidak ditemukan"
)

var errAliasLong = fmt.Errorf("Alias maksimal %d karakter", maxAliasLen)

// NormalizeAlias merapikan alias: karakter kontrol, bidi (override/isolate/penanda arah) dan
// karakter tak terlihat (zero-width, BOM) dibuang; tab/baris baru menjadi spasi; spasi (termasuk
// NBSP) dirapikan menjadi satu spasi tanpa spasi di awal/akhir. Hasil kosong = nil (alias dihapus).
// Maksimal 100 karakter (rune) SETELAH dirapikan.
func NormalizeAlias(s string) (*string, error) {
	s = strings.ToValidUTF8(s, "")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteRune(' ')
		case unicode.IsControl(r), isBidiControl(r),
			r >= 0x200B && r <= 0x200D, r >= 0x2060 && r <= 0x2064, r == 0xFEFF:
			// buang
		default:
			b.WriteRune(r)
		}
	}
	a := strings.Join(strings.Fields(b.String()), " ")
	if a == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(a) > maxAliasLen {
		return nil, errAliasLong
	}
	return &a, nil
}

// displayCustomerName: alias bila ada, selain itu nama akun (dipakai notifikasi Discord internal).
func displayCustomerName(name string, alias *string) string {
	if alias != nil && strings.TrimSpace(*alias) != "" {
		return *alias
	}
	return name
}

// customerSorts: daftar putih urutan daftar pelanggan (nilai `sort` dari klien TIDAK pernah
// disisipkan ke SQL; hanya kunci peta ini).
var customerSorts = map[string]string{
	"newest": "u.created_at DESC, u.id DESC",
	"orders": "order_count DESC, u.created_at DESC, u.id DESC",
	"spent":  "total_spent DESC, u.created_at DESC, u.id DESC",
}

// customerOrderBy mengembalikan klausa ORDER BY untuk kunci urutan ("" = newest).
func customerOrderBy(key string) (string, bool) {
	if key == "" {
		key = "newest"
	}
	s, ok := customerSorts[key]
	return s, ok
}

// Statistik per pelanggan: jumlah pesanan (semua status) dan total belanja (hanya paid/completed).
const customerStatsJoin = ` LEFT JOIN (
	SELECT user_id, COUNT(*) AS order_count,
		COALESCE(SUM(CASE WHEN status IN ('paid','completed') THEN total ELSE 0 END), 0) AS total_spent,
		MAX(created_at) AS last_order_at
	FROM orders WHERE deleted_at IS NULL AND order_no IS NOT NULL GROUP BY user_id
) s ON s.user_id = u.id`

const customerBaseWhere = ` WHERE u.deleted_at IS NULL AND u.role = 'customer'`

type AdminCustomerDTO struct {
	ID          uint64     `json:"id" gorm:"column:id"`
	Name        string     `json:"name" gorm:"column:name"`
	Username    string     `json:"username" gorm:"column:username"`
	Email       string     `json:"email" gorm:"column:email"`
	Phone       *string    `json:"phone" gorm:"column:phone"`
	Alias       *string    `json:"alias" gorm:"column:alias"`
	Status      string     `json:"status" gorm:"column:status"`
	OrderCount  int64      `json:"orderCount" gorm:"column:order_count"`
	TotalSpent  int64      `json:"totalSpent" gorm:"column:total_spent"`
	LastOrderAt *time.Time `json:"lastOrderAt" gorm:"column:last_order_at"`
	CreatedAt   time.Time  `json:"createdAt" gorm:"column:created_at"`
}

const customerSelect = `SELECT u.id, u.name, u.username, u.email, u.phone, u.alias, u.status, u.created_at,
	COALESCE(s.order_count, 0) AS order_count, COALESCE(s.total_spent, 0) AS total_spent, s.last_order_at
	FROM users u` + customerStatsJoin

// AdminListCustomers: GET /api/admin/customers?q=&sort=newest|orders|spent&page=&per_page=(<=100)
func (a *App) AdminListCustomers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	orderBy, ok := customerOrderBy(strings.TrimSpace(q.Get("sort")))
	if !ok {
		writeError(w, http.StatusBadRequest, "Urutan tidak dikenal (pilihan: newest, orders, spent)")
		return
	}
	where := customerBaseWhere
	var args []any
	if s := strings.TrimSpace(truncateUTF8(q.Get("q"), 100)); s != "" {
		like := "%" + escapeLike(s) + "%"
		cond := `u.name LIKE ? OR u.username LIKE ? OR u.email LIKE ? OR u.phone LIKE ? OR u.alias LIKE ?`
		args = append(args, like, like, like, like, like)
		if p, err := NormalizePhone(s); err == nil && p != "" {
			cond += ` OR u.phone LIKE ?`
			args = append(args, "%"+escapeLike(p)+"%")
		}
		where += ` AND (` + cond + `)`
	}
	page, perPage := pageParams(r)
	db := a.db.Load().WithContext(r.Context())
	var total int64
	if err := db.Raw(`SELECT COUNT(*) FROM users u`+where, args...).Scan(&total).Error; err != nil {
		log.Printf("admin pelanggan: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	items := []AdminCustomerDTO{}
	qargs := append(append([]any{}, args...), perPage, (page-1)*perPage)
	if err := db.Raw(customerSelect+where+` ORDER BY `+orderBy+` LIMIT ? OFFSET ?`, qargs...).Scan(&items).Error; err != nil {
		log.Printf("admin pelanggan: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "perPage": perPage})
}

type customerOrderDTO struct {
	ID          uint64    `json:"id" gorm:"column:id"`
	OrderNo     string    `json:"orderNo" gorm:"column:order_no"`
	Status      string    `json:"status" gorm:"column:status"`
	StatusLabel string    `json:"statusLabel" gorm:"-"`
	Total       int64     `json:"total" gorm:"column:total"`
	CreatedAt   time.Time `json:"createdAt" gorm:"column:created_at"`
}

func loadAdminCustomer(db *gorm.DB, id uint64) (*AdminCustomerDTO, error) {
	var rows []AdminCustomerDTO
	if err := db.Raw(customerSelect+customerBaseWhere+` AND u.id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &rows[0], nil
}

// AdminGetCustomer: GET /api/admin/customers/{id} -> data akun + statistik + 10 pesanan terakhir.
func (a *App) AdminGetCustomer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgCustomerMissing)
		return
	}
	db := a.db.Load().WithContext(r.Context())
	c, err := loadAdminCustomer(db, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, msgCustomerMissing)
		return
	}
	if err != nil {
		log.Printf("admin detail pelanggan: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	recent := []customerOrderDTO{}
	if err := db.Raw(`SELECT id, order_no, status, total, created_at FROM orders
		WHERE user_id = ? AND deleted_at IS NULL AND order_no IS NOT NULL
		ORDER BY created_at DESC, id DESC LIMIT 10`, id).Scan(&recent).Error; err != nil {
		log.Printf("admin detail pelanggan (pesanan): %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	for i := range recent {
		recent[i].StatusLabel = statusLabel(recent[i].Status)
	}
	writeJSON(w, http.StatusOK, map[string]any{"customer": c, "recentOrders": recent})
}

type aliasInput struct {
	Alias json.RawMessage `json:"alias"`
}

// parseAliasInput: {"alias": "teks"} atau {"alias": null}; field wajib ada.
func parseAliasInput(in aliasInput) (*string, error) {
	raw := strings.TrimSpace(string(in.Alias))
	if raw == "" {
		return nil, errors.New("Field alias wajib dikirim (string, kosong = hapus alias)")
	}
	if raw == "null" {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(in.Alias, &s); err != nil {
		return nil, errors.New("Alias harus berupa teks")
	}
	return NormalizeAlias(s)
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// AdminUpdateCustomerAlias: PATCH /api/admin/customers/{id}/alias {alias}. Perubahan dan activity
// log customer.alias_update ditulis dalam SATU transaksi. Hanya akun role customer yang belum dihapus.
func (a *App) AdminUpdateCustomerAlias(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgCustomerMissing)
		return
	}
	if ok, wait := a.aliasLimiter.Allow("admin:" + strconv.FormatUint(admin.ID, 10)); !ok {
		retryAfter(w, wait)
		writeError(w, http.StatusTooManyRequests, msgTooManyAdmin)
		return
	}
	var in aliasInput
	if err := decodeJSON(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	alias, err := parseAliasInput(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	db := a.db.Load().WithContext(r.Context())
	err = db.Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ID       uint64  `gorm:"column:id"`
			PublicID string  `gorm:"column:public_id"`
			Alias    *string `gorm:"column:alias"`
		}
		if err := tx.Raw(`SELECT id, public_id, alias FROM users
			WHERE id = ? AND deleted_at IS NULL AND role = 'customer' FOR UPDATE`, id).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return &httpError{http.StatusNotFound, msgCustomerMissing}
		}
		old := rows[0].Alias
		if strOrEmpty(old) == strOrEmpty(alias) {
			return nil // tidak berubah: tanpa UPDATE dan tanpa log
		}
		if err := tx.Exec(`UPDATE users SET alias = ? WHERE id = ?`, alias, id).Error; err != nil {
			return err
		}
		// PII internal: hanya alias lama & baru, tanpa data akun lain.
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "customer.alias_update",
			EntityType: "user", EntityID: rows[0].PublicID,
			Summary: "Alias pelanggan diubah",
			Details: map[string]any{"alias": map[string]any{"dari": old, "menjadi": alias}},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "ubah alias pelanggan")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "alias": alias})
}

// loadCustomerCount untuk ringkasan admin (Dashboard).
func loadCustomerCount(db *gorm.DB) (int64, error) {
	var n int64
	err := db.Raw(`SELECT COUNT(*) FROM users u` + customerBaseWhere).Scan(&n).Error
	return n, err
}
