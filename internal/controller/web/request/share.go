package request

import "strings"

// ShareCreate is the drawer form that creates a public link to a folder.
type ShareCreate struct {
	FolderID  string `form:"folder"     validate:"required,uuid4"`
	ExpiresAt string `form:"expires_at"`
	Unlimited string `form:"unlimited"`
}

// Sanitize trims the fields before validation runs.
func (r *ShareCreate) Sanitize() {
	r.FolderID = strings.TrimSpace(r.FolderID)
	r.ExpiresAt = strings.TrimSpace(r.ExpiresAt)
	r.Unlimited = strings.TrimSpace(r.Unlimited)
}

// ShareUpdateExpiry is the form that changes when a share stops being valid.
type ShareUpdateExpiry struct {
	ExpiresAt string `form:"expires_at"`
	Unlimited string `form:"unlimited"`
}

// Sanitize trims the fields before validation runs.
func (r *ShareUpdateExpiry) Sanitize() {
	r.ExpiresAt = strings.TrimSpace(r.ExpiresAt)
	r.Unlimited = strings.TrimSpace(r.Unlimited)
}
