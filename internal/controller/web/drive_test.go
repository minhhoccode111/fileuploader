package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// A drive action that would push the account past its quota is a client
// problem, not a server fault: the handler must answer 413 and say why.
func TestDriveErrorMapsStorageQuotaExceeded(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	c := &Controller{l: logger.New("error")}

	app.Get("/", func(ctx *fiber.Ctx) error {
		return c.driveError(ctx, entity.ErrStorageQuotaExceeded, "test")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}

	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusRequestEntityTooLarge)
	}

	if len(body) == 0 {
		t.Error("body is empty, want the quota message")
	}
}

// A partial listing must never restart at the first page: an absent or
// tampered cursor is a client error, and the answer carries no rows.
func TestFolderEntriesRejectsAMalformedCursor(t *testing.T) {
	t.Parallel()

	const entries = "/htmx/folders/550e8400-e29b-41d4-a716-446655440000/entries"

	cases := []struct {
		name string
		path string
	}{
		{name: "a missing cursor", path: entries},
		{name: "a tampered cursor", path: entries + "?after=not-a-cursor"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			status, body := folderEntriesResponse(t, nil, tc.path)

			if status != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", status, http.StatusBadRequest)
			}

			if strings.Contains(body, "<tr") {
				t.Errorf("body = %q, want no rows", body)
			}
		})
	}
}

// The load-more link carries the search, so the partial must run the same
// filter: unfiltered rows appended under a filtered table would be wrong.
func TestFolderEntriesKeepsTheSearch(t *testing.T) {
	t.Parallel()

	const folderID = "550e8400-e29b-41d4-a716-446655440000"

	cursor := entity.EntryCursor{
		Kind: entity.EntryKindFile,
		Name: "notes.txt",
		ID:   "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44",
	}
	next := entity.EntryCursor{
		Kind: entity.EntryKindFile,
		Name: "zeta.txt",
		ID:   "0f6a1c2e-3b4d-4e5f-8a9b-0c1d2e3f4a5b",
	}

	folders := &driveFolder{view: entity.FolderView{
		Folder: entity.Folder{ID: folderID, Name: "/"},
		Files: []entity.FileItem{{
			ID:          "b1e2d3c4-5a6f-4b7c-8d9e-0f1a2b3c4d5e",
			Name:        "note.txt",
			ContentType: "text/plain",
			Size:        5,
		}},
		Search: "note",
		Next:   &next,
	}}

	path := "/htmx/folders/" + folderID + "/entries?q=note&after=" + cursor.Encode()

	status, body := folderEntriesResponse(t, folders, path)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if folders.search != "note" {
		t.Errorf("search = %q, want %q", folders.search, "note")
	}

	if folders.folderID != folderID {
		t.Errorf("folderID = %q, want %q", folders.folderID, folderID)
	}

	if folders.after == nil || *folders.after != cursor {
		t.Errorf("after = %v, want %v", folders.after, cursor)
	}

	for _, part := range []string{"note.txt", "Load more"} {
		if !strings.Contains(body, part) {
			t.Errorf("body = %q, want it to contain %q", body, part)
		}
	}
}

const (
	// _driveTestUser is the account behind a drive test request.
	_driveTestUser = "550e8400-e29b-41d4-a716-446655440001"

	// _driveTestRoot is the root folder a drive test browses.
	_driveTestRoot = "550e8400-e29b-41d4-a716-446655440002"

	// _driveTestFolder is the non-root folder a drive test browses.
	_driveTestFolder = "b6a1d2c3-4e5f-4a6b-8c7d-9e0f1a2b3c4d"

	// _driveTestParent is the folder that holds _driveTestFolder.
	_driveTestParent = "0f6a1c2e-3b4d-4e5f-8a9b-0c1d2e3f4a5b"
)

// The folder page runs the term the visitor typed against the direct children
// of the folder, and the form shows the same term, so the input never
// disagrees with the rows it narrowed.
func TestFolderSearchPassesQToTheUseCase(t *testing.T) {
	t.Parallel()

	parent := _driveTestParent

	folders := &driveFolder{view: entity.FolderView{
		Folder:     entity.Folder{ID: _driveTestFolder, ParentID: &parent, Name: "docs"},
		Breadcrumb: []entity.FolderRef{{ID: _driveTestRoot, Name: "/"}, {ID: _driveTestFolder, Name: "docs"}},
	}}

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/folders/"+_driveTestFolder+"?q=note",
		http.NoBody,
	)

	status, _, body := driveResponse(t, drivePageApp(folders), req)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if folders.search != "note" {
		t.Errorf("search = %q, want %q", folders.search, "note")
	}

	for _, part := range []string{
		`value="note"`,
		`action="/folders/` + _driveTestFolder + `"`,
	} {
		if !strings.Contains(body, part) {
			t.Errorf("body = %q, want it to contain %q", body, part)
		}
	}
}

