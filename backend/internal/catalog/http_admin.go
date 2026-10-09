package catalog

// Pengelolaan produk dan kategori oleh admin. Setiap perubahan ditulis bersama
// activity log-nya dalam SATU transaksi.

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"mihanstore/internal/audit"
	"mihanstore/internal/catalog/media"
	"mihanstore/internal/platform"
)

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
	// Foto produk: URL publik foto utama dan thumbnail ("" bila tidak ada foto).
	Image string `json:"image" gorm:"-"`
	Thumb string `json:"thumb" gorm:"-"`
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

func (a *Service) loadAdminProduct(db *gorm.DB, id uint64) (*AdminProductDTO, error) {
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
	a.attachAdminImages(out)
	return &out[0], nil
}

// attachAdminImages mengisi URL foto (image/thumb) dari image_path.
func (a *Service) attachAdminImages(items []AdminProductDTO) {
	for i := range items {
		items[i].Image, items[i].Thumb = media.PublicImageURLs(a.images, items[i].ImagePath)
	}
}

// attachAdminTiers mengisi Tiers (selalu non-nil) untuk daftar produk admin.
func attachAdminTiers(db *gorm.DB, items []AdminProductDTO) error {
	ids := make([]uint64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	tiers, err := LoadTiersFor(db, ids)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].Tiers = TierDTOs(int64(items[i].Price), tiers[items[i].ID])
	}
	return nil
}

