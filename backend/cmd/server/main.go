// Command server: backend MihanStore. Tipis: baca konfigurasi -> app.Build -> http.Server -> shutdown.
// Subperintah `import-regions` menjalankan impor data wilayah sekali jalan (kode yang sama dengan fitur admin).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"mihanstore/internal/app"
	"mihanstore/internal/identity"
	"mihanstore/internal/platform"
	"mihanstore/internal/regions"
)

func main() {
	// Subperintah sekali jalan: impor data wilayah memakai kode yang sama dengan fitur admin.
	if len(os.Args) > 1 && os.Args[1] == "import-regions" {
		os.Exit(regions.RunImportCLI(os.Args[2:]))
	}
	log.SetFlags(log.LstdFlags | log.LUTC)
	cfg := platform.LoadConfig()
	if cfg.DBPassword == "" {
		log.Println("PERINGATAN: DB_PASSWORD kosong")
	}
	identity.InitDummyHash()

	a := app.Build(cfg)
	bgCtx, bgCancel := context.WithCancel(context.Background())
	a.Start(bgCtx) // pemeriksaan awal, goroutine latar (purge bukti transfer), koneksi DB

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           a.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	go func() {
		log.Printf("MihanStore Backend berjalan di :%s (CORS: %s)", cfg.Port, strings.Join(cfg.CORSAllowedOrigins, ", "))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server berhenti: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	bgCancel() // hentikan fetch wilayah & purge yang berjalan (fetch ditandai gagal saat start berikutnya)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Println("server dihentikan")
}
