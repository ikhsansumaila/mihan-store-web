package catalog

import (
	"time"

	"gorm.io/gorm"
)

// Category memetakan tabel categories (migrations/003). Kolom `alive` tidak dipetakan.
type Category struct {
	ID        uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	Slug      string         `gorm:"column:slug"`
	Name      string         `gorm:"column:name"`
	SortOrder int            `gorm:"column:sort_order"`
	CreatedAt time.Time      `gorm:"column:created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (Category) TableName() string { return "categories" }

// Product memetakan tabel products (migrations/003). Harga dalam rupiah.
type Product struct {
	ID          uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	CategoryID  uint64         `gorm:"column:category_id"`
	Name        string         `gorm:"column:name"`
	Description *string        `gorm:"column:description"`
	Price       uint32         `gorm:"column:price"` // harga dasar (eceran)
	Unit        string         `gorm:"column:unit"`  // satuan jual (migrations/010), default "pcs"
	ImagePath   *string        `gorm:"column:image_path"`
	IsActive    bool           `gorm:"column:is_active"`
	CreatedBy   *uint64        `gorm:"column:created_by"`
	UpdatedBy   *uint64        `gorm:"column:updated_by"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (Product) TableName() string { return "products" }
