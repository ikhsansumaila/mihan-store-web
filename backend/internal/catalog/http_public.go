package catalog

// API publik katalog: produk dibaca dari database (tabel products + categories).
// Bentuk respons /api/products dan /api/products/search SAMA seperti versi hardcoded
// lama: [{id, name, category (slug), price, description, image}] dengan field TAMBAHAN di
// belakangnya: unit (satuan jual), tiers (jenjang grosir, [] bila tidak ada), dan thumb (foto kecil).
// Sejak fitur foto produk, `image` berisi URL publik foto utama ("/uploads/products/<id>/<uuid>.jpg")
// atau "" bila tidak ada foto.

import (
	"log"
	"net/http"
	"strings"

	"mihanstore/internal/catalog/media"
	"mihanstore/internal/platform"
)

// PublicProduct adalah bentuk produk di API publik. Field lama (id..image) tidak boleh berubah
// urutan maupun isinya; field baru hanya ditambahkan di belakang.
type PublicProduct struct {
	ID          uint64 `json:"id" gorm:"column:id"`
	Name        string `json:"name" gorm:"column:name"`
	Category    string `json:"category" gorm:"column:category"`
	Price       uint32 `json:"price" gorm:"column:price"`
	Description string `json:"description" gorm:"column:description"`
	Image       string `json:"image" gorm:"column:image"`
	// Tambahan (harga grosir): satuan jual dan jenjang [{minQty, type, value, unitPrice}] urut minQty naik.
	Unit  string    `json:"unit" gorm:"column:unit"`
	Tiers []TierDTO `json:"tiers" gorm:"-"`
	// Tambahan (foto produk): URL thumbnail (sisi terpanjang 400 px). `image` = URL foto utama
	// (1200 px). Keduanya "" bila produk belum punya foto (termasuk nilai lama seperti "kerupuk1.jpg").
	Thumb string `json:"thumb" gorm:"-"`
}

type PublicCategory struct {
	ID        uint64 `json:"id" gorm:"column:id"`
	Slug      string `json:"slug" gorm:"column:slug"`
	Name      string `json:"name" gorm:"column:name"`
	SortOrder int    `json:"sortOrder" gorm:"column:sort_order"`
}

const publicProductSQL = `SELECT p.id, p.name, c.slug AS category, p.price,
  COALESCE(p.description, '') AS description, COALESCE(p.image_path, '') AS image, p.unit
FROM products p JOIN categories c ON c.id = p.category_id
WHERE p.is_active = 1 AND p.deleted_at IS NULL AND c.deleted_at IS NULL`

func (a *Service) publicProducts(w http.ResponseWriter, r *http.Request, q, category string) {
	db := a.db()
	if db == nil {
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	sql := publicProductSQL
	var args []any
	if q != "" {
		sql += ` AND LOWER(p.name) LIKE ?`
		args = append(args, "%"+platform.EscapeLike(strings.ToLower(q))+"%")
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
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	ids := make([]uint64, 0, len(out))
	for _, p := range out {
		ids = append(ids, p.ID)
	}
	tiers, err := LoadTiersFor(db.WithContext(r.Context()), ids)
	if err != nil {
		log.Printf("produk (jenjang): %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	for i := range out {
		out[i].Tiers = TierDTOs(int64(out[i].Price), tiers[out[i].ID])
		out[i].Image, out[i].Thumb = media.PublicImageURLs(a.images, out[i].Image)
	}
	platform.WriteJSON(w, http.StatusOK, out)
}

func (a *Service) GetProducts(w http.ResponseWriter, r *http.Request) {
	a.publicProducts(w, r, "", "")
}

func (a *Service) SearchProducts(w http.ResponseWriter, r *http.Request) {
	q := platform.TruncateUTF8(r.URL.Query().Get("q"), 150)
	category := platform.TruncateUTF8(r.URL.Query().Get("category"), 50)
	a.publicProducts(w, r, q, category)
}

func (a *Service) GetCategories(w http.ResponseWriter, r *http.Request) {
	db := a.db()
	if db == nil {
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	out := []PublicCategory{}
	if err := db.WithContext(r.Context()).Raw(`SELECT id, slug, name, sort_order FROM categories
		WHERE deleted_at IS NULL ORDER BY sort_order, id`).Scan(&out).Error; err != nil {
		log.Printf("kategori: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, out)
}
