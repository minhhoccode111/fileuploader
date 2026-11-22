package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
)

const (
	_folderTestID     = "b6a1d2c3-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
	_folderTestParent = "0f6a1c2e-3b4d-4e5f-8a9b-0c1d2e3f4a5b"
	_folderTestDest   = "550e8400-e29b-41d4-a716-446655440000"
	_folderTestUser   = "user-1"
)

type fakeFolderActions struct {
	usecase.Folder

	folder    entity.Folder
	parent    entity.Folder
	view      entity.FolderView
	all       []entity.Folder
	archive   io.ReadCloser
	filename  string
	getErr    error
	createErr error
	moveErr   error
	dupErr    error
	deleteErr error
	zipErr    error

	lastUserID  string
	lastFolder  string
	lastParent  string
	lastName    string
	lastDest    string
	getCalls    int
	createCalls int
	moveCalls   int
	dupCalls    int
	deleteCalls int
	listCalls   int
	allCalls    int
	zipCalls    int
}

func (f *fakeFolderActions) Get(_ context.Context, userID, folderID string) (entity.Folder, error) {
	f.getCalls++
	f.lastUserID = userID
	f.lastFolder = folderID

	if f.getErr != nil {
		return entity.Folder{}, f.getErr
	}

	return f.folder, nil
}

func (f *fakeFolderActions) List(
	_ context.Context,
	userID, folderID, search string,
	_ *entity.EntryCursor,
) (entity.FolderView, error) {
	f.listCalls++
	f.lastUserID = userID
	f.lastFolder = folderID
	f.view.Search = search

	return f.view, nil
}

func (f *fakeFolderActions) Create(
	_ context.Context,
	userID, parentID, name string,
) (entity.Folder, error) {
	f.createCalls++
	f.lastUserID = userID
	f.lastParent = parentID
	f.lastName = name

	if f.createErr != nil {
		return entity.Folder{}, f.createErr
	}

	return f.folder, nil
}

func (f *fakeFolderActions) Rename(
	_ context.Context,
	userID, folderID, name string,
) (entity.Folder, error) {
	f.lastUserID = userID
	f.lastFolder = folderID
	f.lastName = name

	return f.folder, nil
}

func (f *fakeFolderActions) Move(
	_ context.Context,
	userID, folderID, destinationID string,
) (entity.Folder, error) {
	f.moveCalls++
	f.lastUserID = userID
	f.lastFolder = folderID
	f.lastDest = destinationID

	if f.moveErr != nil {
		return entity.Folder{}, f.moveErr
	}

	return f.folder, nil
}

func (f *fakeFolderActions) Duplicate(
	_ context.Context,
	userID, folderID, destinationID string,
) (entity.Folder, error) {
	f.dupCalls++
	f.lastUserID = userID
	f.lastFolder = folderID
	f.lastDest = destinationID

	if f.dupErr != nil {
		return entity.Folder{}, f.dupErr
	}

	return f.folder, nil
}

func (f *fakeFolderActions) Delete(
	_ context.Context,
	userID, folderID string,
) (entity.Folder, error) {
	f.deleteCalls++
	f.lastUserID = userID
	f.lastFolder = folderID

	return f.parent, f.deleteErr
}

func (f *fakeFolderActions) ListAll(_ context.Context, userID string) ([]entity.Folder, error) {
	f.allCalls++
	f.lastUserID = userID

	return f.all, nil
}

func (f *fakeFolderActions) DownloadZip(
	_ context.Context,
	userID, folderID string,
) (io.ReadCloser, string, error) {
	f.zipCalls++
	f.lastUserID = userID
	f.lastFolder = folderID

	return f.archive, f.filename, f.zipErr
}

func folderActionApp(folders usecase.Folder) *fiber.App {
	c := &Controller{
		folders: folders,
		l:       logger.New("error"),
		v:       validatorx.New(),
	}

	app := fiber.New()

	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals(_sessionLocal, entity.Session{UserID: _folderTestUser})

		return ctx.Next()
	})

	app.Get("/htmx/folders/new", c.folderNew)
	app.Get("/htmx/folders/:folder/edit", c.folderEdit)
	app.Get("/htmx/folders/:folder/move", c.folderMoveForm)
	app.Get("/htmx/folders/:folder/duplicate", c.folderDuplicateForm)
	app.Get("/htmx/folders/tree", c.folderTree)
	app.Post("/htmx/folders", c.folderCreate)
	app.Post("/htmx/folders/:folder/rename", c.folderRename)
	app.Post("/htmx/folders/:folder/move", c.folderMove)
	app.Post("/htmx/folders/:folder/duplicate", c.folderDuplicate)
	app.Post("/htmx/folders/:folder/delete", c.folderDelete)
	app.Get("/folders/:folder/download", c.folderDownloadZip)

	return app
}

