// Package entity defines main entities for business logic (services), data base mapping and
// HTTP response objects if suitable. Each logic group entities in own file.
package entity

import "time"

// UserRole is the role of an account. The users table constrains the column to
// these values with a CHECK constraint, so the database rejects anything else.
type UserRole string

const (
	// UserRoleAdmin may act on other people's data.
	UserRoleAdmin UserRole = "admin"
	// UserRoleUser is the role every registered account gets.
	UserRoleUser UserRole = "user"
)

// IsValid reports whether the role is one the domain knows.
func (r UserRole) IsValid() bool {
	switch r {
	case UserRoleAdmin, UserRoleUser:
		return true
	default:
		return false
	}
}

// UserListItem is the read model of an account in a list. It carries no
// password hash, so the hash never leaves the database.
type UserListItem struct {
	Username  string
	Email     string
	Role      UserRole
	CreatedAt time.Time
	UpdatedAt time.Time
}

// User -.
type User struct {
	ID           string    `json:"id"         example:"550e8400-e29b-41d4-a716-446655440000"`
	Username     string    `json:"username"   example:"johndoe"`
	Email        string    `json:"email"      example:"john@example.com"`
	PasswordHash string    `json:"-"`
	Role         UserRole  `json:"role"       example:"user"`
	CreatedAt    time.Time `json:"created_at" example:"2026-01-01T00:00:00Z"`
	UpdatedAt    time.Time `json:"updated_at" example:"2026-01-01T00:00:00Z"`
}
