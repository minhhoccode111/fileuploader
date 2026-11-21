package request

import "strings"

// ProfileUpdate is the account page's profile form payload. The caps match the
// users table and the rules Register applies, so an account cannot be edited
// into a shape the registration form would refuse.
type ProfileUpdate struct {
	Username string `form:"username" validate:"required,username,min=3,max=255"`
	Email    string `form:"email"    validate:"required,email,max=255"`
}

// Sanitize normalizes the raw form input before validation runs. The username
// is trimmed, and the email is trimmed and lower-cased so it matches the
// stored address.
func (r *ProfileUpdate) Sanitize() {
	r.Username = strings.TrimSpace(r.Username)
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
}

// PasswordUpdate is the account page's password form payload. The current
// password is checked against the stored hash, so knowing the session alone
// is not enough to replace it. The new password rules match Register; the
// current one is only capped, because an older password must still pass.
type PasswordUpdate struct {
	CurrentPassword string `form:"current_password" validate:"required,max=72"`
	NewPassword     string `form:"new_password"     validate:"required,min=6,max=72,password"`
	ConfirmPassword string `form:"confirm_password" validate:"required,max=72"`
}

// Matches reports whether the new password was typed the same way twice.
func (r *PasswordUpdate) Matches() bool {
	return r.NewPassword == r.ConfirmPassword
}
