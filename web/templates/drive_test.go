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
		`hx-trigger="click, revealed"`,
		`colspan="4"`,
	} {
		if !strings.Contains(html, part) {
			t.Errorf("DriveEntries() = %s, want it to contain %q", html, part)
		}
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

// renderTemplate renders one component into its HTML text.
func renderTemplate(t *testing.T, component templ.Component) string {
	t.Helper()

	var buf bytes.Buffer

	if err := component.Render(t.Context(), &buf); err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	return buf.String()
}