func (a *Service) AdminListProducts(w http.ResponseWriter, r *http.Request) {
	db := a.db().WithContext(r.Context())
	q := r.URL.Query()
	where := ""
	var args []any
	if s := strings.TrimSpace(platform.TruncateUTF8(q.Get("q"), 150)); s != "" {
		where += ` AND p.name LIKE ?`
		args = append(args, "%"+platform.EscapeLike(s)+"%")
	}
	if c := strings.TrimSpace(q.Get("category")); c != "" {
		if id, err := strconv.ParseUint(c, 10, 64); err == nil {
			where += ` AND p.category_id = ?`
			args = append(args, id)
		} else {
			where += ` AND c.slug = ?`
			args = append(args, platform.TruncateUTF8(c, 50))
		}
	}
	switch strings.ToLower(q.Get("status")) {
	case "aktif", "active", "1":
		where += ` AND p.is_active = 1`
	case "nonaktif", "inactive", "0":
		where += ` AND p.is_active = 0`
	}
	page, perPage := platform.PageParams(r)
	var total int64
	if err := db.Raw(`SELECT COUNT(*) FROM products p JOIN categories c ON c.id = p.category_id WHERE p.deleted_at IS NULL`+where, args...).Scan(&total).Error; err != nil {
		log.Printf("admin produk: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	items := []AdminProductDTO{}
	qargs := append(append([]any{}, args...), perPage, (page-1)*perPage)
	if err := db.Raw(adminProductSelect+where+` ORDER BY p.id LIMIT ? OFFSET ?`, qargs...).Scan(&items).Error; err != nil {
		log.Printf("admin produk: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	if err := attachAdminTiers(db, items); err != nil {
		log.Printf("admin produk (jenjang): %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	a.attachAdminImages(items)
	platform.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "perPage": perPage})
}

func (a *Service) AdminGetProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	p, err := a.loadAdminProduct(a.db().WithContext(r.Context()), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		platform.WriteError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	if err != nil {
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, p)
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
		"unit": p.Unit, "tiers": TierSummaries(tiers),
	}
}

func (a *Service) AdminCreateProduct(w http.ResponseWriter, r *http.Request) {
	adminID, adminLabel := a.actor(r)
	var in ProductInput
	if err := platform.DecodeJSON(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	f, err := validateProduct(in, true)
	if err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if f.ImagePath != nil && media.IsProductImageKey(*f.ImagePath) {
		platform.WriteError(w, http.StatusBadRequest, msgImageViaUpload)
		return
	}
	var tiers []PriceTier
	if in.Tiers != nil {
		if tiers, err = ValidateTiers(int64(f.Price), *in.Tiers); err != nil {
			a.respondTxError(w, err, "tambah produk")
			return
		}
	}
	unit := DefaultUnit
	if f.Unit != nil {
		unit = *f.Unit
	}
	db := a.db().WithContext(r.Context())
	p := Product{CategoryID: f.CategoryID, Name: f.Name, Description: f.Description, Price: f.Price, Unit: unit,
		ImagePath: f.ImagePath, IsActive: f.IsActive, CreatedBy: adminID, UpdatedBy: adminID}
	err = db.Transaction(func(tx *gorm.DB) error {
		cat, err := lockActiveCategory(tx, f.CategoryID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: "Kategori tidak ditemukan atau sudah dihapus"}
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
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: adminID, ActorLabel: adminLabel, Action: "product.create",
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
		platform.WriteJSON(w, http.StatusCreated, map[string]any{"id": p.ID})
		return
	}
	platform.WriteJSON(w, http.StatusCreated, dto)
}

// lockProduct mengunci baris produk yang belum dihapus (FOR UPDATE).
func lockProduct(tx *gorm.DB, id uint64) (*Product, string, error) {
	var p Product
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", &platform.HTTPError{Status: http.StatusNotFound, Msg: msgProductMissing}
		}
		return nil, "", err
	}
	var slug string
	if err := tx.Raw(`SELECT slug FROM categories WHERE id = ?`, p.CategoryID).Scan(&slug).Error; err != nil {
		return nil, "", err
	}
	return &p, slug, nil
}

func (a *Service) AdminUpdateProduct(w http.ResponseWriter, r *http.Request) {
	adminID, adminLabel := a.actor(r)
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	var in ProductInput
	if err := platform.DecodeJSON(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	db := a.db().WithContext(r.Context())
	err := db.Transaction(func(tx *gorm.DB) error {
		cur, curSlug, err := lockProduct(tx, id)
		if err != nil {
			return err
		}
		f, err := validateProduct(in, cur.IsActive)
		if err != nil {
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: err.Error()}
		}
		newSlug := curSlug
		if f.CategoryID != cur.CategoryID {
			cat, err := lockActiveCategory(tx, f.CategoryID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &platform.HTTPError{Status: http.StatusBadRequest, Msg: "Kategori tidak ditemukan atau sudah dihapus"}
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
			nextTiers, terr = ValidateTiers(int64(f.Price), *in.Tiers)
		} else if f.Price != cur.Price && len(curTiers) > 0 {
			_, terr = ValidateTiers(int64(f.Price), TierInputsFrom(curTiers))
		}
		if terr != nil {
			var tve *TierValidationError
			if errors.As(terr, &tve) && f.Price != cur.Price {
				tve.Lead = fmt.Sprintf("Harga dasar baru %s membuat jenjang grosir tidak valid; ubah atau hapus jenjang yang bermasalah.", FormatRupiah(int64(f.Price)))
			}
			return terr
		}
		// Foto produk hanya diatur lewat /image (unggah/hapus). image_path tidak berubah bila field
		// imagePath tidak dikirim atau produk sudah punya foto unggahan; kunci foto tidak bisa disetel manual.
		switch {
		case in.ImagePath == nil || (cur.ImagePath != nil && media.IsProductImageKey(*cur.ImagePath)):
			f.ImagePath = cur.ImagePath
		case f.ImagePath != nil && media.IsProductImageKey(*f.ImagePath):
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: msgImageViaUpload}
		}
		unit := cur.Unit
		if f.Unit != nil {
			unit = *f.Unit
		}
		before := productSnapshot(cur, curSlug, curTiers)
		next := *cur
		next.Name, next.Description, next.Price, next.CategoryID, next.ImagePath, next.IsActive, next.Unit =
			f.Name, f.Description, f.Price, f.CategoryID, f.ImagePath, f.IsActive, unit
		changes := audit.DiffMaps(before, productSnapshot(&next, newSlug, nextTiers))
		if len(changes) == 0 {
			return nil // tidak ada perubahan: tidak ditulis, tidak dicatat
		}
		if _, err := replaceTiers(tx, id, existing, nextTiers, a.now()); err != nil {
			return err
		}
		if err := tx.Model(&Product{}).Where("id = ?", id).Updates(map[string]any{
			"name": f.Name, "description": f.Description, "price": f.Price, "category_id": f.CategoryID,
			"image_path": f.ImagePath, "is_active": f.IsActive, "unit": unit, "updated_by": adminID,
		}).Error; err != nil {
			return err
		}
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: adminID, ActorLabel: adminLabel, Action: "product.update",
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
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, dto)
}

type activeInput struct {
	Active *bool `json:"active"`
}

func (a *Service) AdminSetProductActive(w http.ResponseWriter, r *http.Request) {
	adminID, adminLabel := a.actor(r)
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	var in activeInput
	if err := platform.DecodeJSON(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	if in.Active == nil {
		platform.WriteError(w, http.StatusBadRequest, "Field active (true/false) wajib diisi")
		return
	}
	db := a.db().WithContext(r.Context())
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
				return &platform.HTTPError{Status: http.StatusConflict, Msg: "Kategori produk ini sudah dihapus. Pindahkan ke kategori lain terlebih dahulu."}
			} else if err != nil {
				return err
			}
		}
		if err := tx.Model(&Product{}).Where("id = ?", id).
			Updates(map[string]any{"is_active": *in.Active, "updated_by": adminID}).Error; err != nil {
			return err
		}
		action, word := "product.deactivate", "dinonaktifkan"
		if *in.Active {
			action, word = "product.activate", "diaktifkan"
		}
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: adminID, ActorLabel: adminLabel, Action: action,
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
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, dto)
}

func (a *Service) AdminDeleteProduct(w http.ResponseWriter, r *http.Request) {
	adminID, adminLabel := a.actor(r)
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgProductMissing)
		return
	}
	db := a.db().WithContext(r.Context())
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
			Updates(map[string]any{"deleted_at": a.now(), "updated_by": adminID}).Error; err != nil {
			return err
		}
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: adminID, ActorLabel: adminLabel, Action: "product.delete",
			EntityType: "product", EntityID: strconv.FormatUint(id, 10),
			Summary: "Produk dihapus: " + cur.Name,
			Details: map[string]any{"sebelum": productSnapshot(cur, slug, rowsToTiers(tierRows))},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "hapus produk")
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Produk dihapus"})
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

