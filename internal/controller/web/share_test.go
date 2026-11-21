package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

const (
	// _shareTestShare is the token of the share a test browses.
	_shareTestShare = "8c1f3e2a-4b5d-4c6e-9f70-1a2b3c4d5e6f"

	// _shareTestOwner is the account the share belongs to. It is never the
	// account behind the request, so a listing that runs under the visitor
	// would be caught.
	_shareTestOwner = "550e8400-e29b-41d4-a716-446655440010"

	// _shareTestVisitor is the account signed in while browsing a share.
	_shareTestVisitor = "550e8400-e29b-41d4-a716-446655440012"

	// _shareTestRoot is the root folder a shared listing opens.
	_shareTestRoot = "550e8400-e29b-41d4-a716-446655440011"

	// _shareTestFolder is a nested folder of the share.
	_shareTestFolder = "b6a1d2c3-4e5f-4a6b-8c7d-9e0f1a2b3c4e"

	// _shareTestFile is a file inside a shared folder.
	_shareTestFile = "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e45"
)

// The next batch of a shared listing is the partial route of the share root:
// the cursor travels alone because the listing is not filtered.
func TestNewSharedDriveBuildsRootNextHref(t *testing.T) {
	t.Parallel()

	next := entity.EntryCursor{
		Kind: entity.EntryKindFile,
		Name: "notes.txt",
		ID:   _shareTestFile,
	}

	page := response.NewSharedDrive(_shareTestShare, _shareTestRoot, &entity.FolderView{
		Folder: entity.Folder{ID: _shareTestRoot, Name: "/"},
		Next:   &next,
	})

	if !strings.HasPrefix(page.NextURL, "/htmx/share/"+_shareTestShare+"/entries?") {
		t.Errorf("NextURL = %q, want the partial route of the share root", page.NextURL)
	}

	if page.SearchAction != "/share/"+_shareTestShare {
		t.Errorf("SearchAction = %q, want the share root page", page.SearchAction)
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
}

// A folder below the share root loads its next batch from its own nested
// partial route, and its search form submits to that same page.
func TestNewSharedDriveBuildsNestedNextHref(t *testing.T) {
	t.Parallel()

	parent := _shareTestRoot
	next := entity.EntryCursor{
		Kind: entity.EntryKindFolder,
		Name: "docs",
		ID:   _shareTestFolder,
	}

	page := response.NewSharedDrive(_shareTestShare, _shareTestRoot, &entity.FolderView{
		Folder: entity.Folder{ID: _shareTestFolder, ParentID: &parent, Name: "docs"},
		Next:   &next,
	})

	want := "/htmx/share/" + _shareTestShare + "/folders/" + _shareTestFolder + "/entries?"
	if !strings.HasPrefix(page.NextURL, want) {
		t.Errorf("NextURL = %q, want it to start with %q", page.NextURL, want)
	}

	wantAction := "/share/" + _shareTestShare + "/folders/" + _shareTestFolder
	if page.SearchAction != wantAction {
		t.Errorf("SearchAction = %q, want %q", page.SearchAction, wantAction)
	}
}

// A share usually opens a folder below the owner's drive root. The page on that
// folder is still the share root, so its search form and load-more link keep the
// canonical share routes instead of naming the folder twice.
func TestNewSharedDriveKeepsTheRootNextHrefForANonRootShare(t *testing.T) {
	t.Parallel()

	parent := _shareTestRoot
	next := entity.EntryCursor{
		Kind: entity.EntryKindFile,
		Name: "note.txt",
		ID:   _shareTestFile,
	}

	page := response.NewSharedDrive(_shareTestShare, _shareTestFolder, &entity.FolderView{
		Folder: entity.Folder{ID: _shareTestFolder, ParentID: &parent, Name: "docs"},
		Next:   &next,
	})

	want := "/htmx/share/" + _shareTestShare + "/entries?"
	if !strings.HasPrefix(page.NextURL, want) {
		t.Errorf("NextURL = %q, want it to start with %q", page.NextURL, want)
	}

	if page.SearchAction != "/share/"+_shareTestShare {
		t.Errorf("SearchAction = %q, want the share root page", page.SearchAction)
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
}

// The partial has to run the same filter as the page it extends: a next link
// without the term would append unfiltered rows under a filtered table.
func TestNewSharedDriveKeepsTheSearch(t *testing.T) {
	t.Parallel()

	next := entity.EntryCursor{
		Kind: entity.EntryKindFile,
		Name: "note.txt",
		ID:   _shareTestFile,
	}

	page := response.NewSharedDrive(_shareTestShare, _shareTestRoot, &entity.FolderView{
		Folder: entity.Folder{ID: _shareTestRoot, Name: "/"},
		Search: "note",
		Next:   &next,
	})

	parsed, err := url.Parse(page.NextURL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v, want nil", err)
	}

	if got := parsed.Query().Get("q"); got != "note" {
		t.Errorf("q = %q, want %q", got, "note")
	}

	if page.Search != "note" {
		t.Errorf("Search = %q, want %q", page.Search, "note")
	}
}

// A partial request must never guess at a page: a missing or tampered cursor is
// a client error, and neither the share nor the folder is read for it.
func TestSharedEntriesRejectsAMalformedCursor(t *testing.T) {
	t.Parallel()

	root := "/htmx/share/" + _shareTestShare + "/entries"
	nested := "/htmx/share/" + _shareTestShare + "/folders/" + _shareTestFolder + "/entries"

	cases := []struct {
		name string
		path string
	}{
		{name: "a missing cursor at the share root", path: root},
		{name: "a tampered cursor at the share root", path: root + "?after=not-a-cursor"},
		{name: "a tampered cursor of a nested folder", path: nested + "?after=not-a-cursor%7B"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			shares := &shareStub{}
			folders := &sharedFolderStub{}

			status, body := sharedResponse(t, sharedApp(shares, folders), tc.path)

			if status != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", status, http.StatusBadRequest)
			}

			if strings.Contains(body, "<tr") {
				t.Errorf("body = %q, want no rows", body)
			}

			if folders.calls != 0 {
				t.Errorf("List calls = %d, want 0", folders.calls)
			}

			if shares.resolves != 0 {
				t.Errorf("ResolveFolder calls = %d, want 0", shares.resolves)
			}
		})
	}
}

