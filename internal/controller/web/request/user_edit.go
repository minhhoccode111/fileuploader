package request

import "strings"

// UserEditQuery is the query string of the account editor request: the
// account the drawer opened for.
type UserEditQuery struct {
	ID string `query:"id" validate:"required,uuid"`
}

// UserUpdate is the edit form payload. The password is optional: an empty
// password keeps the one the account already holds. The name, email and role
// caps match the columns in the users table and the rules Register applies;
// the password cap is bcrypt's 72-byte input limit, enforced in the use case.
type UserUpdate struct {
	ID       string `form:"id"       validate:"required,uuid"`
	Username string `form:"username" validate:"required,username,min=3,max=255"`
	Email    string `form:"email"    validate:"required,email,max=255"`
	Role     string `form:"role"     validate:"required,oneof=admin user"`
	Password string `form:"password" validate:"omitempty,min=6,max=72,password"`

	// Per-account storage and transfer limits. Zero inherits the default;
	// negative values are rejected by validation.
	StorageQuotaBytes       int64 `form:"storage_quota_bytes"     validate:"min=0"`
	StorageUsedBytes        int64 `form:"storage_used_bytes"      validate:"min=0"`
	UploadRateBytesPerSec   int64 `form:"upload_rate_bytes_per_sec" validate:"min=0"`
	DownloadRateBytesPerSec int64 `form:"download_rate_bytes_per_sec" validate:"min=0"`
}

// Sanitize normalizes the raw form input before validation runs. The name is
// trimmed, the email is trimmed and lower-cased so it matches the stored
// address, and the role is lower-cased. The password is left untouched:
// leading and trailing spaces can be intentional characters of a password.
func (r *UserUpdate) Sanitize() {
	r.Username = strings.TrimSpace(r.Username)
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
	r.Role = strings.ToLower(strings.TrimSpace(r.Role))
}
