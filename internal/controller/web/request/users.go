package request

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

const (
	// _userListDateLayout is the format the day filter arrives in.
	_userListDateLayout = "2006-01-02"

	// _userListAllRoles is the role dropdown entry that keeps every role.
	_userListAllRoles = "all"

	// _userListDefaultPage is the page the list starts at. It must stay in
	// step with the page the domain clamps to, entity's _firstUsersPage.
	_userListDefaultPage = 1

	// _userListDefaultLimit is the page size served when the query carries
	// none. It must stay in step with the default the domain clamps to,
	// entity's _defaultUsersPageSize.
	_userListDefaultLimit = 10
)

// UserListQuery is the query string of the account table: what to search for,
// which role and day to keep, which column to order by, and which window of
// the list to show. The page size must be one the pager offers, and the order
// must be one the table can toggle, which is also what keeps a hand-crafted
// URL from asking for the whole table in one page.
type UserListQuery struct {
	Query  string `query:"q"     validate:"max=100"`
	Role   string `query:"role"   validate:"omitempty,oneof=all admin user"`
	Status string `query:"status" validate:"omitempty,oneof=all pending active disabled"`
	Date   string `query:"date"  validate:"omitempty,datetime=2006-01-02"`
	Sort   string `query:"sort"  validate:"omitempty,oneof=username email role created_at updated_at"`
	Dir    string `query:"dir"   validate:"omitempty,oneof=asc desc"`
	Limit  int    `query:"limit" validate:"omitempty,oneof=5 10 20 50"`
	Page   int    `query:"page"  validate:"omitempty,min=1"`
}

// Sanitize normalizes the raw query input before validation runs. The text
// fields are trimmed, and the role, the column and the direction are
// lower-cased, so "Admin" and "ASC" are accepted too.
func (q *UserListQuery) Sanitize() {
	q.Query = strings.TrimSpace(q.Query)
	q.Role = strings.ToLower(strings.TrimSpace(q.Role))
	q.Status = strings.ToLower(strings.TrimSpace(q.Status))
	q.Sort = strings.ToLower(strings.TrimSpace(q.Sort))
	q.Dir = strings.ToLower(strings.TrimSpace(q.Dir))
}

// SortField is the column the table is ordered by. An unknown column falls
// back to the default order, which is what a page opened with a hand-written
// URL renders before the table itself validates the query.
func (q *UserListQuery) SortField() entity.UserSortField {
	field := entity.UserSortField(q.Sort)
	if !field.IsValid() {
		return entity.UserSortCreatedAt
	}

	return field
}

// SortDirection is the direction of the order. An unknown direction falls back
// to the default direction, for the same reason SortField falls back.
func (q *UserListQuery) SortDirection() entity.UserSortDirection {
	direction := entity.UserSortDirection(q.Dir)
	if !direction.IsValid() {
		return entity.UserSortDescending
	}

	return direction
}

// Filter maps the query to the filter the use case applies. The role dropdown
// entry that keeps every role maps to an empty role, which is the "any" the
// domain knows. The day is parsed here, because the database filter needs a
// real date and not the text the browser sent; an unparsable day never
// reaches this point, the validation catches it first. The filter comes back
// clamped by the domain's own Sanitize, so every consumer of the request —
// the list, the pager, the address bar — sees the same page bounds.
func (q *UserListQuery) Filter() entity.UserFilter {
	filter := entity.UserFilter{
		Query: q.Query,
		Sort:  q.SortField(),
		Dir:   q.SortDirection(),
		Limit: q.Limit,
		Page:  q.Page,
	}

	if q.Role != "" && q.Role != _userListAllRoles {
		filter.Role = entity.UserRole(q.Role)
	}

	if q.Status != "" && q.Status != "all" {
		filter.Status = entity.UserStatus(q.Status)
	}

	if q.Date != "" {
		on, err := time.Parse(_userListDateLayout, q.Date)
		if err == nil {
			filter.On = &on
		}
	}

	filter.Sanitize()

	return filter
}

// PageSize is the page size the query asks for, with the fallback the rows
// selector shows when the request carried none. It must stay in step with
// the default the domain clamps to, entity's _defaultUsersPageSize.
func (q *UserListQuery) PageSize() int {
	if q.Limit == 0 {
		return _userListDefaultLimit
	}

	return q.Limit
}

// HasFilters reports whether the query narrows the list at all, which is how
// the empty row between "no accounts yet" and "nothing matches" is chosen.
func (q *UserListQuery) HasFilters() bool {
	return q.Query != "" || (q.Role != "" && q.Role != _userListAllRoles) || (q.Status != "" && q.Status != "all") || q.Date != ""
}

// BookmarkQuery renders the narrowing parts of the query as the query string
// of the page path the address bar holds, so a view can be bookmarked and
// come back the same. The parts that carry no narrowing are dropped, and the
// page window rides along once the visitor leaves the first page.
//
//nolint:gocyclo,cyclop // many independent optional filters
func (q *UserListQuery) BookmarkQuery() url.Values {
	values := url.Values{}

	if q.Query != "" {
		values.Set("q", q.Query)
	}

	if q.Role != "" && q.Role != _userListAllRoles {
		values.Set("role", q.Role)
	}

	if q.Status != "" && q.Status != "all" {
		values.Set("status", q.Status)
	}

	if q.Date != "" {
		values.Set("date", q.Date)
	}

	if q.SortField() != entity.UserSortCreatedAt || q.SortDirection() != entity.UserSortDescending {
		values.Set("sort", string(q.SortField()))
		values.Set("dir", string(q.SortDirection()))
	}

	if q.PageSize() != _userListDefaultLimit {
		values.Set("limit", strconv.Itoa(q.PageSize()))
	}

	if q.Page > _userListDefaultPage {
		values.Set("page", strconv.Itoa(q.Page))
	}

	return values
}