// A shared page narrows the direct children of the folder on the screen, and
// the listing runs under the share owner, never under the visitor.
func TestSharedFolderSearchPassesQToTheUseCase(t *testing.T) {
	t.Parallel()

	parent := _shareTestRoot

	shares := &shareStub{
		share:  entity.Share{ID: _shareTestShare, UserID: _shareTestOwner, FolderID: _shareTestRoot},
		folder: entity.Folder{ID: _shareTestFolder, ParentID: &parent, Name: "docs"},
	}
	folders := &sharedFolderStub{view: entity.FolderView{
		Folder: entity.Folder{ID: _shareTestFolder, ParentID: &parent, Name: "docs"},
	}}

	path := "/share/" + _shareTestShare + "/folders/" + _shareTestFolder + "?q=note"

	status, body := sharedResponse(t, sharedApp(shares, folders), path)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if folders.userID != _shareTestOwner {
		t.Errorf("userID = %q, want the share owner %q", folders.userID, _shareTestOwner)
	}

	if folders.userID == _shareTestVisitor {
		t.Errorf("userID = %q, want it not to be the visitor", folders.userID)
	}

	if folders.search != "note" {
		t.Errorf("search = %q, want %q", folders.search, "note")
	}

	for _, part := range []string{
		`value="note"`,
		`action="/share/` + _shareTestShare + `/folders/` + _shareTestFolder + `"`,
	} {
		if !strings.Contains(body, part) {
			t.Errorf("body = %q, want it to contain %q", body, part)
		}
	}
}

