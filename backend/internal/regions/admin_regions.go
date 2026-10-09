package regions

// Admin -> Data Wilayah (/api/admin/regions/*). Dijaga requireAdmin (Cloudflare Access + CSRF/Origin
// untuk mutasi). Semua aksi yang menulis data dicatat di activity log dalam transaksi yang sama.

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"gorm.io/gorm"

	"mihanstore/internal/platform"
)

type regionRunRow struct {
	ID            uint64     `gorm:"column:id"`
	Status        string     `gorm:"column:status"`
	StartedLabel  string     `gorm:"column:started_label"`
	StartedAt     time.Time  `gorm:"column:started_at"`
	FinishedAt    *time.Time `gorm:"column:finished_at"`
	SavedAt       *time.Time `gorm:"column:saved_at"`
	SavedLabel    *string    `gorm:"column:saved_label"`
	ProgressDone  int64      `gorm:"column:progress_done"`
	ProgressTotal int64      `gorm:"column:progress_total"`
	Counts        *string    `gorm:"column:counts"`
	Diff          *string    `gorm:"column:diff"`
	Stats         *string    `gorm:"column:stats"`
	Source        *string    `gorm:"column:src"`
	Error         *string    `gorm:"column:error"`
}

const regionRunCols = `id, status, started_label, started_at, finished_at, saved_at, saved_label, progress_done, progress_total,
  counts, diff, stats, DATE_FORMAT(source_updated_at, '%Y-%m-%d') AS src, error`

type RunDTO struct {
	ID              uint64          `json:"id"`
	Status          string          `json:"status"`
	StatusLabel     string          `json:"statusLabel"`
	StartedBy       string          `json:"startedBy"`
	StartedAt       time.Time       `json:"startedAt"`
	FinishedAt      *time.Time      `json:"finishedAt"`
	SavedAt         *time.Time      `json:"savedAt"`
	SavedBy         *string         `json:"savedBy"`
	Progress        map[string]any  `json:"progress"`
	Counts          json.RawMessage `json:"counts"`
	Diff            json.RawMessage `json:"diff"`
	Stats           json.RawMessage `json:"stats"`
	SourceUpdatedAt *string         `json:"sourceUpdatedAt"`
	Error           *string         `json:"error"`
}

func rawOrNull(s *string) json.RawMessage {
	if s == nil || *s == "" {
		return json.RawMessage("null")
	}
	return json.RawMessage(*s)
}

func (r regionRunRow) dto() RunDTO {
	phase := 0
	if r.Stats != nil {
		var st regionStats
		if json.Unmarshal([]byte(*r.Stats), &st) == nil {
			phase = st.Phase
		}
	}
	return RunDTO{
		ID: r.ID, Status: r.Status, StatusLabel: regionRunLabels[r.Status], StartedBy: r.StartedLabel, StartedAt: r.StartedAt,
		FinishedAt: r.FinishedAt, SavedAt: r.SavedAt, SavedBy: r.SavedLabel,
		Progress: map[string]any{"done": r.ProgressDone, "total": r.ProgressTotal, "phase": phase,
			"phaseLabel": regionLevelNames[max(0, min(phase, 4))]},
		Counts: rawOrNull(r.Counts), Diff: rawOrNull(r.Diff), Stats: rawOrNull(r.Stats),
		SourceUpdatedAt: r.Source, Error: r.Error,
	}
}

