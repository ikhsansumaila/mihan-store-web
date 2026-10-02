package main

import (
	"time"

	"gorm.io/gorm"
)

// User memetakan tabel users (skema: migrations/001_users_sessions.sql).
// Kolom `alive` sengaja TIDAK dipetakan: dihitung otomatis oleh MySQL dan tidak boleh ditulis.
type User struct {
	ID                uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	PublicID          string         `gorm:"column:public_id"`
	Username          string         `gorm:"column:username"`
	Email             string         `gorm:"column:email"`
	Phone             *string        `gorm:"column:phone"`
	Name              string         `gorm:"column:name"`
	PasswordHash      *string        `gorm:"column:password_hash"` // NULL = akun tanpa password (Google)
	GoogleSub         *string        `gorm:"column:google_sub"`
	AvatarURL         *string        `gorm:"column:avatar_url"`
	Role              string         `gorm:"column:role;default:customer"`
	Status            string         `gorm:"column:status;default:active"`
	EmailVerifiedAt   *time.Time     `gorm:"column:email_verified_at"`
	FailedLogins      uint8          `gorm:"column:failed_logins"`
	LockedUntil       *time.Time     `gorm:"column:locked_until"`
	LastLoginAt       *time.Time     `gorm:"column:last_login_at"`
	PasswordChangedAt *time.Time     `gorm:"column:password_changed_at"`
	CreatedAt         time.Time      `gorm:"column:created_at"`
	UpdatedAt         time.Time      `gorm:"column:updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (User) TableName() string { return "users" }

type Session struct {
	ID        uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    uint64         `gorm:"column:user_id"`
	TokenHash string         `gorm:"column:token_hash"`
	UserAgent *string        `gorm:"column:user_agent"`
	IP        []byte         `gorm:"column:ip"`
	ExpiresAt time.Time      `gorm:"column:expires_at"`
	RevokedAt *time.Time     `gorm:"column:revoked_at"`
	CreatedAt time.Time      `gorm:"column:created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (Session) TableName() string { return "sessions" }

// UserDTO adalah data user ringkas yang dikirim ke klien (tanpa id internal / hash).
type UserDTO struct {
	ID       string  `json:"id"`
	Username string  `json:"username"`
	Email    string  `json:"email"`
	Phone    *string `json:"phone"`
	Name     string  `json:"name"`
	Role     string  `json:"role"`
	Avatar   *string `json:"avatarUrl,omitempty"`
}

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
	Price       uint32         `gorm:"column:price"`
	ImagePath   *string        `gorm:"column:image_path"`
	IsActive    bool           `gorm:"column:is_active"`
	CreatedBy   *uint64        `gorm:"column:created_by"`
	UpdatedBy   *uint64        `gorm:"column:updated_by"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (Product) TableName() string { return "products" }

// ActivityLog memetakan tabel activity_logs (migrations/004). Aplikasi hanya INSERT/SELECT.
type ActivityLog struct {
	ID         uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	UserID     *uint64   `gorm:"column:user_id"`
	ActorLabel *string   `gorm:"column:actor_label"`
	Action     string    `gorm:"column:action"`
	EntityType *string   `gorm:"column:entity_type"`
	EntityID   *string   `gorm:"column:entity_id"`
	Summary    string    `gorm:"column:summary"`
	Details    *string   `gorm:"column:details"` // JSON (string: []byte ditolak MySQL untuk kolom JSON)
	IP         []byte    `gorm:"column:ip"`
	UserAgent  *string   `gorm:"column:user_agent"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (ActivityLog) TableName() string { return "activity_logs" }

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toDTO(u *User) *UserDTO {
	return &UserDTO{ID: u.PublicID, Username: u.Username, Email: u.Email, Phone: u.Phone, Name: u.Name, Role: u.Role, Avatar: u.AvatarURL}
}
