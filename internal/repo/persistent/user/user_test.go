// Package user tests the SQL the account list builds.
package user

import (
	"strings"
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// A query with the LIKE wildcards in the text they came from must reach the
// database escaped, so the search looks for the literal text and not for a
// pattern of its own.
func TestListWhereBuildsAllConditions(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, time.October, 19, 0, 0, 0, 0, time.UTC)

	where := listWhere(&entity.UserFilter{
		Query: "100%_x",
		Role:  entity.UserRoleAdmin,
		On:    &day,
	})
	if where == nil {
		t.Fatal("listWhere() = nil, want conditions")
	}

	sql, args, err := where.ToSql()
	if err != nil {
		t.Fatalf("listWhere().ToSql() error = %v, want nil", err)
	}

	want := "((username ILIKE ? ESCAPE '\\' OR email ILIKE ? ESCAPE '\\') AND role = ? " +
		"AND (created_at::date = ?::date OR updated_at::date = ?::date))"

	if sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}

	escaped := "%100\\%\\_x%"

	wantArgs := []any{escaped, escaped, entity.UserRoleAdmin, day, day}
	if len(args) != len(wantArgs) {
		t.Fatalf("args = %v, want %v", args, wantArgs)
	}

	for i := range wantArgs {
		if args[i] != wantArgs[i] {
			t.Errorf("args[%d] = %v, want %v", i, args[i], wantArgs[i])
		}
	}
}

// A filter with nothing set must leave the query without a WHERE clause.
func TestListWhereIsNilWithoutConditions(t *testing.T) {
	t.Parallel()

	if where := listWhere(&entity.UserFilter{}); where != nil {
		t.Errorf("listWhere() = %v, want nil", where)
	}
}

// The order the list asks for is one the domain knows, and the identifier
// breaks a tie, so a page never repeats or skips a row between two requests.
func TestListOrder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		filter entity.UserFilter
		want   string
	}{
		{
			name:   "the default order puts the newest account first",
			filter: entity.UserFilter{},
			want:   "created_at DESC, id ASC",
		},
		{
			name:   "a column and a direction",
			filter: entity.UserFilter{Sort: entity.UserSortUsername, Dir: entity.UserSortAscending},
			want:   "username ASC, id ASC",
		},
		{
			name:   "another column descending",
			filter: entity.UserFilter{Sort: entity.UserSortRole, Dir: entity.UserSortDescending},
			want:   "role DESC, id ASC",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := strings.Join(listOrder(&tc.filter), ", "); got != tc.want {
				t.Errorf("listOrder() = %q, want %q", got, tc.want)
			}
		})
	}
}