func folderActionResponse(
	t *testing.T,
	app *fiber.App,
	method, path string,
	form url.Values,
	headers map[string]string,
) (status int, head http.Header, body string) {
	t.Helper()

	var bodyReader io.Reader = http.NoBody
	if form != nil {
		bodyReader = strings.NewReader(form.Encode())
	}

	req := httptest.NewRequestWithContext(t.Context(), method, path, bodyReader)
	if form != nil {
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationForm)
	}

	for name, value := range headers {
		req.Header.Set(name, value)
	}

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

func folderForTest(parentID string) entity.Folder {
	return entity.Folder{ID: _folderTestID, UserID: _folderTestUser, ParentID: &parentID, Name: "docs"}
}

func TestFolderNewRendersTheCreateForm(t *testing.T) {
	t.Parallel()

	status, _, body := folderActionResponse(
		t,
		folderActionApp(&fakeFolderActions{}),
		http.MethodGet,
		"/htmx/folders/new?folder="+_folderTestParent,
		nil,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if !strings.Contains(body, `name="folder"`) || !strings.Contains(body, _folderTestParent) {
		t.Errorf("body = %q, want the parent field", body)
	}
}

func TestFolderEditRejectsTheRoot(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderActions{folder: entity.Folder{ID: _folderTestID, Name: "/"}}

	status, _, _ := folderActionResponse(
		t,
		folderActionApp(folders),
		http.MethodGet,
		"/htmx/folders/"+_folderTestID+"/edit",
		nil,
		nil,
	)

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
	}
}

func TestFolderCreateRefreshesTheParent(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderActions{
		folder: entity.Folder{ID: _folderTestID, ParentID: new(_folderTestParent)},
		view:   entity.FolderView{Folder: entity.Folder{ID: _folderTestParent, Name: "/"}},
	}

	status, headers, _ := folderActionResponse(
		t,
		folderActionApp(folders),
		http.MethodPost,
		"/htmx/folders",
		url.Values{"folder": {_folderTestParent}, "name": {"  Reports  "}},
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if folders.createCalls != 1 || folders.lastParent != _folderTestParent || folders.lastName != "Reports" {
		t.Errorf(
			"Create = %d/%q/%q, want 1/%q/Reports",
			folders.createCalls,
			folders.lastParent,
			folders.lastName,
			_folderTestParent,
		)
	}

	if folders.listCalls != 1 || folders.lastFolder != _folderTestParent {
		t.Errorf("List = %d/%q, want 1/%q", folders.listCalls, folders.lastFolder, _folderTestParent)
	}

	if got := headers.Get(_hxTriggerHeader); got != _entrySavedEvent {
		t.Errorf("HX-Trigger = %q, want %q", got, _entrySavedEvent)
	}
}

func TestFolderMoveAndDuplicateRefreshTheSourceParent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		path  string
		calls func(*fakeFolderActions) int
	}{
		{
			name:  "move",
			path:  "/htmx/folders/" + _folderTestID + "/move",
			calls: func(f *fakeFolderActions) int { return f.moveCalls },
		},
		{
			name:  "duplicate",
			path:  "/htmx/folders/" + _folderTestID + "/duplicate",
			calls: func(f *fakeFolderActions) int { return f.dupCalls },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			folders := &fakeFolderActions{
				folder: folderForTest(_folderTestParent),
				view:   entity.FolderView{Folder: entity.Folder{ID: _folderTestParent, Name: "/"}},
			}

			status, _, _ := folderActionResponse(
				t,
				folderActionApp(folders),
				http.MethodPost,
				tc.path,
				url.Values{"folder": {_folderTestDest}},
				nil,
			)

			if status != http.StatusOK {
				t.Fatalf("status = %d, want %d", status, http.StatusOK)
			}

			if folders.getCalls != 1 || tc.calls(folders) != 1 || folders.lastDest != _folderTestDest {
				t.Errorf(
					"Get/mutation/dest = %d/%d/%q, want 1/1/%q",
					folders.getCalls,
					tc.calls(folders),
					folders.lastDest,
					_folderTestDest,
				)
			}

			if folders.lastFolder != _folderTestParent {
				t.Errorf("refreshed folder = %q, want the source parent", folders.lastFolder)
			}
		})
	}
}

