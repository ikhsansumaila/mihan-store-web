package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

// Hardcoded products
var products = []Product{
	{ID: 1, Name: "Kerupuk Finna Udang", Category: "kerupuk", Price: 45000, Description: "Kerupuk udang asli dengan rasa gurih", Image: "kerupuk1.jpg"},
	{ID: 2, Name: "Kerupuk Finna Bawang", Category: "kerupuk", Price: 55000, Description: "Kerupuk bawang renyah", Image: "kerupuk2.jpg"},
	{ID: 3, Name: "Kerupuk Ketela", Category: "kerupuk", Price: 35000, Description: "Kerupuk ketela tradisional", Image: "kerupuk3.jpg"},
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

// Hardcoded users (in-memory)
var users = map[string]User{
	"demo@mihanstore.com": {Email: "demo@mihanstore.com", Password: "123456", Name: "Demo User"},
}

type Product struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Price       int    `json:"price"`
	Description string `json:"description"`
	Image       string `json:"image"`
}

type User struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type AuthRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type AuthResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	User    *User       `json:"user,omitempty"`
	Token   string      `json:"token,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func main() {
	router := mux.NewRouter()

	// CORS middleware
	corsHandler := handlers.CORS(
		handlers.AllowedOrigins([]string{"*"}),
		handlers.AllowedMethods([]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}),
		handlers.AllowedHeaders([]string{"Content-Type", "Authorization"}),
	)

	// API Routes
	api := router.PathPrefix("/api").Subrouter()
	api.HandleFunc("/products", getProducts).Methods("GET")
	api.HandleFunc("/products/search", searchProducts).Methods("GET")
	api.HandleFunc("/auth/register", register).Methods("POST")
	api.HandleFunc("/auth/login", login).Methods("POST")
	api.HandleFunc("/auth/verify", verifyToken).Methods("POST")

	// Health check
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}).Methods("GET")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("🚀 MihanStore Backend running on :%s\n", port)
	http.ListenAndServe(":"+port, corsHandler(router))
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

func register(w http.ResponseWriter, r *http.Request) {
	var req AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid request"})
		return
	}

	if req.Email == "" || req.Password == "" || req.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Email, password, dan nama harus diisi"})
		return
	}

	if _, exists := users[req.Email]; exists {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Email sudah terdaftar"})
		return
	}

	users[req.Email] = User{Email: req.Email, Password: req.Password, Name: req.Name}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(AuthResponse{
		Success: true,
		Message: "Registrasi berhasil",
		User:    &User{Email: req.Email, Name: req.Name},
		Token:   generateToken(req.Email),
	})
}

func login(w http.ResponseWriter, r *http.Request) {
	var req AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid request"})
		return
	}

	user, exists := users[req.Email]
	if !exists || user.Password != req.Password {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Email atau password salah"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AuthResponse{
		Success: true,
		Message: "Login berhasil",
		User:    &User{Email: user.Email, Name: user.Name},
		Token:   generateToken(req.Email),
	})
}

func verifyToken(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	json.NewDecoder(r.Body).Decode(&req)
	token := req["token"]

	if token != "" && len(token) > 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"valid": true})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]bool{"valid": false})
}

func generateToken(email string) string {
	return "token_" + strings.ReplaceAll(email, "@", "_") + "_" + fmt.Sprintf("%d", len(users))
}
