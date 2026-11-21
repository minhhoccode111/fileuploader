package response

import (
	"net/url"
	"strings"
	"testing"

	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// The drawer keeps the raw byte count beside the worded size, so the download
// link can decide whether the page may buffer the file or must hand it to the
// browser.
func TestNewFileInfoKeepsTheRawByteCount(t *testing.T) {
	t.Parallel()

	file := entity.File{
		ID:   "file",
		Name: "holiday.mp4",
		Size: 7 << 20,
	}

	info := NewFileInfo(&file)

	if info.Bytes != file.Size {
		t.Errorf("NewFileInfo() bytes = %d, want %d", info.Bytes, file.Size)
	}

	if info.Size != ByteLabel(file.Size) {
		t.Errorf("NewFileInfo() size = %q, want %q", info.Size, ByteLabel(file.Size))
	}
}

// The next page of a filtered listing is the partial route with the search and
// the cursor of the last row, both escaped for the address bar.
func TestNewDriveBuildsTheNextHref(t *testing.T) {
	t.Parallel()

	next := entity.EntryCursor{Kind: entity.EntryKindFile, Name: "notes.txt", ID: "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44"}
	page := NewDrive(&entity.FolderView{
		Folder: entity.Folder{ID: "folder-1", Name: "/"},
		Search: "no te",
		Next:   &next,
	})

	if !strings.Contains(page.NextURL, "q=no+te") || !strings.Contains(page.NextURL, "after=") {
		t.Errorf("NextURL = %q, want q and after", page.NextURL)
	}

	if !strings.HasPrefix(page.NextURL, "/htmx/folders/folder-1/entries?") {
		t.Errorf("NextURL = %q, want the partial route of the folder", page.NextURL)
	}

	parsed, err := url.Parse(page.NextURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v, want nil", err)
	}

	after, err := entity.DecodeEntryCursor(parsed.Query().Get("after"))
	if err != nil {
		t.Fatalf("DecodeEntryCursor() error = %v, want nil", err)
	}

	if after == nil || *after != next {
		t.Errorf("after = %v, want %v", after, next)
	}

	if got := parsed.Query().Get("q"); got != "no te" {
		t.Errorf("q = %q, want %q", got, "no te")
	}
}

// The search page is a state of its own, so it carries one current Search
// crumb instead of the path of a folder.
func TestNewSearchUsesASingleSearchCrumb(t *testing.T) {
	t.Parallel()

	page := NewSearch(&entity.SearchView{Search: "note"})

	if len(page.Breadcrumb) != 1 {
		t.Fatalf("Breadcrumb length = %d, want 1", len(page.Breadcrumb))
	}

	crumb := page.Breadcrumb[0]
	if crumb.Name != "Search" {
		t.Errorf("crumb name = %q, want %q", crumb.Name, "Search")
	}

	if !crumb.Current {
		t.Error("crumb is not current, want the only crumb of the page to be current")
	}

	if crumb.Href != "/search" {
		t.Errorf("crumb href = %q, want %q", crumb.Href, "/search")
	}
}

// Every global result shows where it lives, root first with Home for the root.
// A file links to the folder that holds it; a folder links to itself, because
// the whole row already opens it.
func TestNewSearchShowsThePathAndTheContainingFolder(t *testing.T) {
	t.Parallel()

	page := NewSearch(&entity.SearchView{
		Search: "note",
		Folders: []entity.FolderItem{{
			ID:   "folder-2",
			Name: "Notes",
			Path: []entity.FolderRef{{ID: "root", Name: "/"}, {ID: "folder-2", Name: "Notes"}},
		}},
		Files: []entity.FileItem{{
			ID:       "file-1",
			FolderID: "folder-1",
			Name:     "note.txt",
			Path:     []entity.FolderRef{{ID: "root", Name: "/"}, {ID: "folder-1", Name: "Docs"}},
		}},
	})

	if !page.ShowPath {
		t.Error("ShowPath = false, want the search page to render the location column")
	}

	if got := page.Files[0].Path; got != "Home / Docs" {
		t.Errorf("file path = %q, want %q", got, "Home / Docs")
	}

	if got := page.Files[0].PathHref; got != "/folders/folder-1" {
		t.Errorf("file PathHref = %q, want %q", got, "/folders/folder-1")
	}

	if got := page.Folders[0].Path; got != "Home / Notes" {
		t.Errorf("folder path = %q, want %q", got, "Home / Notes")
	}

	if got := page.Folders[0].PathHref; got != "/folders/folder-2" {
		t.Errorf("folder PathHref = %q, want %q", got, "/folders/folder-2")
	}
}

// The next page of a search is the search partial under the same term and the
// folder the search came from, so a cleared scope link still knows the way back.
func TestNewSearchBuildsTheNextHref(t *testing.T) {
	t.Parallel()

	next := entity.EntryCursor{Kind: entity.EntryKindFile, Name: "notes.txt", ID: "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44"}
	page := NewSearch(&entity.SearchView{
		Search: "no te",
		From:   "folder-1",
		Next:   &next,
	})

	if !strings.HasPrefix(page.NextURL, "/htmx/search?") {
		t.Fatalf("NextURL = %q, want the search partial route", page.NextURL)
	}

	parsed, err := url.Parse(page.NextURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v, want nil", err)
	}

	if got := parsed.Query().Get("q"); got != "no te" {
		t.Errorf("q = %q, want %q", got, "no te")
	}

	if got := parsed.Query().Get("from"); got != "folder-1" {
		t.Errorf("from = %q, want %q", got, "folder-1")
	}

	after, err := entity.DecodeEntryCursor(parsed.Query().Get("after"))
	if err != nil {
		t.Fatalf("DecodeEntryCursor() error = %v, want nil", err)
	}

	if after == nil || *after != next {
		t.Errorf("after = %v, want %v", after, next)
	}

	if got := page.ClearHref; got != "/folders/folder-1" {
		t.Errorf("ClearHref = %q, want %q", got, "/folders/folder-1")
	}
}

// A term too short to run shows the prompt, and the clear action falls back to
// Home because the search named no folder.
func TestNewSearchSetsShortQuery(t *testing.T) {
	t.Parallel()

	page := NewSearch(&entity.SearchView{Search: "ab", TooShort: true})

	if !page.ShortSearch {
		t.Error("ShortSearch = false, want the prompt for a two-character term")
	}

	if !page.SearchMode {
		t.Error("SearchMode = false, want the search page to render its own rows")
	}

	if got := page.ClearHref; got != "/" {
		t.Errorf("ClearHref = %q, want %q", got, "/")
	}
}

// A listing with no further page carries no next link, so the template renders
// no sentinel.
func TestNewDriveOmitsTheNextHrefAtTheEnd(t *testing.T) {
	t.Parallel()

	page := NewDrive(&entity.FolderView{Folder: entity.Folder{ID: "folder-1", Name: "/"}})

	if page.NextURL != "" {
		t.Errorf("NextURL = %q, want empty", page.NextURL)
	}

	if page.Search != "" {
		t.Errorf("Search = %q, want empty", page.Search)
	}
}

// A path the walk could not resolve still renders a location: a result no
// longer anchored in the drive falls back to Home instead of an empty cell.
func TestNewSearchFallsBackToHomeWhenThePathIsMissing(t *testing.T) {
	t.Parallel()

	page := NewSearch(&entity.SearchView{
		Search:  "note",
		Folders: []entity.FolderItem{{ID: "folder-2", Name: "Notes"}},
		Files:   []entity.FileItem{{ID: "file-1", FolderID: "folder-1", Name: "note.txt"}},
	})

	if got := page.Folders[0].Path; got != "Home" {
		t.Errorf("folder path = %q, want %q", got, "Home")
	}

	if got := page.Files[0].Path; got != "Home" {
		t.Errorf("file path = %q, want %q", got, "Home")
	}
}
