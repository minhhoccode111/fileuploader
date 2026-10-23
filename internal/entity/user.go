// Package entity defines main entities for business logic (services), data base mapping and
// HTTP response objects if suitable. Each logic group entities in own file.
package entity

import (
	"strings"
	"time"
)

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

// The bounds of the account list window. The pager offers these page sizes in
// its dropdown, and the filter is clamped to them before it reaches a
// repository.
const (
	_minUsersPageSize     = 5
	_maxUsersPageSize     = 50
	_defaultUsersPageSize = 10
	_firstUsersPage       = 1
)

// UserFilter narrows the account list to what the admin asked for. An empty
// role and a nil day mean "any", and Sanitize clamps the rest before the
// filter reaches a repository.
type UserFilter struct {
	Query string     // matches the username or the email
	Role  UserRole   // empty means every role
	On    *time.Time // the day an account was created or last updated on
	Limit int        // rows on one page
	Page  int        // the page number, starting at 1
}

// UserList is one page of the account list plus the number of accounts that
// match the filter in total, which is what the pager labels its window with.
type UserList struct {
	Items []UserListItem
	Total int
}

// Sanitize clamps the filter to the bounds the account list serves. The query
// is trimmed, a role the domain does not know drops out, and the page window
// falls back to the defaults the pager offers.
func (f *UserFilter) Sanitize() {
	f.Query = strings.TrimSpace(f.Query)

	if !f.Role.IsValid() {
		f.Role = ""
	}

	if f.On != nil && f.On.IsZero() {
		f.On = nil
	}

	if f.Limit < _minUsersPageSize || f.Limit > _maxUsersPageSize {
		f.Limit = _defaultUsersPageSize
	}

	if f.Page < _firstUsersPage {
		f.Page = _firstUsersPage
	}
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
