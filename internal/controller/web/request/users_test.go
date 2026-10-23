// Package request tests the query the account table answers to.
package request

import (
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// The filter a request maps to is clamped by the domain, so the list, the
// pager and the address bar never see a page size or a page number a form
// never sent.
func TestFilter(t *testing.T) {
	t.Parallel()

	day, err := time.Parse(_userListDateLayout, "2026-10-19")
	if err != nil {
		t.Fatalf("time.Parse() error = %v, want nil", err)
	}

	cases := []struct {
		name string
		req  UserListQuery
		want entity.UserFilter
	}{
		{
			name: "an empty query is the whole list",
			req:  UserListQuery{},
			want: entity.UserFilter{Limit: _userListDefaultLimit, Page: 1},
		},
		{
			name: "the entry that keeps every role drops out",
			req:  UserListQuery{Query: " john ", Role: "_ADMIN", Limit: 5, Page: 2, Date: "2026-10-19"},
			want: entity.UserFilter{
				Query: "john",
				On:    &day,
				Limit: 5,
				Page:  2,
			},
		},
		{
			name: "a page size outside the dropdown falls back",
			req:  UserListQuery{Limit: 3},
			want: entity.UserFilter{Limit: _userListDefaultLimit, Page: 1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.req.Sanitize()

			got := tc.req.Filter()

			if got.Query != tc.want.Query || got.Role != tc.want.Role || got.Limit != tc.want.Limit || got.Page != tc.want.Page {
				t.Errorf("Filter() = %+v, want %+v", got, tc.want)
			}

			if (got.On == nil) != (tc.want.On == nil) {
				t.Errorf("Filter().On = %v, want %v", got.On, tc.want.On)
			}

			if got.On != nil && !got.On.Equal(*tc.want.On) {
				t.Errorf("Filter().On = %v, want %v", got.On, *tc.want.On)
			}
		})
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
