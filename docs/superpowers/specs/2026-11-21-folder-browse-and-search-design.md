# Folder Browsing, Pagination and Search

## Intent

Every folder page currently loads every folder and file directly inside it. There
is no bound on that list. Storage quota does not bound it either: a zero-byte file
reserves no quota, and folder creation has no limiter. One account can therefore
put an unbounded number of rows into one folder, and every visit loads them all,
counts each subfolder with correlated subqueries, renders them all, and ships them
all to the browser.

This design makes folder content browsing bounded. It also adds the two ways a
normal file manager lets a person deal with a large folder: scroll for more, and
search by name. Search works in the current folder only, or across the account's
own drive, as the visitor chooses.

## Goals

- Bound every folder-content response to a fixed number of entries.
- Keep the browse order stable: folders first, then files, each by
  case-insensitive name with the identifier as a tie-breaker.
- Let a visitor load the next batch without losing the folder or the search.
- Search the direct children of the current folder, case-insensitively, by name.
- Search the whole own drive from a dedicated search state.
- Show the location of every global search result.
- Keep local search from changing the breadcrumb or the folder context.
- Apply the same bounded listing and local search to shared folder pages.
- Keep all reads scoped to the account that owns the content.
- Make the "load more" boundary visible: never silently truncate a list.
- Add the indexes global search needs.

## Non-Goals

- Sorting controls. Name order remains the only order.
- Searching shared-with-me content in the global search.
- Searching file contents, metadata, or paths, only names.
- Fuzzy matching, relevance ranking, saved searches or search history.
- Real-time updates while another tab changes the folder.
- Rename, move, delete or share actions from global search results. Those happen
  after opening the containing folder.
- Numbered pages, total page counts or jumping to a page number.
- Paginating the destination picker and folder tree (`ListAll`). Those load every
  folder of the account and are a separate unbounded read.
- Changes to the REST API.
- A new client-side framework or a JavaScript search-as-you-type path.

## Anti-Goals

- Any response that grows with the number of entries in a folder.
- Offset pagination that slows down with depth and skips or repeats rows when an
  upload or rename moves the order.
- Silently dropping rows, or a "load more" that returns nothing because the
  cursor was refused.
- Global search results without a location.
- A global search that lists the entire drive when the query is empty.
- A search that can return another account's rows.
- A local search that rewrites the breadcrumb into a fake "search folder".

## Constraints

- Postgres is the only store for listing. Redis and MinIO are not involved.
- Queries are built with Squirrel, except the existing recursive CTE pattern in
  `internal/repo/persistent/folder/folder.go`.
- The page is server-rendered with templ and htmx, with Alpine only for small
  local state.
- The generated `*_templ.go` files are committed.
- The linter is strict (`funlen`, `gocyclo`, `gocognit`, `mnd`, `err113`,
  `wsl_v5`, `nlreturn`). New code follows the existing sentinel and wrapping
  style.
- Folder and file names are at most 255 characters, are valid UTF-8 and reject
  path separators, control characters, invisible format characters and Windows
  device names (`entity.CleanName`).
- The root folder's stored name is `/`; the UI calls it Home.

## Decisions Made By The User

- Local search covers direct children only. It does not search the subtree.
- Local search must not disturb the breadcrumb or other folder context.
- Global search covers the whole drive.
- The visitor must be able to either browse the whole folder or find a known
  name, like a normal file manager.
- The search scope has a visible choice between the current folder and the whole
  drive.

## Decisions Made Here

These are calls made while writing the design. They are listed so they can be
checked and overridden before planning.

- Batch size is 100 entries per request. No client-supplied page size.
- Browsing uses keyset pagination, never `OFFSET`.
- "Load more" is a button that also fires when scrolled into view. It is not
  numbered pagination.
- Local search is a `q` parameter on the folder route; the breadcrumb stays.
- Global search is a separate `/search` state with one `Search` breadcrumb and
  a location for every result.
- Global search covers the account's own drive only; shared content stays out.
- Global search needs at least three characters. A two-character substring
  cannot use a trigram index either, so the floor matches what the index can
  serve. Local search needs one character and is bounded to one folder.
