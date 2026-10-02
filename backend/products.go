package main

// API publik katalog: produk dibaca dari database (tabel products + categories).
// Bentuk respons /api/products dan /api/products/search SAMA seperti versi hardcoded
// lama: [{id, name, category (slug), price, description, image}].

import (
	"log"
	"net/http"
	"strings"
)

// PublicProduct adalah bentuk produk di API publik (tidak boleh berubah).
type PublicProduct struct {
	ID          uint64 `json:"id" gorm:"column:id"`
	Name        string `json:"name" gorm:"column:name"`
	Category    string `json:"category" gorm:"column:category"`
	Price       uint32 `json:"price" gorm:"column:price"`
	Description string `json:"description" gorm:"column:description"`
	Image       string `json:"image" gorm:"column:image"`
}

type PublicCategory struct {
	ID        uint64 `json:"id" gorm:"column:id"`
	Slug      string `json:"slug" gorm:"column:slug"`
	Name      string `json:"name" gorm:"column:name"`
	SortOrder int    `json:"sortOrder" gorm:"column:sort_order"`
}

const publicProductSQL = `SELECT p.id, p.name, c.slug AS category, p.price,
  COALESCE(p.description, '') AS description, COALESCE(p.image_path, '') AS image
FROM products p JOIN categories c ON c.id = p.category_id
WHERE p.is_active = 1 AND p.deleted_at IS NULL AND c.deleted_at IS NULL`

// escapeLike meloloskan karakter wildcard LIKE (\, %, _).
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (a *App) publicProducts(w http.ResponseWriter, r *http.Request, q, category string) {
	db := a.db.Load()
	if db == nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	sql := publicProductSQL
	var args []any
	if q != "" {
		sql += ` AND LOWER(p.name) LIKE ?`
		args = append(args, "%"+escapeLike(strings.ToLower(q))+"%")
	}
	if category != "" {
		// Pencocokan persis (peka huruf besar/kecil) seperti versi lama.
		sql += ` AND c.slug COLLATE utf8mb4_bin = ?`
		args = append(args, category)
	}
	sql += ` ORDER BY p.id`
	out := []PublicProduct{}
	if err := db.WithContext(r.Context()).Raw(sql, args...).Scan(&out).Error; err != nil {
		log.Printf("produk: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) GetProducts(w http.ResponseWriter, r *http.Request) {
	a.publicProducts(w, r, "", "")
}

func (a *App) SearchProducts(w http.ResponseWriter, r *http.Request) {
	q := truncateUTF8(r.URL.Query().Get("q"), 150)
	category := truncateUTF8(r.URL.Query().Get("category"), 50)
	a.publicProducts(w, r, q, category)
}

func (a *App) GetCategories(w http.ResponseWriter, r *http.Request) {
	db := a.db.Load()
	if db == nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	out := []PublicCategory{}
	if err := db.WithContext(r.Context()).Raw(`SELECT id, slug, name, sort_order FROM categories
		WHERE deleted_at IS NULL ORDER BY sort_order, id`).Scan(&out).Error; err != nil {
		log.Printf("kategori: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
