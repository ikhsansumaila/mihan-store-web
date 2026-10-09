package regions

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/gorm"

	"mihanstore/internal/audit"
	"mihanstore/internal/platform"
)

// ---------- CLI: import-regions ----------

// RunImportCLI: `main import-regions [--no-save]` — impor memakai kode yang sama dengan fitur
// admin, dijalankan sekali (mis. `docker exec mihanstore_backend /app/main import-regions`).
func RunImportCLI(args []string) int {
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
	cfg := platform.LoadConfig()
	db, err := platform.OpenDB(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "koneksi database gagal: %s\n", platform.SanitizeDBErr(err))
		return 1
	}
	svc := New(Deps{
		DB:         func() *gorm.DB { return db },
		Now:        platform.NowUTC,
		Audit:      audit.NewLogger(func() *gorm.DB { return db }, nil, platform.NowUTC),
		Background: context.Background,
		FetchCfg:   FetchConfigFrom(cfg),
		Cooldown:   0,
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	act := Actor{Label: regionCLILabel}

	id, err := svc.StartRun(ctx, db, act)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gagal memulai run: %v\n", err)
		return 1
	}
	fmt.Printf("run #%d dimulai: sumber %s, konkurensi %d, jeda %s\n", id, svc.FetchCfg.Base,
		svc.FetchCfg.Concurrency, svc.FetchCfg.Interval)
	t0 := time.Now()
	var lastPrint time.Time
	onProgress := func(p regionFetchProgress) {
		if p.Final || time.Since(lastPrint) >= 15*time.Second {
			lastPrint = time.Now()
			fmt.Printf("[%5.0fs] tingkat %d: %d/%d permintaan selesai, retry %d, gagal %d, item %v\n",
				time.Since(t0).Seconds(), p.Phase, p.Done, p.Total, p.Retries, p.Failed, levelMap(p.Items))
		}
	}
	if err := svc.RunFetch(ctx, id, act, onProgress); err != nil {
		if ctx.Err() != nil {
			// Dihentikan pengguna: tandai dibatalkan agar kunci terlepas.
			_, _ = svc.DiscardRun(context.Background(), db, id, act)
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
	res, err := svc.SaveRun(ctx, db, id, act)
	if err != nil {
		fmt.Fprintf(os.Stderr, "simpan gagal (data lama tidak berubah): %v\n", err)
		return 1
	}
	fmt.Printf("tersimpan: %s\n", jsonStr(res))
	return 0
}
