# Folder Browsing and Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bound every folder listing to 100 entries, add keyset "load more", and add local and global name search without breaking folder context, shares or actions.

**Architecture:** Two bounded repository pages (folders, then files) are merged by the folder use case with a keyset cursor. The web controller renders the first page normally and the next page through htmx partials. `/search` is a separate read-only page state that resolves result paths in one extra query. Postgres `pg_trgm` indexes back substring search.

**Tech Stack:** Go, Fiber, templ, htmx, Squirrel, pgx/Postgres with `pg_trgm`, plain JavaScript.

**Spec:** `docs/superpowers/specs/2026-11-21-folder-browse-and-search-design.md`

## Global Constraints

- Batch size is 100 entries per request. No client-supplied page size.
- Browsing is keyset pagination, never `OFFSET`.
- Order is folders first, then files, each by `lower(name)` and then `id`.
- Local search is direct children only, minimum 1 character, empty query lists all.
- Global search is own drive only, minimum 3 characters, maximum 100 characters.
- Search is a case-insensitive substring of the name. `%`, `_` and `\` are escaped.
- Home is `/` and keeps that URL for local search; global search started from Home has no `from`.
- Global search results are read-only: folder opens, file downloads, location opens the containing folder.
- Shared pages get local search only, keyed by the share owner's identifier.
- All queries are parameterized and scoped by `user_id`.
- A malformed cursor on a partial request returns 400 and never restarts at the first page.
- `pg_trgm` is a deployment requirement.
- The project's linter rules apply: `err113`, `wsl_v5`, `nlreturn`, `funlen`, `gocyclo`, `gocognit`, `mnd`, `gochecknoglobals`.

## Review Focus

- **Home local search:** `/` must filter the root's direct children, keep the Home URL, and never invent a `from` value. Tested in Task 5.
- **Pagination boundary:** a page exactly full of folders with files behind it must produce a next cursor without duplicating or skipping a row. Tested in Task 3.
- **Bad cursor:** a missing or tampered `after` on "Load more" must return 400 and add no rows. Tested in Tasks 4, 6 and 7.
- **Mutation under a query:** a rename, move, delete or upload while `q` is active must return filtered rows that still match the visible input. Tested in Task 5.
- **Cross-account search:** global and shared search must pass the owning account to every read; a second account's rows must never appear. Tested in Tasks 3 and 7 plus the manual two-account check in Task 8.

---

### Task 1: Entity Cursor And Search Primitives

**Files:**
- Create: `internal/entity/search.go`
- Create: `internal/entity/search_test.go`
- Modify: `internal/entity/folder.go:110-136`
- Modify: `internal/entity/errors.go:5-36`

**Interfaces:**
- Produces:
  - `type EntryKind string`
  - `const EntryKindFolder EntryKind = "folder"`
  - `const EntryKindFile EntryKind = "file"`
  - `type EntryCursor struct { Kind EntryKind; Name string; ID string; FoldersDone bool }`
  - `func (c EntryCursor) Encode() string`
  - `func DecodeEntryCursor(encoded string) (*EntryCursor, error)`
  - `func CleanSearch(raw string) string`
  - `func SearchPattern(term string) string`
  - `var ErrInvalidCursor = errors.New("invalid cursor")`
  - `FolderItem.Path []FolderRef`
  - `FileItem.FolderID string`, `FileItem.Path []FolderRef`
  - `FolderView.Search string`, `FolderView.Next *EntryCursor`
  - `type SearchView struct { Folders []FolderItem; Files []FileItem; Search string; From string; TooShort bool; Next *EntryCursor }`

- [ ] **Step 1: Write the failing entity tests**

Create `internal/entity/search_test.go` with these tests:

```go
package entity

import (
    "encoding/base64"
    "errors"
    "strings"
    "testing"
)

func TestEntryCursorRoundTrip(t *testing.T) {
    t.Parallel()

    in := EntryCursor{Kind: EntryKindFile, Name: "notes.txt", ID: "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44", FoldersDone: true}
    out, err := DecodeEntryCursor(in.Encode())
    if err != nil {
        t.Fatalf("DecodeEntryCursor() error = %v, want nil", err)
    }
    if out == nil || *out != in {
        t.Fatalf("DecodeEntryCursor() = %+v, want %+v", out, in)
    }
}

func TestDecodeEntryCursorRejectsBadValues(t *testing.T) {
    t.Parallel()

    badJSON := base64.RawURLEncoding.EncodeToString([]byte(`{"kind":"bogus","name":"a","id":"not-a-uuid"}`))
    cases := []string{
        "not-base64",
        strings.Repeat("a", 16*1024),
        badJSON,
    }
    for _, encoded := range cases {
        if _, err := DecodeEntryCursor(encoded); !errors.Is(err, ErrInvalidCursor) {
            t.Errorf("DecodeEntryCursor(%q) error = %v, want %v", encoded, err, ErrInvalidCursor)
        }
    }

    if got, err := DecodeEntryCursor(""); err != nil || got != nil {
        t.Errorf("DecodeEntryCursor(\"\") = (%+v, %v), want (nil, nil)", got, err)
    }
}

