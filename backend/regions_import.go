package main

// Siklus hidup impor data wilayah (dipakai fitur admin DAN subperintah CLI `import-regions`):
//
//   startRegionRun   -> baris region_import_runs status 'running' (+ log regions.fetch_start)
//   runRegionFetch   -> fetcher berjenjang; tiap dataset ditulis ke region_import_staging;
//                       selesai -> 'staged' + ringkasan diff (log regions.fetch_done),
//                       gagal   -> 'failed' + staging dihapus (log regions.fetch_failed)
//   saveRegionRun    -> upsert staging -> region_datasets dalam SATU transaksi (hanya dataset yang
//                       berubah di-UPDATE, yang baru di-INSERT, yang hilang di sumber di-soft-delete),
//                       staging dihapus, 'saved' (log regions.save), cache dibersihkan
//   discardRegionRun -> 'discarded' (staged) / 'cancelled' (running), staging dihapus (log regions.discard)
//
// Hanya satu run 'running'/'staged' pada satu waktu: dijaga kolom generated active_lock + UNIQUE di DB
// (berlaku lintas proses, termasuk CLI). Run 'running' yang tertinggal saat server mulai -> 'failed'.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"gorm.io/gorm"
)

const (
	RegionRunRunning   = "running"
	RegionRunStaged    = "staged"
	RegionRunSaved     = "saved"
	RegionRunDiscarded = "discarded"
	RegionRunFailed    = "failed"
	RegionRunCancelled = "cancelled"

	regionStagedTTL       = 24 * time.Hour
	regionProgressEvery   = 2 * time.Second
	regionBatch           = 500
	regionCLILabel        = "CLI (impor awal)"
	regionSystemActor     = "sistem"
	msgRegionRunActive    = "Masih ada pengambilan data wilayah yang berjalan atau menunggu keputusan simpan"
	msgRegionRunNotFound  = "Run tidak ditemukan"
	msgRegionRestartError = "Server dimulai ulang saat pengambilan berjalan"
)

var regionRunLabels = map[string]string{
	RegionRunRunning:   "Berjalan",
	RegionRunStaged:    "Menunggu keputusan simpan",
	RegionRunSaved:     "Tersimpan",
	RegionRunDiscarded: "Dibuang",
	RegionRunFailed:    "Gagal",
	RegionRunCancelled: "Dibatalkan",
}

// regionActor: siapa yang memicu aksi (admin lewat HTTP, atau sistem/CLI).
type regionActor struct {
	UserID *uint64
	Label  string
	IP     net.IP
	UA     string
}

func (a *App) adminRegionActor(r *http.Request) regionActor {
	u := adminFrom(r.Context())
	return regionActor{UserID: uid(u), Label: actorOf(u), IP: a.ips.ClientIP(r), UA: r.UserAgent()}
}

func (act regionActor) log(action, entityID, summary string, details map[string]any) LogEntry {
	label := act.Label
	if act.UserID == nil {
		label = regionSystemActor + ": " + act.Label
	}
	return LogEntry{UserID: act.UserID, ActorLabel: label, Action: action, EntityType: "region_import",
		EntityID: entityID, Summary: summary, Details: details, IP: act.IP, UserAgent: act.UA}
}

// regionRunRegistry: run yang berjalan di PROSES ini (untuk membatalkan dan menunggu goroutine).
type regionRunRegistry struct {
	mu  sync.Mutex
	m   map[uint64]*regionLiveRun
	all sync.WaitGroup
}

type regionLiveRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func newRegionRunRegistry() *regionRunRegistry {
	return &regionRunRegistry{m: map[uint64]*regionLiveRun{}}
}

func (g *regionRunRegistry) add(id uint64, cancel context.CancelFunc) *regionLiveRun {
	g.mu.Lock()
	defer g.mu.Unlock()
	lr := &regionLiveRun{cancel: cancel, done: make(chan struct{})}
	g.m[id] = lr
	g.all.Add(1)
	return lr
}

func (g *regionRunRegistry) finish(id uint64) {
	g.mu.Lock()
	lr := g.m[id]
	delete(g.m, id)
	g.mu.Unlock()
	if lr != nil {
		close(lr.done)
		g.all.Done()
	}
}

// stop membatalkan run di proses ini dan menunggu goroutine-nya selesai (maks. timeout).
func (g *regionRunRegistry) stop(id uint64, timeout time.Duration) {
	g.mu.Lock()
	lr := g.m[id]
	g.mu.Unlock()
	if lr == nil {
		return
	}
	lr.cancel()
	select {
	case <-lr.done:
	case <-time.After(timeout):
	}
}

