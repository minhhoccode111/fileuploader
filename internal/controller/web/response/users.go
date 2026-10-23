package response

import (
	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// _joinedLayout is the date and time the account list prints for an
// account's creation and last update.
const _joinedLayout = "2006-01-02 15:04"

// _userPagerFirstPage is the page number the account list starts at.
const _userPagerFirstPage = 1

// UserRow is one account in the admin user table. It is a presentation type: the
// admin flag and the joined date are already what the template prints.
type UserRow struct {
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

// NewUserRow maps the read model to the row the account table renders.
func NewUserRow(item *entity.UserListItem) UserRow {
	return UserRow{
		Username: item.Username,
		Email:    item.Email,
		IsAdmin:  item.Role == entity.UserRoleAdmin,
		Joined:   item.CreatedAt.Format(_joinedLayout),
		Updated:  item.UpdatedAt.Format(_joinedLayout),
	}
}

// NewUserPager maps the list result and the filter it was fetched with to the
// pager the account table renders. A filter is expected to be clamped, and a
// page that fell off the end of the list, because a filter shrank it under
// the visitor, is pulled back to the last page there is.
func NewUserPager(list entity.UserList, filter entity.UserFilter) UserPager {
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
	Limit  string
	Page   int
	Active bool
}
