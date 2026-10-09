package admin

import (
	"log"
	"net/http"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/audit"
	"mihanstore/internal/identity"
	"mihanstore/internal/platform"
)

func (a *Service) AdminMe(w http.ResponseWriter, r *http.Request) {
	u := From(r.Context())
	platform.WriteJSON(w, http.StatusOK, map[string]any{"user": map[string]any{
		"id": u.PublicID, "username": u.Username, "email": u.Email, "name": u.Name, "role": u.Role,
	}})
}

func (a *Service) AdminSummary(w http.ResponseWriter, r *http.Request) {
	db := a.db().WithContext(r.Context())
	var counts struct {
		Total    int64 `gorm:"column:total" json:"total"`
		Active   int64 `gorm:"column:active" json:"active"`
		Inactive int64 `gorm:"column:inactive" json:"inactive"`
	}
	if err := db.Raw(`SELECT COUNT(*) AS total, COALESCE(SUM(is_active = 1), 0) AS active,
		COALESCE(SUM(is_active = 0), 0) AS inactive FROM products WHERE deleted_at IS NULL`).Scan(&counts).Error; err != nil {
		log.Printf("admin summary: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	var cats int64
	if err := db.Raw("SELECT count(*) FROM `categories` WHERE `categories`.`deleted_at` IS NULL").Scan(&cats).Error; err != nil {
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	logs, _, err := audit.QueryLogs(db, audit.LogFilter{Page: 1, PerPage: 10})
	if err != nil {
		log.Printf("admin summary logs: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	orders, err := a.loadOrderCounts(db)
	if err != nil {
		log.Printf("admin summary orders: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	customers, err := identity.LoadCustomerCount(db)
	if err != nil {
		log.Printf("admin summary customers: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{
		"products":       counts,
		"customers":      customers,
		"categories":     cats,
		"orders":         orders,
		"recentActivity": logs,
	})
}

// orderCounts untuk ringkasan admin.
type orderCounts struct {
	PendingConfirmation int64 `json:"pendingConfirmation" gorm:"column:pending_confirmation"`
	PendingPayment      int64 `json:"pendingPayment" gorm:"column:pending_payment"`
	Paid                int64 `json:"paid" gorm:"column:paid"`
	Last7Days           int64 `json:"last7Days" gorm:"column:last7"`
}

func (a *Service) loadOrderCounts(db *gorm.DB) (orderCounts, error) {
	var c orderCounts
	err := db.Raw(`SELECT COALESCE(SUM(status = 'pending_confirmation'), 0) AS pending_confirmation,
		COALESCE(SUM(status = 'pending_payment'), 0) AS pending_payment,
		COALESCE(SUM(status = 'paid'), 0) AS paid,
		COALESCE(SUM(created_at >= ?), 0) AS last7
		FROM orders WHERE deleted_at IS NULL AND order_no IS NOT NULL`, a.now().Add(-7*24*time.Hour)).Scan(&c).Error
	return c, err
}