func idStr(id uint64) string { return fmt.Sprintf("%d", id) }

// ---------- Mulai ----------

// startRegionRun membuat run baru (status running). Run staged > 24 jam dibuang otomatis lebih dulu.
func (a *App) startRegionRun(ctx context.Context, db *gorm.DB, act regionActor) (uint64, error) {
	var runID uint64
	now := a.now()
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var active []struct {
			ID         uint64     `gorm:"column:id"`
			Status     string     `gorm:"column:status"`
			FinishedAt *time.Time `gorm:"column:finished_at"`
		}
		if err := tx.Raw(`SELECT id, status, finished_at FROM region_import_runs
			WHERE status IN ('running','staged') AND deleted_at IS NULL FOR UPDATE`).Scan(&active).Error; err != nil {
			return err
		}
		for _, r := range active {
			if r.Status == RegionRunStaged && r.FinishedAt != nil && now.Sub(*r.FinishedAt) > regionStagedTTL {
				if err := a.closeRunTx(tx, r.ID, RegionRunStaged, RegionRunDiscarded, "Kedaluwarsa (lebih dari 24 jam tanpa keputusan)", now); err != nil {
					return err
				}
				if err := a.logActivity(tx, act.log("regions.discard", idStr(r.ID),
					fmt.Sprintf("Hasil fetch wilayah #%d dibuang otomatis (kedaluwarsa)", r.ID),
					map[string]any{"runId": r.ID, "alasan": "kedaluwarsa > 24 jam", "otomatis": true})); err != nil {
					return err
				}
				continue
			}
			return &httpError{http.StatusConflict, msgRegionRunActive}
		}
		if a.regionCooldown > 0 {
			var last *time.Time
			if err := tx.Raw(`SELECT MAX(started_at) FROM region_import_runs WHERE deleted_at IS NULL`).Scan(&last).Error; err != nil {
				return err
			}
			if last != nil && now.Sub(*last) < a.regionCooldown {
				wait := a.regionCooldown - now.Sub(*last)
				return &httpError{http.StatusTooManyRequests, fmt.Sprintf("Fetch ulang baru bisa dilakukan %d menit lagi (jeda agar sopan ke wilayah.id).", int(wait.Minutes())+1)}
			}
		}
		if err := tx.Exec(`INSERT INTO region_import_runs (status, started_by, started_label, started_at, progress_done, progress_total, created_at, updated_at)
			VALUES ('running', ?, ?, ?, 0, 0, ?, ?)`, act.UserID, truncateUTF8(act.Label, 100), now, now, now).Error; err != nil {
			if isDuplicateKey(err) {
				return &httpError{http.StatusConflict, msgRegionRunActive}
			}
			return err
		}
		if err := tx.Raw(`SELECT LAST_INSERT_ID()`).Scan(&runID).Error; err != nil || runID == 0 {
			return errors.New("gagal membaca id run")
		}
		return a.logActivity(tx, act.log("regions.fetch_start", idStr(runID),
			fmt.Sprintf("Fetch data wilayah #%d dimulai", runID),
			map[string]any{"runId": runID, "sumber": a.regionFetchCfg.Base}))
	})
	if err != nil && isDuplicateKey(err) {
		return 0, &httpError{http.StatusConflict, msgRegionRunActive}
	}
	return runID, err
}

// closeRunTx: ubah status run (bersyarat status lama) dan hapus staging-nya.
func (a *App) closeRunTx(tx *gorm.DB, id uint64, from, to, reason string, now time.Time) error {
	var errCol any
	if reason != "" {
		errCol = truncateUTF8(reason, 500)
	}
	res := tx.Exec(`UPDATE region_import_runs SET status = ?, finished_at = COALESCE(finished_at, ?), error = COALESCE(?, error)
		WHERE id = ? AND status = ?`, to, now, errCol, id, from)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return &httpError{http.StatusConflict, "Status run sudah berubah. Muat ulang halaman."}
	}
	return tx.Exec(`DELETE FROM region_import_staging WHERE run_id = ?`, id).Error
}

// ---------- Jalankan ----------

var errRegionRunGone = errors.New("run dihentikan dari luar (dibatalkan atau ditandai gagal)")

