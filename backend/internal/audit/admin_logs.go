package audit

// Daftar dan penghapusan activity log oleh admin. Handler HTTP memakai dependensi eksplisit (AdminAPI):
// identitas admin (modul admin/identity) dan penerjemah galat transaksi diberikan perakit (internal/app).

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/platform"
)

const logRetentionDays = 180

// LogFilter: filter daftar log (To eksklusif, UTC).
type LogFilter struct {
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

// QueryLogs mengambil satu halaman log beserta total.
func QueryLogs(db *gorm.DB, f LogFilter) ([]AdminLogDTO, int64, error) {
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
		like := "%" + platform.EscapeLike(strings.ToLower(f.Actor)) + "%"
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

// AdminAPI: rute admin untuk activity log.
type AdminAPI struct {
	Logger *Logger
	DB     func() *gorm.DB
	// Actor mengembalikan id pengguna dan label admin yang sedang login (untuk log purge).
	Actor func(r *http.Request) (userID *uint64, label string)
	// RespondTxError menulis galat transaksi (httpError / 500) ke klien.
	RespondTxError func(w http.ResponseWriter, err error, ctx string)
}

// ListLogs: GET /api/admin/activity-logs
func (a *AdminAPI) ListLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := LogFilter{
		Action:     platform.TruncateUTF8(strings.TrimSpace(q.Get("action")), 50),
		Actor:      platform.TruncateUTF8(strings.TrimSpace(q.Get("actor")), 100),
		EntityType: platform.TruncateUTF8(strings.TrimSpace(q.Get("entity_type")), 50),
	}
	from, ok1 := platform.ParseWIBDate(q.Get("from"))
	to, ok2 := platform.ParseWIBDate(q.Get("to"))
	if !ok1 || !ok2 {
		platform.WriteError(w, http.StatusBadRequest, "Format tanggal harus YYYY-MM-DD")
		return
	}
	f.From = from
	if to != nil {
		end := to.Add(24 * time.Hour) // inklusif sampai akhir hari "to"
		f.To = &end
	}
	f.Page, f.PerPage = platform.PageParams(r)
	items, total, err := QueryLogs(a.DB().WithContext(r.Context()), f)
	if err != nil {
		log.Printf("admin log: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": f.Page, "perPage": f.PerPage})
}

// AdminPurgeLogs memanggil procedure purge_activity_logs (hanya menghapus log > 180 hari)
// dan mencatat activity_log.purge dalam transaksi yang sama.
// PurgeLogs: POST /api/admin/activity-logs/purge
func (a *AdminAPI) PurgeLogs(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if err := platform.DecodeJSON(w, r, &body, true); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	db := a.DB().WithContext(r.Context())
	userID, label := a.Actor(r)
	var deleted int64
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`CALL purge_activity_logs(@mihan_purged)`).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT COALESCE(@mihan_purged, 0)`).Scan(&deleted).Error; err != nil {
			return err
		}
		return a.Logger.Log(tx, a.Logger.ReqMeta(r, Entry{
			UserID: userID, ActorLabel: label, Action: "activity_log.purge",
			EntityType: "activity_log",
			Summary:    "Log aktivitas lebih dari 6 bulan dihapus",
			Details:    map[string]any{"jumlahDihapus": deleted, "batasHari": logRetentionDays},
		}))
	})
	if err != nil {
		a.RespondTxError(w, err, "hapus log")
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "deleted": deleted,
		"message": "Log lebih dari 6 bulan dihapus"})
}