// Home keeps its own address for a local search: the form submits to "/" and
// the root folder is the one the term narrows.
func TestHomeSearchKeepsTheHomePath(t *testing.T) {
	t.Parallel()

	folders := &driveFolder{view: entity.FolderView{
		Folder:     entity.Folder{ID: _driveTestRoot, Name: "/"},
		Breadcrumb: []entity.FolderRef{{ID: _driveTestRoot, Name: "/"}},
	}}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=note", http.NoBody)

	status, _, body := driveResponse(t, drivePageApp(folders), req)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if folders.folderID != "" {
		t.Errorf("folderID = %q, want the root named by an empty id", folders.folderID)
	}

	if folders.search != "note" {
		t.Errorf("search = %q, want %q", folders.search, "note")
	}

	for _, part := range []string{`action="/"`, `hx-get="/"`, `value="note"`} {
		if !strings.Contains(body, part) {
			t.Errorf("body = %q, want it to contain %q", body, part)
		}
	}
}

// The scope control leaves the folder for the whole-drive search. The folder
// only lends its identifier as the way back; no listing runs.
func TestFolderScopeAllRedirectsToSearch(t *testing.T) {
	t.Parallel()

	want := "/search?from=" + _driveTestFolder + "&q=note"

	t.Run("a browser navigation", func(t *testing.T) {
		t.Parallel()

		folders := &driveFolder{}

		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			"/folders/"+_driveTestFolder+"?q=note&scope=all",
			http.NoBody,
		)

		status, header, _ := driveResponse(t, drivePageApp(folders), req)

		if status != http.StatusFound {
			t.Errorf("status = %d, want %d", status, http.StatusFound)
		}

		if got := header.Get(fiber.HeaderLocation); got != want {
			t.Errorf("Location = %q, want %q", got, want)
		}

		if folders.listCalls != 0 {
			t.Errorf("List calls = %d, want 0", folders.listCalls)
		}
	})

	t.Run("an htmx request", func(t *testing.T) {
		t.Parallel()

		folders := &driveFolder{}

		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			"/folders/"+_driveTestFolder+"?q=note&scope=all",
			http.NoBody,
		)
		req.Header.Set(_hxRequestHeader, "true")

		_, header, _ := driveResponse(t, drivePageApp(folders), req)

		if got := header.Get("HX-Location"); !strings.Contains(got, want) {
			t.Errorf("HX-Location = %q, want it to contain %q", got, want)
		}

		if folders.listCalls != 0 {
			t.Errorf("List calls = %d, want 0", folders.listCalls)
		}
	})
}

// Home keeps no folder address, so its whole-drive search has no way back.
func TestHomeScopeAllRedirectsToSearchWithoutFrom(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/?q=note&scope=all",
		http.NoBody,
	)

	status, header, _ := driveResponse(t, drivePageApp(&driveFolder{}), req)

	if status != http.StatusFound {
		t.Errorf("status = %d, want %d", status, http.StatusFound)
	}

	if got := header.Get(fiber.HeaderLocation); got != "/search?q=note" {
		t.Errorf("Location = %q, want %q", got, "/search?q=note")
	}
}

// An action answers with the rows of the page the visitor is on, so the term
// on that page has to reach the listing again.
func TestEntrySearchReadsTheHtmxCurrentURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		header string
		want   string
	}{
		{name: "the page URL", header: "http://x/folders/folder-1?q=note&scope=folder", want: "note"},
		{name: "a malformed URL", header: "://bad", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := &Controller{l: logger.New("error")}

			app := fiber.New()
			app.Get("/probe", func(ctx *fiber.Ctx) error {
				return ctx.SendString(c.entrySearch(ctx))
			})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/probe", http.NoBody)
			req.Header.Set(_hxCurrentURLHeader, tc.header)

			status, _, body := driveResponse(t, app, req)

			if status != http.StatusOK {
				t.Fatalf("status = %d, want %d", status, http.StatusOK)
			}

			if body != tc.want {
				t.Errorf("entrySearch() = %q, want %q", body, tc.want)
			}
		})
	}
}

// A create refreshes the table it changed; under a filtered page the refreshed
// rows have to match the input the visitor still sees.
func TestFolderCreateRefreshesTheActiveFilter(t *testing.T) {
	t.Parallel()

	folders := &driveFolder{view: entity.FolderView{
		Folder: entity.Folder{ID: _driveTestFolder, Name: "docs"},
		Files: []entity.FileItem{{
			ID:          "b1e2d3c4-5a6f-4b7c-8d9e-0f1a2b3c4d5e",
			Name:        "note.txt",
			ContentType: "text/plain",
			Size:        5,
		}},
	}}

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/htmx/folders",
		strings.NewReader("folder="+_driveTestFolder+"&name=notes"),
	)
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationForm)
	req.Header.Set(_hxCurrentURLHeader, "http://x/folders/"+_driveTestFolder+"?q=note&scope=folder")

	status, _, body := driveResponse(t, drivePageApp(folders), req)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if folders.search != "note" {
		t.Errorf("search = %q, want %q", folders.search, "note")
	}

	if folders.folderID != _driveTestFolder {
		t.Errorf("folderID = %q, want %q", folders.folderID, _driveTestFolder)
	}

	if !strings.Contains(body, "note.txt") {
		t.Errorf("body = %q, want the refreshed rows", body)
	}
}