func loadRegionRun(db *gorm.DB, id uint64) (*regionRunRow, error) {
	var rows []regionRunRow
	if err := db.Raw(`SELECT `+regionRunCols+` FROM region_import_runs WHERE id = ? AND deleted_at IS NULL`, id).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// regionStoredStatus: ringkasan data tersimpan (kosong bila belum ada).
func regionStoredStatus(db *gorm.DB) (map[string]any, error) {
	var lv []struct {
		Level    int   `gorm:"column:level"`
		Datasets int64 `gorm:"column:datasets"`
		Items    int64 `gorm:"column:items"`
		Bytes    int64 `gorm:"column:bytes"`
	}
	if err := db.Raw(`SELECT level, COUNT(*) AS datasets, COALESCE(SUM(item_count), 0) AS items,
		COALESCE(SUM(JSON_STORAGE_SIZE(data)), 0) AS bytes FROM region_datasets WHERE deleted_at IS NULL GROUP BY level`).Scan(&lv).Error; err != nil {
		return nil, err
	}
	var items, datasets [5]int64
	var bytes, total int64
	for _, x := range lv {
		if x.Level >= 1 && x.Level <= 4 {
			items[x.Level], datasets[x.Level] = x.Items, x.Datasets
			bytes += x.Bytes
			total += x.Datasets
		}
	}
	var last []struct {
		ID      uint64     `gorm:"column:id"`
		SavedAt *time.Time `gorm:"column:saved_at"`
		SavedBy *string    `gorm:"column:saved_label"`
		Source  *string    `gorm:"column:src"`
	}
	if err := db.Raw(`SELECT id, saved_at, saved_label, DATE_FORMAT(source_updated_at, '%Y-%m-%d') AS src FROM region_import_runs
		WHERE status = 'saved' AND deleted_at IS NULL ORDER BY saved_at DESC, id DESC LIMIT 1`).Scan(&last).Error; err != nil {
		return nil, err
	}
	out := map[string]any{
		"hasData": items[1] > 0, "datasetTotal": total, "datasets": levelMap(datasets), "items": levelMap(items),
		"sizeBytes": bytes, "sourceUpdatedAt": nil, "lastSaved": nil,
	}
	if len(last) > 0 {
		out["sourceUpdatedAt"] = last[0].Source
		out["lastSaved"] = map[string]any{"runId": last[0].ID, "at": last[0].SavedAt, "by": last[0].SavedBy}
	}
	return out, nil
}

func (s *Service) regionStatusPayload(db *gorm.DB) (map[string]any, error) {
	stored, err := regionStoredStatus(db)
	if err != nil {
		return nil, err
	}
	var rows []regionRunRow
	if err := db.Raw(`SELECT ` + regionRunCols + ` FROM region_import_runs WHERE deleted_at IS NULL ORDER BY id DESC LIMIT 10`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	runs := make([]RunDTO, 0, len(rows))
	var active *RunDTO
	for _, r := range rows {
		d := r.dto()
		runs = append(runs, d)
		if active == nil && (r.Status == RunRunning || r.Status == RunStaged) {
			dd := d
			active = &dd
		}
	}
	if active == nil {
		var act []regionRunRow
		if err := db.Raw(`SELECT ` + regionRunCols + ` FROM region_import_runs WHERE status IN ('running','staged') AND deleted_at IS NULL LIMIT 1`).Scan(&act).Error; err != nil {
			return nil, err
		}
		if len(act) > 0 {
			d := act[0].dto()
			active = &d
		}
	}
	return map[string]any{"stored": stored, "activeRun": active, "runs": runs, "source": s.FetchCfg.Base,
		"stagedTtlHours": int(regionStagedTTL.Hours())}, nil
}

// AdminStatus: GET /api/admin/regions/status
func (s *Service) AdminStatus(w http.ResponseWriter, r *http.Request) {
	p, err := s.regionStatusPayload(s.db().WithContext(r.Context()))
	if err != nil {
		log.Printf("admin wilayah status: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, p)
}

// AdminFetch: POST /api/admin/regions/fetch — memulai run di latar belakang (202).
func (s *Service) AdminFetch(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if err := platform.DecodeJSON(w, r, &in, true); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	db := s.db()
	act := s.actor(r)
	id, err := s.StartRun(r.Context(), db, act)
	if err != nil {
		s.respondTxError(w, err, "admin wilayah fetch")
		return
	}
	s.launchRun(id, act)
	run, err := loadRegionRun(db.WithContext(r.Context()), id)
	if err != nil || run == nil {
		platform.WriteJSON(w, http.StatusAccepted, map[string]any{"run": map[string]any{"id": id, "status": RunRunning}})
		return
	}
	platform.WriteJSON(w, http.StatusAccepted, map[string]any{"run": run.dto()})
}

func runIDFrom(r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	return id, err == nil && id > 0
}

// AdminRun: GET /api/admin/regions/runs/{id}
func (s *Service) AdminRun(w http.ResponseWriter, r *http.Request) {
	id, ok := runIDFrom(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgRegionRunNotFound)
		return
	}
	run, err := loadRegionRun(s.db().WithContext(r.Context()), id)
	if err != nil {
		log.Printf("admin wilayah run: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	if run == nil {
		platform.WriteError(w, http.StatusNotFound, msgRegionRunNotFound)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"run": run.dto()})
}

// AdminSave: POST /api/admin/regions/runs/{id}/save
func (s *Service) AdminSave(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if err := platform.DecodeJSON(w, r, &in, true); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	id, ok := runIDFrom(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgRegionRunNotFound)
		return
	}
	db := s.db()
	res, err := s.SaveRun(r.Context(), db, id, s.actor(r))
	if err != nil {
		s.respondTxError(w, err, "admin wilayah simpan")
		return
	}
	p, err := s.regionStatusPayload(db.WithContext(r.Context()))
	if err != nil {
		platform.WriteJSON(w, http.StatusOK, map[string]any{"result": res})
		return
	}
	p["result"] = res
	platform.WriteJSON(w, http.StatusOK, p)
}

// AdminDiscard: POST /api/admin/regions/runs/{id}/discard (juga membatalkan run yang berjalan).
func (s *Service) AdminDiscard(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if err := platform.DecodeJSON(w, r, &in, true); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	id, ok := runIDFrom(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgRegionRunNotFound)
		return
	}
	db := s.db()
	to, err := s.DiscardRun(r.Context(), db, id, s.actor(r))
	if err != nil {
		var he *platform.HTTPError
		if !errors.As(err, &he) {
			log.Printf("admin wilayah buang: %v", err)
		}
		s.respondTxError(w, err, "admin wilayah buang")
		return
	}
	p, err := s.regionStatusPayload(db.WithContext(r.Context()))
	if err != nil {
		platform.WriteJSON(w, http.StatusOK, map[string]any{"status": to})
		return
	}
	p["status"] = to
	platform.WriteJSON(w, http.StatusOK, p)
}
