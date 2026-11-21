// Package file tests the SQL the paged file reads build.
package file

import (
	"strings"
	"testing"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// A page scopes to the account and the folder, and the search must reach the
// database as a literal substring even when it holds wildcards.
func TestFilePageWhereScopesToUserAndEscapesSearch(t *testing.T) {
	t.Parallel()

	where := filePageWhere("user-1", "folder-1", "100%_x", nil)

	sql, args, err := where.ToSql()
	if err != nil {
		t.Fatal(err)
	}

	// sq.And groups the conjunction in one pair of parentheses.
	want := "(user_id = ? AND folder_id = ? AND lower(name) LIKE lower(?) ESCAPE '\\')"
	if sql != want {
		t.Errorf("sql = %q, want %q", sql, want)
	}

	if args[0] != "user-1" || args[2] != `%100\%\_x%` {
		t.Errorf("args = %v, want the user and escaped pattern", args)
	}
}

// The page resumes after the cursor row: the name decides first and the
// identifier breaks a tie, so no row repeats or disappears between requests.
func TestFilePageWhereResumesAfterCursor(t *testing.T) {
	t.Parallel()

	after := &entity.EntryCursor{
		Kind: entity.EntryKindFile,
		Name: "notes.txt",
		ID:   "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44",
	}

	sql, args, err := filePageWhere("user-1", "", "", after).ToSql()
	if err != nil {
		t.Fatal(err)
	}

	want := "(lower(name) > lower(?) OR (lower(name) = lower(?) AND id > ?::uuid))"
	if !strings.Contains(sql, want) {
		t.Errorf("sql = %q, want it to contain %q", sql, want)
	}

	if len(args) != 4 {
		t.Fatalf("args = %v, want the user and three cursor arguments", args)
	}

	if args[1] != "notes.txt" || args[2] != "notes.txt" || args[3] != after.ID {
		t.Errorf("args = %v, want the cursor name twice and the identifier", args)
	}
}

// A page of the whole drive carries no folder condition, and an empty search
// must not narrow the rows.
func TestFilePageWhereDropsTheFolderAndSearchWhenUnset(t *testing.T) {
	t.Parallel()

	sql, args, err := filePageWhere("user-1", "", "", nil).ToSql()
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(sql, "folder_id") {
		t.Errorf("sql = %q, want no folder condition", sql)
	}

	if strings.Contains(sql, "LIKE") {
		t.Errorf("sql = %q, want no search condition", sql)
	}

	if len(args) != 1 || args[0] != "user-1" {
		t.Errorf("args = %v, want only the user", args)
	}
}
