package main

// Pencatatan aktivitas (tabel activity_logs, append-only bagi user aplikasi).
//
// - Perubahan data admin (produk/kategori/purge) dicatat di TRANSAKSI yang sama dengan
//   perubahannya (logActivity(tx, ...)), sehingga tidak ada perubahan tanpa log.
// - Log autentikasi bersifat best-effort (logAuth): kegagalan menulis log tidak
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

	"gorm.io/gorm"
)

const (
	maxDetailString = 300  // karakter per nilai string
	maxDetailBytes  = 8192 // ukuran JSON details maksimal
	maxDetailDepth  = 4
	maxDetailItems  = 50
)

// LogEntry adalah satu baris activity log yang akan ditulis.
type LogEntry struct {
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

func (e LogEntry) toRow() ActivityLog {
	row := ActivityLog{
		UserID:     e.UserID,
		ActorLabel: strPtr(truncateUTF8(e.ActorLabel, 100)),
		Action:     truncateUTF8(e.Action, 50),
		EntityType: strPtr(truncateUTF8(e.EntityType, 50)),
		EntityID:   strPtr(truncateUTF8(e.EntityID, 64)),
		Summary:    truncateUTF8(e.Summary, 255),
		Details:    detailsJSON(e.Details),
		IP:         ipBytes(e.IP),
		UserAgent:  strPtr(truncateUTF8(e.UserAgent, 255)),
	}
	if row.Summary == "" {
		row.Summary = row.Action
	}
	return row
}

// logActivity menulis log memakai db/tx yang diberikan. Untuk perubahan admin, panggil
// dengan tx transaksi yang sama agar log dan perubahan sukses/gagal bersama.
func (a *App) logActivity(db *gorm.DB, e LogEntry) error {
	row := e.toRow()
	row.CreatedAt = a.now()
	return db.Create(&row).Error
}

// logAuth: pencatatan best-effort (tidak menggagalkan permintaan).
func (a *App) logAuth(r *http.Request, e LogEntry) {
	db := a.db.Load()
	if db == nil {
		return
	}
	if e.IP == nil {
		e.IP = a.ips.ClientIP(r)
	}
	if e.UserAgent == "" {
		e.UserAgent = r.UserAgent()
	}
	if err := a.logActivity(db.WithContext(r.Context()), e); err != nil {
		log.Printf("activity log (%s) gagal ditulis: %v", e.Action, err)
	}
}

// reqMeta mengisi IP dan User-Agent dari permintaan.
func (a *App) reqMeta(r *http.Request, e LogEntry) LogEntry {
	e.IP = a.ips.ClientIP(r)
	e.UserAgent = r.UserAgent()
	return e
}

// diffMaps menghasilkan {field: {from, to}} untuk field yang berubah.
func diffMaps(before, after map[string]any) map[string]any {
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

func uid(u *User) *uint64 {
	if u == nil {
		return nil
	}
	id := u.ID
	return &id
}

func actorOf(u *User) string {
	if u == nil {
		return ""
	}
	if u.Email != "" {
		return u.Email
	}
	return u.Username
}
