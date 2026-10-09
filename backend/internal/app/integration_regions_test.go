//go:build integration

// Tes integrasi data wilayah terhadap database UJI (migrasi 001–015, hak per tabel seperti produksi).
// Sumber data = server tiruan httptest (mockRegionServer); wilayah.id asli TIDAK pernah dipanggil.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/platform"
	"mihanstore/internal/regions"
	"mihanstore/internal/regions/regionstest"
)

// seedRegionsInteg mengisi region_datasets dengan pohon tiruan (bila belum ada), langsung lewat
// user aplikasi uji. Dipakai tes pesanan yang butuh data wilayah.
func seedRegionsInteg(t *testing.T, db *gorm.DB) {
	t.Helper()
	for key, items := range regionstest.Tree() {
		var n int64
		if err := db.Raw(`SELECT COUNT(*) FROM region_datasets WHERE dataset_key = ? AND deleted_at IS NULL`, key).Scan(&n).Error; err != nil {
			t.Fatalf("seed wilayah: %v", err)
		}
		if n > 0 {
			continue
		}
		level, parent, _ := regions.ParseDatasetKey(key)
		hash, data := regions.ItemsHash(regionItems(items))
		var p any
		if parent != "" {
			p = parent
		}
		if err := db.Exec(`INSERT INTO region_datasets (dataset_key, level, parent_code, data, item_count, content_hash, source_updated_at)
			VALUES (?, ?, ?, ?, ?, ?, '2025-07-04')`, key, level, p, string(data), len(items), hash).Error; err != nil {
			t.Fatalf("seed wilayah %s: %v", key, err)
		}
	}
}

type regionInteg struct {
	app *App
	h   http.Handler
	db  *gorm.DB
	m   *regionstest.Server
}

func setupRegionsInteg(t *testing.T) *regionInteg {
	t.Helper()
	app, h, db, _ := setupOrders(t)
	m := regionstest.NewServer()
	ts := httptest.NewServer(m)
	t.Cleanup(ts.Close)
	cfg := regions.DefaultFetchConfig(ts.URL)
	cfg.Interval, cfg.MinProvinces, cfg.BackoffBase, cfg.BackoffMax, cfg.Timeout = 0, 1, time.Millisecond, 5*time.Millisecond, 3*time.Second
	app.regions.FetchCfg = cfg
	app.regions.Cooldown = 0
	app.regions.Limiter = platform.NewRateLimiter(100000, time.Minute)
	app.regions.InvalidateCache()
	if strings.Contains(app.regions.FetchCfg.Base, "wilayah.id") {
		t.Fatal("tes tidak boleh memakai wilayah.id asli")
	}
	return &regionInteg{app: app, h: h, db: db, m: m}
}

var integActor = regions.Actor{Label: "uji-integrasi"}

// fetchSync menjalankan satu run sampai selesai (tanpa goroutine) dan mengembalikan id + status.
func (ri *regionInteg) fetchSync(t *testing.T) (uint64, string) {
	t.Helper()
	id, err := ri.app.regions.StartRun(context.Background(), ri.db, integActor)
	if err != nil {
		t.Fatalf("mulai run: %v", err)
	}
	_ = ri.app.regions.RunFetch(context.Background(), id, integActor, nil)
	var st string
	ri.db.Raw(`SELECT status FROM region_import_runs WHERE id = ?`, id).Scan(&st)
	return id, st
}

func (ri *regionInteg) count(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := ri.db.Raw(q, args...).Scan(&n).Error; err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (ri *regionInteg) runDiff(t *testing.T, id uint64) regions.Diff {
	var s string
	ri.db.Raw(`SELECT diff FROM region_import_runs WHERE id = ?`, id).Scan(&s)
	var d regions.Diff
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		t.Fatalf("diff run %d: %v (%s)", id, err, s)
	}
	return d
}

