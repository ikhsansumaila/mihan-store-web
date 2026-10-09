package catalog

// Validasi input katalog (produk & kategori), murni tanpa HTTP/DB.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"mihanstore/internal/platform"
)

const (
	maxProductName    = 150
	maxProductDesc    = 2000
	maxCategoryName   = 80
	msgProductMissing = "Produk tidak ditemukan"
	msgCatMissing     = "Kategori tidak ditemukan"
	msgImageViaUpload = "Foto produk diatur lewat tombol \"Pilih foto\" (unggah), bukan lewat path gambar."
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
	f.Name = platform.CleanText(in.Name, false)
	if f.Name == "" || utf8.RuneCountInString(f.Name) > maxProductName {
		return f, errProductName
	}
	if in.Description != nil {
		d := platform.CleanText(*in.Description, true)
		if utf8.RuneCountInString(d) > maxProductDesc {
			return f, errProductDesc
		}
		if d != "" {
			f.Description = &d
		}
	}
	if in.Price == nil || *in.Price < 0 || *in.Price > MaxProductPrice {
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
		u, err := NormalizeUnit(*in.Unit)
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
	c.Name = platform.CleanText(in.Name, false)
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
