package entity

import "time"

// Share is one public link to a folder of an account. A nil expiry means the
// link does not expire. The identifier is the token a visitor uses in the URL,
// so it is never derived from the folder it points at.
type Share struct {
	ID        string
	UserID    string
	FolderID  string
	ExpiresAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsExpired reports whether the share has passed its expiry. A share without
// an expiry never expires.
func (s *Share) IsExpired(now time.Time) bool {
	if s.ExpiresAt == nil {
		return false
	}

	return !s.ExpiresAt.After(now)
}