// The partial that follows a shared page belongs to that page: it lists the
// next batch of the same folder, under the same term, as rows alone.
func TestSharedEntriesKeepsTheSearchAndTheOwner(t *testing.T) {
	t.Parallel()

	next := entity.EntryCursor{
		Kind: entity.EntryKindFile,
		Name: "zeta.txt",
		ID:   _shareTestFile,
	}

	shares := &shareStub{
		share:  entity.Share{ID: _shareTestShare, UserID: _shareTestOwner, FolderID: _shareTestRoot},
		folder: entity.Folder{ID: _shareTestRoot, Name: "/"},
	}
	folders := &sharedFolderStub{view: entity.FolderView{
		Folder: entity.Folder{ID: _shareTestRoot, Name: "/"},
		Files: []entity.FileItem{{
			ID:          _shareTestFile,
			Name:        "note.txt",
			ContentType: "text/plain",
			Size:        5,
		}},
		Next: &next,
	}}

	cursor := entity.EntryCursor{
		Kind: entity.EntryKindFolder,
		Name: "docs",
		ID:   _shareTestFolder,
	}

	path := "/htmx/share/" + _shareTestShare + "/entries?q=note&after=" + cursor.Encode()

	status, body := sharedResponse(t, sharedApp(shares, folders), path)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if folders.userID != _shareTestOwner {
		t.Errorf("userID = %q, want the share owner %q", folders.userID, _shareTestOwner)
	}

	if folders.search != "note" {
		t.Errorf("search = %q, want %q", folders.search, "note")
	}

	if folders.after == nil || *folders.after != cursor {
		t.Errorf("after = %v, want %v", folders.after, cursor)
	}

	for _, part := range []string{"note.txt", "Load more"} {
		if !strings.Contains(body, part) {
			t.Errorf("body = %q, want it to contain %q", body, part)
		}
	}

	if strings.Contains(body, "<table") {
		t.Errorf("body = %q, want rows alone", body)
	}
}

// shareStub answers the share reads of a shared-page test and counts the
// resolutions, so a caller can prove a partial never touched the tree.
type shareStub struct {
	usecase.Share

	share    entity.Share
	folder   entity.Folder
	err      error
	gets     int
	resolves int
}

func (s *shareStub) Get(_ context.Context, _ string) (entity.Share, error) {
	s.gets++

	return s.share, s.err
}

func (s *shareStub) ResolveFolder(_ context.Context, _, _ string) (entity.Share, entity.Folder, error) {
	s.resolves++

	return s.share, s.folder, s.err
}

// sharedFolderStub records the listing call of a shared page or partial and
// answers with the canned view.
type sharedFolderStub struct {
	usecase.Folder

	view     entity.FolderView
	userID   string
	folderID string
	search   string
	after    *entity.EntryCursor
	calls    int
}

func (f *sharedFolderStub) List(
	_ context.Context,
	userID, folderID, search string,
	after *entity.EntryCursor,
) (entity.FolderView, error) {
	f.calls++
	f.userID = userID
	f.folderID = folderID
	f.search = search
	f.after = after

	view := f.view
	view.Folder.ID = folderID
	view.Search = search

	return view, nil
}

// sharedApp wires the page and partial routes of a share behind a middleware
// that parks the session of the visitor the way the auth guard would.
func sharedApp(shares usecase.Share, folders usecase.Folder) *fiber.App {
	c := &Controller{l: logger.New("error"), v: validator.New(), shares: shares, folders: folders}

	app := fiber.New()

	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals(_sessionLocal, entity.Session{UserID: _shareTestVisitor})

		return ctx.Next()
	})

	app.Get("/share/:share", c.sharedRoot)
	app.Get("/share/:share/folders/:folder", c.sharedFolderRoute)
	app.Get("/htmx/share/:share/entries", c.sharedEntries)
	app.Get("/htmx/share/:share/folders/:folder/entries", c.sharedEntriesRoute)

	return app
}

// sharedResponse sends one request to a shared route and returns the status and
// the body of the answer.
func sharedResponse(t *testing.T, app *fiber.App, path string) (status int, body string) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}

	return res.StatusCode, string(raw)
}
