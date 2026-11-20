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

// UserStatus is the lifecycle state of an account. New accounts start pending
// and only an administrator moves them to active; an active account can be
// disabled and re-enabled later.
type UserStatus string

const (
	// UserStatusPending is a registration that has not been approved yet.
	UserStatusPending UserStatus = "pending"
	// UserStatusActive may sign in and use the app.
	UserStatusActive UserStatus = "active"
	// UserStatusDisabled may not sign in; its live sessions are ended.
	UserStatusDisabled UserStatus = "disabled"
)

// IsValid reports whether the status is one the domain knows.
func (s UserStatus) IsValid() bool {
	switch s {
	case UserStatusPending, UserStatusActive, UserStatusDisabled:
		return true
	default:
		return false
	}
}

// CanSignIn reports whether an account in this state may sign in. Only active
// accounts may; an unknown or unset status is denied, so the zero value is
// fail-closed.
func (s UserStatus) CanSignIn() bool {
	return s == UserStatusActive
}

// UserSortField names a column the account list can be ordered by. The set is
// closed: every field maps to a column of the users table, so a hand-written
// query never reaches the SQL.
type UserSortField string

const (
	// UserSortUsername orders the list by the username.
	UserSortUsername UserSortField = "username"
	// UserSortEmail orders the list by the email.
	UserSortEmail UserSortField = "email"
	// UserSortRole orders the list by the role.
	UserSortRole UserSortField = "role"
	// UserSortCreatedAt orders the list by the day the account was created.
	UserSortCreatedAt UserSortField = "created_at"
	// UserSortUpdatedAt orders the list by the day the account last changed.
	UserSortUpdatedAt UserSortField = "updated_at"
)

// IsValid reports whether the column is one the list can be ordered by.
func (f UserSortField) IsValid() bool {
	switch f {
	case UserSortUsername, UserSortEmail, UserSortRole, UserSortCreatedAt, UserSortUpdatedAt:
		return true
	default:
		return false
	}
}

// UserSortDirection is one of the two orders a column can take.
type UserSortDirection string

const (
	// UserSortAscending orders the smallest value first.
	UserSortAscending UserSortDirection = "asc"
	// UserSortDescending orders the largest value first.
	UserSortDescending UserSortDirection = "desc"
)

// IsValid reports whether the direction is one the list can be ordered by.
func (d UserSortDirection) IsValid() bool {
	switch d {
	case UserSortAscending, UserSortDescending:
		return true
	default:
		return false
	}
}

// UserListItem is the read model of an account in a list. It carries the ID
// the edit drawer opens an account by, and no password hash, so the hash never
// leaves the database.
type UserListItem struct {
	ID                      string
	Username                string
	Email                   string
	Role                    UserRole
	Status                  UserStatus
	StorageQuotaBytes       int64
	StorageUsedBytes        int64
	UploadRateBytesPerSec   int64
	DownloadRateBytesPerSec int64
	CreatedAt               time.Time
	UpdatedAt               time.Time
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
	Query  string            // matches the username or the email
	Role   UserRole          // empty means every role
	Status UserStatus        // empty means every status
	On     *time.Time        // the day an account was created or last updated on
	Sort   UserSortField     // the column the list is ordered by
	Dir    UserSortDirection // the direction of the order
	Limit  int               // rows on one page
	Page   int               // the page number, starting at 1
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

	if !f.Status.IsValid() {
		f.Status = ""
	}

	if f.On != nil && f.On.IsZero() {
		f.On = nil
	}

	if !f.Sort.IsValid() {
		f.Sort = UserSortCreatedAt
	}

	if !f.Dir.IsValid() {
		f.Dir = UserSortDescending
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
	ID                      string     `json:"id"         example:"550e8400-e29b-41d4-a716-446655440000"`
	Username                string     `json:"username"   example:"johndoe"`
	Email                   string     `json:"email"      example:"john@example.com"`
	PasswordHash            string     `json:"-"`
	Role                    UserRole   `json:"role"                      example:"user"`
	Status                  UserStatus `json:"status"                    example:"active"`
	StorageQuotaBytes       int64      `json:"storage_quota_bytes"        example:"0"`
	StorageUsedBytes        int64      `json:"storage_used_bytes"         example:"0"`
	UploadRateBytesPerSec   int64      `json:"upload_rate_bytes_per_sec"  example:"0"`
	DownloadRateBytesPerSec int64      `json:"download_rate_bytes_per_sec" example:"0"`
	CreatedAt               time.Time  `json:"created_at"                example:"2026-01-01T00:00:00Z"`
	UpdatedAt               time.Time  `json:"updated_at"                example:"2026-01-01T00:00:00Z"`
}

// EffectiveStorageQuota resolves a per-user override against the global default.
// A zero stored value means "inherit the default".
//
//nolint:gocritic // value receiver keeps the method callable on composite literals
func (u User) EffectiveStorageQuota(defaultQuota int64) int64 {
	if u.StorageQuotaBytes != 0 {
		return u.StorageQuotaBytes
	}

	return defaultQuota
}

// EffectiveUploadRate resolves a per-user override against the global default.
//
//nolint:gocritic // value receiver keeps the method callable on composite literals
func (u User) EffectiveUploadRate(defaultRate int64) int64 {
	if u.UploadRateBytesPerSec != 0 {
		return u.UploadRateBytesPerSec
	}

	return defaultRate
}

// EffectiveDownloadRate resolves a per-user override against the global default.
//
//nolint:gocritic // value receiver keeps the method callable on composite literals
func (u User) EffectiveDownloadRate(defaultRate int64) int64 {
	if u.DownloadRateBytesPerSec != 0 {
		return u.DownloadRateBytesPerSec
	}

	return defaultRate
}
