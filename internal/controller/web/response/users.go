package response

import (
	"net/url"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// _joinedLayout is the date and time the account list prints for an
// account's creation and last update.
const _joinedLayout = "2006-01-02 15:04"

// _userPagerFirstPage is the page number the account list starts at.
const _userPagerFirstPage = 1

// _userFiltersAllRoles is the role dropdown entry that keeps every role.
const _userFiltersAllRoles = "all"

// UserDefaults are the environment defaults an account inherits from when
// one of its limits is stored as zero. The account list and the editor drawer
// word every inheriting value against them, so an admin sees the limit that
// actually applies instead of a zero.
type UserDefaults struct {
	StorageQuotaBytes       int64
	UploadRateBytesPerSec   int64
	DownloadRateBytesPerSec int64
}

// UserRow is one account in the admin user table. It is a presentation type: the
// admin flag and the joined date are already what the template prints.
type UserRow struct {
	ID       string
	Username string
	Email    string
	IsAdmin  bool
	Status   entity.UserStatus
	Joined   string
	Updated  string

	// Defaults are the environment limits a stored zero inherits. They stay
	// zero when a caller has none, which keeps every inheriting value
	// worded as Unlimited.
	Defaults UserDefaults

	// Per-account storage and transfer limits. The canonical fields mirror the
	// entity; the aliases keep the row usable by callers that name them
	// differently. They hold the stored values, so the editor form shows what
	// the account carries, not what it inherits.
	StorageQuotaBytes       int64
	StorageUsedBytes        int64
	UploadRateBytesPerSec   int64
	DownloadRateBytesPerSec int64

	StorageQuota int64
	StorageUsed  int64
	Quota        int64
	Used         int64
	UploadRate   int64
	Upload       int64
	DownloadRate int64
	Download     int64
}

// StorageQuotaValue returns the quota the row carries, preferring the canonical
// field and falling back to any alias that was set.
//
//nolint:gocritic // value receiver matches the rest of the presentation type
func (r UserRow) StorageQuotaValue() int64 {
	if r.StorageQuotaBytes != 0 {
		return r.StorageQuotaBytes
	}

	if r.StorageQuota != 0 {
		return r.StorageQuota
	}

	return r.Quota
}

// StorageUsedValue returns the used bytes the row carries.
//
//nolint:gocritic // value receiver matches the rest of the presentation type
func (r UserRow) StorageUsedValue() int64 {
	if r.StorageUsedBytes != 0 {
		return r.StorageUsedBytes
	}

	if r.StorageUsed != 0 {
		return r.StorageUsed
	}

	return r.Used
}

// EffectiveStorageQuota returns the quota the account actually stores
// against: its own override, or the environment default a zero inherits.
//
//nolint:gocritic // value receiver matches the rest of the presentation type
func (r UserRow) EffectiveStorageQuota() int64 {
	if quota := r.StorageQuotaValue(); quota != 0 {
		return quota
	}

	return r.Defaults.StorageQuotaBytes
}

// EffectiveUploadRate returns the upload rate the account actually runs at.
//
//nolint:gocritic // value receiver matches the rest of the presentation type
func (r UserRow) EffectiveUploadRate() int64 {
	if rate := r.UploadRateValue(); rate != 0 {
		return rate
	}

	return r.Defaults.UploadRateBytesPerSec
}

// EffectiveDownloadRate returns the download rate the account actually runs at.
//
//nolint:gocritic // value receiver matches the rest of the presentation type
func (r UserRow) EffectiveDownloadRate() int64 {
	if rate := r.DownloadRateValue(); rate != 0 {
		return rate
	}

	return r.Defaults.DownloadRateBytesPerSec
}

// EffectiveStorageQuotaLabel words the quota an inheriting account runs
// under, for the note under the editor's quota field.
//
//nolint:gocritic // value receiver matches the rest of the presentation type
func (r UserRow) EffectiveStorageQuotaLabel() string {
	return limitLabel(r.EffectiveStorageQuota())
}

// EffectiveUploadRateLabel words the upload rate an inheriting account runs
// at, for the note under the editor's upload field.
//
//nolint:gocritic // value receiver matches the rest of the presentation type
func (r UserRow) EffectiveUploadRateLabel() string {
	return rateLabel(r.EffectiveUploadRate())
}

// EffectiveDownloadRateLabel words the download rate an inheriting account
// runs at, for the note under the editor's download field.
//
//nolint:gocritic // value receiver matches the rest of the presentation type
func (r UserRow) EffectiveDownloadRateLabel() string {
	return rateLabel(r.EffectiveDownloadRate())
}

// StorageLabel words the used bytes against the quota that actually applies.
// A stored zero inherits the environment default, so the label prints the
// default — and calls it Unlimited only when the default is unlimited too.
//
//nolint:gocritic // value receiver matches the rest of the presentation type
func (r UserRow) StorageLabel() string {
	used := ByteLabel(r.StorageUsedValue())

	quota := r.EffectiveStorageQuota()
	if quota <= 0 {
		return used + " / Unlimited"
	}

	return used + " / " + ByteLabel(quota)
}

// UploadRateValue returns the upload rate the row carries.
//
//nolint:gocritic // value receiver matches the rest of the presentation type
func (r UserRow) UploadRateValue() int64 {
	if r.UploadRateBytesPerSec != 0 {
		return r.UploadRateBytesPerSec
	}

	if r.UploadRate != 0 {
		return r.UploadRate
	}

	return r.Upload
}

// DownloadRateValue returns the download rate the row carries.
//
//nolint:gocritic // value receiver matches the rest of the presentation type
func (r UserRow) DownloadRateValue() int64 {
	if r.DownloadRateBytesPerSec != 0 {
		return r.DownloadRateBytesPerSec
	}

	if r.DownloadRate != 0 {
		return r.DownloadRate
	}

	return r.Download
}

// UserPager is the pagination state of the account table: which window is on
// the screen, how many windows the filter has, and the neighbors the pager
// can walk to. A zero neighbor means the edge of the list is reached.
type UserPager struct {
	Page  int
	Pages int
	Total int
	Prev  int
	Next  int
}

// UserSort is one clickable column title of the account table: the column it
// orders by, the direction its click asks for, the address the click leads to,
// and whether the table is already ordered by it.
type UserSort struct {
	Field         string
	NextDirection string
	Query         string
	Active        bool
	Ascending     bool
}

// Values is what the click sends besides the filters the form already holds.
func (s UserSort) Values() map[string]string {
	return map[string]string{"sort": s.Field, "dir": s.NextDirection}
}

// AriaSort is the state the table header announces to a screen reader.
func (s UserSort) AriaSort() string {
	if !s.Active {
		return "none"
	}

	if s.Ascending {
		return "ascending"
	}

	return "descending"
}

// NewUserRow maps the read model to the row the account table renders. The
// defaults are the environment limits a stored zero inherits, so the row can
// word every inheriting value against them.
//
//nolint:dupl // mirrors NewUserRowFromUser by design
func NewUserRow(item *entity.UserListItem, defaults UserDefaults) UserRow {
	row := UserRow{
		ID:       item.ID,
		Username: item.Username,
		Email:    item.Email,
		IsAdmin:  item.Role == entity.UserRoleAdmin,
		Status:   item.Status,
		Joined:   item.CreatedAt.Format(_joinedLayout),
		Updated:  item.UpdatedAt.Format(_joinedLayout),

		Defaults: defaults,

		StorageQuotaBytes:       item.StorageQuotaBytes,
		StorageUsedBytes:        item.StorageUsedBytes,
		UploadRateBytesPerSec:   item.UploadRateBytesPerSec,
		DownloadRateBytesPerSec: item.DownloadRateBytesPerSec,
	}
	row.StorageQuota = row.StorageQuotaBytes
	row.Quota = row.StorageQuotaBytes
	row.StorageUsed = row.StorageUsedBytes
	row.Used = row.StorageUsedBytes
	row.UploadRate = row.UploadRateBytesPerSec
	row.Upload = row.UploadRateBytesPerSec
	row.DownloadRate = row.DownloadRateBytesPerSec
	row.Download = row.DownloadRateBytesPerSec

	return row
}

// NewUserRowFromUser maps the account behind an edit or an update to the row
// shape the account table and the editor drawer render. The defaults are the
// environment limits a stored zero inherits, so the drawer's notes can name
// the limit that actually applies.
//
//nolint:dupl // mirrors NewUserRow by design
func NewUserRowFromUser(user *entity.User, defaults UserDefaults) UserRow {
	row := UserRow{
		ID:       user.ID,
		Username: user.Username,
		Email:    user.Email,
		IsAdmin:  user.Role == entity.UserRoleAdmin,
		Status:   user.Status,
		Joined:   user.CreatedAt.Format(_joinedLayout),
		Updated:  user.UpdatedAt.Format(_joinedLayout),

		Defaults: defaults,

		StorageQuotaBytes:       user.StorageQuotaBytes,
		StorageUsedBytes:        user.StorageUsedBytes,
		UploadRateBytesPerSec:   user.UploadRateBytesPerSec,
		DownloadRateBytesPerSec: user.DownloadRateBytesPerSec,
	}
	row.StorageQuota = row.StorageQuotaBytes
	row.Quota = row.StorageQuotaBytes
	row.StorageUsed = row.StorageUsedBytes
	row.Used = row.StorageUsedBytes
	row.UploadRate = row.UploadRateBytesPerSec
	row.Upload = row.UploadRateBytesPerSec
	row.DownloadRate = row.DownloadRateBytesPerSec
	row.Download = row.DownloadRateBytesPerSec

	return row
}

// NewUserPager maps the list result and the filter it was fetched with to the
// pager the account table renders. A filter is expected to be clamped, and a
// page that fell off the end of the list, because a filter shrank it under
// the visitor, is pulled back to the last page there is.
func NewUserPager(list entity.UserList, filter *entity.UserFilter) UserPager {
	pages := list.Total / filter.Limit
	if list.Total%filter.Limit != 0 {
		pages++
	}

	if pages == 0 {
		pages = _userPagerFirstPage
	}

	page := min(max(filter.Page, _userPagerFirstPage), pages)

	pager := UserPager{Page: page, Pages: pages, Total: list.Total}

	if page > _userPagerFirstPage {
		pager.Prev = page - 1
	}

	if page < pages {
		pager.Next = page + 1
	}

	return pager
}

// UserFilters is the state of the search bar of the account list: what the
// visitor asked for, so the inputs, the clear button and the rows-per-page
// selector come back the way they were left. The page rides along too, so a
// bookmarked window of the list opens on the same window.
type UserFilters struct {
	Query  string
	Role   string
	Status string
	Date   string
	Sort   string
	Dir    string
	Limit  string
	Page   int
	Active bool
}

// SortColumn is the state of one column title. A click on the column the table
// is ordered by turns the order around; a click on another column starts it
// ascending. The address the click falls back to holds the filters the table
// holds and the order the click asks for, so the view can be bookmarked even
// without htmx.
func (f *UserFilters) SortColumn(field string) UserSort {
	next := string(entity.UserSortAscending)

	if f.Sort == field {
		if f.Dir == string(entity.UserSortAscending) {
			next = string(entity.UserSortDescending)
		}
	}

	values := url.Values{}

	if f.Query != "" {
		values.Set("q", f.Query)
	}

	if f.Role != "" && f.Role != _userFiltersAllRoles {
		values.Set("role", f.Role)
	}

	if f.Status != "" && f.Status != "all" {
		values.Set("status", f.Status)
	}

	if f.Date != "" {
		values.Set("date", f.Date)
	}

	if f.Limit != "" {
		values.Set("limit", f.Limit)
	}

	values.Set("sort", field)
	values.Set("dir", next)

	return UserSort{
		Field:         field,
		NextDirection: next,
		Query:         values.Encode(),
		Active:        f.Sort == field,
		Ascending:     f.Dir == string(entity.UserSortAscending),
	}
}
