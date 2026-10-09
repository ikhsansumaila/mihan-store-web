package app

// Daftar rute HTTP lengkap (satu-satunya tempat rute didaftarkan) beserta middleware global.

import (
	"net/http"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"

	"mihanstore/internal/platform"
	"mihanstore/internal/regions"
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
	api.HandleFunc("/products", app.catalog.GetProducts).Methods("GET")
	api.HandleFunc("/products/search", app.catalog.SearchProducts).Methods("GET")
	api.HandleFunc("/categories", app.catalog.GetCategories).Methods("GET")
	// Data wilayah (publik, hanya baca dari DB).
	api.HandleFunc("/regions/provinces", app.regions.PublicRegions(regions.LevelProvince)).Methods("GET")
	api.HandleFunc("/regions/regencies/{code}", app.regions.PublicRegions(regions.LevelRegency)).Methods("GET")
	api.HandleFunc("/regions/districts/{code}", app.regions.PublicRegions(regions.LevelDistrict)).Methods("GET")
	api.HandleFunc("/regions/villages/{code}", app.regions.PublicRegions(regions.LevelVillage)).Methods("GET")
	api.HandleFunc("/auth/register", app.identity.Register).Methods("POST")
	api.HandleFunc("/auth/login", app.identity.Login).Methods("POST")
	api.HandleFunc("/auth/verify", app.identity.Verify).Methods("POST")
	api.HandleFunc("/auth/logout", app.identity.Logout).Methods("POST")
	api.HandleFunc("/auth/me", app.identity.Me).Methods("GET")
	api.HandleFunc("/auth/google", app.identity.GoogleLogin).Methods("POST")
	api.HandleFunc("/auth/google/complete", app.identity.GoogleComplete).Methods("POST")

	// Pelanggan (wajib login, sesi Bearer): keranjang, pesanan, info pembayaran toko.
	api.Handle("/cart", app.identity.Customer(app.cart.GetCart)).Methods("GET")
	api.Handle("/cart", app.identity.Customer(app.cart.ClearCart)).Methods("DELETE")
	api.Handle("/cart/items", app.identity.Customer(app.cart.AddCartItem)).Methods("POST")
	api.Handle("/cart/items", app.identity.Customer(app.cart.SetCartItem)).Methods("PUT")
	api.Handle("/cart/items/{productId:[0-9]+}", app.identity.Customer(app.cart.DeleteCartItem)).Methods("DELETE")
	api.Handle("/cart/ack-prices", app.identity.Customer(app.cart.AckCartPrices)).Methods("POST")
	api.Handle("/orders", app.identity.Customer(app.orders.ListMyOrders)).Methods("GET")
	api.Handle("/orders", app.identity.Customer(app.orders.CreateOrder)).Methods("POST")
	api.Handle("/orders/{orderNo}", app.identity.Customer(app.orders.GetMyOrder)).Methods("GET")
	api.Handle("/orders/{orderNo}/cancel", app.identity.Customer(app.orders.CancelMyOrder)).Methods("POST")
	api.Handle("/orders/{orderNo}/payment-proof", app.identity.Customer(app.orders.CustomerUploadPaymentProof)).Methods("POST")
	api.Handle("/orders/{orderNo}/payment-proof", app.identity.Customer(app.orders.CustomerGetPaymentProof)).Methods("GET")
	api.Handle("/orders/{orderNo}/payment-proof", app.identity.Customer(app.orders.CustomerDeletePaymentProof)).Methods("DELETE")
	api.Handle("/store-info", app.identity.Customer(app.orders.StoreInfo)).Methods("GET")
	// Notifikasi push pelanggan (status pesanan miliknya sendiri).
	api.Handle("/push/public-key", app.identity.Customer(app.pushSubs.PushPublicKey)).Methods("GET")
	api.Handle("/push/subscribe", app.identity.Customer(app.pushSubs.PushSubscribe)).Methods("POST")
	api.Handle("/push/subscribe", app.identity.Customer(app.pushSubs.PushUnsubscribe)).Methods("DELETE")

	// Admin: identitas dari Cloudflare Access + ADMIN_EMAILS (lihat admin.go).
	admin := api.PathPrefix("/admin").Subrouter()
	admin.Use(app.admin.RequireAdmin)
	admin.HandleFunc("/me", app.admin.AdminMe).Methods("GET")
	admin.HandleFunc("/summary", app.admin.AdminSummary).Methods("GET")
	admin.HandleFunc("/products", app.catalog.AdminListProducts).Methods("GET")
	admin.HandleFunc("/products", app.catalog.AdminCreateProduct).Methods("POST")
	admin.HandleFunc("/products/{id:[0-9]+}", app.catalog.AdminGetProduct).Methods("GET")
	admin.HandleFunc("/products/{id:[0-9]+}", app.catalog.AdminUpdateProduct).Methods("PUT")
	admin.HandleFunc("/products/{id:[0-9]+}", app.catalog.AdminDeleteProduct).Methods("DELETE")
	admin.HandleFunc("/products/{id:[0-9]+}/active", app.catalog.AdminSetProductActive).Methods("PATCH")
	admin.HandleFunc("/products/{id:[0-9]+}/image", app.catalog.AdminUploadProductImage).Methods("POST")
	admin.HandleFunc("/products/{id:[0-9]+}/image", app.catalog.AdminDeleteProductImage).Methods("DELETE")
	admin.HandleFunc("/categories", app.catalog.AdminListCategories).Methods("GET")
	admin.HandleFunc("/categories", app.catalog.AdminCreateCategory).Methods("POST")
	admin.HandleFunc("/categories/{id:[0-9]+}", app.catalog.AdminUpdateCategory).Methods("PUT")
	admin.HandleFunc("/categories/{id:[0-9]+}", app.catalog.AdminDeleteCategory).Methods("DELETE")
	admin.HandleFunc("/activity-logs", app.auditAdmin.ListLogs).Methods("GET")
	admin.HandleFunc("/activity-logs/purge", app.auditAdmin.PurgeLogs).Methods("POST")
	admin.HandleFunc("/orders", app.orders.AdminListOrders).Methods("GET")
	admin.HandleFunc("/orders/{id:[0-9]+}", app.orders.AdminGetOrder).Methods("GET")
	admin.HandleFunc("/orders/{id:[0-9]+}/pricing", app.orders.AdminUpdatePricing).Methods("PATCH")
	admin.HandleFunc("/orders/{id:[0-9]+}/confirm", app.orders.AdminConfirmOrder).Methods("POST")
	admin.HandleFunc("/orders/{id:[0-9]+}/shipping-suggestions", app.orders.AdminShippingSuggestions).Methods("GET")
	admin.HandleFunc("/orders/{id:[0-9]+}/shipping-default", app.orders.AdminSetShippingDefault).Methods("POST")
	admin.HandleFunc("/orders/{id:[0-9]+}/payment-proof", app.orders.AdminGetPaymentProof).Methods("GET")
	admin.HandleFunc("/orders/{id:[0-9]+}/status", app.orders.AdminUpdateStatus).Methods("PATCH")
	admin.HandleFunc("/orders/{id:[0-9]+}/note", app.orders.AdminUpdateNote).Methods("PATCH")
	admin.HandleFunc("/customers", app.identity.AdminListCustomers).Methods("GET")
	admin.HandleFunc("/customers/{id:[0-9]+}", app.identity.AdminGetCustomer).Methods("GET")
	admin.HandleFunc("/customers/{id:[0-9]+}/alias", app.identity.AdminUpdateCustomerAlias).Methods("PATCH")
	admin.HandleFunc("/settings", app.orders.AdminGetSettings).Methods("GET")
	admin.HandleFunc("/settings", app.orders.AdminUpdateSettings).Methods("PUT")
	admin.HandleFunc("/push/public-key", app.pushSubs.AdminPushPublicKey).Methods("GET")
	admin.HandleFunc("/push/subscribe", app.pushSubs.AdminPushSubscribe).Methods("POST")
	admin.HandleFunc("/push/subscribe", app.pushSubs.AdminPushUnsubscribe).Methods("DELETE")
	admin.HandleFunc("/push/test", app.pushSubs.AdminPushTest).Methods("POST")
	admin.HandleFunc("/regions/status", app.regions.AdminStatus).Methods("GET")
	admin.HandleFunc("/regions/fetch", app.regions.AdminFetch).Methods("POST")
	admin.HandleFunc("/regions/runs/{id:[0-9]+}", app.regions.AdminRun).Methods("GET")
	admin.HandleFunc("/regions/runs/{id:[0-9]+}/save", app.regions.AdminSave).Methods("POST")
	admin.HandleFunc("/regions/runs/{id:[0-9]+}/discard", app.regions.AdminDiscard).Methods("POST")

	// Liveness: selalu 200 selama proses hidup.
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		platform.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}).Methods("GET")
	// Readiness: memeriksa database (tanpa detail internal).
	router.HandleFunc("/health/ready", app.Ready).Methods("GET")

	return platform.Recoverer(platform.SecurityHeaders(corsHandler(router)))
}
