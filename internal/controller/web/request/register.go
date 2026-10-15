package request

import "strings"

// Register is the registration form payload. The username and email caps match
// the columns in the users table (both VARCHAR(255)); the password cap is
// bcrypt's 72-byte input limit, enforced in the use case.
type Register struct {
	Username string `form:"username" validate:"required,username,min=3,max=255"`
	Email    string `form:"email"    validate:"required,email,max=255"`
	Password string `form:"password" validate:"required,min=6,max=72,password"`
}

// Sanitize normalizes the raw form input before validation runs. The username is
// trimmed and the email is trimmed and lower-cased, so "  User@Example.COM  "
// matches the stored address. The password is left untouched: leading and
// trailing spaces can be intentional characters of a password.
func (r *Register) Sanitize() {
	r.Username = strings.TrimSpace(r.Username)
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
}
