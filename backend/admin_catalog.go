package main

// Pengelolaan produk dan kategori oleh admin. Setiap perubahan ditulis bersama
// activity log-nya dalam SATU transaksi.

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxProductPrice   = 1_000_000_000
	maxProductName    = 150
	maxProductDesc    = 2000
	maxCategoryName   = 80
	maxPerPage        = 100
	defaultPerPage    = 20
	msgProductMissing = "Produk tidak ditemukan"
	msgCatMissing     = "Kategori tidak ditemukan"
)

var (
	slugRe      = regexp.MustCompile(`^[a-z0-9_]{2,50}$`)
	imagePathRe = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,255}$`)

	errProductName  = fmt.Errorf("Nama produk wajib diisi (maksimal %d karakter)", maxProductName)
	errProductPrice = errors.New("Harga harus bilangan bulat 0 sampai 1.000.000.000")
	errProductDesc  = fmt.Errorf("Deskripsi maksimal %d karakter", maxProductDesc)
	errProductCat   = errors.New("Kategori wajib dipilih")
	errImagePath    = errors.New("Path gambar hanya boleh berisi huruf, angka, titik, garis bawah, tanda hubung, dan garis miring (maks. 255), tanpa '..'")
	errSlug         = errors.New("Slug harus 2–50 karakter: huruf kecil, angka, atau garis bawah (_)")
	errCatName      = fmt.Errorf("Nama kategori wajib diisi (maksimal %d karakter)", maxCategoryName)
	errSortOrder    = errors.New("Urutan harus bilangan bulat antara -1.000.000 dan 1.000.000")
)

// cleanText membuang karakter kontrol & bidi. keepNewlines mempertahankan \n (deskripsi).
func cleanText(s string, keepNewlines bool) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	for _, r := range s {
		if r == '\n' && keepNewlines {
			b.WriteRune(r)
			continue
		}
		if unicode.IsControl(r) || isBidiControl(r) {
			if r == '\t' || r == '\n' || r == '\r' {
				b.WriteRune(' ')
			}
			continue
		}
		b.WriteRune(r)
	}
	if !keepNewlines {
		return strings.Join(strings.Fields(b.String()), " ")
	}
	return strings.TrimSpace(b.String())
}

// ProductInput adalah body POST/PUT produk.
type ProductInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Price       *int64  `json:"price"`
	CategoryID  *uint64 `json:"categoryId"`
	ImagePath   *string `json:"imagePath"`
	IsActive    *bool   `json:"isActive"`
	// Unit: satuan jual (nil = tidak diubah; produk baru "pcs").
	Unit *string `json:"unit"`
	// Tiers: daftar jenjang grosir PENGGANTI seluruh daftar lama (nil = tidak diubah; [] = hapus semua).
	Tiers *[]TierInput `json:"tiers"`
}

// productFields adalah hasil validasi.
type productFields struct {
	Name        string
	Description *string
	Price       uint32
	CategoryID  uint64
	ImagePath   *string
	IsActive    bool
	Unit        *string // nil = tidak dikirim
}

func validateImagePath(p string) (*string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return nil, nil
	}
	if !imagePathRe.MatchString(p) || strings.HasPrefix(p, "/") || strings.Contains(p, "..") || strings.Contains(p, "//") {
		return nil, errImagePath
	}
	return &p, nil
}

func validateProduct(in ProductInput, defaultActive bool) (productFields, error) {
	var f productFields
	f.Name = cleanText(in.Name, false)
	if f.Name == "" || utf8.RuneCountInString(f.Name) > maxProductName {
		return f, errProductName
	}
	if in.Description != nil {
		d := cleanText(*in.Description, true)
		if utf8.RuneCountInString(d) > maxProductDesc {
			return f, errProductDesc
		}
		if d != "" {
			f.Description = &d
		}
	}
	if in.Price == nil || *in.Price < 0 || *in.Price > maxProductPrice {
		return f, errProductPrice
	}
	f.Price = uint32(*in.Price)
	if in.CategoryID == nil || *in.CategoryID == 0 {
		return f, errProductCat
	}
	f.CategoryID = *in.CategoryID
	if in.ImagePath != nil {
		p, err := validateImagePath(*in.ImagePath)
		if err != nil {
			return f, err
		}
		f.ImagePath = p
	}
	f.IsActive = defaultActive
	if in.IsActive != nil {
		f.IsActive = *in.IsActive
	}
	if in.Unit != nil {
		u, err := normalizeUnit(*in.Unit)
		if err != nil {
			return f, err
		}
		f.Unit = &u
	}
	return f, nil
}

// CategoryInput adalah body POST/PUT kategori.
type CategoryInput struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	SortOrder *int   `json:"sortOrder"`
}

func validateCategory(in CategoryInput) (Category, error) {
	var c Category
	c.Slug = strings.TrimSpace(in.Slug)
	if !slugRe.MatchString(c.Slug) {
		return c, errSlug
	}
	c.Name = cleanText(in.Name, false)
	if c.Name == "" || utf8.RuneCountInString(c.Name) > maxCategoryName {
		return c, errCatName
	}
	if in.SortOrder != nil {
		if *in.SortOrder < -1_000_000 || *in.SortOrder > 1_000_000 {
			return c, errSortOrder
		}
		c.SortOrder = *in.SortOrder
	}
	return c, nil
}

// ---------- DTO & query ----------

type AdminProductDTO struct {
	ID              uint64    `json:"id" gorm:"column:id"`
	Name            string    `json:"name" gorm:"column:name"`
	Description     string    `json:"description" gorm:"column:description"`
	Price           uint32    `json:"price" gorm:"column:price"`
	Unit            string    `json:"unit" gorm:"column:unit"`
	Tiers           []TierDTO `json:"tiers" gorm:"-"`
	ImagePath       string    `json:"imagePath" gorm:"column:image_path"`
	IsActive        bool      `json:"isActive" gorm:"column:is_active"`
	CategoryID      uint64    `json:"categoryId" gorm:"column:category_id"`
	CategorySlug    string    `json:"categorySlug" gorm:"column:category_slug"`
	CategoryName    string    `json:"categoryName" gorm:"column:category_name"`
	CategoryDeleted bool      `json:"categoryDeleted" gorm:"column:category_deleted"`
	CreatedAt       time.Time `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt       time.Time `json:"updatedAt" gorm:"column:updated_at"`
	CreatedBy       *string   `json:"createdBy" gorm:"column:created_by_name"`
	UpdatedBy       *string   `json:"updatedBy" gorm:"column:updated_by_name"`
}

