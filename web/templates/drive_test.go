package templates

import (
	"bytes"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
)

// The drawer hands the raw byte count to the download link, so the page can
// decide between its progress bar and the browser's own download.
func TestFileDrawerPassesTheRawSizeToTheDownloadLink(t *testing.T) {
	t.Parallel()

	const size = int64(42) << 20

	info := response.FileInfo{
		ID:    "file",
		Name:  "big.iso",
		Type:  "application/octet-stream",
		Size:  "42 MB",
		Bytes: size,
	}

	var buf bytes.Buffer

	err := FileDrawerContent(info).Render(t.Context(), &buf)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	html := buf.String()

	if !strings.Contains(html, `data-download-size="44040192"`) {
		t.Errorf("FileDrawerContent() = %s, want the raw size on the download link", html)
	}

	if !strings.Contains(
		html,
		"downloadWithProgress(this.href, this.dataset.downloadName, this.dataset.downloadSize)",
	) {
		t.Error("FileDrawerContent() did not pass the raw size to downloadWithProgress")
	}
}

// The table ends with a load-more sentinel only while a next page exists. The
// sentinel names the partial route and replaces itself with the rows it loads.
func TestDriveEntriesShowsLoadMoreOnlyWithANextURL(t *testing.T) {
	t.Parallel()

	page := response.Drive{
		FolderID: "folder-1",
		Folders:  []response.FolderRow{{ID: "f", Name: "Docs", Href: "/folders/f", Items: "0", Updated: "now"}},
		NextURL:  "/htmx/folders/folder-1/entries?after=abc",
	}

	html := renderTemplate(t, DriveEntries(page))

	for _, part := range []string{
		"Load more",
		`hx-get="/htmx/folders/folder-1/entries?after=abc"`,
		`hx-target="closest tr"`,
		`hx-swap="outerHTML"`,
		`hx-sync="this:drop"`,
		`hx-trigger="click, revealed"`,
		`colspan="4"`,
	} {
		if !strings.Contains(html, part) {
			t.Errorf("DriveEntries() = %s, want it to contain %q", html, part)
		}
	}

	// The search page reuses the same rows, so a sentinel id would appear
	// twice when a folder page and a search page are both loaded.
	if strings.Contains(html, `id="load-more"`) {
		t.Errorf("DriveEntries() = %s, want the sentinel to carry no page-wide id", html)
	}

	page.NextURL = ""

	if html := renderTemplate(t, DriveEntries(page)); strings.Contains(html, "Load more") {
		t.Errorf("DriveEntries() = %s, want no load-more row at the end", html)
	}
}

// The partial response replaces the sentinel row, so it must hold rows only:
// no table and no body around them.
func TestEntryRowsRendersRowsWithoutTheTable(t *testing.T) {
	t.Parallel()

	page := response.Drive{
		FolderID: "folder-1",
		Folders:  []response.FolderRow{{ID: "f", Name: "Docs", Href: "/folders/f", Items: "0", Updated: "now"}},
		Files:    []response.FileRow{{ID: "file", Name: "notes.txt", Type: "text/plain", Size: "1 B", Updated: "now"}},
		NextURL:  "/htmx/folders/folder-1/entries?after=abc",
	}

	html := renderTemplate(t, EntryRows(page))

	for _, part := range []string{"<tr", "Docs", "notes.txt", "Load more"} {
		if !strings.Contains(html, part) {
			t.Errorf("EntryRows() = %s, want it to contain %q", html, part)
		}
	}

	for _, part := range []string{"<table", "<tbody", "<thead"} {
		if strings.Contains(html, part) {
			t.Errorf("EntryRows() = %s, want no %q in a partial response", html, part)
		}
	}
}

// The local search form carries the term and the scope choice, so submitting
// it either narrows the folder on the screen or leaves for the whole drive.
func TestEntrySearchFormCarriesTheTermAndTheScopes(t *testing.T) {
	t.Parallel()

	page := response.Drive{Search: "note", SearchAction: "/folders/folder-1"}

	html := renderTemplate(t, EntrySearchForm(page))

	for _, part := range []string{
		`name="q"`,
		`value="note"`,
		`maxlength="100"`,
		`name="scope"`,
		`value="folder"`,
		`This folder`,
		`value="all"`,
		`All files`,
		`action="/folders/folder-1"`,
		`hx-get="/folders/folder-1"`,
		`hx-target="#page-content"`,
		`hx-swap="innerHTML"`,
		`hx-push-url="true"`,
	} {
		if !strings.Contains(html, part) {
			t.Errorf("EntrySearchForm() = %s, want it to contain %q", html, part)
		}
	}
}

