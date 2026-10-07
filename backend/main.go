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

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

func newRouter(app *App) http.Handler {
	router := mux.NewRouter()

	// CORS: hanya origin yang diizinkan (env CORS_ALLOWED_ORIGINS), tanpa credentials.
	corsHandler := handlers.CORS(
		handlers.AllowedOrigins(app.cfg.CORSAllowedOrigins),
		handlers.AllowedMethods([]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}),
		handlers.AllowedHeaders([]string{"Content-Type", "Authorization"}),
		handlers.MaxAge(600),
	)

	// API Routes
	api := router.PathPrefix("/api").Subrouter()
	api.HandleFunc("/products", app.GetProducts).Methods("GET")
	api.HandleFunc("/products/search", app.SearchProducts).Methods("GET")
	api.HandleFunc("/categories", app.GetCategories).Methods("GET")
	// Data wilayah (publik, hanya baca dari DB).
	api.HandleFunc("/regions/provinces", app.PublicRegions(regionLevelProvince)).Methods("GET")
	api.HandleFunc("/regions/regencies/{code}", app.PublicRegions(regionLevelRegency)).Methods("GET")
	api.HandleFunc("/regions/districts/{code}", app.PublicRegions(regionLevelDistrict)).Methods("GET")
	api.HandleFunc("/regions/villages/{code}", app.PublicRegions(regionLevelVillage)).Methods("GET")
	api.HandleFunc("/auth/register", app.Register).Methods("POST")
	api.HandleFunc("/auth/login", app.Login).Methods("POST")
	api.HandleFunc("/auth/verify", app.Verify).Methods("POST")
	api.HandleFunc("/auth/logout", app.Logout).Methods("POST")
	api.HandleFunc("/auth/me", app.Me).Methods("GET")
	api.HandleFunc("/auth/google", app.GoogleLogin).Methods("POST")
	api.HandleFunc("/auth/google/complete", app.GoogleComplete).Methods("POST")

	// Pelanggan (wajib login, sesi Bearer): keranjang, pesanan, info pembayaran toko.
	api.Handle("/cart", app.customer(app.GetCart)).Methods("GET")
	api.Handle("/cart", app.customer(app.ClearCart)).Methods("DELETE")
	api.Handle("/cart/items", app.customer(app.AddCartItem)).Methods("POST")
	api.Handle("/cart/items", app.customer(app.SetCartItem)).Methods("PUT")
	api.Handle("/cart/items/{productId:[0-9]+}", app.customer(app.DeleteCartItem)).Methods("DELETE")
	api.Handle("/cart/ack-prices", app.customer(app.AckCartPrices)).Methods("POST")
	api.Handle("/orders", app.customer(app.ListMyOrders)).Methods("GET")
	api.Handle("/orders", app.customer(app.CreateOrder)).Methods("POST")
	api.Handle("/orders/{orderNo}", app.customer(app.GetMyOrder)).Methods("GET")
	api.Handle("/orders/{orderNo}/cancel", app.customer(app.CancelMyOrder)).Methods("POST")
	api.Handle("/store-info", app.customer(app.StoreInfo)).Methods("GET")
	// Notifikasi push pelanggan (status pesanan miliknya sendiri).
	api.Handle("/push/public-key", app.customer(app.PushPublicKey)).Methods("GET")
	api.Handle("/push/subscribe", app.customer(app.PushSubscribe)).Methods("POST")
	api.Handle("/push/subscribe", app.customer(app.PushUnsubscribe)).Methods("DELETE")

	// Admin: identitas dari Cloudflare Access + ADMIN_EMAILS (lihat admin.go).
	admin := api.PathPrefix("/admin").Subrouter()
	admin.Use(app.requireAdmin)
	admin.HandleFunc("/me", app.AdminMe).Methods("GET")
	admin.HandleFunc("/summary", app.AdminSummary).Methods("GET")
	admin.HandleFunc("/products", app.AdminListProducts).Methods("GET")
	admin.HandleFunc("/products", app.AdminCreateProduct).Methods("POST")
	admin.HandleFunc("/products/{id:[0-9]+}", app.AdminGetProduct).Methods("GET")
	admin.HandleFunc("/products/{id:[0-9]+}", app.AdminUpdateProduct).Methods("PUT")
	admin.HandleFunc("/products/{id:[0-9]+}", app.AdminDeleteProduct).Methods("DELETE")
	admin.HandleFunc("/products/{id:[0-9]+}/active", app.AdminSetProductActive).Methods("PATCH")
	admin.HandleFunc("/products/{id:[0-9]+}/image", app.AdminUploadProductImage).Methods("POST")
	admin.HandleFunc("/products/{id:[0-9]+}/image", app.AdminDeleteProductImage).Methods("DELETE")
	admin.HandleFunc("/categories", app.AdminListCategories).Methods("GET")
	admin.HandleFunc("/categories", app.AdminCreateCategory).Methods("POST")
	admin.HandleFunc("/categories/{id:[0-9]+}", app.AdminUpdateCategory).Methods("PUT")
	admin.HandleFunc("/categories/{id:[0-9]+}", app.AdminDeleteCategory).Methods("DELETE")
	admin.HandleFunc("/activity-logs", app.AdminListLogs).Methods("GET")
	admin.HandleFunc("/activity-logs/purge", app.AdminPurgeLogs).Methods("POST")
	admin.HandleFunc("/orders", app.AdminListOrders).Methods("GET")
	admin.HandleFunc("/orders/{id:[0-9]+}", app.AdminGetOrder).Methods("GET")
	admin.HandleFunc("/orders/{id:[0-9]+}/pricing", app.AdminUpdatePricing).Methods("PATCH")
	admin.HandleFunc("/orders/{id:[0-9]+}/status", app.AdminUpdateStatus).Methods("PATCH")
	admin.HandleFunc("/orders/{id:[0-9]+}/note", app.AdminUpdateNote).Methods("PATCH")
	admin.HandleFunc("/customers", app.AdminListCustomers).Methods("GET")
	admin.HandleFunc("/customers/{id:[0-9]+}", app.AdminGetCustomer).Methods("GET")
	admin.HandleFunc("/customers/{id:[0-9]+}/alias", app.AdminUpdateCustomerAlias).Methods("PATCH")
	admin.HandleFunc("/settings", app.AdminGetSettings).Methods("GET")
	admin.HandleFunc("/settings", app.AdminUpdateSettings).Methods("PUT")
	admin.HandleFunc("/push/public-key", app.AdminPushPublicKey).Methods("GET")
	admin.HandleFunc("/push/subscribe", app.AdminPushSubscribe).Methods("POST")
	admin.HandleFunc("/push/subscribe", app.AdminPushUnsubscribe).Methods("DELETE")
	admin.HandleFunc("/push/test", app.AdminPushTest).Methods("POST")
	admin.HandleFunc("/regions/status", app.AdminRegionStatus).Methods("GET")
	admin.HandleFunc("/regions/fetch", app.AdminRegionFetch).Methods("POST")
	admin.HandleFunc("/regions/runs/{id:[0-9]+}", app.AdminRegionRun).Methods("GET")
	admin.HandleFunc("/regions/runs/{id:[0-9]+}/save", app.AdminRegionSave).Methods("POST")
	admin.HandleFunc("/regions/runs/{id:[0-9]+}/discard", app.AdminRegionDiscard).Methods("POST")

	// Liveness: selalu 200 selama proses hidup.
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}).Methods("GET")
	// Readiness: memeriksa database (tanpa detail internal).
	router.HandleFunc("/health/ready", app.Ready).Methods("GET")

	return recoverer(securityHeaders(corsHandler(router)))
}

func main() {
	// Subperintah sekali jalan: impor data wilayah memakai kode yang sama dengan fitur admin.
	if len(os.Args) > 1 && os.Args[1] == "import-regions" {
		os.Exit(runImportRegionsCLI(os.Args[2:]))
	}
	log.SetFlags(log.LstdFlags | log.LUTC)
	cfg := LoadConfig()
	if cfg.DBPassword == "" {
		log.Println("PERINGATAN: DB_PASSWORD kosong")
	}
	initDummyHash()

	app := NewApp(cfg)
	app.imageGuard.startupCheck(cfg.UploadsDir)
	bgCtx, bgCancel := context.WithCancel(context.Background())
	app.bgCtx = bgCtx
	// Run wilayah yang tertinggal 'running' dari proses sebelumnya -> failed.
	app.afterConnect = app.sweepRegionRuns
	log.Printf("sumber data wilayah: %s", app.regionFetchCfg.Base)
	go app.connectWithRetry(cfg)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newRouter(app),
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
	bgCancel() // hentikan fetch wilayah yang berjalan (ditandai gagal saat start berikutnya)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Println("server dihentikan")
}
