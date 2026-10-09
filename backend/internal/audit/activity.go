package audit

// Pencatatan aktivitas (tabel activity_logs, append-only bagi user aplikasi).
//
// - Perubahan data admin (produk/kategori/purge) dicatat di TRANSAKSI yang sama dengan
//   perubahannya (Logger.Log(tx, ...)), sehingga tidak ada perubahan tanpa log.
// - Log autentikasi bersifat best-effort (Logger.Auth): kegagalan menulis log tidak
//   menggagalkan permintaan.
// - details TIDAK PERNAH memuat password, hash, token, header Authorization/cookie,
//   atau id_token/credential Google (lihat sanitizeDetails); nilai panjang dipotong.

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/platform"
)

const (
	maxDetailString = 300  // karakter per nilai string
	maxDetailBytes  = 8192 // ukuran JSON details maksimal
	maxDetailDepth  = 4
	maxDetailItems  = 50
)

// Entry adalah satu baris activity log yang akan ditulis.
type Entry struct {
	UserID     *uint64
	ActorLabel string
	Action     string
	EntityType string
	EntityID   string
	Summary    string
	Details    map[string]any
	IP         net.IP
	UserAgent  string
}

// sensitiveKey: kunci yang nilainya tidak boleh pernah masuk log.
func sensitiveKey(k string) bool {
	k = strings.ToLower(k)
	for _, s := range []string{"password", "passwd", "hash", "token", "secret", "authorization",
		"cookie", "credential", "id_token", "idtoken", "jwt", "assertion", "session", "otp", "apikey", "api_key"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// sanitizeDetails membuat salinan details yang aman untuk disimpan.
func sanitizeDetails(v any, depth int) any {
	if depth > maxDetailDepth {
		return "[terlalu dalam]"
	}
	switch t := v.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return t
	case string:
		return truncateRunes(t, maxDetailString)
	case *string:
		if t == nil {
			return nil
		}
		return truncateRunes(*t, maxDetailString)
	case map[string]any:
		out := make(map[string]any, len(t))
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i >= maxDetailItems {
				out["_dipotong"] = true
				break
			}
			if sensitiveKey(k) {
				out[truncateRunes(k, 64)] = "[disembunyikan]"
				continue
			}
			out[truncateRunes(k, 64)] = sanitizeDetails(t[k], depth+1)
		}
		return out
	case []any:
		n := min(len(t), maxDetailItems)
		out := make([]any, 0, n)
		for _, x := range t[:n] {
			out = append(out, sanitizeDetails(x, depth+1))
		}
		return out
	case []string:
		n := min(len(t), maxDetailItems)
		out := make([]any, 0, n)
		for _, x := range t[:n] {
			out = append(out, truncateRunes(x, maxDetailString))
		}
		return out
	default:
		// Tipe lain (struct dsb.) diserialisasi lalu dibaca ulang sebagai map/slice.
		b, err := json.Marshal(t)
		if err != nil {
			return "[tidak dapat dicatat]"
		}
		var generic any
		if json.Unmarshal(b, &generic) != nil {
			return "[tidak dapat dicatat]"
		}
		if _, isStr := generic.(string); isStr {
			return truncateRunes(generic.(string), maxDetailString)
		}
		return sanitizeDetails(generic, depth)
	}
}

func truncateRunes(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// detailsJSON menghasilkan JSON details yang sudah disanitasi (nil bila kosong).
func detailsJSON(d map[string]any) *string {
	if len(d) == 0 {
		return nil
	}
	b, err := json.Marshal(sanitizeDetails(d, 0))
	if err != nil {
		return nil
	}
	if len(b) > maxDetailBytes {
		b, _ = json.Marshal(map[string]any{"_dipotong": true, "ukuran": len(b)})
	}
	s := string(b)
	return &s
}

func (e Entry) toRow() ActivityLog {
	row := ActivityLog{
		UserID:     e.UserID,
		ActorLabel: platform.StrPtr(platform.TruncateUTF8(e.ActorLabel, 100)),
		Action:     platform.TruncateUTF8(e.Action, 50),
		EntityType: platform.StrPtr(platform.TruncateUTF8(e.EntityType, 50)),
		EntityID:   platform.StrPtr(platform.TruncateUTF8(e.EntityID, 64)),
		Summary:    platform.TruncateUTF8(e.Summary, 255),
		Details:    detailsJSON(e.Details),
		IP:         platform.IPBytes(e.IP),
		UserAgent:  platform.StrPtr(platform.TruncateUTF8(e.UserAgent, 255)),
	}
	if row.Summary == "" {
		row.Summary = row.Action
	}
	return row
}

// ActivityLog memetakan tabel activity_logs (migrations/004). Aplikasi hanya INSERT/SELECT.
type ActivityLog struct {
	ID         uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	UserID     *uint64   `gorm:"column:user_id"`
	ActorLabel *string   `gorm:"column:actor_label"`
	Action     string    `gorm:"column:action"`
	EntityType *string   `gorm:"column:entity_type"`
	EntityID   *string   `gorm:"column:entity_id"`
	Summary    string    `gorm:"column:summary"`
	Details    *string   `gorm:"column:details"` // JSON (string: []byte ditolak MySQL untuk kolom JSON)
	IP         []byte    `gorm:"column:ip"`
	UserAgent  *string   `gorm:"column:user_agent"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (ActivityLog) TableName() string { return "activity_logs" }

// Logger menulis activity log. Dependensi diberikan eksplisit (tanpa struct App):
// db = koneksi saat ini (nil bila belum terhubung), clientIP = penentu IP klien, now = jam.
type Logger struct {
	db       func() *gorm.DB
	clientIP func(*http.Request) net.IP
	now      func() time.Time
}

// NewLogger: now dibaca setiap kali menulis (jam bisa diganti saat uji).
func NewLogger(db func() *gorm.DB, clientIP func(*http.Request) net.IP, now func() time.Time) *Logger {
	return &Logger{db: db, clientIP: clientIP, now: now}
}

// Log menulis log memakai db/tx yang diberikan. Untuk perubahan admin, panggil
// dengan tx transaksi yang sama agar log dan perubahan sukses/gagal bersama.
func (l *Logger) Log(db *gorm.DB, e Entry) error {
	row := e.toRow()
	row.CreatedAt = l.now()
	return db.Create(&row).Error
}

// Auth: pencatatan best-effort (tidak menggagalkan permintaan).
func (l *Logger) Auth(r *http.Request, e Entry) {
	db := l.db()
	if db == nil {
		return
	}
	if e.IP == nil {
		e.IP = l.clientIP(r)
	}
	if e.UserAgent == "" {
		e.UserAgent = r.UserAgent()
	}
	if err := l.Log(db.WithContext(r.Context()), e); err != nil {
		log.Printf("activity log (%s) gagal ditulis: %v", e.Action, err)
	}
}

// ReqMeta mengisi IP dan User-Agent dari permintaan.
func (l *Logger) ReqMeta(r *http.Request, e Entry) Entry {
	e.IP = l.clientIP(r)
	e.UserAgent = r.UserAgent()
	return e
}

// DiffMaps menghasilkan {field: {from, to}} untuk field yang berubah.
func DiffMaps(before, after map[string]any) map[string]any {
	out := map[string]any{}
	for k, nv := range after {
		ov := before[k]
		bo, _ := json.Marshal(ov)
		bn, _ := json.Marshal(nv)
		if string(bo) != string(bn) {
			out[k] = map[string]any{"dari": ov, "menjadi": nv}
		}
	}
	return out
}