func TestFolderDeleteRedirectsWithTheSearch(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderActions{parent: entity.Folder{
		ID:       _folderTestParent,
		ParentID: new("root-id"),
		Name:     "docs",
	}}

	status, headers, _ := folderActionResponse(
		t,
		folderActionApp(folders),
		http.MethodPost,
		"/htmx/folders/"+_folderTestID+"/delete",
		nil,
		map[string]string{_hxCurrentURLHeader: "http://x/folders/" + _folderTestParent + "?q=note"},
	)

	if status != http.StatusFound {
		t.Fatalf("status = %d, want %d", status, http.StatusFound)
	}

	want := "/folders/" + _folderTestParent + "?q=note"
	if got := headers.Get(fiber.HeaderLocation); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

func TestFolderDeleteFallsBackHomeWhenTheParentIsUnknown(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderActions{parent: entity.Folder{}}

	status, headers, _ := folderActionResponse(
		t,
		folderActionApp(folders),
		http.MethodPost,
		"/htmx/folders/"+_folderTestID+"/delete",
		nil,
		map[string]string{_hxCurrentURLHeader: "http://x/folders/" + _folderTestParent + "?q=note"},
	)

	if status != http.StatusFound {
		t.Fatalf("status = %d, want %d", status, http.StatusFound)
	}

	if got := headers.Get(fiber.HeaderLocation); got != "/?q=note" {
		t.Errorf("Location = %q, want /?q=note", got)
	}
}

func TestFolderDownloadZipStreamsTheArchive(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderActions{
		archive:  io.NopCloser(strings.NewReader("zip-bytes")),
		filename: "docs.zip",
	}

	status, headers, body := folderActionResponse(
		t,
		folderActionApp(folders),
		http.MethodGet,
		"/folders/"+_folderTestID+"/download",
		nil,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if got := headers.Get(fiber.HeaderContentType); got != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", got)
	}

	if !strings.Contains(headers.Get(fiber.HeaderContentDisposition), "docs.zip") {
		t.Errorf("Content-Disposition = %q, want docs.zip", headers.Get(fiber.HeaderContentDisposition))
	}

	if body != "zip-bytes" {
		t.Errorf("body = %q, want zip-bytes", body)
	}
}

func TestFolderDownloadZipRedirectsOnFailure(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderActions{zipErr: entity.ErrFolderNotFound}

	status, headers, _ := folderActionResponse(
		t,
		folderActionApp(folders),
		http.MethodGet,
		"/folders/"+_folderTestID+"/download",
		nil,
		nil,
	)

	if status != http.StatusFound {
		t.Fatalf("status = %d, want %d", status, http.StatusFound)
	}

	if got := headers.Get(fiber.HeaderLocation); got != "/" {
		t.Errorf("Location = %q, want /", got)
	}
}

func TestFolderTreeRendersFolders(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderActions{all: []entity.Folder{
		{ID: _folderTestParent, Name: "/"},
		{ID: _folderTestID, ParentID: new(_folderTestParent), Name: "docs"},
	}}

	status, _, body := folderActionResponse(
		t,
		folderActionApp(folders),
		http.MethodGet,
		"/htmx/folders/tree",
		nil,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if folders.allCalls != 1 {
		t.Errorf("ListAll calls = %d, want 1", folders.allCalls)
	}

	if !strings.Contains(body, "docs") {
		t.Errorf("body = %q, want the folder", body)
	}
}

//nolint:funlen // one case per malformed form.
func TestFolderActionsRejectMalformedForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		path       string
		form       url.Values
		wantStatus int
		check      func(*fakeFolderActions) bool
	}{
		{
			name:       "a create with a bad parent",
			path:       "/htmx/folders",
			form:       url.Values{"folder": {"not-a-uuid"}, "name": {"docs"}},
			wantStatus: http.StatusUnprocessableEntity,
			check:      func(f *fakeFolderActions) bool { return f.createCalls == 0 },
		},
		{
			name:       "a create with an empty name",
			path:       "/htmx/folders",
			form:       url.Values{"folder": {_folderTestParent}, "name": {""}},
			wantStatus: http.StatusUnprocessableEntity,
			check:      func(f *fakeFolderActions) bool { return f.createCalls == 0 },
		},
		{
			name:       "a move with a bad destination",
			path:       "/htmx/folders/" + _folderTestID + "/move",
			form:       url.Values{"folder": {"not-a-uuid"}},
			wantStatus: http.StatusUnprocessableEntity,
			check:      func(f *fakeFolderActions) bool { return f.moveCalls == 0 },
		},
		{
			name:       "a duplicate with a bad destination",
			path:       "/htmx/folders/" + _folderTestID + "/duplicate",
			form:       url.Values{"folder": {"not-a-uuid"}},
			wantStatus: http.StatusUnprocessableEntity,
			check:      func(f *fakeFolderActions) bool { return f.dupCalls == 0 },
		},
		{
			name:       "a bad folder ID",
			path:       "/htmx/folders/not-a-uuid/rename",
			form:       url.Values{"name": {"docs"}},
			wantStatus: http.StatusNotFound,
			check:      func(f *fakeFolderActions) bool { return f.getCalls == 0 },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			folders := &fakeFolderActions{folder: folderForTest(_folderTestParent)}

			status, _, _ := folderActionResponse(
				t,
				folderActionApp(folders),
				http.MethodPost,
				tc.path,
				tc.form,
				nil,
			)

			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}

			if !tc.check(folders) {
				t.Errorf("mutation ran for a malformed request: %+v", folders)
			}
		})
	}
}