- Search is submitted, not run on each keystroke.
- Matching is a case-insensitive substring of the name.
- The search term and the current folder survive scope changes and form
  submission through ordinary query parameters.
- Global search results are read-only: a folder opens, a file downloads, and a
  file's location links to its containing folder. Actions live in the folder
  page.
- A mutation on a folder page keeps the active `q` and refreshes the first
  filtered batch. A changed row that no longer matches leaves the view.
- Home is `/`, keeps its URL for local search, and does not pass `from` to a
  global search. A global search started from Home therefore has no
  `This folder` option and returns Home when cleared.
- Shared folder pages get bounded listing and local search, but no global search.
- Paths are resolved for the current page only, in one extra query.
- `pg_trgm` is required for production-scale substring search.
- The folder tree and destination picker stay as they are for this project.

## Behavior

### Browsing a folder

`GET /folders/:folder` loads the folder, its breadcrumb, and the first batch of
direct children. Home is `GET /` and renders the root folder at the same URL,
with the same first batch.

Folders come first, then files, each ordered by `lower(name)`, then `id`.

When more entries exist, the table ends with a "Load more" row. Clicking it, or
scrolling it into view, asks the partial endpoint for the next batch and appends
the rows in place. The browser URL does not carry the cursor. Refreshing the page
or opening a bookmark starts again at the first batch, which is the normal
"load more" contract.

An empty folder says so. A search with no matches says that instead. A batch that
is cut off always shows a way to continue.

### Local search

A search form sits above the entries. It carries the current `q` and a scope
choice:

- `This folder` submits to the current folder route: `/folders/:folder?q=...`,
  or `/?q=...` on Home.
- `All files` submits to `/search?q=...&from=:folder`.

The local page renders the folder toolbar, breadcrumb and actions unchanged. Only
the entries reflect `q`. An empty `q` shows the full first batch. A query of one
character is accepted. Whitespace around the query is trimmed, invalid UTF-8 is
discarded, and the stored query is capped at 100 characters.

The form is a normal GET form with htmx enhancement, so it works without
JavaScript.

### Global search

`GET /search?q=...&from=:folder` renders a separate page state:

- The breadcrumb is a single `Search` crumb, not a path.
- The title is `Search`.
- The form keeps `q` and `from`.
- If `from` is present, the scope choice offers `This folder` and `All files`.
  Without `from`, only `All files` is offered.
- An empty query or a query shorter than three characters shows a prompt and
  does not query the database.
- Each result shows its location, rendered from the folder chain, root first,
  with Home for the root.
- A folder result opens that folder in the normal folder page. The search state
  is left behind.
- A file result downloads the file. It does not open the action drawer.
- A file's location is also a link to its containing folder, so a file found by
  search can be reached in its folder and acted on there.
- The form's submit button returns to the folder named by `from` when the scope
  is `This folder`; otherwise it stays on `/search`.
- A `Clear` link returns to the folder named by `from` with no query, or to Home
  when there is no `from`. Submitting an empty query is not the clear action; it
  shows the prompt.

`from` is validated as an identifier but is not trusted for authorization. It
only builds a link. If the folder is gone or belongs to someone else, opening it
follows the existing not-found behavior.

### Scope changes

Both forms carry a `scope` value. A folder request with `scope=all` redirects to
`/search` with the same `q` and `from=:folder`. A Home request with `scope=all`
redirects to `/search` with the same `q` and no `from`, because Home keeps its
URL and does not invent a root-folder address. A search request with
`scope=folder` and a valid `from` redirects to `/folders/:from` with the same
`q`. A search request with `scope=folder` and no valid `from` stays on search.
Redirects use `middleware.Redirect`, so htmx receives `HX-Location` and a normal
browser receives a 302.

This keeps the scope control working with plain HTML and without client-side URL
construction.

### Shared folders

`GET /share/:share` and `/share/:share/folders/:folder` use the same bounded
listing and local search. The share resolution already decides which folder the
visitor may see and which account owns it. The listing is still keyed by the
owner's user identifier, never by the visitor's. There is no global search on a
shared page, because a share grants one subtree, not a drive.

### Actions

