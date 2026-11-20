// Package request tests the query the account table answers to.
package request

import (
	"reflect"
	"testing"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/minhhoccode111/fileuploader/internal/entity"
)

type filterCase struct {
	name string
	req  UserListQuery
	want entity.UserFilter
}

func runFilterCases(t *testing.T, cases []filterCase) {
	t.Helper()

	for i := range cases {
		tc := &cases[i]

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.req.Sanitize()

			got := tc.req.Filter()

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Filter() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// The filter a request maps to is clamped by the domain, so the list, the
// pager and the address bar never see a page size or a page number a form
// never sent.
func TestFilterNarrowsTheList(t *testing.T) {
	t.Parallel()

	day, err := time.Parse(_userListDateLayout, "2026-10-19")
	if err != nil {
		t.Fatalf("time.Parse() error = %v, want nil", err)
	}

	runFilterCases(t, []filterCase{
		{
			name: "an empty query is the whole list",
			req:  UserListQuery{},
			want: entity.UserFilter{
				Sort:  entity.UserSortCreatedAt,
				Dir:   entity.UserSortDescending,
				Limit: _userListDefaultLimit,
				Page:  1,
			},
		},
		{
			name: "the entry that keeps every role drops out",
			req:  UserListQuery{Query: " john ", Role: "_ADMIN", Limit: 5, Page: 2, Date: "2026-10-19"},
			want: entity.UserFilter{
				Query: "john",
				On:    &day,
				Sort:  entity.UserSortCreatedAt,
				Dir:   entity.UserSortDescending,
				Limit: 5,
				Page:  2,
			},
		},
		{
			name: "a page size outside the dropdown falls back",
			req:  UserListQuery{Limit: 3},
			want: entity.UserFilter{
				Sort:  entity.UserSortCreatedAt,
				Dir:   entity.UserSortDescending,
				Limit: _userListDefaultLimit,
				Page:  1,
			},
		},
	})
}

// A column and a direction reach the filter in the shape the domain knows.
// An unknown column falls back to the default order the list opens with.
func TestFilterOrdersTheList(t *testing.T) {
	t.Parallel()

	runFilterCases(t, []filterCase{
		{
			name: "a column and a direction reach the filter",
			req:  UserListQuery{Sort: " username ", Dir: " ASC "},
			want: entity.UserFilter{
				Sort:  entity.UserSortUsername,
				Dir:   entity.UserSortAscending,
				Limit: _userListDefaultLimit,
				Page:  1,
			},
		},
		{
			name: "the storage column reaches the filter",
			req:  UserListQuery{Sort: "storage_used_bytes", Dir: "desc"},
			want: entity.UserFilter{
				Sort:  entity.UserSortStorageUsed,
				Dir:   entity.UserSortDescending,
				Limit: _userListDefaultLimit,
				Page:  1,
			},
		},
		{
			name: "an unknown column falls back to the default order",
			req:  UserListQuery{Sort: "password_hash", Dir: "asc"},
			want: entity.UserFilter{
				Sort:  entity.UserSortCreatedAt,
				Dir:   entity.UserSortAscending,
				Limit: _userListDefaultLimit,
				Page:  1,
			},
		},
	})
}

// The validator accepts the storage column like the other columns of the
// table, so a click on its title reaches the list instead of a 400.
func TestUserListQueryAcceptsTheStorageSort(t *testing.T) {
	t.Parallel()

	v := validator.New()

	err := v.Struct(UserListQuery{Sort: "storage_used_bytes", Dir: "asc"})
	if err != nil {
		t.Errorf("Struct() error = %v, want the storage column to pass validation", err)
	}
}

func TestHasFilters(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  UserListQuery
		want bool
	}{
		{name: "nothing set", req: UserListQuery{}, want: false},
		{name: "the entry that keeps every role", req: UserListQuery{Role: "all"}, want: false},
		{name: "a search term", req: UserListQuery{Query: "john"}, want: true},
		{name: "a role", req: UserListQuery{Role: "admin"}, want: true},
		{name: "a day", req: UserListQuery{Date: "2026-10-19"}, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.req.HasFilters(); got != tc.want {
				t.Errorf("HasFilters() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The bookmark drops the parts that carry no narrowing, so the address bar
// holds only what the view needs to come back the same.
func TestBookmarkQuery(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  UserListQuery
		want string
	}{
		{
			name: "the defaults collapse to nothing",
			req:  UserListQuery{Role: "all", Limit: _userListDefaultLimit},
			want: "",
		},
		{
			name: "the page rides along once the visitor leaves the first page",
			req:  UserListQuery{Query: "john", Role: "admin", Limit: 50, Page: 3},
			want: "limit=50&page=3&q=john&role=admin",
		},
		{
			name: "the first page stays out of the bookmark",
			req:  UserListQuery{Query: "john", Limit: 50, Page: 1},
			want: "limit=50&q=john",
		},
		{
			name: "a column order rides along",
			req:  UserListQuery{Sort: "username", Dir: "asc"},
			want: "dir=asc&sort=username",
		},
		{
			name: "the default column in the other direction rides along",
			req:  UserListQuery{Sort: "created_at", Dir: "asc"},
			want: "dir=asc&sort=created_at",
		},
		{
			name: "the default order stays out of the bookmark",
			req:  UserListQuery{Sort: "created_at", Dir: "desc"},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.req.BookmarkQuery().Encode(); got != tc.want {
				t.Errorf("BookmarkQuery().Encode() = %q, want %q", got, tc.want)
			}
		})
	}
}