// publicGet memanggil API publik wilayah (dengan header opsional).
func publicGet(h http.Handler, path string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.RemoteAddr = "203.0.113.9:4444"
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func names(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var b struct {
		Data []regions.Item `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("respons wilayah: %v %s", err, w.Body.String())
	}
	var n []string
	for _, it := range b.Data {
		n = append(n, it.Name)
	}
	return strings.Join(n, ",")
}

func TestIntegrationRegionSchemaAndGrants(t *testing.T) {
	ri := setupRegionsInteg(t)
	for _, tbl := range []string{"region_datasets", "region_import_runs", "region_import_staging"} {
		ri.count(t, "SELECT COUNT(*) FROM "+tbl)
	}
	// Ditolak (1142): hapus data hidup / riwayat run, ubah staging, DDL.
	for _, q := range []string{
		"DELETE FROM region_datasets WHERE id = 0",
		"DELETE FROM region_import_runs WHERE id = 0",
		"UPDATE region_import_staging SET item_count = 0 WHERE id = 0",
		"TRUNCATE TABLE region_datasets",
		"ALTER TABLE region_datasets ADD COLUMN x INT",
		"DROP TABLE region_import_staging",
	} {
		if err := ri.db.Exec(q).Error; err == nil || (mysqlErrNo(err) != 1142 && mysqlErrNo(err) != 1370) {
			t.Errorf("harus ditolak (1142): %s -> %v", q, err)
		}
	}
	if err := ri.db.Exec("DELETE FROM region_import_staging WHERE id = 0").Error; err != nil {
		t.Errorf("DELETE staging harus boleh: %v", err)
	}
	// Kolom generated tidak bisa ditulis; data harus array JSON.
	if err := ri.db.Exec(`INSERT INTO region_datasets (dataset_key, level, data, item_count, content_hash, alive) VALUES ('x', 1, '[]', 0, 'h', 1)`).Error; err == nil {
		t.Error("alive tidak boleh ditulis")
	}
	if err := ri.db.Exec(`INSERT INTO region_datasets (dataset_key, level, data, item_count, content_hash) VALUES ('x', 1, '{}', 0, 'h')`).Error; err == nil {
		t.Error("data bukan array harus ditolak CHECK")
	}
	// Pesanan: kolom wilayah ada dan nullable.
	if err := ri.db.Exec("SELECT province_code, province_name, regency_code, regency_name, district_code, district_name, village_code, village_name FROM orders LIMIT 1").Error; err != nil {
		t.Fatalf("kolom wilayah pesanan: %v", err)
	}
}

func TestIntegrationRegionLifecycle(t *testing.T) {
	ri := setupRegionsInteg(t)
	db, h := ri.db, ri.h
	ctx := context.Background()

	// a) Fetch + simpan pohon tiruan -> data hidup = persis pohon tiruan.
	id, st := ri.fetchSync(t)
	if st != regions.RunStaged {
		t.Fatalf("run harus staged: %s", st)
	}
	if n := ri.count(t, "SELECT COUNT(*) FROM region_import_staging WHERE run_id = ?", id); n != int64(len(regionstest.Tree())) {
		t.Fatalf("staging: %d", n)
	}
	if _, err := ri.app.regions.SaveRun(ctx, db, id, integActor); err != nil {
		t.Fatal(err)
	}
	if n := ri.count(t, "SELECT COUNT(*) FROM region_import_staging WHERE run_id = ?", id); n != 0 {
		t.Fatal("staging harus dihapus setelah simpan")
	}
	alive := ri.count(t, "SELECT COUNT(*) FROM region_datasets WHERE deleted_at IS NULL")
	if alive != int64(len(regionstest.Tree())) {
		t.Fatalf("dataset hidup %d, mau %d", alive, len(regionstest.Tree()))
	}
	totalRows := ri.count(t, "SELECT COUNT(*) FROM region_datasets")
	var lastUpd time.Time
	db.Raw("SELECT MAX(updated_at) FROM region_datasets").Scan(&lastUpd)

	// b) Fetch lagi tanpa perubahan -> tidak ada UPDATE/INSERT, tabel tidak membengkak.
	time.Sleep(10 * time.Millisecond)
	id, _ = ri.fetchSync(t)
	if d := ri.runDiff(t, id); d.New != 0 || d.Changed != 0 || d.Missing != 0 || d.Unchanged != len(regionstest.Tree()) {
		t.Fatalf("diff tanpa perubahan: %+v", d)
	}
	res, err := ri.app.regions.SaveRun(ctx, db, id, integActor)
	if err != nil || res.Updated != 0 || res.Inserted != 0 || res.Deleted != 0 {
		t.Fatalf("simpan tanpa perubahan: %+v %v", res, err)
	}
	var upd2 time.Time
	db.Raw("SELECT MAX(updated_at) FROM region_datasets").Scan(&upd2)
	if ri.count(t, "SELECT COUNT(*) FROM region_datasets") != totalRows || !upd2.Equal(lastUpd) {
		t.Fatal("simpan tanpa perubahan tidak boleh menulis baris")
	}

	// c) Sumber berubah: nama desa berubah, kecamatan baru, kab/kota 31.71 hilang.
	ri.m.Mu.Lock()
	ri.m.Tree["villages:31.74.06"][3].Name = "Lebak Bulus Baru"
	ri.m.Tree["districts:31.74"] = append(ri.m.Tree["districts:31.74"], regionstest.It("31.74.10", "Pesanggrahan"))
	ri.m.Tree["villages:31.74.10"] = []regionstest.Item{regionstest.It("31.74.10.1001", "Bintaro")}
	ri.m.Tree["regencies:31"] = ri.m.Tree["regencies:31"][1:]
	ri.m.Updated = "2025-12-01"
	ri.m.Mu.Unlock()
	id, _ = ri.fetchSync(t)
	d := ri.runDiff(t, id)
	if d.New != 1 || d.Changed != 3 || d.Missing != 2 {
		t.Fatalf("diff berubah: %+v", d)
	}
	// Selama staged, data lama tetap dipakai.
	if w := publicGet(h, "/api/regions/villages/31.74.06", nil); !strings.Contains(names(t, w), "Lebak Bulus,") {
		t.Fatalf("data lama harus tetap tampil selama staged: %s", w.Body.String())
	}
	// Batal -> staging dihapus, data tidak berubah.
	if to, err := ri.app.regions.DiscardRun(ctx, db, id, integActor); err != nil || to != regions.RunDiscarded {
		t.Fatalf("buang: %s %v", to, err)
	}
	if ri.count(t, "SELECT COUNT(*) FROM region_import_staging WHERE run_id = ?", id) != 0 ||
		ri.count(t, "SELECT COUNT(*) FROM region_datasets WHERE deleted_at IS NULL") != alive {
		t.Fatal("batal tidak boleh mengubah data")
	}
	if _, err := ri.app.regions.SaveRun(ctx, db, id, integActor); err == nil {
		t.Fatal("run dibuang tidak boleh disimpan")
	}

	// d) Fetch lagi lalu simpan -> upsert di tempat + soft delete kunci hilang + cache dibersihkan.
	publicGet(h, "/api/regions/districts/31.71", nil) // isi cache
	id, _ = ri.fetchSync(t)
	res, err = ri.app.regions.SaveRun(ctx, db, id, integActor)
	if err != nil || res.Updated != 3 || res.Inserted != 1 || res.Deleted != 2 || res.Restored != 0 {
		t.Fatalf("simpan perubahan: %+v %v", res, err)
	}
	if ri.count(t, "SELECT COUNT(*) FROM region_datasets") != totalRows+1 {
		t.Fatal("hanya 1 baris baru yang boleh bertambah")
	}
	if ri.count(t, "SELECT COUNT(*) FROM region_datasets WHERE dataset_key IN ('districts:31.71','villages:31.71.01') AND deleted_at IS NOT NULL") != 2 {
		t.Fatal("kunci hilang harus di-soft-delete")
	}
	if w := publicGet(h, "/api/regions/villages/31.74.06", nil); !strings.Contains(names(t, w), "Lebak Bulus Baru") {
		t.Fatalf("cache harus dibersihkan setelah simpan: %s", w.Body.String())
	}
	if w := publicGet(h, "/api/regions/districts/31.71", nil); w.Code != 404 {
		t.Fatalf("kab/kota hilang harus 404: %d", w.Code)
	}
	if w := publicGet(h, "/api/regions/villages/31.74.06", nil); !strings.Contains(w.Body.String(), `"updatedAt":"2025-12-01"`) {
		t.Fatalf("tanggal sumber: %s", w.Body.String())
	}

	// e) Kembali ke pohon awal: baris lama dipulihkan (bukan baris baru).
	ri.m.Mu.Lock()
	ri.m.Tree = regionstest.Tree()
	ri.m.Mu.Unlock()
	id, _ = ri.fetchSync(t)
	res, err = ri.app.regions.SaveRun(ctx, db, id, integActor)
	if err != nil || res.Restored != 2 || res.Deleted != 1 || res.Inserted != 0 || res.Updated != 3 {
		t.Fatalf("pulihkan: %+v %v", res, err)
	}
	if ri.count(t, "SELECT COUNT(*) FROM region_datasets") != totalRows+1 ||
		ri.count(t, "SELECT COUNT(*) FROM region_datasets WHERE deleted_at IS NULL") != alive {
		t.Fatal("tabel tidak boleh membengkak setelah kunci muncul lagi")
	}

	// f) Kunci satu run: run kedua ditolak; DB juga menolak dua baris aktif.
	id1, err := ri.app.regions.StartRun(ctx, db, integActor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ri.app.regions.StartRun(ctx, db, integActor); err == nil || !strings.Contains(err.Error(), "Masih ada") {
		t.Fatalf("run kedua harus ditolak: %v", err)
	}
	if err := db.Exec(`INSERT INTO region_import_runs (status, started_label, started_at) VALUES ('staged', 'x', NOW(3))`).Error; !platform.IsDuplicateKey(err) {
		t.Fatalf("UNIQUE active_lock harus menolak: %v", err)
	}
	// g) Restart saat running -> sweep menandai failed dan membersihkan staging.
	db.Exec(`INSERT INTO region_import_staging (run_id, dataset_key, level, data, item_count, content_hash) VALUES (?, 'provinces', 1, '[]', 0, 'x')`, id1)
	ri.app.regions.SweepRuns(db)
	var row struct {
		Status string
		Error  *string
	}
	db.Raw("SELECT status, error FROM region_import_runs WHERE id = ?", id1).Scan(&row)
	if row.Status != regions.RunFailed || row.Error == nil || !strings.Contains(*row.Error, "dimulai ulang") ||
		ri.count(t, "SELECT COUNT(*) FROM region_import_staging WHERE run_id = ?", id1) != 0 {
		t.Fatalf("sweep: %+v", row)
	}

	// h) Staged > 24 jam dibuang otomatis saat fetch berikutnya.
	idOld, _ := ri.fetchSync(t)
	db.Exec("UPDATE region_import_runs SET finished_at = ? WHERE id = ?", time.Now().UTC().Add(-25*time.Hour), idOld)
	idNew, err := ri.app.regions.StartRun(ctx, db, integActor)
	if err != nil {
		t.Fatalf("staged kedaluwarsa harus dibuang otomatis: %v", err)
	}
	var stOld string
	db.Raw("SELECT status FROM region_import_runs WHERE id = ?", idOld).Scan(&stOld)
	if stOld != regions.RunDiscarded || ri.count(t, "SELECT COUNT(*) FROM region_import_staging WHERE run_id = ?", idOld) != 0 {
		t.Fatalf("run lama: %s", stOld)
	}
	// Run yang ditandai bukan-running dari luar (mis. dibatalkan admin) berhenti sendiri.
	db.Exec("UPDATE region_import_runs SET status = 'cancelled' WHERE id = ?", idNew)
	if err := ri.app.regions.RunFetch(ctx, idNew, integActor, nil); err == nil {
		t.Fatal("run yang dibatalkan dari luar harus berhenti")
	}
	if ri.count(t, "SELECT COUNT(*) FROM region_import_staging WHERE run_id = ?", idNew) != 0 {
		t.Fatal("staging run batal harus bersih")
	}

	// i) Gagal: provinsi tidak bisa diambil -> failed, staging bersih, data lama aman.
	ri.m.Mu.Lock()
	ri.m.Fail["provinces"] = func(w http.ResponseWriter, n int) bool { w.WriteHeader(404); return true }
	ri.m.Mu.Unlock()
	idF, st := ri.fetchSync(t)
	ri.m.Mu.Lock()
	delete(ri.m.Fail, "provinces")
	ri.m.Mu.Unlock()
	if st != regions.RunFailed || ri.count(t, "SELECT COUNT(*) FROM region_import_staging WHERE run_id = ?", idF) != 0 ||
		ri.count(t, "SELECT COUNT(*) FROM region_datasets WHERE deleted_at IS NULL") != alive {
		t.Fatalf("run gagal: %s", st)
	}

	// Log aktivitas tiap aksi.
	for _, a := range []string{"regions.fetch_start", "regions.fetch_done", "regions.save", "regions.discard", "regions.fetch_failed"} {
		if lastLog(t, db, a) == nil {
			t.Errorf("log %s tidak ada", a)
		}
	}
	if l := lastLog(t, db, "regions.save"); !strings.Contains(det(l), `"item"`) || *l.ActorLabel != "sistem: uji-integrasi" {
		t.Errorf("log simpan: %s %v", det(l), *l.ActorLabel)
	}
}

func TestIntegrationRegionAdminEndpoints(t *testing.T) {
	ri := setupRegionsInteg(t)
	h, db := ri.h, ri.db
	ri.m.Delay = func() { time.Sleep(30 * time.Millisecond) }

	// Pelanggan biasa / tanpa token Access -> ditolak.
	tok := newCustomer(t, h, "Pelanggan Wilayah")
	if r := call(t, h, "GET", "/api/admin/regions/status", tok, nil); r.Code != 401 {
		t.Fatalf("pelanggan -> admin wilayah: %d", r.Code)
	}
	if r := adminCall(t, h, "GET", "/api/admin/regions/status", "bukan.admin@uji.test", nil); r.Code != 403 {
		t.Fatalf("email bukan admin: %d", r.Code)
	}
	if r := adminCall(t, h, "GET", "/api/admin/regions/status", integAdmin, nil); r.Code != 200 || r.Body["stored"] == nil {
		t.Fatalf("status: %d %v", r.Code, r.Body)
	}

	// Fetch -> berjalan di latar -> batalkan saat berjalan.
	r := adminCall(t, h, "POST", "/api/admin/regions/fetch", integAdmin, map[string]any{})
	if r.Code != 202 {
		t.Fatalf("fetch: %d %v", r.Code, r.Body)
	}
	run := r.Body["run"].(map[string]any)
	id := uint64(num(run["id"]))
	if r := adminCall(t, h, "POST", "/api/admin/regions/fetch", integAdmin, map[string]any{}); r.Code != 409 {
		t.Fatalf("fetch kedua harus 409: %d", r.Code)
	}
	time.Sleep(80 * time.Millisecond)
	r = adminCall(t, h, "POST", fmt.Sprintf("/api/admin/regions/runs/%d/discard", id), integAdmin, map[string]any{})
	if r.Code != 200 || r.Body["status"] != regions.RunCancelled {
		t.Fatalf("batalkan run berjalan: %d %v", r.Code, r.Body)
	}
	time.Sleep(100 * time.Millisecond)
	if ri.count(t, "SELECT COUNT(*) FROM region_import_staging WHERE run_id = ?", id) != 0 {
		t.Fatal("staging run dibatalkan harus bersih")
	}

	// Fetch -> tunggu staged (polling) -> simpan.
	ri.m.Delay = nil
	r = adminCall(t, h, "POST", "/api/admin/regions/fetch", integAdmin, map[string]any{})
	id = uint64(num(r.Body["run"].(map[string]any)["id"]))
	var status string
	for i := 0; i < 100; i++ {
		rr := adminCall(t, h, "GET", fmt.Sprintf("/api/admin/regions/runs/%d", id), integAdmin, nil)
		status = rr.Body["run"].(map[string]any)["status"].(string)
		if status != regions.RunRunning {
			if rr.Body["run"].(map[string]any)["diff"] == nil {
				t.Fatal("ringkasan diff kosong")
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != regions.RunStaged {
		t.Fatalf("run harus staged: %s", status)
	}
	// Tanpa header CSRF -> 403.
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/admin/regions/runs/%d/save", id), strings.NewReader("{}"))
	req.Header.Set("Cf-Access-Jwt-Assertion", accessToken(t, integAdmin))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("simpan tanpa CSRF: %d", w.Code)
	}
	r = adminCall(t, h, "POST", fmt.Sprintf("/api/admin/regions/runs/%d/save", id), integAdmin, map[string]any{})
	if r.Code != 200 || r.Body["result"] == nil {
		t.Fatalf("simpan: %d %v", r.Code, r.Body)
	}
	stored := r.Body["stored"].(map[string]any)
	if stored["hasData"] != true || stored["lastSaved"].(map[string]any)["by"] != integAdmin {
		t.Fatalf("status tersimpan: %v", stored)
	}
	if r := adminCall(t, h, "POST", fmt.Sprintf("/api/admin/regions/runs/%d/save", id), integAdmin, map[string]any{}); r.Code != 409 {
		t.Fatalf("simpan dua kali: %d", r.Code)
	}
	if r := adminCall(t, h, "GET", "/api/admin/regions/runs/999999", integAdmin, nil); r.Code != 404 {
		t.Fatalf("run tidak ada: %d", r.Code)
	}
	l := lastLog(t, db, "regions.save")
	if l == nil || l.UserID == nil || *l.ActorLabel != integAdmin {
		t.Fatal("log simpan admin")
	}
	st := adminCall(t, h, "GET", "/api/admin/regions/status", integAdmin, nil)
	if runs := st.Body["runs"].([]any); len(runs) == 0 || len(runs) > 10 || st.Body["activeRun"] != nil {
		t.Fatalf("riwayat: %v", st.Body)
	}
}

func TestIntegrationRegionPublicAPI(t *testing.T) {
	ri := setupRegionsInteg(t)
	h, db := ri.h, ri.db
	w := publicGet(h, "/api/regions/provinces", nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=3600" || w.Header().Get("ETag") == "" ||
		names(t, w) != "Banten,DKI Jakarta" || !strings.Contains(w.Body.String(), `"meta":{"updatedAt":`) {
		t.Fatalf("provinsi: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	etag := w.Header().Get("ETag")
	if w := publicGet(h, "/api/regions/provinces", map[string]string{"If-None-Match": etag}); w.Code != 304 || w.Body.Len() != 0 {
		t.Fatalf("304: %d", w.Code)
	}
	if w := publicGet(h, "/api/regions/regencies/31", nil); w.Code != 200 || !strings.Contains(names(t, w), "Kota Administrasi Jakarta Selatan") {
		t.Fatalf("kab/kota: %d %s", w.Code, w.Body.String())
	}
	if w := publicGet(h, "/api/regions/districts/3174", nil); w.Code != 200 || names(t, w) != "Cilandak,Jagakarsa" {
		t.Fatalf("kecamatan (kode tanpa titik): %d %s", w.Code, w.Body.String())
	}
	if w := publicGet(h, "/api/regions/villages/31.74.06", nil); w.Code != 200 || !strings.Contains(names(t, w), "Lebak Bulus") {
		t.Fatalf("desa: %d", w.Code)
	}
	for path, code := range map[string]int{
		"/api/regions/regencies/99":            404,
		"/api/regions/villages/31.74.99":       404,
		"/api/regions/regencies/3":             400,
		"/api/regions/districts/31.74.06":      400,
		"/api/regions/villages/31'%20OR%201=1": 400,
	} {
		if w := publicGet(h, path, nil); w.Code != code || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d (mau %d) %q", path, w.Code, code, w.Header().Get("Cache-Control"))
		}
	}
	// Batas laju per IP.
	ri.app.regions.Limiter = platform.NewRateLimiter(2, time.Minute)
	publicGet(h, "/api/regions/provinces", nil)
	publicGet(h, "/api/regions/provinces", nil)
	if w := publicGet(h, "/api/regions/provinces", nil); w.Code != 429 {
		t.Fatalf("batas laju: %d", w.Code)
	}
	ri.app.regions.Limiter = platform.NewRateLimiter(100000, time.Minute)

	// Belum ada data -> 503 (provinsi disembunyikan sementara lewat soft delete).
	db.Exec("UPDATE region_datasets SET deleted_at = NOW(3) WHERE dataset_key = 'provinces' AND deleted_at IS NULL")
	ri.app.regions.InvalidateCache()
	defer func() {
		db.Exec("UPDATE region_datasets SET deleted_at = NULL WHERE dataset_key = 'provinces' AND id = (SELECT id FROM (SELECT MAX(id) AS id FROM region_datasets WHERE dataset_key = 'provinces') x)")
		ri.app.regions.InvalidateCache()
	}()
	if w := publicGet(h, "/api/regions/provinces", nil); w.Code != 503 || !strings.Contains(w.Body.String(), "Data wilayah belum tersedia") {
		t.Fatalf("503: %d %s", w.Code, w.Body.String())
	}
	if w := publicGet(h, "/api/regions/regencies/31", nil); w.Code != 503 {
		t.Fatalf("503 turunan: %d", w.Code)
	}
}

func TestIntegrationCheckoutRegionChain(t *testing.T) {
	ri := setupRegionsInteg(t)
	h, db := ri.h, ri.db
	tok := newCustomer(t, h, "Pembeli Wilayah")
	add := func() { call(t, h, "POST", "/api/cart/items", tok, map[string]any{"productId": 3, "qty": 1}) }
	add()

	body := func(mut func(map[string]any)) map[string]any {
		b := checkoutBody(uuid(t))
		mut(b)
		return b
	}
	cases := []struct {
		name  string
		mut   func(map[string]any)
		field string
		msg   string
	}{
		{"klien lama tanpa kode", func(b map[string]any) {
			delete(b, "provinceCode")
			delete(b, "regencyCode")
			delete(b, "districtCode")
			delete(b, "villageCode")
		}, "region", "Perbarui halaman lalu coba lagi"},
		{"kab/kota bukan anak provinsi", func(b map[string]any) { b["regencyCode"] = "31.74" }, "regencyCode", "tidak sesuai"},
		{"desa palsu", func(b map[string]any) { b["villageCode"] = "36.71.01.9999" }, "villageCode", "tidak ditemukan"},
		{"kecamatan palsu", func(b map[string]any) {
			b["districtCode"], b["villageCode"] = "36.71.09", "36.71.09.1001"
		}, "districtCode", "tidak ditemukan"},
		{"provinsi palsu", func(b map[string]any) {
			b["provinceCode"], b["regencyCode"], b["districtCode"], b["villageCode"] = "99", "99.01", "99.01.01", "99.01.01.1001"
		}, "provinceCode", "tidak ditemukan"},
		{"format kode salah", func(b map[string]any) { b["villageCode"] = "36.71.01.1' OR 1=1" }, "villageCode", "tidak valid"},
		{"kode pos kosong", func(b map[string]any) { b["postalCode"] = "" }, "postalCode", "wajib"},
		{"desa kosong", func(b map[string]any) { b["villageCode"] = "" }, "villageCode", "Pilih kelurahan/desa"},
	}
	for _, c := range cases {
		r := call(t, h, "POST", "/api/orders", tok, body(c.mut))
		if r.Code != 422 || r.Body["field"] != c.field || !strings.Contains(fmt.Sprint(r.Body["error"]), c.msg) {
			t.Errorf("%s: %d %v", c.name, r.Code, r.Body)
		}
	}

	// Sukses: nama wilayah dari DB, bukan dari klien; city = nama kab/kota.
	b := body(func(b map[string]any) {
		b["provinceName"], b["regencyName"], b["villageName"], b["city"] = "Provinsi Palsu", "Kota Palsu", "Desa Palsu", "Kota Palsu"
		b["districtCode"], b["villageCode"] = "3671", "3671011001" // tetap 422: induk tidak cocok (kode kecamatan = format kab/kota)
	})
	if r := call(t, h, "POST", "/api/orders", tok, b); r.Code != 422 {
		t.Fatalf("kode kecamatan berformat salah: %d", r.Code)
	}
	b["districtCode"], b["villageCode"] = "367101", "3671011001" // tanpa titik -> dinormalkan
	r := call(t, h, "POST", "/api/orders", tok, b)
	if r.Code != 201 {
		t.Fatalf("checkout: %d %v", r.Code, r.Body)
	}
	rcp := r.Body["recipient"].(map[string]any)
	reg, _ := rcp["region"].(map[string]any)
	if reg == nil || reg["village"].(map[string]any)["name"] != "Sukarasa" || reg["regency"].(map[string]any)["name"] != "Kota Tangerang" ||
		reg["province"].(map[string]any)["code"] != "36" || rcp["city"] != "Kota Tangerang" ||
		rcp["fullAddress"] != "Jl. Melati No. 9, RT 3, Sukarasa, Kec. Tangerang, Kota Tangerang, Banten 15111" {
		t.Fatalf("penerima: %v", rcp)
	}
	orderNo := r.Body["orderNo"].(string)
	var o orderRow
	db.Raw("SELECT "+orderCols+" FROM orders o WHERE order_no = ?", orderNo).Scan(&o)
	if deref(o.VillageName) != "Sukarasa" || deref(o.VillageCode) != "36.71.01.1001" || deref(o.ProvinceName) != "Banten" ||
		o.City != "Kota Tangerang" || deref(o.DistrictName) != "Tangerang" {
		t.Fatalf("baris pesanan: %+v", o)
	}
	// Respons admin juga memuat wilayah & alamat tersusun.
	ar := adminCall(t, h, "GET", fmt.Sprintf("/api/admin/orders/%d", o.ID), integAdmin, nil)
	if arc := ar.Body["recipient"].(map[string]any); arc["region"] == nil || !strings.HasSuffix(arc["fullAddress"].(string), "Banten 15111") {
		t.Fatalf("admin: %v", arc)
	}
	if l := lastLog(t, db, "order.create"); strings.Contains(det(l), "Sukarasa") || strings.Contains(det(l), "Melati") {
		t.Fatalf("log pesanan tidak boleh memuat alamat: %s", det(l))
	}

	// Pesanan lama (fixture sebelum 014) tetap tampil seperti dulu.
	var oldID uint64
	db.Raw("SELECT id FROM orders WHERE order_no = 'MS-261001-0900'").Scan(&oldID)
	if oldID != 0 {
		ar := adminCall(t, h, "GET", fmt.Sprintf("/api/admin/orders/%d", oldID), integAdmin, nil)
		arc := ar.Body["recipient"].(map[string]any)
		if ar.Code != 200 || arc["region"] != nil || arc["city"] != "Tangerang" || arc["fullAddress"] != "Jl. Lama No. 1, Tangerang" {
			t.Fatalf("pesanan lama: %d %v", ar.Code, arc)
		}
	} else {
		t.Log("fixture pesanan lama tidak ada (OLD_ORDER_FIXTURE=0)")
	}

	// Data wilayah belum ada -> 503 dengan pesan jelas; keranjang tetap.
	add()
	db.Exec("UPDATE region_datasets SET deleted_at = NOW(3) WHERE dataset_key = 'provinces' AND deleted_at IS NULL")
	ri.app.regions.InvalidateCache()
	r = call(t, h, "POST", "/api/orders", tok, checkoutBody(uuid(t)))
	db.Exec("UPDATE region_datasets SET deleted_at = NULL WHERE dataset_key = 'provinces' AND id = (SELECT id FROM (SELECT MAX(id) AS id FROM region_datasets WHERE dataset_key = 'provinces') x)")
	ri.app.regions.InvalidateCache()
	if r.Code != 503 || !strings.Contains(fmt.Sprint(r.Body["error"]), "Data wilayah belum tersedia") {
		t.Fatalf("503 checkout: %d %v", r.Code, r.Body)
	}
	if c := call(t, h, "GET", "/api/cart", tok, nil); num(c.Body["lineCount"]) != 1 {
		t.Fatal("keranjang harus tetap saat 503")
	}
}