Folder-page actions keep their current refresh target and their current result:
a create, rename, move, duplicate or delete answers with the first batch of the
folder. They now also keep the active local `q`, so the input, the scope and the
rows never disagree.

The `q` comes from htmx's `HX-Current-URL` header for an action form. Upload
completion is a plain `fetch`, so its script adds the current `q` to the complete
request. A changed row that no longer matches the query disappears from the
filtered list, which is the correct result.

Search results have no action drawer, so no action form targets a missing
element. Their file location links back to the containing folder, where the
normal actions are available.

## Data Model

All names below are proposals; the builder may rename them, but the shape and
meaning must stay.

```go
type EntryKind string

const (
    EntryKindFolder EntryKind = "folder"
    EntryKindFile   EntryKind = "file"
)

// EntryCursor is the last row of the previous batch. Name is the display name;
// the query compares lower(name) so every page uses the database's own
// case-folding.
type EntryCursor struct {
    Kind        EntryKind
    Name        string
    ID          string
    FoldersDone bool
}
```

`EntryCursor` encodes to one opaque URL value (JSON then base64url). Decoding a
malformed, oversized or wrong-kind value fails. A failed cursor on a partial
request is answered with 400 and no rows; it must never fall back to the first
batch and append duplicates. A failed cursor on a full page is ignored because
full pages always start at the first batch.

`FolderItem` gains an optional `Path []FolderRef`, filled only for global results.
`FileItem` gains `FolderID string` and the same optional `Path`. The path is the
chain from root to the folder whose contents the row lives in; for a folder row
it is the chain to that folder itself.

```go
type FolderView struct {
    Folder     Folder
    Breadcrumb []FolderRef
    Folders    []FolderItem
    Files      []FileItem
    Search     string
    Next       *EntryCursor
}

type SearchView struct {
    Folders []FolderItem
    Files   []FileItem
    Search  string
    From    string
    Next    *EntryCursor
}
```

The two slices stay separate because the order is always all folders, then all
files. Splitting the batch at that boundary loses nothing.

## Pagination Algorithm

The use case owns the merge of two repository pages. The repositories never
return more rows than asked, and the use case never loads a full folder.

1. Normalize the search term and decode the cursor.
2. If the cursor continues a file page, skip the folder query.
3. Otherwise ask the folder repository for at most `limit+1` folders after the
   cursor, or from the start.
4. If there are more than `limit` folders, return the first `limit` and set the
   next cursor to the last folder, `FoldersDone=false`.
5. Otherwise keep all folders and ask the file repository for
   `limit-len(folders)+1` files. Use the file cursor when the previous page ended
   in files, or start from the beginning when folders just ended.
6. If more files came back than fit, return the first that fit and set the next
   cursor to the last returned file.
7. If the page is exactly full of folders and there are more entries, one extra
   file is fetched only to learn that a next batch exists. It is not shown; the
   next cursor points at the last folder with `FoldersDone=true`.
8. No next cursor means the batch is the end of the list; no sentinel is shown.

This makes a page one or two queries, each with a hard limit. A page that is all
folders needs one. A page that crosses the folder/file boundary needs two.

### Cursor comparison

Each repository uses the same order and comparison:

```sql
ORDER BY lower(name) ASC, id ASC
```

and, when a cursor is present:

```sql
(lower(name), id) > ($name, $id)
```

The kind is not part of the SQL comparison because the two tables are queried
separately. The use case uses it to decide which table continues.

### Search predicate

The term is matched against the name only:

```sql
lower(name) LIKE '%' || lower($term) || '%' ESCAPE '\'
```

`%`, `_` and `\` in the visitor's term are escaped before they reach the
parameter. The term is trimmed, invalid UTF-8 is dropped, and it is capped at 100
characters. Local search runs from one character. Global search runs from three,
because a two-character substring cannot use the trigram index. The predicate is
applied inside each repository query, before the limit, so the database never
returns more matching rows than the batch needs.

## Repository Contracts

```go
type FolderRepo interface {
    // ... existing methods
    ListPage(ctx context.Context, userID, parentID, search string, after *entity.EntryCursor, limit int) ([]entity.FolderItem, error)
    Paths(ctx context.Context, userID string, folderIDs []string) (map[string][]entity.FolderRef, error)
}

