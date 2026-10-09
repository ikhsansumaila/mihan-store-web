package catalog

import (
	"strings"
	"testing"
)

// ---------- Validasi ----------

func i64(v int64) *int64   { return &v }
func u64(v uint64) *uint64 { return &v }
func sp(s string) *string  { return &s }

func TestValidateProduct(t *testing.T) {
	ok := ProductInput{Name: "  Kerupuk\tBaru  ", Description: sp("Baris 1\nBaris 2\x00"), Price: i64(15000), CategoryID: u64(1), ImagePath: sp("produk/kerupuk-5.jpg")}
	f, err := validateProduct(ok, true)
	if err != nil || f.Name != "Kerupuk Baru" || *f.Description != "Baris 1\nBaris 2" || f.Price != 15000 || !f.IsActive || *f.ImagePath != "produk/kerupuk-5.jpg" {
		t.Fatalf("produk sah: %v %+v", err, f)
	}
	if f, _ := validateProduct(ProductInput{Name: "A", Price: i64(0), CategoryID: u64(2), ImagePath: sp("  ")}, true); f.ImagePath != nil || f.Description != nil {
		t.Error("image/deskripsi kosong harus NULL")
	}
	bad := map[string]ProductInput{
		"nama kosong":      {Name: "  ", Price: i64(1), CategoryID: u64(1)},
		"nama 151":         {Name: strings.Repeat("a", 151), Price: i64(1), CategoryID: u64(1)},
		"harga negatif":    {Name: "A", Price: i64(-1), CategoryID: u64(1)},
		"harga > 1M":       {Name: "A", Price: i64(1_000_000_001), CategoryID: u64(1)},
		"tanpa harga":      {Name: "A", CategoryID: u64(1)},
		"tanpa kategori":   {Name: "A", Price: i64(1)},
		"deskripsi 2001":   {Name: "A", Price: i64(1), CategoryID: u64(1), Description: sp(strings.Repeat("é", 2001))},
		"image traversal":  {Name: "A", Price: i64(1), CategoryID: u64(1), ImagePath: sp("../etc/passwd")},
		"image absolut":    {Name: "A", Price: i64(1), CategoryID: u64(1), ImagePath: sp("/etc/passwd")},
		"image spasi":      {Name: "A", Price: i64(1), CategoryID: u64(1), ImagePath: sp("a b.jpg")},
		"image url":        {Name: "A", Price: i64(1), CategoryID: u64(1), ImagePath: sp("https://x/y.jpg")},
		"image terlalu pj": {Name: "A", Price: i64(1), CategoryID: u64(1), ImagePath: sp(strings.Repeat("a", 256))},
	}
	for name, in := range bad {
		if _, err := validateProduct(in, true); err == nil {
			t.Errorf("%s: harus ditolak", name)
		}
	}
	if f, _ := validateProduct(ProductInput{Name: "A", Price: i64(1_000_000_000), CategoryID: u64(1)}, false); f.Price != 1_000_000_000 || f.IsActive {
		t.Error("harga maksimum harus diterima; default nonaktif harus dipakai")
	}
}

func TestValidateCategory(t *testing.T) {
	so := 5
	c, err := validateCategory(CategoryInput{Slug: "bumbu_dapur", Name: " Bumbu  Dapur ", SortOrder: &so})
	if err != nil || c.Slug != "bumbu_dapur" || c.Name != "Bumbu Dapur" || c.SortOrder != 5 {
		t.Fatalf("kategori sah: %v %+v", err, c)
	}
	big := 2_000_000
	for name, in := range map[string]CategoryInput{
		"slug huruf besar": {Slug: "Bumbu", Name: "B"},
		"slug 1 huruf":     {Slug: "b", Name: "B"},
		"slug spasi":       {Slug: "bu mbu", Name: "B"},
		"slug tanda hub":   {Slug: "bu-mbu", Name: "B"},
		"slug 51":          {Slug: strings.Repeat("a", 51), Name: "B"},
		"nama kosong":      {Slug: "ok", Name: ""},
		"nama 81":          {Slug: "ok", Name: strings.Repeat("a", 81)},
		"urutan besar":     {Slug: "ok", Name: "B", SortOrder: &big},
	} {
		if _, err := validateCategory(in); err == nil {
			t.Errorf("%s: harus ditolak", name)
		}
	}
}