// launchRegionRun menjalankan runRegionFetch di goroutine latar (server).
func (a *App) launchRegionRun(id uint64, act regionActor) {
	ctx, cancel := context.WithCancel(a.bgCtx)
	a.regionRuns.add(id, cancel)
	go func() {
		defer a.regionRuns.finish(id)
		defer cancel()
		defer func() {
			if v := recover(); v != nil {
				log.Printf("wilayah: panic run #%d: %v", id, v)
			}
		}()
		if err := a.runRegionFetch(ctx, id, act, nil); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("wilayah: run #%d selesai dengan galat: %v", id, err)
		}
	}()
}

type regionStats struct {
	Phase      int      `json:"phase"`
	Requests   int64    `json:"requests"`
	Retries    int64    `json:"retries"`
	Failed     int64    `json:"failed"`
	DurationMs int64    `json:"durationMs"`
	FailedKeys []string `json:"failedKeys,omitempty"`
}

func levelMap(v [5]int64) map[string]int64 {
	return map[string]int64{"provinces": v[1], "regencies": v[2], "districts": v[3], "villages": v[4]}
}

func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// runRegionFetch menjalankan pengambilan untuk run `id` (status running) sampai staged/failed.
// onProgress (opsional, CLI) dipanggil setiap pembaruan kemajuan.
func (a *App) runRegionFetch(ctx context.Context, id uint64, act regionActor, onProgress func(regionFetchProgress)) error {
	db := a.db.Load()
	if db == nil {
		return errors.New("database belum terhubung")
	}
	bg := db.WithContext(context.Background()) // penulisan status akhir tidak boleh ikut batal
	f := newRegionFetcher(a.regionFetchCfg)
	start := time.Now()
	var lastWrite time.Time
	sink := func(res regionFetchResult) error {
		hash, data := regionItemsHash(res.Items)
		var parent any
		if res.Parent != "" {
			parent = res.Parent
		}
		return db.WithContext(ctx).Exec(`INSERT INTO region_import_staging (run_id, dataset_key, level, parent_code, data, item_count, content_hash, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, res.Key, res.Level, parent, string(data), len(res.Items), hash, a.now()).Error
	}
	progress := func(p regionFetchProgress) error {
		if onProgress != nil {
			onProgress(p)
		}
		if !p.Final && time.Since(lastWrite) < regionProgressEvery {
			return nil
		}
		lastWrite = time.Now()
		st := regionStats{Phase: p.Phase, Requests: p.Requests, Retries: p.Retries, Failed: p.Failed, DurationMs: time.Since(start).Milliseconds()}
		res := db.WithContext(ctx).Exec(`UPDATE region_import_runs SET progress_done = ?, progress_total = ?, counts = ?, stats = ?
			WHERE id = ? AND status = 'running'`, p.Done, p.Total, jsonStr(levelMap(p.Items)), jsonStr(st), id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// Bisa juga karena nilai sama persis; pastikan statusnya masih running.
			var status string
			if err := db.WithContext(ctx).Raw(`SELECT status FROM region_import_runs WHERE id = ?`, id).Scan(&status).Error; err != nil {
				return err
			}
			if status != RegionRunRunning {
				return errRegionRunGone
			}
		}
		return nil
	}

	sum, err := f.Run(ctx, sink, progress)
	stats := regionStats{Phase: 4, Requests: sum.Requests, Retries: sum.Retries, Failed: sum.Failed,
		DurationMs: sum.Duration.Milliseconds(), FailedKeys: sum.FailedKeys}
	now := a.now()

	if err != nil {
		if ctx.Err() != nil {
			// Dibatalkan admin (handler discard mengurus status & staging) atau server berhenti
			// (run tetap 'running' dan ditandai gagal saat start berikutnya).
			return err
		}
		if errors.Is(err, errRegionRunGone) {
			_ = bg.Exec(`DELETE FROM region_import_staging WHERE run_id = ?`, id).Error
			return err
		}
		msg := truncateUTF8(err.Error(), 480)
		txErr := bg.Transaction(func(tx *gorm.DB) error {
			res := tx.Exec(`UPDATE region_import_runs SET status = 'failed', finished_at = ?, error = ?, counts = ?, stats = ?
				WHERE id = ? AND status = 'running'`, now, msg, jsonStr(levelMap(sum.Items)), jsonStr(stats), id)
			if res.Error != nil {
				return res.Error
			}
			if err := tx.Exec(`DELETE FROM region_import_staging WHERE run_id = ?`, id).Error; err != nil {
				return err
			}
			if res.RowsAffected == 0 {
				return nil
			}
			return a.logActivity(tx, act.log("regions.fetch_failed", idStr(id), fmt.Sprintf("Fetch data wilayah #%d gagal", id),
				map[string]any{"runId": id, "galat": msg, "permintaan": sum.Requests, "retry": sum.Retries, "gagal": sum.Failed,
					"item": levelMap(sum.Items)}))
		})
		if txErr != nil {
			log.Printf("wilayah: gagal menandai run #%d gagal: %v", id, txErr)
		}
		return err
	}

	diff, derr := computeRunDiff(bg, id, sum.FailedKeys)
	if derr != nil {
		log.Printf("wilayah: diff run #%d: %v", id, derr)
		_ = bg.Exec(`UPDATE region_import_runs SET status = 'failed', finished_at = ?, error = ? WHERE id = ? AND status = 'running'`,
			now, "Gagal menghitung ringkasan", id).Error
		_ = bg.Exec(`DELETE FROM region_import_staging WHERE run_id = ?`, id).Error
		return derr
	}
	var src any
	if sum.SourceUpdatedAt != "" {
		src = sum.SourceUpdatedAt
	}
	staged := false
	txErr := bg.Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`UPDATE region_import_runs SET status = 'staged', finished_at = ?, counts = ?, diff = ?, stats = ?,
			source_updated_at = ?, progress_done = progress_total WHERE id = ? AND status = 'running'`,
			now, jsonStr(levelMap(sum.Items)), jsonStr(diff), jsonStr(stats), src, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errRegionRunGone
		}
		staged = true
		return a.logActivity(tx, act.log("regions.fetch_done", idStr(id),
			fmt.Sprintf("Fetch data wilayah #%d selesai: %d provinsi, %d kab/kota, %d kecamatan, %d kelurahan/desa",
				id, sum.Items[1], sum.Items[2], sum.Items[3], sum.Items[4]),
			map[string]any{"runId": id, "item": levelMap(sum.Items), "datasetBaru": diff.New, "datasetBerubah": diff.Changed,
				"datasetHilang": diff.Missing, "permintaan": sum.Requests, "retry": sum.Retries, "gagal": sum.Failed,
				"durasiDetik": int(sum.Duration.Seconds()), "tanggalSumber": sum.SourceUpdatedAt, "peringatan": len(diff.Warnings)}))
	})
	if txErr != nil || !staged {
		_ = bg.Exec(`DELETE FROM region_import_staging WHERE run_id = ?`, id).Error
		if txErr == nil {
			txErr = errRegionRunGone
		}
		return txErr
	}
	return nil
}

// ---------- Ringkasan (diff) ----------

type regionKeyInfo struct {
	Key   string `gorm:"column:dataset_key"`
	Level int    `gorm:"column:level"`
	Hash  string `gorm:"column:content_hash"`
	Items int64  `gorm:"column:item_count"`
}

type regionDiff struct {
	New            int              `json:"new"`
	Changed        int              `json:"changed"`
	Missing        int              `json:"missing"`
	Unchanged      int              `json:"unchanged"`
	Protected      int              `json:"protected"` // hilang di hasil fetch karena permintaan gagal -> dipertahankan
	EmptyDatasets  int              `json:"emptyDatasets"`
	ItemsStored    map[string]int64 `json:"itemsStored"`
	ItemsNew       map[string]int64 `json:"itemsNew"`
	DatasetsStored map[string]int64 `json:"datasetsStored"`
	DatasetsNew    map[string]int64 `json:"datasetsNew"`
	FailedKeys     []string         `json:"failedKeys"`
	Warnings       []string         `json:"warnings"`
}

// regionKeyProtected: dataset `key` termasuk wilayah yang permintaannya gagal (dirinya atau turunannya).
func regionKeyProtected(key string, failedKeys []string) bool {
	_, parent, ok := parseRegionDatasetKey(key)
	if !ok {
		return false
	}
	for _, fk := range failedKeys {
		if fk == key {
			return true
		}
		_, fp, ok := parseRegionDatasetKey(fk)
		if !ok || fp == "" {
			continue
		}
		if parent == fp || strings.HasPrefix(parent, fp+".") {
			return true
		}
	}
	return false
}

// computeRegionDiff (logika murni): bandingkan dataset hidup vs hasil fetch.
func computeRegionDiff(stored, staged []regionKeyInfo, failedKeys []string) regionDiff {
	var itemsStored, itemsNew, dsStored, dsNew [5]int64
	d := regionDiff{FailedKeys: append([]string{}, failedKeys...), Warnings: []string{}}
	old := make(map[string]regionKeyInfo, len(stored))
	for _, s := range stored {
		old[s.Key] = s
		if s.Level >= 1 && s.Level <= 4 {
			itemsStored[s.Level] += s.Items
			dsStored[s.Level]++
		}
	}
	seen := make(map[string]bool, len(staged))
	for _, s := range staged {
		seen[s.Key] = true
		if s.Level >= 1 && s.Level <= 4 {
			itemsNew[s.Level] += s.Items
			dsNew[s.Level]++
		}
		if s.Items == 0 && s.Level > 1 {
			d.EmptyDatasets++
		}
		o, ok := old[s.Key]
		switch {
		case !ok:
			d.New++
		case o.Hash != s.Hash:
			d.Changed++
		default:
			d.Unchanged++
		}
	}
	for _, s := range stored {
		if seen[s.Key] {
			continue
		}
		if regionKeyProtected(s.Key, failedKeys) {
			d.Protected++
			// Item lama yang dipertahankan ikut dihitung agar perbandingan jumlah adil.
			if s.Level >= 1 && s.Level <= 4 {
				itemsNew[s.Level] += s.Items
			}
			continue
		}
		d.Missing++
	}
	d.ItemsStored, d.ItemsNew = levelMap(itemsStored), levelMap(itemsNew)
	d.DatasetsStored, d.DatasetsNew = levelMap(dsStored), levelMap(dsNew)
	for l := 1; l <= 4; l++ {
		name := regionLevelNames[l]
		if itemsNew[l] == 0 {
			d.Warnings = append(d.Warnings, fmt.Sprintf("Tingkat %s kosong (0 data)", name))
			continue
		}
		if itemsStored[l] > 0 && float64(itemsNew[l]) < float64(itemsStored[l])*0.9 {
			drop := 100 * float64(itemsStored[l]-itemsNew[l]) / float64(itemsStored[l])
			d.Warnings = append(d.Warnings, fmt.Sprintf("Jumlah %s turun %.1f%% (%d → %d)", name, drop, itemsStored[l], itemsNew[l]))
		}
	}
	if len(failedKeys) > 0 {
		d.Warnings = append(d.Warnings, fmt.Sprintf("%d permintaan gagal setelah retry; data lama untuk wilayah tersebut dipertahankan", len(failedKeys)))
	}
	if d.EmptyDatasets > 0 {
		d.Warnings = append(d.Warnings, fmt.Sprintf("%d dataset kosong (wilayah tanpa anak)", d.EmptyDatasets))
	}
	return d
}

func computeRunDiff(db *gorm.DB, runID uint64, failedKeys []string) (regionDiff, error) {
	var stored, staged []regionKeyInfo
	if err := db.Raw(`SELECT dataset_key, level, content_hash, item_count FROM region_datasets WHERE deleted_at IS NULL`).Scan(&stored).Error; err != nil {
		return regionDiff{}, err
	}
	if err := db.Raw(`SELECT dataset_key, level, content_hash, item_count FROM region_import_staging WHERE run_id = ?`, runID).Scan(&staged).Error; err != nil {
		return regionDiff{}, err
	}
	return computeRegionDiff(stored, staged, failedKeys), nil
}

// ---------- Simpan ----------

type regionSaveResult struct {
	Inserted  int64            `json:"inserted"`
	Updated   int64            `json:"updated"`
	Restored  int64            `json:"restored"`
	Deleted   int64            `json:"deleted"`
	Protected int              `json:"protected"`
	Unchanged int64            `json:"unchanged"`
	Items     map[string]int64 `json:"items"`
}

func chunk[T any](s []T, n int) [][]T {
	var out [][]T
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	if len(s) > 0 {
		out = append(out, s)
	}
	return out
}

// saveRegionRun memindahkan staging run (status staged) ke region_datasets dalam satu transaksi.
// Data lama tetap terbaca pembaca lain sampai commit.
func (a *App) saveRegionRun(ctx context.Context, db *gorm.DB, id uint64, act regionActor) (*regionSaveResult, error) {
	var out regionSaveResult
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var runs []struct {
			Status string  `gorm:"column:status"`
			Diff   *string `gorm:"column:diff"`
			Source *string `gorm:"column:src"`
		}
		if err := tx.Raw(`SELECT status, diff, DATE_FORMAT(source_updated_at, '%Y-%m-%d') AS src FROM region_import_runs
			WHERE id = ? AND deleted_at IS NULL FOR UPDATE`, id).Scan(&runs).Error; err != nil {
			return err
		}
		if len(runs) == 0 {
			return &httpError{http.StatusNotFound, msgRegionRunNotFound}
		}
		if runs[0].Status != RegionRunStaged {
			return &httpError{http.StatusConflict, "Run ini tidak bisa disimpan (status: " + strings.ToLower(regionRunLabels[runs[0].Status]) + ")"}
		}
		var diff regionDiff
		if runs[0].Diff != nil {
			_ = json.Unmarshal([]byte(*runs[0].Diff), &diff)
		}
		var src any
		if runs[0].Source != nil {
			src = *runs[0].Source
		}
		now := a.now()

		type existing struct {
			ID      uint64 `gorm:"column:id"`
			Key     string `gorm:"column:dataset_key"`
			Deleted bool   `gorm:"column:deleted"`
		}
		var rows []existing
		if err := tx.Raw(`SELECT id, dataset_key, deleted_at IS NOT NULL AS deleted FROM region_datasets ORDER BY id`).Scan(&rows).Error; err != nil {
			return err
		}
		alive := map[string]uint64{}
		dead := map[string]uint64{} // id terbaru yang sudah di-soft-delete per kunci
		for _, r := range rows {
			if r.Deleted {
				dead[r.Key] = r.ID
			} else {
				alive[r.Key] = r.ID
			}
		}
		var stagedKeys []string
		if err := tx.Raw(`SELECT dataset_key FROM region_import_staging WHERE run_id = ? ORDER BY dataset_key`, id).Scan(&stagedKeys).Error; err != nil {
			return err
		}
		if len(stagedKeys) == 0 {
			return &httpError{http.StatusConflict, "Hasil fetch kosong atau sudah dibersihkan. Lakukan fetch ulang."}
		}
		inStaging := make(map[string]bool, len(stagedKeys))
		var toInsert []string
		var toRestore []uint64
		for _, k := range stagedKeys {
			inStaging[k] = true
			if _, ok := alive[k]; ok {
				continue
			}
			if did, ok := dead[k]; ok {
				toRestore = append(toRestore, did)
			} else {
				toInsert = append(toInsert, k)
			}
		}
		var toDelete []uint64
		for k, aid := range alive {
			if inStaging[k] {
				continue
			}
			if regionKeyProtected(k, diff.FailedKeys) {
				out.Protected++
				continue
			}
			toDelete = append(toDelete, aid)
		}
		sort.Slice(toDelete, func(i, j int) bool { return toDelete[i] < toDelete[j] })

		// 1) Ubah HANYA dataset yang isinya berubah.
		res := tx.Exec(`UPDATE region_datasets d JOIN region_import_staging s ON s.run_id = ? AND s.dataset_key = d.dataset_key
			SET d.data = s.data, d.item_count = s.item_count, d.content_hash = s.content_hash, d.level = s.level,
			    d.parent_code = s.parent_code, d.source_updated_at = ?, d.imported_run_id = ?, d.updated_at = ?
			WHERE d.deleted_at IS NULL AND d.content_hash <> s.content_hash`, id, src, id, now)
		if res.Error != nil {
			return res.Error
		}
		out.Updated = res.RowsAffected
		// 2) Kunci yang dulu hilang lalu muncul lagi: pulihkan baris lama (tabel tidak membengkak).
		for _, ids := range chunk(toRestore, regionBatch) {
			res := tx.Exec(`UPDATE region_datasets d JOIN region_import_staging s ON s.run_id = ? AND s.dataset_key = d.dataset_key
				SET d.data = s.data, d.item_count = s.item_count, d.content_hash = s.content_hash, d.level = s.level,
				    d.parent_code = s.parent_code, d.source_updated_at = ?, d.imported_run_id = ?, d.updated_at = ?, d.deleted_at = NULL
				WHERE d.id IN ?`, id, src, id, now, ids)
			if res.Error != nil {
				return res.Error
			}
			out.Restored += res.RowsAffected
		}
		// 3) Dataset baru.
		for _, keys := range chunk(toInsert, regionBatch) {
			res := tx.Exec(`INSERT INTO region_datasets (dataset_key, level, parent_code, data, item_count, content_hash,
				source_updated_at, imported_run_id, created_at, updated_at)
				SELECT s.dataset_key, s.level, s.parent_code, s.data, s.item_count, s.content_hash, ?, ?, ?, ?
				FROM region_import_staging s WHERE s.run_id = ? AND s.dataset_key IN ?`, src, id, now, now, id, keys)
			if res.Error != nil {
				return res.Error
			}
			out.Inserted += res.RowsAffected
		}
		// 4) Dataset yang tidak ada lagi di sumber: soft delete.
		for _, ids := range chunk(toDelete, regionBatch) {
			res := tx.Exec(`UPDATE region_datasets SET deleted_at = ? WHERE id IN ? AND deleted_at IS NULL`, now, ids)
			if res.Error != nil {
				return res.Error
			}
			out.Deleted += res.RowsAffected
		}
		out.Unchanged = int64(len(stagedKeys)) - out.Inserted - out.Restored - out.Updated
		// 5) Bersihkan staging, tandai tersimpan.
		if err := tx.Exec(`DELETE FROM region_import_staging WHERE run_id = ?`, id).Error; err != nil {
			return err
		}
		res = tx.Exec(`UPDATE region_import_runs SET status = 'saved', saved_at = ?, saved_by = ?, saved_label = ? WHERE id = ? AND status = 'staged'`,
			now, act.UserID, truncateUTF8(act.Label, 100), id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return &httpError{http.StatusConflict, "Status run sudah berubah. Muat ulang halaman."}
		}
		var lv []struct {
			Level int   `gorm:"column:level"`
			Items int64 `gorm:"column:items"`
		}
		if err := tx.Raw(`SELECT level, COALESCE(SUM(item_count), 0) AS items FROM region_datasets WHERE deleted_at IS NULL GROUP BY level`).Scan(&lv).Error; err != nil {
			return err
		}
		var items [5]int64
		for _, x := range lv {
			if x.Level >= 1 && x.Level <= 4 {
				items[x.Level] = x.Items
			}
		}
		out.Items = levelMap(items)
		return a.logActivity(tx, act.log("regions.save", idStr(id),
			fmt.Sprintf("Data wilayah #%d disimpan: %d provinsi, %d kab/kota, %d kecamatan, %d kelurahan/desa", id, items[1], items[2], items[3], items[4]),
			map[string]any{"runId": id, "datasetBaru": out.Inserted, "datasetBerubah": out.Updated, "datasetDipulihkan": out.Restored,
				"datasetDihapus": out.Deleted, "datasetDipertahankan": out.Protected, "datasetSama": out.Unchanged,
				"item": out.Items, "tanggalSumber": src}))
	})
	if err != nil {
		return nil, err
	}
	a.regions.invalidate()
	return &out, nil
}

// ---------- Buang / batalkan ----------

// discardRegionRun: staged -> discarded, running -> cancelled. Staging dihapus; data tersimpan tidak berubah.
func (a *App) discardRegionRun(ctx context.Context, db *gorm.DB, id uint64, act regionActor) (string, error) {
	var status string
	if err := db.WithContext(ctx).Raw(`SELECT status FROM region_import_runs WHERE id = ? AND deleted_at IS NULL`, id).Scan(&status).Error; err != nil {
		return "", err
	}
	if status == "" {
		return "", &httpError{http.StatusNotFound, msgRegionRunNotFound}
	}
	if status == RegionRunRunning {
		// Hentikan goroutine di proses ini dulu agar tidak menulis staging lagi. Run di proses lain
		// (CLI) berhenti sendiri saat melihat statusnya bukan running lagi.
		a.regionRuns.stop(id, 30*time.Second)
	}
	var to string
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cur string
		if err := tx.Raw(`SELECT status FROM region_import_runs WHERE id = ? FOR UPDATE`, id).Scan(&cur).Error; err != nil {
			return err
		}
		switch cur {
		case RegionRunRunning:
			to = RegionRunCancelled
		case RegionRunStaged:
			to = RegionRunDiscarded
		default:
			return &httpError{http.StatusConflict, "Run sudah selesai (status: " + strings.ToLower(regionRunLabels[cur]) + ")"}
		}
		reason := ""
		if to == RegionRunCancelled {
			reason = "Dibatalkan oleh admin"
		}
		if err := a.closeRunTx(tx, id, cur, to, reason, a.now()); err != nil {
			return err
		}
		summary := fmt.Sprintf("Hasil fetch wilayah #%d dibuang (data lama tidak berubah)", id)
		if to == RegionRunCancelled {
			summary = fmt.Sprintf("Fetch data wilayah #%d dibatalkan saat berjalan", id)
		}
		return a.logActivity(tx, act.log("regions.discard", idStr(id), summary,
			map[string]any{"runId": id, "status": map[string]any{"dari": cur, "menjadi": to}}))
	})
	return to, err
}

// sweepRegionRuns: dipanggil saat server mulai. Run 'running' (proses lama sudah mati) -> failed.
func (a *App) sweepRegionRuns(db *gorm.DB) {
	var ids []uint64
	if err := db.Raw(`SELECT id FROM region_import_runs WHERE status = 'running' AND deleted_at IS NULL`).Scan(&ids).Error; err != nil {
		log.Printf("wilayah: pemeriksaan run tertinggal gagal: %v", err)
		return
	}
	act := regionActor{Label: "server start"}
	for _, id := range ids {
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := a.closeRunTx(tx, id, RegionRunRunning, RegionRunFailed, msgRegionRestartError, a.now()); err != nil {
				return err
			}
			return a.logActivity(tx, act.log("regions.fetch_failed", idStr(id),
				fmt.Sprintf("Fetch data wilayah #%d ditandai gagal (server dimulai ulang)", id),
				map[string]any{"runId": id, "galat": msgRegionRestartError}))
		})
		if err != nil {
			log.Printf("wilayah: gagal menandai run #%d: %v", id, err)
		} else {
			log.Printf("wilayah: run #%d tertinggal saat restart ditandai gagal", id)
		}
	}
}

// ---------- CLI: import-regions ----------

// runImportRegionsCLI: `main import-regions [--no-save]` — impor memakai kode yang sama dengan fitur
// admin, dijalankan sekali (mis. `docker exec mihanstore_backend /app/main import-regions`).
func runImportRegionsCLI(args []string) int {
	noSave := false
	for _, a := range args {
		switch a {
		case "--no-save":
			noSave = true
		default:
			fmt.Fprintf(os.Stderr, "argumen tidak dikenal: %s\npemakaian: main import-regions [--no-save]\n", a)
			return 2
		}
	}
	log.SetFlags(log.LstdFlags | log.LUTC)
	cfg := LoadConfig()
	db, err := openDB(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "koneksi database gagal: %s\n", sanitizeDBErr(err))
		return 1
	}
	app := NewApp(cfg)
	app.db.Store(db)
	app.regionCooldown = 0
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	act := regionActor{Label: regionCLILabel}

	id, err := app.startRegionRun(ctx, db, act)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gagal memulai run: %v\n", err)
		return 1
	}
	fmt.Printf("run #%d dimulai: sumber %s, konkurensi %d, jeda %s\n", id, app.regionFetchCfg.Base,
		app.regionFetchCfg.Concurrency, app.regionFetchCfg.Interval)
	t0 := time.Now()
	var lastPrint time.Time
	onProgress := func(p regionFetchProgress) {
		if p.Final || time.Since(lastPrint) >= 15*time.Second {
			lastPrint = time.Now()
			fmt.Printf("[%5.0fs] tingkat %d: %d/%d permintaan selesai, retry %d, gagal %d, item %v\n",
				time.Since(t0).Seconds(), p.Phase, p.Done, p.Total, p.Retries, p.Failed, levelMap(p.Items))
		}
	}
	if err := app.runRegionFetch(ctx, id, act, onProgress); err != nil {
		if ctx.Err() != nil {
			// Dihentikan pengguna: tandai dibatalkan agar kunci terlepas.
			_, _ = app.discardRegionRun(context.Background(), db, id, act)
		}
		fmt.Fprintf(os.Stderr, "fetch gagal: %v\n", err)
		return 1
	}
	var run struct {
		Counts *string `gorm:"column:counts"`
		Diff   *string `gorm:"column:diff"`
		Stats  *string `gorm:"column:stats"`
	}
	db.Raw(`SELECT counts, diff, stats FROM region_import_runs WHERE id = ?`, id).Scan(&run)
	pr := func(label string, s *string) {
		if s != nil {
			fmt.Printf("%s: %s\n", label, *s)
		}
	}
	fmt.Printf("fetch selesai dalam %s\n", time.Since(t0).Round(time.Second))
	pr("jumlah", run.Counts)
	pr("statistik", run.Stats)
	pr("ringkasan", run.Diff)
	if noSave {
		fmt.Printf("--no-save: run #%d dibiarkan staged (simpan/buang dari Admin -> Data Wilayah)\n", id)
		return 0
	}
	res, err := app.saveRegionRun(ctx, db, id, act)
	if err != nil {
		fmt.Fprintf(os.Stderr, "simpan gagal (data lama tidak berubah): %v\n", err)
		return 1
	}
	fmt.Printf("tersimpan: %s\n", jsonStr(res))
	return 0
}
