// Package request holds the form payloads the web controllers decode.
package request

import "strings"

// Login is the login form payload.
type Login struct {
	Email    string `form:"email"    validate:"required,email,max=255"`
	Password string `form:"password" validate:"required,max=255"`
}

// Sanitize normalizes the raw form input before validation runs. The email is
// trimmed and lower-cased so "  User@Example.COM  " matches the stored address
// and passes the "email" rule. The password is left untouched: leading and
// trailing spaces can be intentional characters of a password.
func (r *Login) Sanitize() {
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
}
