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