// A rename answers with a location, not a table. The way back keeps the
// filter, so a renamed row that no longer matches the term leaves the view
// instead of the filter leaving with it.
func TestFolderRenameRedirectsWithTheActiveFilter(t *testing.T) {
	t.Parallel()

	root := _driveTestRoot

	folders := &driveFolder{
		parent: entity.Folder{ID: _driveTestParent, ParentID: &root, Name: "docs"},
	}

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/htmx/folders/"+_driveTestFolder+"/rename",
		strings.NewReader("name=notes"),
	)
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationForm)
	req.Header.Set(_hxCurrentURLHeader, "http://x/folders/"+_driveTestFolder+"?q=note&scope=folder")

	status, header, _ := driveResponse(t, drivePageApp(folders), req)

	if status != http.StatusFound {
		t.Errorf("status = %d, want %d", status, http.StatusFound)
	}

	want := "/folders/" + _driveTestParent + "?q=note"
	if got := header.Get(fiber.HeaderLocation); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

// Delete also leaves the table for the folder that held the row, so its way
// back keeps the filter for the same reason.
func TestFolderDeleteRedirectsWithTheActiveFilter(t *testing.T) {
	t.Parallel()

	root := _driveTestRoot

	folders := &driveFolder{
		parent: entity.Folder{ID: _driveTestParent, ParentID: &root, Name: "docs"},
	}

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/htmx/folders/"+_driveTestFolder+"/delete",
		http.NoBody,
	)
	req.Header.Set(_hxCurrentURLHeader, "http://x/folders/"+_driveTestFolder+"?q=note")

	status, header, _ := driveResponse(t, drivePageApp(folders), req)

	if status != http.StatusFound {
		t.Errorf("status = %d, want %d", status, http.StatusFound)
	}

	want := "/folders/" + _driveTestParent + "?q=note"
	if got := header.Get(fiber.HeaderLocation); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

// driveFolder records the listing call of a page or partial route and answers
// with the canned view. The search it received is echoed the way the use case
// echoes it, so a caller can check what the page renders.
type driveFolder struct {
	usecase.Folder

	view      entity.FolderView
	parent    entity.Folder
	folderID  string
	search    string
	after     *entity.EntryCursor
	listCalls int
}

func (f *driveFolder) List(
	_ context.Context,
	_, folderID, search string,
	after *entity.EntryCursor,
) (entity.FolderView, error) {
	f.listCalls++
	f.folderID = folderID
	f.search = search
	f.after = after
	f.view.Search = search

	return f.view, nil
}

func (f *driveFolder) Create(_ context.Context, _, parentID, name string) (entity.Folder, error) {
	return entity.Folder{ID: "created", ParentID: &parentID, Name: name}, nil
}

func (f *driveFolder) Get(_ context.Context, _, _ string) (entity.Folder, error) {
	return f.parent, nil
}

func (f *driveFolder) Rename(_ context.Context, _, folderID, name string) (entity.Folder, error) {
	return entity.Folder{ID: folderID, ParentID: &f.parent.ID, Name: name}, nil
}

func (f *driveFolder) Delete(_ context.Context, _, _ string) (entity.Folder, error) {
	return f.parent, nil
}

// drivePageApp wires the page and create routes of the drive behind a
// middleware that parks the session the auth guard would park.
func drivePageApp(folders usecase.Folder) *fiber.App {
	c := &Controller{l: logger.New("error"), v: validator.New(), folders: folders}

	app := fiber.New()

	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals(_sessionLocal, entity.Session{UserID: _driveTestUser})

		return ctx.Next()
	})

	app.Get("/", c.home)
	app.Get("/folders/:folder", c.folderPage)
	app.Post("/htmx/folders", c.folderCreate)
	app.Post("/htmx/folders/:folder/rename", c.folderRename)
	app.Post("/htmx/folders/:folder/delete", c.folderDelete)

	return app
}

// driveResponse sends one request and returns the status, the headers and the
// body of the answer. The body is read and closed here, so a caller never
// holds a response it has to release.
func driveResponse(
	t *testing.T,
	app *fiber.App,
	req *http.Request,
) (status int, header http.Header, body string) {
	t.Helper()

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}

	return res.StatusCode, res.Header, string(raw)
}

// folderEntriesResponse sends one request to the partial route and returns the
// status and the body of the answer.
func folderEntriesResponse(t *testing.T, folders usecase.Folder, path string) (status int, body string) {
	t.Helper()

	app := fiber.New()
	c := &Controller{l: logger.New("error"), v: validator.New(), folders: folders}

	app.Get("/htmx/folders/:folder/entries", func(ctx *fiber.Ctx) error {
		ctx.Locals(_sessionLocal, entity.Session{UserID: "550e8400-e29b-41d4-a716-446655440001"})

		return c.folderEntries(ctx)
	})

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