// A filtered folder with no hits is not an empty folder, so the two states say
// different things to the visitor.
func TestDriveEntriesWordsTheEmptyStateOfAFilteredFolder(t *testing.T) {
	t.Parallel()

	filtered := renderTemplate(t, DriveEntries(response.Drive{Search: "note"}))
	if !strings.Contains(filtered, "No matches in this folder.") {
		t.Errorf("DriveEntries(filtered) = %s, want the no-match copy", filtered)
	}

	if strings.Contains(filtered, "This folder is empty.") {
		t.Errorf("DriveEntries(filtered) = %s, want no empty-folder copy", filtered)
	}

	unfiltered := renderTemplate(t, DriveEntries(response.Drive{}))
	if !strings.Contains(unfiltered, "This folder is empty.") {
		t.Errorf("DriveEntries(unfiltered) = %s, want the empty-folder copy", unfiltered)
	}
}

// The shared table ends with a load-more sentinel only while a next page
// exists. The sentinel names the share partial and spans its three columns.
func TestSharedEntryRowsShowsLoadMoreOnlyWithANextURL(t *testing.T) {
	t.Parallel()

	page := response.SharedDrive{
		ShareID: "share-1",
		Folders: []response.SharedFolderRow{{
			ID:      "folder-1",
			Name:    "Docs",
			Href:    "/share/share-1/folders/folder-1",
			Items:   "0 items",
			Updated: "now",
		}},
		Files: []response.SharedFileRow{{
			ID:       "file-1",
			Name:     "notes.txt",
			Type:     "text/plain",
			Size:     "1 B",
			Updated:  "now",
			Download: "/share/share-1/files/file-1/download",
		}},
		NextURL: "/htmx/share/share-1/entries?after=abc",
	}

	html := renderTemplate(t, SharedEntryRows(page))

	for _, part := range []string{
		"<tr",
		"Docs",
		"notes.txt",
		"Load more",
		`hx-get="/htmx/share/share-1/entries?after=abc"`,
		`hx-target="closest tr"`,
		`hx-swap="outerHTML"`,
		`hx-sync="this:drop"`,
		`hx-trigger="click, revealed"`,
		`colspan="3"`,
	} {
		if !strings.Contains(html, part) {
			t.Errorf("SharedEntryRows() = %s, want it to contain %q", html, part)
		}
	}

	if strings.Contains(html, `id="load-more"`) {
		t.Errorf("SharedEntryRows() = %s, want the sentinel to carry no page-wide id", html)
	}

	page.NextURL = ""

	if html := renderTemplate(t, SharedEntryRows(page)); strings.Contains(html, "Load more") {
		t.Errorf("SharedEntryRows() = %s, want no load-more row at the end", html)
	}
}

// A filtered shared folder with no hits is not an empty folder, so its copy
// matches the owner's filtered listing.
func TestSharedDriveContentWordsTheEmptyStateOfAFilteredFolder(t *testing.T) {
	t.Parallel()

	filtered := renderTemplate(t, SharedDriveContent(response.SharedDrive{Search: "note"}))
	if !strings.Contains(filtered, "No matches in this folder.") {
		t.Errorf("SharedDriveContent(filtered) = %s, want the no-match copy", filtered)
	}

	if strings.Contains(filtered, "This shared folder is empty.") {
		t.Errorf("SharedDriveContent(filtered) = %s, want no empty-folder copy", filtered)
	}

	unfiltered := renderTemplate(t, SharedDriveContent(response.SharedDrive{}))
	if !strings.Contains(unfiltered, "This shared folder is empty.") {
		t.Errorf("SharedDriveContent(unfiltered) = %s, want the empty-folder copy", unfiltered)
	}
}

