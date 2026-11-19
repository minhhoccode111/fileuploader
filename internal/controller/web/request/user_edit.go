package request

import (
	"errors"
	"strconv"
	"strings"
)

const (
	_requestKibi = 1 << 10
	_requestMibi = 1 << 20
	_requestGibi = 1 << 30
	_requestTibi = 1 << 40
)

// ErrInvalidByteSize is returned when a size or rate string cannot be parsed.
var ErrInvalidByteSize = errors.New("invalid byte size")

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
	Status   string `form:"status"   validate:"omitempty,oneof=pending active disabled"`
	Password string `form:"password" validate:"omitempty,min=6,max=72,password"`

	// Per-account storage and transfer limits. Zero inherits the default;
	// negative values are rejected by validation. The byte fields keep the
	// form working with a raw number; the string fields let the admin type a
	// value with a K, M or G suffix, which is parsed into bytes.
	StorageQuotaBytes       int64 `form:"storage_quota_bytes"     validate:"min=0"`
	StorageUsedBytes        int64 `form:"storage_used_bytes"      validate:"min=0"`
	UploadRateBytesPerSec   int64 `form:"upload_rate_bytes_per_sec" validate:"min=0"`
	DownloadRateBytesPerSec int64 `form:"download_rate_bytes_per_sec" validate:"min=0"`

	StorageQuota string `form:"storage_quota"`
	UploadRate   string `form:"upload_rate"`
	DownloadRate string `form:"download_rate"`
}

// Sanitize normalizes the raw form input before validation runs. The name is
// trimmed, the email is trimmed and lower-cased so it matches the stored
// address, and the role is lower-cased. The password is left untouched:
// leading and trailing spaces can be intentional characters of a password.
func (r *UserUpdate) Sanitize() {
	r.Username = strings.TrimSpace(r.Username)
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
	r.Role = strings.ToLower(strings.TrimSpace(r.Role))
	r.Status = strings.ToLower(strings.TrimSpace(r.Status))

	r.StorageQuota = strings.TrimSpace(r.StorageQuota)
	r.UploadRate = strings.TrimSpace(r.UploadRate)
	r.DownloadRate = strings.TrimSpace(r.DownloadRate)
}

// Limits returns the storage quota and transfer rates to persist, parsing any
// K/M/G suffix and falling back to the raw byte fields when the text field
// is empty.
func (r *UserUpdate) Limits() (storageQuota, uploadRate, downloadRate int64, err error) {
	storageQuota = r.StorageQuotaBytes
	if r.StorageQuota != "" {
		value, parseErr := ParseByteSize(r.StorageQuota)
		if parseErr != nil {
			return 0, 0, 0, parseErr
		}

		storageQuota = value
	}

	uploadRate = r.UploadRateBytesPerSec
	if r.UploadRate != "" {
		value, parseErr := ParseByteSize(r.UploadRate)
		if parseErr != nil {
			return 0, 0, 0, parseErr
		}

		uploadRate = value
	}

	downloadRate = r.DownloadRateBytesPerSec
	if r.DownloadRate != "" {
		value, parseErr := ParseByteSize(r.DownloadRate)
		if parseErr != nil {
			return 0, 0, 0, parseErr
		}

		downloadRate = value
	}

	return storageQuota, uploadRate, downloadRate, nil
}

// byteSizeFactor returns the number of bytes one unit represents.
func byteSizeFactor(unit string) (int64, bool) {
	factors := map[string]int64{
		"":      1,
		"b":     1,
		"byte":  1,
		"bytes": 1,
		"k":     _requestKibi,
		"kb":    _requestKibi,
		"kib":   _requestKibi,
		"m":     _requestMibi,
		"mb":    _requestMibi,
		"mib":   _requestMibi,
		"g":     _requestGibi,
		"gb":    _requestGibi,
		"gib":   _requestGibi,
		"t":     _requestTibi,
		"tb":    _requestTibi,
		"tib":   _requestTibi,
	}

	factor, ok := factors[unit]

	return factor, ok
}

// ParseByteSize turns a value such as "1.5 GB", "50M" or "1048576" into a
// byte count. A plain number is read as bytes.
func ParseByteSize(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}

	// Split off the unit, which may be glued to the number or separated by
	// spaces.
	number := value
	unit := ""

	if i := strings.IndexFunc(value, func(r rune) bool {
		return (r < '0' || r > '9') && r != '.'
	}); i >= 0 {
		number = value[:i]
		unit = value[i:]
	}

	parsed, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
	if err != nil {
		return 0, ErrInvalidByteSize
	}

	factor, ok := byteSizeFactor(strings.ToLower(strings.TrimSpace(unit)))
	if !ok {
		return 0, ErrInvalidByteSize
	}

	total := parsed * float64(factor)
	if total < 0 || total > 9.223372036854776e18 {
		return 0, ErrInvalidByteSize
	}

	return int64(total), nil
}
