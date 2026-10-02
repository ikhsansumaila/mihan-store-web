package main

import (
	"context"
	"encoding/json"
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

// Hardcoded products
var products = []Product{
	{ID: 1, Name: "Kerupuk Finna Udang", Category: "kerupuk", Price: 45000, Description: "Kerupuk udang asli dengan rasa gurih", Image: "kerupuk1.jpg"},
	{ID: 2, Name: "Kerupuk Finna Bawang", Category: "kerupuk", Price: 55000, Description: "Kerupuk bawang renyah", Image: "kerupuk2.jpg"},
	{ID: 3, Name: "Kerupuk Gondang", Category: "kerupuk", Price: 35000, Description: "Kerupuk ketela tradisional", Image: "kerupuk3.jpg"},
	{ID: 4, Name: "Kerupuk Aci", Category: "kerupuk", Price: 25000, Description: "Kerupuk aci warna-warni", Image: "kerupuk4.jpg"},
	{ID: 5, Name: "Tepung Terigu Protein Tinggi", Category: "tepung", Price: 65000, Description: "Tepung terigu 1kg protein tinggi", Image: "tepung1.jpg"},
	{ID: 6, Name: "Tepung Maizena", Category: "tepung", Price: 28000, Description: "Tepung maizena 500gr", Image: "tepung2.jpg"},
	{ID: 7, Name: "Tepung Beras", Category: "tepung", Price: 32000, Description: "Tepung beras premium 500gr", Image: "tepung3.jpg"},
	{ID: 8, Name: "Tepung Gandum", Category: "tepung", Price: 48000, Description: "Tepung gandum organik 1kg", Image: "tepung4.jpg"},
	{ID: 9, Name: "Saos Tomat Murni", Category: "saos", Price: 18000, Description: "Saos tomat 400ml tanpa pengawet", Image: "saos1.jpg"},
	{ID: 10, Name: "Saos Cabe Merah", Category: "saos", Price: 22000, Description: "Saos cabe merah pedas 300ml", Image: "saos2.jpg"},
	{ID: 11, Name: "Sambal Bajak Pedas", Category: "sambal", Price: 35000, Description: "Sambal bajak tradisional 350gr", Image: "sambal1.jpg"},
	{ID: 12, Name: "Sambal Matah Segar", Category: "sambal", Price: 28000, Description: "Sambal matah segar 250gr", Image: "sambal2.jpg"},
	{ID: 13, Name: "Sendok Plastik Putih", Category: "sendok_plastik", Price: 5000, Description: "1 pak isi 50 pcs sendok plastik", Image: "sendok1.jpg"},
	{ID: 14, Name: "Sendok Plastik Warna", Category: "sendok_plastik", Price: 7000, Description: "1 pak isi 50 pcs warna-warni", Image: "sendok2.jpg"},
	{ID: 15, Name: "Box Hampers Kecil", Category: "box_hampers", Price: 75000, Description: "Box hampers isi produk pilihan", Image: "hampers1.jpg"},
	{ID: 16, Name: "Box Hampers Sedang", Category: "box_hampers", Price: 150000, Description: "Box hampers isi 8 item premium", Image: "hampers2.jpg"},
	{ID: 17, Name: "Box Hampers Besar", Category: "box_hampers", Price: 300000, Description: "Box hampers lengkap 15 item", Image: "hampers3.jpg"},
	{ID: 18, Name: "Box Hampers VIP", Category: "box_hampers", Price: 500000, Description: "Box hampers eksklusif 20 item mewah", Image: "hampers4.jpg"},
	{ID: 19, Name: "Saos Kecap Manis", Category: "saos", Price: 16000, Description: "Kecap manis premium 420ml", Image: "saos3.jpg"},
	{ID: 20, Name: "Sambal Tomat Pedas", Category: "sambal", Price: 32000, Description: "Sambal tomat ulek 300gr", Image: "sambal3.jpg"},
}

type Product struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Price       int    `json:"price"`
	Description string `json:"description"`
	Image       string `json:"image"`
}

func newRouter(app *App) http.Handler {
	router := mux.NewRouter()

	// CORS: hanya origin yang diizinkan (env CORS_ALLOWED_ORIGINS), tanpa credentials.
	corsHandler := handlers.CORS(
		handlers.AllowedOrigins(app.cfg.CORSAllowedOrigins),
		handlers.AllowedMethods([]string{"GET", "POST", "OPTIONS"}),
		handlers.AllowedHeaders([]string{"Content-Type", "Authorization"}),
		handlers.MaxAge(600),
	)

	// API Routes
	api := router.PathPrefix("/api").Subrouter()
	api.HandleFunc("/products", getProducts).Methods("GET")
	api.HandleFunc("/products/search", searchProducts).Methods("GET")
	api.HandleFunc("/auth/register", app.Register).Methods("POST")
	api.HandleFunc("/auth/login", app.Login).Methods("POST")
	api.HandleFunc("/auth/verify", app.Verify).Methods("POST")
	api.HandleFunc("/auth/logout", app.Logout).Methods("POST")
	api.HandleFunc("/auth/me", app.Me).Methods("GET")

	// Liveness: selalu 200 selama proses hidup.
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}).Methods("GET")
	// Readiness: memeriksa database (tanpa detail internal).
	router.HandleFunc("/health/ready", app.Ready).Methods("GET")

	return recoverer(securityHeaders(corsHandler(router)))
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	cfg := LoadConfig()
	if cfg.DBPassword == "" {
		log.Println("PERINGATAN: DB_PASSWORD kosong")
	}
	initDummyHash()

	app := NewApp(cfg)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Println("server dihentikan")
}

func getProducts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(products)
}

func searchProducts(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(r.URL.Query().Get("q"))
	category := r.URL.Query().Get("category")

	var results []Product
	for _, p := range products {
		matchQuery := query == "" || strings.Contains(strings.ToLower(p.Name), query)
		matchCategory := category == "" || p.Category == category

		if matchQuery && matchCategory {
			results = append(results, p)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if results == nil {
		results = []Product{}
	}
	json.NewEncoder(w).Encode(results)
}