// The shared page narrows the folder on the screen only: its search form
// carries the term on the address of the page, and offers no scope control
// that could leave for the owner's drive.
func TestSharedSearchFormCarriesTheTerm(t *testing.T) {
	t.Parallel()

	page := response.SharedDrive{
		ShareID:      "share-1",
		FolderID:     "folder-1",
		Search:       "note",
		SearchAction: "/share/share-1/folders/folder-1",
	}

	html := renderTemplate(t, SharedDriveContent(page))

	for _, part := range []string{
		`name="q"`,
		`value="note"`,
		`maxlength="100"`,
		`action="/share/share-1/folders/folder-1"`,
		`hx-get="/share/share-1/folders/folder-1"`,
		`hx-target="#page-content"`,
		`hx-swap="innerHTML"`,
		`hx-push-url="true"`,
	} {
		if !strings.Contains(html, part) {
			t.Errorf("SharedDriveContent() = %s, want it to contain %q", html, part)
		}
	}

	if strings.Contains(html, `name="scope"`) {
		t.Errorf("SharedDriveContent() = %s, want no scope control on a shared page", html)
	}
}

// The search page states the location of every result, so its table carries a
// Location column the folder table does not have.
func TestSearchContentShowsTheLocationColumn(t *testing.T) {
	t.Parallel()

	page := response.Drive{
		SearchMode:   true,
		ShowPath:     true,
		Search:       "note",
		SearchAction: "/search",
		Breadcrumb:   []response.Crumb{{Name: "Search", Href: "/search", Current: true}},
		Files: []response.FileRow{{
			ID:       "file-1",
			Name:     "notes.txt",
			Type:     "text/plain",
			Size:     "1 B",
			Updated:  "now",
			Path:     "Home / Docs",
			PathHref: "/folders/folder-1",
		}},
	}

	html := renderTemplate(t, SearchContent(page))

	for _, part := range []string{
		`<th>Location</th>`,
		`<th>Name</th>`,
		`aria-current="page"`,
		`Search</span>`,
	} {
		if !strings.Contains(html, part) {
			t.Errorf("SearchContent() = %s, want it to contain %q", html, part)
		}
	}
}

// The global search looks at folders and files, so its empty state names
// results instead of only files.
func TestSearchContentWordsTheEmptyState(t *testing.T) {
	t.Parallel()

	html := renderTemplate(t, SearchContent(response.Drive{
		SearchMode: true,
		Search:     "note",
		Breadcrumb: []response.Crumb{{Name: "Search", Href: "/search", Current: true}},
	}))

	if !strings.Contains(html, "No results match that search.") {
		t.Errorf("SearchContent() = %s, want the no-result copy", html)
	}

	if strings.Contains(html, "No files match that search.") {
		t.Errorf("SearchContent() = %s, want no file-only copy", html)
	}
}

// A search result is read-only: a file name downloads the file, its location
// links to the folder that holds it, and no action button opens a drawer.
func TestSearchEntryRowsLinkTheFileToItsFolderAndNotItsDrawer(t *testing.T) {
	t.Parallel()

	page := response.Drive{
		SearchMode: true,
		ShowPath:   true,
		Files: []response.FileRow{{
			ID:       "file-1",
			Name:     "notes.txt",
			Type:     "text/plain",
			Size:     "1 B",
			Updated:  "now",
			Path:     "Home / Docs",
			PathHref: "/folders/folder-1",
		}},
		NextURL: "/htmx/search?q=note&after=abc",
	}

	html := renderTemplate(t, SearchEntryRows(page))

	for _, part := range []string{
		`href="/files/file-1/download"`,
		`href="/folders/folder-1"`,
		`data-full-name="notes.txt"`,
		"Home / Docs",
		`hx-get="/htmx/search?q=note&amp;after=abc"`,
		`colspan="4"`,
	} {
		if !strings.Contains(html, part) {
			t.Errorf("SearchEntryRows() = %s, want it to contain %q", html, part)
		}
	}

	for _, part := range []string{
		`hx-get="/htmx/files/file-1"`,
		"Actions for",
	} {
		if strings.Contains(html, part) {
			t.Errorf("SearchEntryRows() = %s, want no %q in a read-only result", html, part)
		}
	}
}

// renderTemplate renders one component into its HTML text.
func renderTemplate(t *testing.T, component templ.Component) string {
	t.Helper()

	var buf bytes.Buffer

	if err := component.Render(t.Context(), &buf); err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	return buf.String()
}