type FileRepo interface {
    // ... existing methods
    ListPage(ctx context.Context, userID, folderID, search string, after *entity.EntryCursor, limit int) ([]entity.FileItem, error)
}
```

- An empty `parentID`/`folderID` means the whole own drive and is used only by
  global search. A non-empty value is always checked by the use case's folder
  read first.
- `ListPage` returns at most `limit` rows.
- `Paths` resolves many folders in one recursive query. It is called only for
  global search, with at most 100 identifiers. It returns root-first chains and
  omits identifiers the account does not own.
- `FolderRepo.List` is replaced by `ListPage`. `FileRepo.List` stays for the
  internal full-subtree loops used by zip download and duplicate; those are not
  folder browsing.

## Paths For Global Results

After a global batch is loaded, collect the folder identifiers needed:

- a folder result uses its own identifier;
- a file result uses its `FolderID`.

Resolve them in one query with a recursive CTE rooted at those folders, walking
to the root. Map the root's `/` to `Home` in the response layer. A missing path
(a folder deleted in another tab) renders Home rather than leaking or failing the
page.

## Routes

| Route                                            | Guard | Response                                                          |
| ------------------------------------------------ | ----- | ----------------------------------------------------------------- |
| `GET /`                                          | auth  | Full Home page, root folder; honors `q`                           |
| `GET /?scope=all`                                | auth  | Redirect to `/search` without `from`                              |
| `GET /folders/:folder`                           | auth  | Full folder page; honors `q`                                      |
| `GET /folders/:folder?scope=all`                 | auth  | Redirect to `/search` with `from=:folder`                         |
| `GET /search`                                    | auth  | Full search page; honors `q` and `from`                           |
| `GET /search?scope=folder&from=:folder`          | auth  | Redirect to `/folders/:folder`                                    |
| `GET /htmx/folders/:folder/entries`              | auth  | Entry rows and next sentinel; honors `q`, `after`                 |
| `GET /htmx/search`                               | auth  | Search result rows and next sentinel; honors `q`, `from`, `after` |
| `GET /share/:share`                              | share | Full shared folder page; honors `q`                               |
| `GET /share/:share/folders/:folder`              | share | Same, nested                                                      |
| `GET /htmx/share/:share/entries`                 | share | Shared entry rows and next sentinel                               |
| `GET /htmx/share/:share/folders/:folder/entries` | share | Same, nested                                                      |

The exact share partial paths may be collapsed into one handler, but the share
token must be resolved on every partial request. A partial endpoint never accepts
a bare owner identifier from the client.

The partial endpoints accept `after` only. They never accept a page size.

## Response And Templates

`response.Drive` and a new `response.Search` share the row types:

- `FolderRow` gains `Path string` and `PathHref string`.
- `FileRow` gains `Path string` and `PathHref string`, the link to the file's
  containing folder.
- Both pages gain `Next string` (the encoded cursor, empty when done) and
  `Search string`.
- `Search` carries `From string` and a `SearchMode` flag. In search mode a file
  row links to its download and omits the action drawer; a folder row still opens
  the folder.
- A `Location` column appears only on the search page, and its text is a link:
  a folder result links to that folder, a file result links to its containing
  folder. Path text is escaped by templ; names cannot contain `/`, so a simple
  `/` separator is safe.

`DriveEntries` renders the table, the rows and the sentinel. A smaller
`EntryRows` component renders only rows and the sentinel, for the partial
endpoints. The sentinel is a row with a "Load more" button carrying
`hx-get` to the matching partial endpoint and `hx-trigger="click, revealed"`.
The response replaces the sentinel row, so appended rows arrive in order.

Empty-state copy:

- folder, no query: "This folder is empty."
- local query, no matches: "No matches in this folder."
- global query, no matches: "No files match that search."
- global, query too short: "Type at least three characters to search your drive."

## Indexes And Scale

Migration `20261121..._folder_browse_indexes`:

- `CREATE EXTENSION IF NOT EXISTS pg_trgm;`
- `folders_user_name_idx ON folders (user_id, lower(name), id)`
- `files_user_name_idx ON files (user_id, lower(name), id)`
- `folders_name_trgm_idx ON folders USING gin (lower(name) gin_trgm_ops)`
- `files_name_trgm_idx ON files USING gin (lower(name) gin_trgm_ops)`

The existing `(user_id, parent_id, lower(name))` and
`(folder_id, lower(name))` indexes continue to serve local listing and folder
counts. The new user-name indexes serve global ordering; the trigram indexes
serve substring search. Both are needed because an ordered scan and a substring
match use different access paths.

The down migration drops the four indexes and the extension. If a deployment
already used `pg_trgm` for something else, the down migration must not drop the
extension; the builder should check ownership or leave the extension in place.
The migration must be tested up and down against the dev database, and
`docs/schema.sql` regenerated.

Manual scale check, recorded in the plan's verification notes:

- seed one account with 100,000 files in one folder and 1,000 subfolders;
- load the folder page and confirm the response carries 100 rows;
- run a local one-character search and a global three-character search;
- run `EXPLAIN (ANALYZE, BUFFERS)` on both and confirm the trigram index is used
  for the global substring search and the folder index for the local one;
- confirm the page does not fetch all rows.

## Security And Correctness

- Every listing, search and path query filters by the owning `user_id`.
- The shared route uses the owner identifier resolved from the share, never a
  client-supplied one.
- Search terms are always bound parameters; LIKE metacharacters are escaped.
- The response size is bounded by the batch size, independent of data volume.
- Cursors are opaque but not trusted. A tampered cursor can only move the
  visitor within their own result set; it cannot change authorization, the
  search term, the folder or the batch size.
- A malformed cursor on "load more" is a 400, not a silent restart.
- Global search never includes shared content and never searches another account.
- An empty or too-short global query does not run a query or list the drive.
- Names in paths are rendered through templ, so they cannot inject HTML.
- No action form is rendered on the search page, so no form targets a missing
  element.

## Testing

Entity:

- cursor round trip, malformed values, oversized values, wrong kind;
- search trimming, rune cap, invalid UTF-8, LIKE escaping;
- empty global query, one-character global query and two-character global query
  produce no repository call.

Use case:

- first page with folders only and more folders;
- page crossing from folders to files with no duplicate or skipped row;
- page exactly full of folders with files behind it;
- page ending exactly at the end of the data, no sentinel;
- cursor continuing a file page skips the folder query;
- local search passes the folder identifier; global passes none;
- a local search with `q` keeps the term through a mutation and filters the
  returned batch;
- paths are filled for global results and not for local;
- a missing path falls back to Home;
- shared listing uses the owner identifier.

Repository:

- query builder tests for order, cursor condition and escaped search predicate;
- live integration check (or the existing manual database check) for `ListPage`,
  `Paths`, and the migration.

Controller:

- `/` and `/folders/:id` both accept `q` and render the filtered first batch;
- Home's `scope=all` redirects to `/search` without `from`;
- `/search` requires auth and renders the prompt for an empty or two-character
  query, with no repository call;
- `q` survives a render and is escaped;
- scope redirects in both directions, including no `from`;
- invalid `from` does not redirect to an arbitrary folder;
- partial load-more with a bad cursor returns 400 and no rows;
- folder and share partials work without a session where the share allows it;
- a mutation request whose `HX-Current-URL` carries `q` returns filtered rows and
  leaves the search form consistent.

Templates:

- sentinel appears only when `Next` is set;
- the search page shows Location and no action drawer;
- a file row's Location is a link to its containing folder;
- the folder page shows no Location and keeps the drawer;
- empty states differ for full, local search and global search;
- the global page offers a Clear link to `from`, or Home when there is no
  `from`, and submitting an empty query does not act as Clear.

Regression:

- `go test ./...`
- `make format`
- `make linter-golangci`
- `make pre-commit`
- migrate up, down, up on the dev database
- browser smoke test: browse a folder with more than 100 entries, load more,
  local search, global search, open a folder result, download a file result,
  clear search, open a share and repeat local search.

## Rollout

The migration is additive; existing pages keep working while the new columns and
indexes are unpopulated. No feature flag is needed. The new routes are additions.
A deployment without `pg_trgm` fails at migration time, so the plan must state
that requirement in the migration and the README/AGENTS notes.