func (a *Service) AdminListCategories(w http.ResponseWriter, r *http.Request) {
	out := []AdminCategoryDTO{}
	if err := a.db().WithContext(r.Context()).Raw(adminCategorySelect + ` ORDER BY c.sort_order, c.id`).Scan(&out).Error; err != nil {
		log.Printf("admin kategori: %v", err)
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (a *Service) loadAdminCategory(db *gorm.DB, id uint64) (*AdminCategoryDTO, error) {
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

func (a *Service) AdminCreateCategory(w http.ResponseWriter, r *http.Request) {
	adminID, adminLabel := a.actor(r)
	var in CategoryInput
	if err := platform.DecodeJSON(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	c, err := validateCategory(in)
	if err != nil {
		platform.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	db := a.db().WithContext(r.Context())
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("Slug", "Name", "SortOrder").Create(&c).Error; err != nil {
			if platform.IsDuplicateKey(err) {
				return &platform.HTTPError{Status: http.StatusConflict, Msg: msgSlugTaken}
			}
			return err
		}
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: adminID, ActorLabel: adminLabel, Action: "category.create",
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
		platform.WriteJSON(w, http.StatusCreated, map[string]any{"id": c.ID})
		return
	}
	platform.WriteJSON(w, http.StatusCreated, dto)
}

func lockCategory(tx *gorm.DB, id uint64) (*Category, error) {
	var c Category
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &platform.HTTPError{Status: http.StatusNotFound, Msg: msgCatMissing}
		}
		return nil, err
	}
	return &c, nil
}

func (a *Service) AdminUpdateCategory(w http.ResponseWriter, r *http.Request) {
	adminID, adminLabel := a.actor(r)
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgCatMissing)
		return
	}
	var in CategoryInput
	if err := platform.DecodeJSON(w, r, &in, false); err != nil {
		platform.RespondDecodeError(w, err)
		return
	}
	db := a.db().WithContext(r.Context())
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
			return &platform.HTTPError{Status: http.StatusBadRequest, Msg: err.Error()}
		}
		changes := audit.DiffMaps(categorySnapshot(cur), categorySnapshot(&nc))
		if len(changes) == 0 {
			return nil
		}
		if err := tx.Model(&Category{}).Where("id = ?", id).
			Updates(map[string]any{"slug": nc.Slug, "name": nc.Name, "sort_order": nc.SortOrder}).Error; err != nil {
			if platform.IsDuplicateKey(err) {
				return &platform.HTTPError{Status: http.StatusConflict, Msg: msgSlugTaken}
			}
			return err
		}
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: adminID, ActorLabel: adminLabel, Action: "category.update",
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
		platform.WriteError(w, http.StatusServiceUnavailable, platform.MsgServiceDown)
		return
	}
	platform.WriteJSON(w, http.StatusOK, dto)
}

func (a *Service) AdminDeleteCategory(w http.ResponseWriter, r *http.Request) {
	adminID, adminLabel := a.actor(r)
	id, ok := platform.PathID(r)
	if !ok {
		platform.WriteError(w, http.StatusNotFound, msgCatMissing)
		return
	}
	db := a.db().WithContext(r.Context())
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
			return &platform.HTTPError{Status: http.StatusConflict, Msg: fmt.Sprintf("Kategori masih memiliki %d produk aktif. Pindahkan atau nonaktifkan produk tersebut terlebih dahulu.", active)}
		}
		if err := tx.Model(&Category{}).Where("id = ?", id).Update("deleted_at", a.now()).Error; err != nil {
			return err
		}
		return a.audit.Log(tx, a.audit.ReqMeta(r, audit.Entry{
			UserID: adminID, ActorLabel: adminLabel, Action: "category.delete",
			EntityType: "category", EntityID: strconv.FormatUint(id, 10),
			Summary: "Kategori dihapus: " + cur.Name,
			Details: map[string]any{"sebelum": categorySnapshot(cur)},
		}))
	})
	if err != nil {
		a.respondTxError(w, err, "hapus kategori")
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Kategori dihapus"})
}
