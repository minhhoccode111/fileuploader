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

// UserRow is one account in the admin user table. It is a presentation type: the
// admin flag and the joined date are already what the template prints.
type UserRow struct {
	ID       string
	Username string
	Email    string
	IsAdmin  bool
	Joined   string
	Updated  string
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

// NewUserRow maps the read model to the row the account table renders.
func NewUserRow(item *entity.UserListItem) UserRow {
	return UserRow{
		ID:       item.ID,
		Username: item.Username,
		Email:    item.Email,
		IsAdmin:  item.Role == entity.UserRoleAdmin,
		Joined:   item.CreatedAt.Format(_joinedLayout),
		Updated:  item.UpdatedAt.Format(_joinedLayout),
	}
}

// NewUserRowFromUser maps the account behind an edit or an update to the row
// shape the account table and the editor drawer render.
func NewUserRowFromUser(user *entity.User) UserRow {
	return UserRow{
		ID:       user.ID,
		Username: user.Username,
		Email:    user.Email,
		IsAdmin:  user.Role == entity.UserRoleAdmin,
		Joined:   user.CreatedAt.Format(_joinedLayout),
		Updated:  user.UpdatedAt.Format(_joinedLayout),
	}
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
