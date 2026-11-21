// Package folder tests the SQL the paged folder reads build.
package folder

import (
	"strings"
	"testing"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// A page scopes to the account and the parent, and the search must reach the
// database as a literal substring even when it holds wildcards.
func TestFolderPageWhereScopesToUserAndEscapesSearch(t *testing.T) {
	t.Parallel()

	where := folderPageWhere("user-1", "folder-1", "100%_x", nil)

	sql, args, err := where.ToSql()
	if err != nil {
		t.Fatal(err)
	}

	// sq.And groups the conjunction in one pair of parentheses.
	want := "(f.user_id = ? AND f.parent_id = ? AND lower(f.name) LIKE lower(?) ESCAPE '\\')"
	if sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}

	if args[0] != "user-1" || args[2] != `%100\%\_x%` {
		t.Errorf("args = %v, want the user and escaped pattern", args)
	}
}

// The page resumes after the cursor row: the name decides first and the
// identifier breaks a tie, so no row repeats or disappears between requests.
func TestFolderPageWhereResumesAfterCursor(t *testing.T) {
	t.Parallel()

	after := &entity.EntryCursor{
		Kind: entity.EntryKindFolder,
		Name: "Docs",
		ID:   "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44",
	}

	sql, args, err := folderPageWhere("user-1", "", "", after).ToSql()
	if err != nil {
		t.Fatal(err)
	}

	want := "(lower(f.name) > lower(?) OR (lower(f.name) = lower(?) AND f.id > ?::uuid))"
	if !strings.Contains(sql, want) {
		t.Errorf("sql = %q, want it to contain %q", sql, want)
	}

	if len(args) != 4 {
		t.Fatalf("args = %v, want the user and three cursor arguments", args)
	}

	if args[1] != "Docs" || args[2] != "Docs" || args[3] != after.ID {
		t.Errorf("args = %v, want the cursor name twice and the identifier", args)
	}
}

// A page of the whole drive carries no parent condition, and an empty search
// must not narrow the rows.
func TestFolderPageWhereDropsTheParentAndSearchWhenUnset(t *testing.T) {
	t.Parallel()

	sql, args, err := folderPageWhere("user-1", "", "", nil).ToSql()
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(sql, "f.parent_id") {
		t.Errorf("sql = %q, want no parent condition", sql)
	}

	if strings.Contains(sql, "LIKE") {
		t.Errorf("sql = %q, want no search condition", sql)
	}

	if len(args) != 1 || args[0] != "user-1" {
		t.Errorf("args = %v, want only the user", args)
	}
}

// A listing row keeps the shape the drive table renders: the effective
// modified time and the entry count, both scoped to the account. The hard
// limit and the cursor order must not change either.
func TestFolderPageQuerySelectsTheListingShape(t *testing.T) {
	t.Parallel()

	sql, args, err := folderPageQuery(
		sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
		"user-1",
		"folder-1",
		"",
		nil,
		101,
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"f.id",
		"f.name",
		"GREATEST(",
		"AS updated_at",
		"item_count",
		"FROM folders f",
		"ORDER BY lower(f.name) ASC, f.id ASC",
		"LIMIT 101",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("sql = %q, want it to contain %q", sql, want)
		}
	}

	if len(args) != 2 || args[0] != "user-1" || args[1] != "folder-1" {
		t.Errorf("args = %v, want the user and the parent", args)
	}
}

// The path walk scopes both steps to the account: the anchor may not start
// from another account's folder, and the recursive step may not climb into
// one.
func TestPathsQueryScopesBothStepsToTheUser(t *testing.T) {
	t.Parallel()

	steps := strings.Split(_pathsQuery, "UNION ALL")
	if len(steps) != 2 {
		t.Fatalf("_pathsQuery has %d steps, want the anchor and the recursive step", len(steps))
	}

	if !strings.Contains(steps[0], "user_id = $1") || !strings.Contains(steps[1], "user_id = $1") {
		t.Errorf("_pathsQuery = %s, want both steps scoped to the user", _pathsQuery)
	}

	if !strings.Contains(steps[0], "id = ANY($2)") {
		t.Errorf("anchor = %s, want the identifiers the caller named", steps[0])
	}

	if !strings.Contains(steps[1], "parent.user_id = $1") {
		t.Errorf("recursive step = %s, want the parent scoped to the user", steps[1])
	}
}

// The identifiers a page carries are strings; the query binds UUIDs, so every
// value has to convert and a malformed one has to fail before the query runs.
func TestPathIDsConvertToUUIDs(t *testing.T) {
	t.Parallel()

	const (
		first  = "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44"
		second = "0f6a1c2e-3b4d-4e5f-8a9b-0c1d2e3f4a5b"
	)

	ids, err := pathIDs([]string{first, second})
	if err != nil {
		t.Fatalf("pathIDs() error = %v, want nil", err)
	}

	want := []pgtype.UUID{
		{Bytes: uuid.MustParse(first), Valid: true},
		{Bytes: uuid.MustParse(second), Valid: true},
	}

	if len(ids) != len(want) {
		t.Fatalf("pathIDs() = %v, want %v", ids, want)
	}

	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("pathIDs()[%d] = %v, want %v", i, ids[i], want[i])
		}
	}

	if _, err := pathIDs([]string{"not-a-uuid"}); err == nil {
		t.Error("pathIDs() error = nil, want a malformed identifier rejected")
	}
}
