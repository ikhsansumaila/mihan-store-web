package main

// Daftar dan penghapusan activity log oleh admin.

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Rentang tanggal filter ditafsirkan dalam WIB (UTC+7); data disimpan dalam UTC.
var wib = time.FixedZone("WIB", 7*3600)

const logRetentionDays = 180

type logFilter struct {
	Action     string
	Actor      string
	EntityType string
	From, To   *time.Time // UTC; To eksklusif
	Page       int
	PerPage    int
}

type AdminLogDTO struct {
	ID         uint64          `json:"id"`
	CreatedAt  time.Time       `json:"createdAt"`
	Action     string          `json:"action"`
	EntityType *string         `json:"entityType"`
	EntityID   *string         `json:"entityId"`
	Summary    string          `json:"summary"`
	Details    json.RawMessage `json:"details"`
	ActorLabel *string         `json:"actorLabel"`
	Username   *string         `json:"username"`
	UserEmail  *string         `json:"userEmail"`
	IP         string          `json:"ip"`
	UserAgent  *string         `json:"userAgent"`
}

type logRow struct {
	ID         uint64    `gorm:"column:id"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	Action     string    `gorm:"column:action"`
	EntityType *string   `gorm:"column:entity_type"`
	EntityID   *string   `gorm:"column:entity_id"`
	Summary    string    `gorm:"column:summary"`
	Details    *string   `gorm:"column:details"`
	ActorLabel *string   `gorm:"column:actor_label"`
	Username   *string   `gorm:"column:username"`
	UserEmail  *string   `gorm:"column:user_email"`
	IP         []byte    `gorm:"column:ip"`
	UserAgent  *string   `gorm:"column:user_agent"`
}

func ipString(b []byte) string {
	if len(b) == 4 || len(b) == 16 {
		return net.IP(b).String()
	}
	return ""
}

func (a *App) queryLogs(db *gorm.DB, f logFilter) ([]AdminLogDTO, int64, error) {
	where := ` WHERE 1=1`
	var args []any
	if f.Action != "" {
		where += ` AND l.action = ?`
		args = append(args, f.Action)
	}
	if f.EntityType != "" {
		where += ` AND l.entity_type = ?`
		args = append(args, f.EntityType)
	}
	if f.Actor != "" {
		like := "%" + escapeLike(strings.ToLower(f.Actor)) + "%"
		where += ` AND (l.actor_label LIKE ? OR u.email LIKE ? OR u.username LIKE ?)`
		args = append(args, like, like, like)
	}
	if f.From != nil {
		where += ` AND l.created_at >= ?`
		args = append(args, *f.From)
	}
	if f.To != nil {
		where += ` AND l.created_at < ?`
		args = append(args, *f.To)
	}
	from := ` FROM activity_logs l LEFT JOIN users u ON u.id = l.user_id`
	var total int64
	if err := db.Raw(`SELECT COUNT(*)`+from+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []logRow
	qargs := append(append([]any{}, args...), f.PerPage, (f.Page-1)*f.PerPage)
	if err := db.Raw(`SELECT l.id, l.created_at, l.action, l.entity_type, l.entity_id, l.summary, l.details,
		l.actor_label, u.username, u.email AS user_email, l.ip, l.user_agent`+from+where+
		` ORDER BY l.created_at DESC, l.id DESC LIMIT ? OFFSET ?`, qargs...).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]AdminLogDTO, 0, len(rows))
	for _, r := range rows {
		d := AdminLogDTO{ID: r.ID, CreatedAt: r.CreatedAt, Action: r.Action, EntityType: r.EntityType,
			EntityID: r.EntityID, Summary: r.Summary, ActorLabel: r.ActorLabel, Username: r.Username,
			UserEmail: r.UserEmail, IP: ipString(r.IP), UserAgent: r.UserAgent, Details: json.RawMessage("null")}
		if r.Details != nil && json.Valid([]byte(*r.Details)) {
			d.Details = json.RawMessage(*r.Details)
		}
		out = append(out, d)
	}
	return out, total, nil
}

func parseWIBDate(s string) (*time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	t, err := time.ParseInLocation("2006-01-02", s, wib)
	if err != nil {
		return nil, false
	}
	u := t.UTC()
	return &u, true
}

func (a *App) AdminListLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := logFilter{
		Action:     truncateUTF8(strings.TrimSpace(q.Get("action")), 50),
		Actor:      truncateUTF8(strings.TrimSpace(q.Get("actor")), 100),
		EntityType: truncateUTF8(strings.TrimSpace(q.Get("entity_type")), 50),
	}
	from, ok1 := parseWIBDate(q.Get("from"))
	to, ok2 := parseWIBDate(q.Get("to"))
	if !ok1 || !ok2 {
		writeError(w, http.StatusBadRequest, "Format tanggal harus YYYY-MM-DD")
		return
	}
	f.From = from
	if to != nil {
		end := to.Add(24 * time.Hour) // inklusif sampai akhir hari "to"
		f.To = &end
	}
	f.Page, f.PerPage = pageParams(r)
	items, total, err := a.queryLogs(a.db.Load().WithContext(r.Context()), f)
	if err != nil {
		log.Printf("admin log: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": f.Page, "perPage": f.PerPage})
}

// AdminPurgeLogs memanggil procedure purge_activity_logs (hanya menghapus log > 180 hari)
// dan mencatat activity_log.purge dalam transaksi yang sama.
func (a *App) AdminPurgeLogs(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	var body struct{}
	if err := decodeJSON(w, r, &body, true); err != nil {
		respondDecodeError(w, err)
		return
	}
	db := a.db.Load().WithContext(r.Context())
	var deleted int64
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`CALL purge_activity_logs(@mihan_purged)`).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT COALESCE(@mihan_purged, 0)`).Scan(&deleted).Error; err != nil {
			return err
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "activity_log.purge",
			EntityType: "activity_log",
			Summary:    "Log aktivitas lebih dari 6 bulan dihapus",
			Details:    map[string]any{"jumlahDihapus": deleted, "batasHari": logRetentionDays},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "hapus log")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "deleted": deleted,
		"message": "Log lebih dari 6 bulan dihapus"})
}
