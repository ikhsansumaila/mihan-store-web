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
	PasswordHash      string         `gorm:"column:password_hash"`
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
}

func toDTO(u *User) *UserDTO {
	return &UserDTO{ID: u.PublicID, Username: u.Username, Email: u.Email, Phone: u.Phone, Name: u.Name, Role: u.Role}
}
