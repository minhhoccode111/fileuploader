package request

import "strings"

// Role is the role-change form payload. The role must be one the domain
// knows, which is also what the CHECK constraint on the users table allows.
type Role struct {
	Email string `form:"email" validate:"required,email,max=255"`
	Role  string `form:"role"  validate:"required,oneof=admin user"`
}

// Sanitize normalizes the raw form input before validation runs. The email is
// trimmed and lower-cased so it matches the stored address, and the role is
// lower-cased so "Admin" is accepted too.
func (r *Role) Sanitize() {
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
	r.Role = strings.ToLower(strings.TrimSpace(r.Role))
}