func TestCleanSearchTrimsCapsAndDropsInvalidUTF8(t *testing.T) {
    t.Parallel()

    if got := CleanSearch("  notes  "); got != "notes" {
        t.Errorf("CleanSearch() = %q, want %q", got, "notes")
    }
    if got := CleanSearch(strings.Repeat("a", 101)); len([]rune(got)) != 100 {
        t.Errorf("CleanSearch() length = %d, want 100", len([]rune(got)))
    }
    if got := CleanSearch("a" + string([]byte{0xff}) + "b"); got != "ab" {
        t.Errorf("CleanSearch() = %q, want %q", got, "ab")
    }
}

func TestSearchPatternEscapesLikeMetacharacters(t *testing.T) {
    t.Parallel()

    if got := SearchPattern(`100%_x\y`); got != `%100\%\_x\\y%` {
        t.Errorf("SearchPattern() = %q, want %q", got, `%100\%\_x\\y%`)
    }
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/entity/ -run 'TestEntryCursor|TestDecodeEntryCursor|TestCleanSearch|TestSearchPattern' -count=1`

Expected: build failure because `EntryCursor`, `DecodeEntryCursor`,
`CleanSearch` and `SearchPattern` do not exist.

- [ ] **Step 3: Implement the cursor and search helpers**

Create `internal/entity/search.go`. Encode the cursor as JSON then
`base64.RawURLEncoding`. Decode refuses an encoded value longer than 8192 bytes,
a non-`folder`/`file` kind, an empty name, a name longer than 255 runes, an empty
ID, and an ID that `uuid.Parse` rejects. `DecodeEntryCursor("")` returns
`(nil, nil)`.

`CleanSearch` trims, drops invalid UTF-8 with
`strings.ToValidUTF8(raw, "")`, then keeps at most 100 runes.
`SearchPattern` escapes `\` first, then `%` and `_`, wraps the result in `%...%`
and is the only place those escapes are built.

- [ ] **Step 4: Add the listing fields and sentinel**

Add `ErrInvalidCursor` to `internal/entity/errors.go`.

In `internal/entity/folder.go`, add `Path []FolderRef` to `FolderItem`, add
`FolderID string` and `Path []FolderRef` to `FileItem`, add `Search string` and
`Next *EntryCursor` to `FolderView`, and add the `SearchView` type from the
Interfaces block.

- [ ] **Step 5: Run the entity tests**

Run: `go test ./internal/entity/ -count=1`

Expected: `ok github.com/minhhoccode111/fileuploader/internal/entity`.

- [ ] **Step 6: Commit**

```bash
git add internal/entity/search.go internal/entity/search_test.go internal/entity/folder.go internal/entity/errors.go
git commit -m "feat(entity): add browse cursor and search primitives"
```

---

### Task 2: Paged Repository Reads And Search Indexes

**Files:**
- Modify: `internal/repo/persistent/folder/folder.go:216-266`
- Create: `internal/repo/persistent/folder/folder_page_test.go`
- Modify: `internal/repo/persistent/file/file.go:96-133`
- Create: `internal/repo/persistent/file/file_page_test.go`
- Create migration pair with `make migrate-create add_folder_browse_indexes`
- Modify: `docs/schema.sql`

**Interfaces:**
- Consumes: `entity.EntryCursor`, `entity.SearchPattern`, `entity.FolderItem`, `entity.FileItem`, `entity.FolderRef`.
- Produces (on the concrete repositories; Task 3 promotes them to the interfaces):
  - `(*folder.Repo).ListPage(ctx context.Context, userID, parentID, search string, after *entity.EntryCursor, limit int) ([]entity.FolderItem, error)`
  - `(*folder.Repo).Paths(ctx context.Context, userID string, folderIDs []string) (map[string][]entity.FolderRef, error)`
  - `(*file.Repo).ListPage(ctx context.Context, userID, folderID, search string, after *entity.EntryCursor, limit int) ([]entity.FileItem, error)`

- [ ] **Step 1: Write the failing query-builder tests**

Create `internal/repo/persistent/folder/folder_page_test.go` (package `folder`)
and `internal/repo/persistent/file/file_page_test.go` (package `file`). Add a
pure `folderPageWhere` / `filePageWhere` helper in each repository and test it:

```go
func TestFolderPageWhereScopesToUserAndEscapesSearch(t *testing.T) {
    where := folderPageWhere("user-1", "folder-1", "100%_x", nil)
    sql, args, err := where.ToSql()
    if err != nil {
        t.Fatal(err)
    }
    want := "user_id = ? AND parent_id = ? AND lower(name) LIKE lower(?) ESCAPE '\\'"
    if sql != want {
        t.Errorf("sql = %q, want %q", sql, want)
    }
    if args[0] != "user-1" || args[2] != `%100\%\_x%` {
        t.Errorf("args = %v, want the user and escaped pattern", args)
    }
}
```

Add a cursor case asserting the SQL contains
`(lower(name) > lower(?) OR (lower(name) = lower(?) AND id > ?::uuid))` and the
three cursor arguments. Add an empty-parent case asserting there is no
`parent_id` condition and an empty-search case asserting there is no `LIKE`.
Mirror the same two tests for files with `folder_id`.

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/repo/persistent/folder/ ./internal/repo/persistent/file/ -run 'PageWhere' -count=1`

Expected: build failure because the helpers and new repository methods do not
exist.

- [ ] **Step 3: Implement the page helpers and queries**

In each repository, build a `sq.SelectBuilder` with the conditions in a fixed
order so the SQL tests are deterministic:

- select `id, name, updated_at` for folders, or
  `id, folder_id, name, content_type, size, updated_at` for files;
- always add `user_id = ?`;
- add `parent_id = ?` / `folder_id = ?` only when the identifier is non-empty;
- add `lower(name) LIKE lower(?) ESCAPE '\'` only when search is non-empty,
  using `entity.SearchPattern(search)`;
- add the cursor condition when `after != nil`;
- `OrderBy("lower(name) ASC", "id ASC").Limit(uint64(max(limit, 1)))`.

Use `sq.And{...}` with explicit entries rather than a `sq.Eq` map, because map
condition order is not stable and the tests compare the SQL text.

Set `FileItem.FolderID` from the selected `folder_id`. `FolderItem` keeps its
current fields and leaves `Path` empty.

Implement `Paths` with the existing recursive CTE style. Convert the incoming
string IDs to `[]pgtype.UUID`, pass them as `$2`, start the CTE at those folders
with `id = ANY($2) AND user_id = $1`, walk to the root, and select
`leaf, id, name, depth` ordered by `leaf`, depth descending. Group refs by leaf
into root-first `[]entity.FolderRef`. An unknown or foreign folder simply has no
entry in the map.

- [ ] **Step 4: Keep the interfaces unchanged for now**

Do not add the new methods to `internal/repo/contracts.go` yet. The concrete
methods are enough for this task, and adding them to the interfaces now would
force every use-case fake to grow stubs before the use case uses them. Task 3
adds the interface methods and updates the fakes in the same change that switches
the caller.

- [ ] **Step 5: Create and verify the migration**

Run: `make migrate-create add_folder_browse_indexes`

Fill the generated `.up.sql` with:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS folders_user_name_idx
    ON folders (user_id, lower(name), id);
CREATE INDEX IF NOT EXISTS files_user_name_idx
    ON files (user_id, lower(name), id);
CREATE INDEX IF NOT EXISTS folders_name_trgm_idx
    ON folders USING gin (lower(name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS files_name_trgm_idx
    ON files USING gin (lower(name) gin_trgm_ops);
```

The `.down.sql` drops the four indexes with `DROP INDEX IF EXISTS` and leaves
`pg_trgm` installed, because the extension can predate this migration and other
objects may use it.

Run: `make migrate-up`, then `make migrate-down`, then `make migrate-up` again,
then `make schema`.

Expected: each command succeeds, `make schema` rewrites `docs/schema.sql`, and
`git diff docs/schema.sql` shows the new indexes.

- [ ] **Step 6: Run the repository and full test suites**

Run: `go test ./internal/repo/persistent/folder/ ./internal/repo/persistent/file/ -count=1`

Expected: both packages pass.

Run: `go test ./... -count=1`

Expected: pass, with the old `FolderRepo.List` still present.

- [ ] **Step 7: Commit**

```bash
git add internal/repo/contracts.go internal/repo/persistent/folder internal/repo/persistent/file migrations docs/schema.sql
git commit -m "feat(repo): page folder reads and add search indexes"
```

---

### Task 3: Use-Case Merge, Local And Global Search

**Files:**
- Modify: `internal/usecase/contracts.go:36-52`
- Modify: `internal/usecase/folder/folder.go:105-135`
- Modify: `internal/usecase/folder/folder_test.go`
- Modify: `internal/usecase/file/file_test.go`
- Modify: `internal/usecase/user/user_test.go`
- Modify: `internal/controller/web/drive.go` (update the `List` call sites)
- Modify: `internal/controller/web/share.go` (update the shared `List` call site)
- Modify: `internal/controller/web/upload_test.go:110-123`
- Modify: `internal/repo/contracts.go` (remove `FolderRepo.List`)
- Modify: `internal/repo/persistent/folder/folder.go` (remove old `List`)

**Interfaces:**
- Consumes: `FolderRepo.ListPage`, `FileRepo.ListPage`, `FolderRepo.Paths`, `entity.CleanSearch`.
- Produces:
  - `Folder.List(ctx context.Context, userID, folderID, search string, after *entity.EntryCursor) (entity.FolderView, error)`
  - `Folder.Search(ctx context.Context, userID, search, from string, after *entity.EntryCursor) (entity.SearchView, error)`

- [ ] **Step 1: Write the failing use-case tests**

Add to `internal/usecase/folder/folder_test.go`:

- `TestListMergesFoldersBeforeFiles` — fake returns 2 folders and 2 files; expect
  `view.Folders` then `view.Files`, `view.Search == "note"`, no next cursor.
- `TestListStopsAtTheFolderLimit` — fake folder page returns 101 folders; expect
  100 folders, no files requested, and a folder cursor at the 100th row.
- `TestListFillsTheRestWithFiles` — 3 folders and 200 files; expect 3 folders and
  97 files, plus a file cursor at the 97th file.
- `TestListDetectsFilesBehindAFullFolderPage` — exactly 100 folders and 1 file;
  expect 100 folders, no files shown, and a folder cursor with
  `FoldersDone: true`.
- `TestListContinuesAFilePageWithoutFolders` — cursor kind `file`; expect no
  folder repository call and the file page fetched after that cursor.
- `TestListPassesTheFolderAndSearchToBothRepos` — assert the recorded
  `parentID`/`folderID` and search term.
- `TestSearchRequiresThreeCharacters` — empty, one-character and two-character
  queries make no repository calls and set `TooShort`.
- `TestSearchScopesTheWholeDriveToTheUser` — record the `userID` passed to both
  repositories; assert it is the requested account.
- `TestSearchFillsPathsForFoldersAndFiles` — fake `Paths` returns chains; assert
  each folder and file has the matching path and the root is the `/` ref.
- `TestSearchLeavesLocalPathsEmpty` — a normal `List` does not call `Paths`.

Extend the fakes with page hooks rather than fixed slices, for example:

```go
type fakeFolderRepo struct {
    // ... existing fields
    folderPage func(parentID, search string, after *entity.EntryCursor, limit int) ([]entity.FolderItem, error)
    lastFolderAfter *entity.EntryCursor
    lastFolderLimit int
    paths map[string][]entity.FolderRef
    pathIDs []string
}
```

Implement `ListPage`, `Paths`, and remove the old `List` method. Update
`fakeFileRepo` in the same file and the folder/file fakes in
`internal/usecase/file/file_test.go` and `internal/usecase/user/user_test.go` so
they satisfy the changed interfaces. Update `fakeUploadFolder.List` in
`internal/controller/web/upload_test.go` to the new signature and ignore the
extra arguments.

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/usecase/folder/ -run 'TestList|TestSearch' -count=1`

Expected: build failure because `List` has the old signature and `Search` does
not exist.

- [ ] **Step 3: Implement the merge**

Replace the two full-list calls in `internal/usecase/folder/folder.go` with one
private browse helper:

```go
func (uc *UseCase) browse(
    ctx context.Context,
    userID, folderID, search string,
    after *entity.EntryCursor,
    limit int,
) ([]entity.FolderItem, []entity.FileItem, *entity.EntryCursor, error)
```

Use `const _pageSize = 100` and `const _globalSearchMinRunes = 3`.

The helper follows the spec algorithm:

1. Skip folders when the cursor kind is `file`, or when the cursor is a folder
   with `FoldersDone`.
2. Fetch `limit+1` folders from the cursor, or from the start.
3. If folders overflow, return `limit` folders and a folder cursor with
   `FoldersDone:false`.
4. Otherwise fetch `limit-len(folders)+1` files, starting after the file cursor
   when the cursor kind is `file`, or at the beginning otherwise.
5. If files overflow, return what fits and a file cursor at the last shown file.
6. If the page is exactly full of folders and one file was fetched only to prove
   a next page exists, return a folder cursor at the last folder with
   `FoldersDone:true`.
7. No overflow means no next cursor.

Cursors carry the display name. The SQL helpers lower it on both sides of the
comparison.

- [ ] **Step 4: Implement `List` and `Search`**

`List` cleans the search, reads the folder, builds the breadcrumb, calls
`browse` with the folder ID, and returns `FolderView` with `Search` and `Next`.
An empty folder ID still resolves the root through `uc.read`.

`Search` cleans the search, returns an empty `SearchView` with `TooShort:true`
when it has fewer than three runes, otherwise calls `browse` with an empty
folder ID. For a non-short search, collect folder IDs (folder's own ID; file's
`FolderID`), call `uc.folders.Paths` once, and copy the chains onto the items.

Update `internal/usecase/contracts.go`. In `internal/repo/contracts.go`, add
`ListPage` and `Paths` to `FolderRepo` and `ListPage` to `FileRepo`, then remove
the old `FolderRepo.List` after no caller uses it. Remove the old `List` method
from `internal/repo/persistent/folder/folder.go`.

The interface change breaks the two existing web call sites. In
`internal/controller/web/drive.go`, pass `""` and `nil` for the new search and
cursor arguments; Task 4 and Task 5 replace those placeholders. In
`internal/controller/web/share.go`, do the same for the shared folder call; Task
6 adds shared search and paging.

- [ ] **Step 5: Run the use-case tests**

Run: `go test ./internal/usecase/folder/ -count=1`

Expected: pass.

Run: `go test ./internal/usecase/... ./internal/controller/web/ -count=1`

Expected: pass; only compile fixes in fakes, no behavior change yet.

- [ ] **Step 6: Commit**

```bash
git add internal/usecase internal/controller/web/upload_test.go internal/repo/contracts.go internal/repo/persistent/folder/folder.go
git commit -m "feat(folder): merge paged listings and search result paths"
```

---

### Task 4: Bounded Page, Load More, And Partial Route

**Files:**
- Modify: `internal/controller/web/response/drive.go:42-136`
- Modify: `internal/controller/web/response/drive_test.go`
- Modify: `internal/controller/web/drive.go:30-103`
- Modify: `internal/controller/web/router.go:66-122`
- Modify: `web/templates/drive.templ:173-281`
- Modify: `web/templates/drive_test.go`
- Modify: generated `web/templates/drive_templ.go` through `make templ-generate`

**Interfaces:**
- Consumes: `Folder.List(ctx, userID, folderID, "", nil)`, `entity.EntryCursor`.
- Produces:
  - `response.Drive.Search string`, `response.Drive.NextURL string`
  - `func NewDrive(view *entity.FolderView) Drive` builds `NextURL` as
    `/htmx/folders/<folderID>/entries?q=<q>&after=<cursor>` (omit empty `q`).
  - `func (c *Controller) folderEntries(ctx *fiber.Ctx) error`
  - `templ EntryRows(page response.Drive)`
  - Route `GET /htmx/folders/:folder/entries`

- [ ] **Step 1: Write the failing response and template tests**

Add to `internal/controller/web/response/drive_test.go`:

```go
func TestNewDriveBuildsTheNextHref(t *testing.T) {
    next := entity.EntryCursor{Kind: entity.EntryKindFile, Name: "notes.txt", ID: "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44"}
    page := NewDrive(&entity.FolderView{
        Folder: entity.Folder{ID: "folder-1", Name: "/"},
        Search: "no te",
        Next:   &next,
    })
    if !strings.Contains(page.NextURL, "q=no+te") || !strings.Contains(page.NextURL, "after=") {
        t.Errorf("NextURL = %q, want q and after", page.NextURL)
    }
}
```

Add to `web/templates/drive_test.go`:

```go
func TestDriveEntriesShowsLoadMoreOnlyWithANextURL(t *testing.T) {
    page := response.Drive{
        FolderID: "folder-1",
        Folders: []response.FolderRow{{ID: "f", Name: "Docs", Href: "/folders/f", Items: "0", Updated: "now"}},
        NextURL: "/htmx/folders/folder-1/entries?after=abc",
    }
    var buf bytes.Buffer
    if err := DriveEntries(page).Render(t.Context(), &buf); err != nil {
        t.Fatal(err)
    }
    if !strings.Contains(buf.String(), "Load more") {
        t.Error("DriveEntries() has no load-more row")
    }
}
```

Add a second case with `NextURL: ""` asserting `Load more` is absent.

Add a controller test in `internal/controller/web/drive_test.go` for
`folderEntries` with `after` set to `"not-a-cursor"`; expect `400` and no rows
in the body if Fiber sends a status text.

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/controller/web/response/ ./web/templates/ ./internal/controller/web/ -run 'NextHref|LoadMore|FolderEntries' -count=1`

Expected: build failure for missing fields and handler.

- [ ] **Step 3: Map the next URL**

Add `Search` and `NextURL` to `response.Drive`. In `NewDrive`, build `NextURL`
only when `view.Next != nil`, using `net/url.Values` so `q` and the cursor are
escaped. The base is `/htmx/folders/` plus the folder ID plus `/entries`.

- [ ] **Step 4: Render rows and the sentinel**

Refactor `DriveEntries` in `web/templates/drive.templ`:

- keep the empty-state block when both slices are empty;
- otherwise render the table and call `@EntryRows(page)` inside `<tbody>`;
- in `EntryRows`, render the existing folder rows, then the existing file rows,
  then, when `page.NextURL != ""`, a final `<tr>` with a button:
  `hx-get={ page.NextURL }`, `hx-target="closest tr"`,
  `hx-swap="outerHTML"`, `hx-trigger="click, revealed"`, text `Load more`, and a
  single cell spanning the table's four columns.

The partial response is rows only, so it replaces the sentinel row and adds the
next sentinel. No page number and no offset appear anywhere.

- [ ] **Step 5: Add the partial handler and route**

In `internal/controller/web/drive.go`, add `folderEntries`:

1. validate the folder ID;
2. resolve the user;
3. reject an empty or malformed `after` with `ctx.SendStatus(400)`;
4. call `c.folders.List(ctx.UserContext(), userID, folderID, "", after)`;
5. render `templates.EntryRows(response.NewDrive(&view))` with
   `c.renderPage`.

Register `app.Get("/htmx/folders/:folder/entries", middleware.RequireAuth(u), c.folderEntries)`.

Change `drivePage` and `renderEntries` to call the new `List` signature with an
empty search and nil cursor. No search UI yet.

- [ ] **Step 6: Run the tests**

Run: `make templ-generate && go test ./internal/controller/web/... ./web/templates/ -count=1`

Expected: pass. Run `node --check web/static/js/app.js` if any JS was touched;
this task touches none.

- [ ] **Step 7: Commit**

```bash
git add internal/controller/web/response/drive.go internal/controller/web/response/drive_test.go internal/controller/web/drive.go internal/controller/web/router.go web/templates/drive.templ web/templates/drive_templ.go web/templates/drive_test.go
git commit -m "feat(web): page folder entries with load more"
```

---

### Task 5: Local Search, Scope Redirect, And Mutation Consistency

**Files:**
- Modify: `internal/controller/web/drive.go:30-103`
- Modify: `internal/controller/web/folder.go` (rename redirect keeps `q`)
- Modify: `internal/controller/web/router.go:66-122`
- Modify: `internal/controller/web/response/drive.go`
- Modify: `internal/controller/web/drive_test.go`
- Modify: `internal/controller/web/upload_test.go`
- Modify: `web/templates/drive.templ:57-171`
- Modify: `web/templates/drive_templ.go`
- Modify: `web/static/js/app.js:330-383`
- Modify: `web/templates/drive_test.go`

**Interfaces:**
- Consumes: `Folder.List(ctx, userID, folderID, search, after)`.
- Produces:
  - `response.Drive.SearchAction string`
  - `func (c *Controller) entrySearch(ctx *fiber.Ctx) string`
  - `templ EntrySearchForm(page response.Drive)`
  - Scope values `folder` and `all`.
  - Upload completion sends `?q=<current query>` to `/complete`.

- [ ] **Step 1: Write the failing tests**

Add controller tests:

- `TestFolderSearchPassesQToTheUseCase` — GET `/folders/folder-1?q=note`, fake
  records `search == "note"` and renders the form with `value="note"`.
- `TestHomeSearchKeepsTheHomePath` — fake `home` resolves the root and records
  `search`; the rendered form action is `/`.
- `TestFolderScopeAllRedirectsToSearch` — GET
  `/folders/folder-1?q=note&scope=all`; expect `Location`
  `/search?from=folder-1&q=note` (or the same path in `HX-Location` for an htmx
  request).
- `TestEntrySearchReadsTheHtmxCurrentURL` — `HX-Current-URL`
  `http://x/folders/folder-1?q=note&scope=folder` returns `note`; a malformed
  URL returns `""`.
- `TestFolderCreateRefreshesTheActiveFilter` — POST create with
  `HX-Current-URL` carrying `q=note`; fake `List` receives `note`, so the returned
  rows match the visible input.

Add a template test that `EntrySearchForm` renders `name="q"`, the current
value, and both scope options. Add a template test that an empty filtered folder
renders `No matches in this folder.` and an empty unfiltered folder renders
`This folder is empty.`

There is no JS test harness; after editing `app.js`, run
`node --check web/static/js/app.js` and rely on the browser smoke test.

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/controller/web/ -run 'Search|Scope|EntrySearch' -count=1`

Expected: build failure for `entrySearch`, `SearchAction`, or the form component.

- [ ] **Step 3: Thread `q` through the house page**

In `drive`, read `ctx.Query("q")` and pass it to `drivePage`. `drivePage` calls
`List(ctx, userID, folderID, search, nil)`. In `home`, keep folder ID empty.

Before rendering a folder or Home page, handle `scope=all`:

- folder: `/search` with the current `q` and `from=<folderID>`;
- Home: `/search` with the current `q` and no `from`.

Use `middleware.Redirect` and `url.Values`.

- [ ] **Step 4: Preserve `q` on action responses**

Add `entrySearch`:

1. use `ctx.Query("q")` when present (upload completion);
2. otherwise parse `ctx.Get("HX-Current-URL")` with `net/url` and read `q`;
3. return `entity.CleanSearch` of the result.

`renderEntries` calls `c.folders.List(..., entrySearch(ctx), nil)` and renders
`DriveEntries` as before. Action handlers need no signature change because
`renderEntries` reads the header itself.

`folderRename` is the one action that redirects instead of refreshing the table.
Append the active `q` to its parent redirect so the filtered page comes back
consistent. Use `url.Values`; Home stays `/`.

In `completeUpload` in `web/static/js/app.js`, append the current page `q` to the
complete URL:

```js
const q = new URLSearchParams(window.location.search).get("q") ?? "";
const suffix = q === "" ? "" : "?q=" + encodeURIComponent(q);
const response = await fetch("/htmx/uploads/" + id + "/complete" + suffix, { method: "POST" });
```

- [ ] **Step 5: Add the search form**

Add `SearchAction` to `response.Drive`, set to `/` when the folder is root and
`/folders/<id>` otherwise. Add `EntrySearchForm(page)` above the entries table in
`DriveContent`. The form is `method="get"`, `action={ page.SearchAction }`,
`hx-get={ page.SearchAction }`, `hx-target="#page-content"`,
`hx-swap="innerHTML"`, `hx-push-url="true"`. It contains:

- a text input `name="q"`, `value={ page.Search }`, `maxlength="100"`;
- a select `name="scope"` with `This folder` (`folder`) selected on a folder
  page and `All files` (`all`);
- a submit button.

On a folder page the form submits to the folder route; `scope=all` is handled by
the redirect from Step 3. On Home it submits to `/`.

In `DriveEntries`, change the empty-state copy when `page.Search != ""` to
`No matches in this folder.` so a filtered empty list is not described as an
empty folder. Keep `This folder is empty.` for an unfiltered empty list.

- [ ] **Step 6: Run the tests**

Run: `make templ-generate && go test ./internal/controller/web/... ./web/templates/ -count=1`

Expected: pass.

Run: `node --check web/static/js/app.js`

Expected: exit 0.

- [ ] **Step 7: Commit**

```bash
git add internal/controller/web/drive.go internal/controller/web/folder.go internal/controller/web/file.go internal/controller/web/upload.go internal/controller/web/router.go internal/controller/web/response/drive.go internal/controller/web/drive_test.go internal/controller/web/upload_test.go web/templates/drive.templ web/templates/drive_templ.go web/templates/drive_test.go web/static/js/app.js
git commit -m "feat(web): search the current folder without losing context"
```

---

### Task 6: Shared Folder Pagination And Local Search

**Files:**
- Modify: `internal/controller/web/share.go:246-311`
- Modify: `internal/controller/web/router.go:66-73`
- Modify: `internal/controller/web/response/share.go:39-121`
- Modify: `web/templates/share.templ:156-230`
- Modify: `web/templates/share_templ.go`
- Modify: `internal/controller/web/share_test.go` (create if absent)
- Modify: `web/templates/drive_test.go` or add `share_test.go`

**Interfaces:**
- Consumes: `Folder.List(ctx, ownerID, folderID, search, after)`, `entity.EntryCursor`.
- Produces:
  - `response.SharedDrive.Search string`, `response.SharedDrive.NextURL string`,
    `response.SharedDrive.SearchAction string`
  - `func NewSharedDrive(shareID string, view *entity.FolderView) SharedDrive` builds
    `NextURL` against the share partial routes.
  - `func (c *Controller) sharedEntries(ctx *fiber.Ctx) error`
  - `func (c *Controller) sharedEntriesRoute(ctx *fiber.Ctx) error`
  - Routes:
    - `GET /htmx/share/:share/entries`
    - `GET /htmx/share/:share/folders/:folder/entries`

- [ ] **Step 1: Write the failing tests**

Add response tests:

- `TestNewSharedDriveBuildsRootNextHref` — expect
  `/htmx/share/<share>/entries?after=...`.
- `TestNewSharedDriveBuildsNestedNextHref` — expect
  `/htmx/share/<share>/folders/<folder>/entries?after=...`.
- `TestNewSharedDriveKeepsTheSearch` — expect `q=note` in `NextURL`.

Add template tests: shared rows show `Load more` when `NextURL` is set, and the
shared page renders the current `q` in its search form.

Add controller tests: a shared partial with `after=bogus` returns 400; a shared
folder page with `q=note` passes `note` to the use case with the share owner's
identifier.

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/controller/web/ ./web/templates/ -run 'Shared.*(Next|Search|Cursor|Load)' -count=1`

Expected: build failure for missing fields and handlers.

- [ ] **Step 3: Extend the shared page**

Add `Search`, `NextURL`, and `SearchAction` to `response.SharedDrive`.
`NewSharedDrive` builds `NextURL` from `view.Next` and `view.Search`, using the
root or nested share partial path. `SearchAction` is the current share page path.

In `sharedFolder`, read `q` and pass it to `c.folders.List`. In
`SharedDriveContent`, add a local search form (no scope select, no global link)
and refactor the table body into `SharedEntryRows(page)` with a "Load more"
sentinel. The shared table has Name, Size and Modified columns, so its sentinel
cell spans three columns. The empty copy stays "This shared folder is empty."

- [ ] **Step 4: Add shared partial handlers**

Add `sharedEntries` and `sharedEntriesRoute`, both resolving the share token with
the existing `resolveSharedFolder` path and the owner identifier from the
resolved share. Reject an empty or malformed `after` with 400. Call
`Folder.List` with `q` and the cursor, then render `templates.SharedEntryRows`.
Register both routes without the auth guard, because the share token is the
authorization.

- [ ] **Step 5: Run the tests**

Run: `make templ-generate && go test ./internal/controller/web/... ./web/templates/ -count=1`

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/controller/web/share.go internal/controller/web/share_test.go internal/controller/web/router.go internal/controller/web/response/share.go web/templates/share.templ web/templates/share_templ.go web/templates/drive_test.go
git commit -m "feat(share): page shared folders and search locally"
```

---

### Task 7: Global Search Page And Result Paths

**Files:**
- Modify: `internal/controller/web/response/drive.go`
- Create: `internal/controller/web/search.go`
- Modify: `internal/controller/web/router.go:66-78`
- Create: `internal/controller/web/search_test.go`
- Modify: `web/templates/titles.go`
- Modify: `web/templates/drive.templ`
- Modify: `web/templates/drive_templ.go`
- Modify: `web/templates/drive_test.go`

**Interfaces:**
- Consumes: `Folder.Search(ctx, userID, search, from, after)`, `entity.SearchView`.
- Produces:
  - `response.Drive.SearchMode`, `ShowPath`, `ShortSearch`, `From`, `ClearHref`
  - `func NewSearch(view *entity.SearchView) Drive`
  - `templ Search(view View, title string, page response.Drive)`
  - `templ SearchContent(page response.Drive)`
  - `templ SearchEntryRows(page response.Drive)`
  - `const templates.SearchTitle = "Search"`
  - `func (c *Controller) searchPage(ctx *fiber.Ctx) error`
  - `func (c *Controller) searchEntries(ctx *fiber.Ctx) error`
  - Routes:
    - `GET /search`
    - `GET /htmx/search`

- [ ] **Step 1: Write the failing tests**

Add response tests:

- `TestNewSearchUsesASingleSearchCrumb` — expect one crumb named `Search`,
  current.
- `TestNewSearchShowsThePathAndTheContainingFolder` — a file row's
  `PathHref == "/folders/<folderID>"`, a folder row's `PathHref` points at that
  folder, and the path starts with `Home`.
- `TestNewSearchBuildsTheNextHref` — expect `/htmx/search` with `q` and `after`.
- `TestNewSearchSetsShortQuery` — a two-character view sets the prompt flag.

Add template tests: search page has a `Location` header, no action drawer for a
file row, and a file location is a link to `/folders/<folderID>`.

Add controller tests:

- `TestSearchPageRendersThePromptForAShortQuery` — q `ab`; fake `Search` is
  called or the view is short; no result table.
- `TestSearchScopeFolderRedirectsBack` — `/search?q=note&from=folder-1&scope=folder`
  redirects to `/folders/folder-1?q=note`.
- `TestSearchPartialRejectsABadCursor` — 400.
- `TestSearchPassesTheOwnerAndFromToTheUseCase` — fake records
  `userID == "user-1"`, `search == "note"`, `from == "folder-1"`.

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/controller/web/ ./web/templates/ -run 'Search' -count=1`

Expected: build failure for missing response helpers, template and handler.

- [ ] **Step 3: Map the search page**

Extend `response.Drive` with the search flags and `ClearHref`. Add
`NewSearch(view)`:

- crumb: one current `Search` crumb with href `/search`;
- `SearchMode: true`, `ShowPath: true`;
- `From`, `Search`, `ShortSearch` (from `view.TooShort`);
- folders and files mapped with `Path` and `PathHref`;
- `SearchAction: "/search"`, `ClearHref` to `/folders/<from>` or `/`;
- `NextURL` to `/htmx/search?q=...&from=...&after=...`.

Add a path label helper that maps the root `/` to `Home` and joins names with
` / `. Add `templates.SearchTitle`.

- [ ] **Step 4: Add the search templates**

In `web/templates/drive.templ`, add:

- `Search(view, title, page)` using `Layout` and `SearchContent`;
- `SearchContent` with a `Search` breadcrumb, the search form (scope `all`, with
  a hidden `from` and a `This folder` option when `From` is set), a `Clear`
  link, and the entries table;
- `SearchEntryRows` delegating to the shared rows and sentinel;
- a `Location` column only when `ShowPath` is set. The search table has Name,
  Location, Size and Modified, so its sentinel spans four columns.

In search mode a file row links to `/files/<id>/download`, has no action button,
and its location links to `PathHref`. A folder row keeps its open-folder link.

- [ ] **Step 5: Add the handlers and routes**

Create `internal/controller/web/search.go`:

- `searchPage`: require user, read `q` and `from`, validate `from` as a UUID
  (otherwise empty), handle `scope=folder` with a valid `from` by redirecting to
  the folder with the same `q`, call `c.folders.Search(ctx.UserContext(), userID,
  q, from, nil)`, map with `response.NewSearch`, and render full/fragment via
  `c.render`.
- `searchEntries`: require user, reject an empty/malformed `after` with 400,
  call `Search` with the cursor, and render `templates.SearchEntryRows`.

Register `GET /search` with `middleware.RequireAuth(u)` and
`GET /htmx/search` with the same guard.

- [ ] **Step 6: Run the tests**

Run: `make templ-generate && go test ./internal/controller/web/... ./web/templates/ -count=1`

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add internal/controller/web/search.go internal/controller/web/search_test.go internal/controller/web/response/drive.go internal/controller/web/router.go web/templates/titles.go web/templates/drive.templ web/templates/drive_templ.go web/templates/drive_test.go
git commit -m "feat(search): add drive-wide name search"
```

---

### Task 8: Documentation, Gates, And Manual Verification

**Files:**
- Modify: `AGENTS.md` (upload/listing/search notes)
- Modify: `docs/schema.sql` if Task 2's dump changed again
- Modify: `docs/superpowers/specs/2026-11-21-folder-browse-and-search-design.md`
  only if an implementation choice differs from a call recorded there

- [ ] **Step 1: Update the project notes**

Add a short section to `AGENTS.md`:

- folder pages load 100 entries, folders first, keyset `after`, "Load more";
- `q` searches direct children; `/search` is own-drive only and needs three
  characters;
- shared pages have local search only;
- `pg_trgm` is a deployment requirement and the migration installs it;
- Home is `/` and used for its local search;
- global results are read-only and link file locations to their folder.

- [ ] **Step 2: Run the full local gate**

Run: `make pre-commit`

Expected: modules verify, templ generates, assets and CSS build, format runs,
schema writes, and `golangci-lint` prints `0 issues.`

- [ ] **Step 3: Run all tests**

Run: `go test ./... -count=1`

Expected: every package passes.

- [ ] **Step 4: Re-run migrations and schema**

Run: `make migrate-down`, `make migrate-up`, `make schema`, `make migrate-status`

Expected: down leaves the schema without the new indexes but with `pg_trgm`;
up restores them; `docs/schema.sql` matches the live schema.

- [ ] **Step 5: Manual two-account and browser checks**

With `make compose-up` and `make dev`:

- seed one account with 150 entries in a folder and a second account with a
  distinct name; search as the first and confirm the second account's row never
  appears;
- load a folder, scroll the load-more row, and confirm the next 100 are appended
  exactly once;
- search locally for a name and confirm the breadcrumb and URL folder stay;
- switch to `All files`, open a folder result, download a file result, use its
  location link to reach the containing folder;
- clear search and confirm it returns to the source folder or Home;
- rename a file while a local `q` is active and confirm the input and rows agree;
- open a share in a private window, search locally, load more, and download;
- confirm `EXPLAIN (ANALYZE, BUFFERS)` uses the trigram index for a three-character
  global search.

- [ ] **Step 6: Commit**

```bash
git add AGENTS.md docs/schema.sql docs/superpowers/specs/2026-11-21-folder-browse-and-search-design.md
git commit -m "docs: describe folder paging and search"
```

---

## Self-Review

- Every spec section maps to a task: entity primitives (1), repo/indexes (2),
  merge/search (3), bounded page (4), local search and mutations (5), shared (6),
  global (7), docs/gates (8).
- Type names are consistent: `EntryCursor`, `CleanSearch`, `SearchPattern`,
  `ListPage`, `Paths`, `FolderView.Search/Next`, `SearchView.TooShort`,
  `Drive.SearchMode/ShowPath/NextURL`, `SearchTitle`.
- Every Review Focus line has a test in its owning task; the two-account check is
  both a use-case assertion and a manual browser step.
- The plan leaves implementation choices (SQL builder details, templ layout,
  JavaScript URL construction) to the builder while pinning the public names,
  values and behavior.