const adminProductSelect = `SELECT p.id, p.name, COALESCE(p.description, '') AS description, p.price, p.unit,
  COALESCE(p.image_path, '') AS image_path, p.is_active, p.category_id,
  c.slug AS category_slug, c.name AS category_name, (c.deleted_at IS NOT NULL) AS category_deleted,
  p.created_at, p.updated_at, cu.username AS created_by_name, uu.username AS updated_by_name
FROM products p
JOIN categories c ON c.id = p.category_id
LEFT JOIN users cu ON cu.id = p.created_by
LEFT JOIN users uu ON uu.id = p.updated_by
WHERE p.deleted_at IS NULL`

func pageParams(r *http.Request) (page, perPage int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ = strconv.Atoi(r.URL.Query().Get("per_page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		page = 100000
	}
	if perPage < 1 {
		perPage = defaultPerPage
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	return
}

func pathID(r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(mux.Vars(r)["id"], 10, 64)
	return id, err == nil && id > 0
}

func (a *App) loadAdminProduct(db *gorm.DB, id uint64) (*AdminProductDTO, error) {
	var out []AdminProductDTO
	if err := db.Raw(adminProductSelect+` AND p.id = ?`, id).Scan(&out).Error; err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	if err := attachAdminTiers(db, out); err != nil {
		return nil, err
	}
	return &out[0], nil
}

// attachAdminTiers mengisi Tiers (selalu non-nil) untuk daftar produk admin.
func attachAdminTiers(db *gorm.DB, items []AdminProductDTO) error {
	ids := make([]uint64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	tiers, err := loadTiersFor(db, ids)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].Tiers = tierDTOs(int64(items[i].Price), tiers[items[i].ID])
	}
	return nil
}

func (a *App) AdminListProducts(w http.ResponseWriter, r *http.Request) {
	db := a.db.Load().WithContext(r.Context())
	q := r.URL.Query()
	where := ""
	var args []any
	if s := strings.TrimSpace(truncateUTF8(q.Get("q"), 150)); s != "" {
		where += ` AND p.name LIKE ?`
		args = append(args, "%"+escapeLike(s)+"%")
	}
	if c := strings.TrimSpace(q.Get("category")); c != "" {
		if id, err := strconv.ParseUint(c, 10, 64); err == nil {
			where += ` AND p.category_id = ?`
			args = append(args, id)
		} else {
			where += ` AND c.slug = ?`
			args = append(args, truncateUTF8(c, 50))
		}
	}
	switch strings.ToLower(q.Get("status")) {
	case "aktif", "active", "1":
		where += ` AND p.is_active = 1`
	case "nonaktif", "inactive", "0":
		where += ` AND p.is_active = 0`
	}
	page, perPage := pageParams(r)
	var total int64
	if err := db.Raw(`SELECT COUNT(*) FROM products p JOIN categories c ON c.id = p.category_id WHERE p.deleted_at IS NULL`+where, args...).Scan(&total).Error; err != nil {
		log.Printf("admin produk: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	items := []AdminProductDTO{}
	qargs := append(append([]any{}, args...), perPage, (page-1)*perPage)
	if err := db.Raw(adminProductSelect+where+` ORDER BY p.id LIMIT ? OFFSET ?`, qargs...).Scan(&items).Error; err != nil {
		log.Printf("admin produk: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	if err := attachAdminTiers(db, items); err != nil {
		log.Printf("admin produk (jenjang): %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "perPage": perPage})
}

func (a *App) AdminGetProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	p, err := a.loadAdminProduct(a.db.Load().WithContext(r.Context()), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// lockActiveCategory memastikan kategori ada & belum dihapus (dikunci FOR SHARE agar
// tidak bisa dihapus bersamaan).
func lockActiveCategory(tx *gorm.DB, id uint64) (*Category, error) {
	var c Category
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", id).Take(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func productSnapshot(p *Product, catSlug string, tiers []PriceTier) map[string]any {
	desc, img := "", ""
	if p.Description != nil {
		desc = *p.Description
	}
	if p.ImagePath != nil {
		img = *p.ImagePath
	}
	return map[string]any{
		"name": p.Name, "description": desc, "price": p.Price, "categoryId": p.CategoryID,
		"category": catSlug, "imagePath": img, "isActive": p.IsActive,
		"unit": p.Unit, "tiers": tierSummaries(tiers),
	}
}

// httpError dipakai untuk membatalkan transaksi dengan respons tertentu.
type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

func (a *App) respondTxError(w http.ResponseWriter, err error, ctx string) {
	var he *httpError
	if errors.As(err, &he) {
		writeError(w, he.status, he.msg)
		return
	}
	var tve *TierValidationError
	if errors.As(err, &tve) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": tve.Error(), "tierErrors": tve.Errors})
		return
	}
	log.Printf("%s: %v", ctx, err)
	writeError(w, http.StatusInternalServerError, msgServerError)
}

func (a *App) AdminCreateProduct(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	var in ProductInput
	if err := decodeJSON(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	f, err := validateProduct(in, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var tiers []PriceTier
	if in.Tiers != nil {
		if tiers, err = validateTiers(int64(f.Price), *in.Tiers); err != nil {
			a.respondTxError(w, err, "tambah produk")
			return
		}
	}
	unit := defaultUnit
	if f.Unit != nil {
		unit = *f.Unit
	}
	db := a.db.Load().WithContext(r.Context())
	p := Product{CategoryID: f.CategoryID, Name: f.Name, Description: f.Description, Price: f.Price, Unit: unit,
		ImagePath: f.ImagePath, IsActive: f.IsActive, CreatedBy: uid(admin), UpdatedBy: uid(admin)}
	err = db.Transaction(func(tx *gorm.DB) error {
		cat, err := lockActiveCategory(tx, f.CategoryID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &httpError{http.StatusBadRequest, "Kategori tidak ditemukan atau sudah dihapus"}
		}
		if err != nil {
			return err
		}
		if err := tx.Select("CategoryID", "Name", "Description", "Price", "Unit", "ImagePath", "IsActive", "CreatedBy", "UpdatedBy").Create(&p).Error; err != nil {
			return err
		}
		if _, err := replaceTiers(tx, p.ID, nil, tiers, a.now()); err != nil {
			return err
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "product.create",
			EntityType: "product", EntityID: strconv.FormatUint(p.ID, 10),
			Summary: "Produk ditambahkan: " + p.Name,
			Details: map[string]any{"sesudah": productSnapshot(&p, cat.Slug, tiers)},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "tambah produk")
		return
	}
	dto, err := a.loadAdminProduct(db, p.ID)
	if err != nil {
		writeJSON(w, http.StatusCreated, map[string]any{"id": p.ID})
		return
	}
	writeJSON(w, http.StatusCreated, dto)
}

// lockProduct mengunci baris produk yang belum dihapus (FOR UPDATE).
func lockProduct(tx *gorm.DB, id uint64) (*Product, string, error) {
	var p Product
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", &httpError{http.StatusNotFound, msgProductMissing}
		}
		return nil, "", err
	}
	var slug string
	if err := tx.Raw(`SELECT slug FROM categories WHERE id = ?`, p.CategoryID).Scan(&slug).Error; err != nil {
		return nil, "", err
	}
	return &p, slug, nil
}

func (a *App) AdminUpdateProduct(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	var in ProductInput
	if err := decodeJSON(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	db := a.db.Load().WithContext(r.Context())
	err := db.Transaction(func(tx *gorm.DB) error {
		cur, curSlug, err := lockProduct(tx, id)
		if err != nil {
			return err
		}
		f, err := validateProduct(in, cur.IsActive)
		if err != nil {
			return &httpError{http.StatusBadRequest, err.Error()}
		}
		newSlug := curSlug
		if f.CategoryID != cur.CategoryID {
			cat, err := lockActiveCategory(tx, f.CategoryID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &httpError{http.StatusBadRequest, "Kategori tidak ditemukan atau sudah dihapus"}
			}
			if err != nil {
				return err
			}
			newSlug = cat.Slug
		}
		existing, err := lockTiers(tx, id)
		if err != nil {
			return err
		}
		curTiers := rowsToTiers(existing)
		// Jenjang baru: daftar pengganti dari body, atau jenjang lama yang divalidasi ulang terhadap
		// harga dasar baru (jenjang percent mengikuti harga dasar; fixed bisa menjadi tidak valid).
		nextTiers := curTiers
		var terr error
		if in.Tiers != nil {
			nextTiers, terr = validateTiers(int64(f.Price), *in.Tiers)
		} else if f.Price != cur.Price && len(curTiers) > 0 {
			_, terr = validateTiers(int64(f.Price), tierInputsFrom(curTiers))
		}
		if terr != nil {
			var tve *TierValidationError
			if errors.As(terr, &tve) && f.Price != cur.Price {
				tve.Lead = fmt.Sprintf("Harga dasar baru %s membuat jenjang grosir tidak valid; ubah atau hapus jenjang yang bermasalah.", formatRupiah(int64(f.Price)))
			}
			return terr
		}
		unit := cur.Unit
		if f.Unit != nil {
			unit = *f.Unit
		}
		before := productSnapshot(cur, curSlug, curTiers)
		next := *cur
		next.Name, next.Description, next.Price, next.CategoryID, next.ImagePath, next.IsActive, next.Unit =
			f.Name, f.Description, f.Price, f.CategoryID, f.ImagePath, f.IsActive, unit
		changes := diffMaps(before, productSnapshot(&next, newSlug, nextTiers))
		if len(changes) == 0 {
			return nil // tidak ada perubahan: tidak ditulis, tidak dicatat
		}
		if _, err := replaceTiers(tx, id, existing, nextTiers, a.now()); err != nil {
			return err
		}
		if err := tx.Model(&Product{}).Where("id = ?", id).Updates(map[string]any{
			"name": f.Name, "description": f.Description, "price": f.Price, "category_id": f.CategoryID,
			"image_path": f.ImagePath, "is_active": f.IsActive, "unit": unit, "updated_by": uid(admin),
		}).Error; err != nil {
			return err
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "product.update",
			EntityType: "product", EntityID: strconv.FormatUint(id, 10),
			Summary: "Produk diubah: " + f.Name,
			Details: map[string]any{"perubahan": changes},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "ubah produk")
		return
	}
	dto, err := a.loadAdminProduct(db, id)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

type activeInput struct {
	Active *bool `json:"active"`
}

func (a *App) AdminSetProductActive(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	var in activeInput
	if err := decodeJSON(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	if in.Active == nil {
		writeError(w, http.StatusBadRequest, "Field active (true/false) wajib diisi")
		return
	}
	db := a.db.Load().WithContext(r.Context())
	err := db.Transaction(func(tx *gorm.DB) error {
		cur, _, err := lockProduct(tx, id)
		if err != nil {
			return err
		}
		if cur.IsActive == *in.Active {
			return nil
		}
		if *in.Active {
			// Mengaktifkan produk di kategori yang sudah dihapus tidak diizinkan.
			if _, err := lockActiveCategory(tx, cur.CategoryID); errors.Is(err, gorm.ErrRecordNotFound) {
				return &httpError{http.StatusConflict, "Kategori produk ini sudah dihapus. Pindahkan ke kategori lain terlebih dahulu."}
			} else if err != nil {
				return err
			}
		}
		if err := tx.Model(&Product{}).Where("id = ?", id).
			Updates(map[string]any{"is_active": *in.Active, "updated_by": uid(admin)}).Error; err != nil {
			return err
		}
		action, word := "product.deactivate", "dinonaktifkan"
		if *in.Active {
			action, word = "product.activate", "diaktifkan"
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: action,
			EntityType: "product", EntityID: strconv.FormatUint(id, 10),
			Summary: "Produk " + word + ": " + cur.Name,
			Details: map[string]any{"perubahan": map[string]any{"isActive": map[string]any{"dari": cur.IsActive, "menjadi": *in.Active}}},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "status produk")
		return
	}
	dto, err := a.loadAdminProduct(db, id)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (a *App) AdminDeleteProduct(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	db := a.db.Load().WithContext(r.Context())
	err := db.Transaction(func(tx *gorm.DB) error {
		cur, slug, err := lockProduct(tx, id)
		if err != nil {
			return err
		}
		tierRows, err := lockTiers(tx, id)
		if err != nil {
			return err
		}
		if err := tx.Model(&Product{}).Where("id = ?", id).
			Updates(map[string]any{"deleted_at": a.now(), "updated_by": uid(admin)}).Error; err != nil {
			return err
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "product.delete",
			EntityType: "product", EntityID: strconv.FormatUint(id, 10),
			Summary: "Produk dihapus: " + cur.Name,
			Details: map[string]any{"sebelum": productSnapshot(cur, slug, rowsToTiers(tierRows))},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "hapus produk")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Produk dihapus"})
}

// ---------- Kategori ----------

type AdminCategoryDTO struct {
	ID             uint64    `json:"id" gorm:"column:id"`
	Slug           string    `json:"slug" gorm:"column:slug"`
	Name           string    `json:"name" gorm:"column:name"`
	SortOrder      int       `json:"sortOrder" gorm:"column:sort_order"`
	ActiveProducts int64     `json:"activeProducts" gorm:"column:active_products"`
	TotalProducts  int64     `json:"totalProducts" gorm:"column:total_products"`
	CreatedAt      time.Time `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt      time.Time `json:"updatedAt" gorm:"column:updated_at"`
}

const adminCategorySelect = `SELECT c.id, c.slug, c.name, c.sort_order, c.created_at, c.updated_at,
  (SELECT COUNT(*) FROM products p WHERE p.category_id = c.id AND p.deleted_at IS NULL AND p.is_active = 1) AS active_products,
  (SELECT COUNT(*) FROM products p WHERE p.category_id = c.id AND p.deleted_at IS NULL) AS total_products
FROM categories c WHERE c.deleted_at IS NULL`

func (a *App) AdminListCategories(w http.ResponseWriter, r *http.Request) {
	out := []AdminCategoryDTO{}
	if err := a.db.Load().WithContext(r.Context()).Raw(adminCategorySelect + ` ORDER BY c.sort_order, c.id`).Scan(&out).Error; err != nil {
		log.Printf("admin kategori: %v", err)
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (a *App) loadAdminCategory(db *gorm.DB, id uint64) (*AdminCategoryDTO, error) {
	var out []AdminCategoryDTO
	if err := db.Raw(adminCategorySelect+` AND c.id = ?`, id).Scan(&out).Error; err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &out[0], nil
}

func categorySnapshot(c *Category) map[string]any {
	return map[string]any{"slug": c.Slug, "name": c.Name, "sortOrder": c.SortOrder}
}

const msgSlugTaken = "Slug kategori sudah dipakai"

func (a *App) AdminCreateCategory(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	var in CategoryInput
	if err := decodeJSON(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	c, err := validateCategory(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	db := a.db.Load().WithContext(r.Context())
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("Slug", "Name", "SortOrder").Create(&c).Error; err != nil {
			if isDuplicateKey(err) {
				return &httpError{http.StatusConflict, msgSlugTaken}
			}
			return err
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "category.create",
			EntityType: "category", EntityID: strconv.FormatUint(c.ID, 10),
			Summary: "Kategori ditambahkan: " + c.Name,
			Details: map[string]any{"sesudah": categorySnapshot(&c)},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "tambah kategori")
		return
	}
	dto, err := a.loadAdminCategory(db, c.ID)
	if err != nil {
		writeJSON(w, http.StatusCreated, map[string]any{"id": c.ID})
		return
	}
	writeJSON(w, http.StatusCreated, dto)
}

func lockCategory(tx *gorm.DB, id uint64) (*Category, error) {
	var c Category
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &httpError{http.StatusNotFound, msgCatMissing}
		}
		return nil, err
	}
	return &c, nil
}

func (a *App) AdminUpdateCategory(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgCatMissing)
		return
	}
	var in CategoryInput
	if err := decodeJSON(w, r, &in, false); err != nil {
		respondDecodeError(w, err)
		return
	}
	db := a.db.Load().WithContext(r.Context())
	err := db.Transaction(func(tx *gorm.DB) error {
		cur, err := lockCategory(tx, id)
		if err != nil {
			return err
		}
		if in.SortOrder == nil {
			so := cur.SortOrder
			in.SortOrder = &so
		}
		nc, err := validateCategory(in)
		if err != nil {
			return &httpError{http.StatusBadRequest, err.Error()}
		}
		changes := diffMaps(categorySnapshot(cur), categorySnapshot(&nc))
		if len(changes) == 0 {
			return nil
		}
		if err := tx.Model(&Category{}).Where("id = ?", id).
			Updates(map[string]any{"slug": nc.Slug, "name": nc.Name, "sort_order": nc.SortOrder}).Error; err != nil {
			if isDuplicateKey(err) {
				return &httpError{http.StatusConflict, msgSlugTaken}
			}
			return err
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "category.update",
			EntityType: "category", EntityID: strconv.FormatUint(id, 10),
			Summary: "Kategori diubah: " + nc.Name,
			Details: map[string]any{"perubahan": changes},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "ubah kategori")
		return
	}
	dto, err := a.loadAdminCategory(db, id)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, msgServiceDown)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (a *App) AdminDeleteCategory(w http.ResponseWriter, r *http.Request) {
	admin := adminFrom(r.Context())
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, msgCatMissing)
		return
	}
	db := a.db.Load().WithContext(r.Context())
	err := db.Transaction(func(tx *gorm.DB) error {
		cur, err := lockCategory(tx, id)
		if err != nil {
			return err
		}
		var active int64
		if err := tx.Raw(`SELECT COUNT(*) FROM products WHERE category_id = ? AND deleted_at IS NULL AND is_active = 1 FOR UPDATE`, id).Scan(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return &httpError{http.StatusConflict, fmt.Sprintf("Kategori masih memiliki %d produk aktif. Pindahkan atau nonaktifkan produk tersebut terlebih dahulu.", active)}
		}
		if err := tx.Model(&Category{}).Where("id = ?", id).Update("deleted_at", a.now()).Error; err != nil {
			return err
		}
		return a.logActivity(tx, a.reqMeta(r, LogEntry{
			UserID: uid(admin), ActorLabel: actorOf(admin), Action: "category.delete",
			EntityType: "category", EntityID: strconv.FormatUint(id, 10),
			Summary: "Kategori dihapus: " + cur.Name,
			Details: map[string]any{"sebelum": categorySnapshot(cur)},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "hapus kategori")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Kategori dihapus"})
}
