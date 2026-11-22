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
	_fileTestID     = "9f5d0a3e-4c22-4bb3-9a49-41b2c3d4e5f6"
	_fileTestFolder = "550e8400-e29b-41d4-a716-446655440000"
	_fileTestDest   = "0f6a1c2e-3b4d-4e5f-8a9b-0c1d2e3f4a5b"
	_fileTestUser   = "user-1"
)

type fakeFileActions struct {
	usecase.File

	file         entity.File
	getErr       error
	renameErr    error
	moveErr      error
	duplicateErr error
	deleteErr    error

	lastUserID   string
	lastFileID   string
	renamed      string
	movedTo      string
	duplicatedTo string
	deletedID    string
	getCalls     int
	renameCalls  int
	moveCalls    int
	dupCalls     int
	deleteCalls  int
}

func (f *fakeFileActions) Get(_ context.Context, userID, fileID string) (entity.File, error) {
	f.getCalls++
	f.lastUserID = userID
	f.lastFileID = fileID

	if f.getErr != nil {
		return entity.File{}, f.getErr
	}

	return f.file, nil
}

func (f *fakeFileActions) Rename(
	_ context.Context,
	userID, fileID, name string,
) (entity.File, error) {
	f.renameCalls++
	f.lastUserID = userID
	f.lastFileID = fileID
	f.renamed = name

	if f.renameErr != nil {
		return entity.File{}, f.renameErr
	}

	return f.file, nil
}

func (f *fakeFileActions) Move(
	_ context.Context,
	userID, fileID, folderID string,
) (entity.File, error) {
	f.moveCalls++
	f.lastUserID = userID
	f.lastFileID = fileID
	f.movedTo = folderID

	if f.moveErr != nil {
		return entity.File{}, f.moveErr
	}

	return f.file, nil
}

func (f *fakeFileActions) Duplicate(
	_ context.Context,
	userID, fileID, folderID string,
) (entity.File, error) {
	f.dupCalls++
	f.lastUserID = userID
	f.lastFileID = fileID
	f.duplicatedTo = folderID

	if f.duplicateErr != nil {
		return entity.File{}, f.duplicateErr
	}

	return f.file, nil
}

func (f *fakeFileActions) Delete(
	_ context.Context,
	userID, fileID string,
) (entity.File, error) {
	f.deleteCalls++
	f.lastUserID = userID
	f.deletedID = fileID

	return f.file, f.deleteErr
}

type fakeFileFolders struct {
	usecase.Folder

	folder  entity.Folder
	view    entity.FolderView
	all     []entity.Folder
	getErr  error
	listErr error

	lastUserID   string
	lastFolderID string
	listCalls    int
	allCalls     int
}

func (f *fakeFileFolders) Get(_ context.Context, userID, folderID string) (entity.Folder, error) {
	f.lastUserID = userID
	f.lastFolderID = folderID

	if f.getErr != nil {
		return entity.Folder{}, f.getErr
	}

	return f.folder, nil
}

func (f *fakeFileFolders) List(
	_ context.Context,
	userID, folderID, search string,
	_ *entity.EntryCursor,
) (entity.FolderView, error) {
	f.listCalls++
	f.lastUserID = userID
	f.lastFolderID = folderID
	f.view.Search = search

	if f.listErr != nil {
		return entity.FolderView{}, f.listErr
	}

	return f.view, nil
}

func (f *fakeFileFolders) ListAll(_ context.Context, userID string) ([]entity.Folder, error) {
	f.allCalls++
	f.lastUserID = userID

	return f.all, nil
}

func fileActionApp(files usecase.File, folders usecase.Folder) *fiber.App {
	c := &Controller{
		files:   files,
		folders: folders,
		l:       logger.New("error"),
		v:       validatorx.New(),
	}

	app := fiber.New()

	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals(_sessionLocal, entity.Session{UserID: _fileTestUser})

		return ctx.Next()
	})

	app.Get("/htmx/files/new", c.fileNew)
	app.Get("/htmx/files/:file", c.fileDrawer)
	app.Get("/htmx/files/:file/move", c.fileMoveForm)
	app.Get("/htmx/files/:file/duplicate", c.fileDuplicateForm)
	app.Post("/htmx/files/:file/rename", c.fileRename)
	app.Post("/htmx/files/:file/move", c.fileMove)
	app.Post("/htmx/files/:file/duplicate", c.fileDuplicate)
	app.Post("/htmx/files/:file/delete", c.fileDelete)

	return app
}

func fileActionResponse(
	t *testing.T,
	app *fiber.App,
	method, path string,
	form url.Values,
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

func fileForTest() entity.File {
	return entity.File{
		ID:          _fileTestID,
		UserID:      _fileTestUser,
		FolderID:    _fileTestFolder,
		Name:        "note.txt",
		ContentType: "text/plain",
		Size:        5,
		StorageKey:  "users/user/files/file",
	}
}

func TestFileNewRendersTheUploadForm(t *testing.T) {
	t.Parallel()

	status, _, body := fileActionResponse(
		t,
		fileActionApp(&fakeFileActions{}, &fakeFileFolders{}),
		http.MethodGet,
		"/htmx/files/new?folder="+_fileTestFolder,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if !strings.Contains(body, `name="file"`) {
		t.Errorf("body = %q, want the upload input", body)
	}
}

func TestFileDrawerRendersTheFile(t *testing.T) {
	t.Parallel()

	files := &fakeFileActions{file: fileForTest()}

	status, _, body := fileActionResponse(
		t,
		fileActionApp(files, &fakeFileFolders{}),
		http.MethodGet,
		"/htmx/files/"+_fileTestID,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if files.getCalls != 1 {
		t.Errorf("Get calls = %d, want 1", files.getCalls)
	}

	for _, part := range []string{"note.txt", "/files/" + _fileTestID + "/download"} {
		if !strings.Contains(body, part) {
			t.Errorf("body = %q, want it to contain %q", body, part)
		}
	}
}

func TestFileDrawerRejectsABadID(t *testing.T) {
	t.Parallel()

	files := &fakeFileActions{}

	status, _, _ := fileActionResponse(
		t,
		fileActionApp(files, &fakeFileFolders{}),
		http.MethodGet,
		"/htmx/files/not-a-uuid",
		nil,
	)

	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
	}

	if files.getCalls != 0 {
		t.Errorf("Get calls = %d, want 0", files.getCalls)
	}
}

func TestFileRenameRefreshesTheFolder(t *testing.T) {
	t.Parallel()

	files := &fakeFileActions{file: fileForTest()}
	folders := &fakeFileFolders{view: entity.FolderView{Folder: entity.Folder{ID: _fileTestFolder, Name: "/"}}}

	status, headers, _ := fileActionResponse(
		t,
		fileActionApp(files, folders),
		http.MethodPost,
		"/htmx/files/"+_fileTestID+"/rename",
		url.Values{"name": {"  new.txt  "}},
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if files.renameCalls != 1 || files.lastFileID != _fileTestID || files.renamed != "new.txt" {
		t.Errorf("Rename = %d/%q/%q, want 1/%q/new.txt", files.renameCalls, files.lastFileID, files.renamed, _fileTestID)
	}

	if folders.listCalls != 1 || folders.lastFolderID != _fileTestFolder {
		t.Errorf("List = %d/%q, want 1/%q", folders.listCalls, folders.lastFolderID, _fileTestFolder)
	}

	if got := headers.Get(_hxTriggerHeader); got != _entrySavedEvent {
		t.Errorf("HX-Trigger = %q, want %q", got, _entrySavedEvent)
	}
}

func TestFileMoveAndDuplicateRefreshTheSourceFolder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		path        string
		calls       func(*fakeFileActions) int
		destination func(*fakeFileActions) string
	}{
		{
			name:        "move",
			path:        "/htmx/files/" + _fileTestID + "/move",
			calls:       func(f *fakeFileActions) int { return f.moveCalls },
			destination: func(f *fakeFileActions) string { return f.movedTo },
		},
		{
			name:        "duplicate",
			path:        "/htmx/files/" + _fileTestID + "/duplicate",
			calls:       func(f *fakeFileActions) int { return f.dupCalls },
			destination: func(f *fakeFileActions) string { return f.duplicatedTo },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			files := &fakeFileActions{file: fileForTest()}
			folders := &fakeFileFolders{view: entity.FolderView{Folder: entity.Folder{ID: _fileTestFolder, Name: "/"}}}

			status, _, _ := fileActionResponse(
				t,
				fileActionApp(files, folders),
				http.MethodPost,
				tc.path,
				url.Values{"folder": {_fileTestDest}},
			)

			if status != http.StatusOK {
				t.Fatalf("status = %d, want %d", status, http.StatusOK)
			}

			if files.getCalls != 1 || tc.calls(files) != 1 {
				t.Fatalf("Get/mutation calls = %d/%d, want 1/1", files.getCalls, tc.calls(files))
			}

			if got := tc.destination(files); got != _fileTestDest {
				t.Errorf("destination = %q, want %q", got, _fileTestDest)
			}

			if folders.lastFolderID != _fileTestFolder {
				t.Errorf("refreshed folder = %q, want the source folder", folders.lastFolderID)
			}
		})
	}
}

func TestFileDeleteRefreshesTheFolder(t *testing.T) {
	t.Parallel()

	files := &fakeFileActions{file: fileForTest()}
	folders := &fakeFileFolders{view: entity.FolderView{Folder: entity.Folder{ID: _fileTestFolder, Name: "/"}}}

	status, headers, _ := fileActionResponse(
		t,
		fileActionApp(files, folders),
		http.MethodPost,
		"/htmx/files/"+_fileTestID+"/delete",
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if files.deleteCalls != 1 || folders.lastFolderID != _fileTestFolder {
		t.Errorf("Delete calls = %d, folder = %q, want 1 and %q", files.deleteCalls, folders.lastFolderID, _fileTestFolder)
	}

	if got := headers.Get(_hxTriggerHeader); got != _entrySavedEvent {
		t.Errorf("HX-Trigger = %q, want %q", got, _entrySavedEvent)
	}
}

func TestFileDeleteTreatsCleanupFailureAsSuccess(t *testing.T) {
	t.Parallel()

	files := &fakeFileActions{file: fileForTest(), deleteErr: entity.ErrStorageCleanup}
	folders := &fakeFileFolders{view: entity.FolderView{Folder: entity.Folder{ID: _fileTestFolder, Name: "/"}}}

	status, headers, _ := fileActionResponse(
		t,
		fileActionApp(files, folders),
		http.MethodPost,
		"/htmx/files/"+_fileTestID+"/delete",
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if got := headers.Get(_hxTriggerHeader); got != _entrySavedEvent {
		t.Errorf("HX-Trigger = %q, want %q", got, _entrySavedEvent)
	}
}

func TestFileMoveFormRendersTheDestinations(t *testing.T) {
	t.Parallel()

	files := &fakeFileActions{file: fileForTest()}
	folders := &fakeFileFolders{
		folder: entity.Folder{ID: _fileTestFolder, UserID: _fileTestUser},
		all:    []entity.Folder{{ID: _fileTestDest, Name: "Docs"}},
	}

	status, _, body := fileActionResponse(
		t,
		fileActionApp(files, folders),
		http.MethodGet,
		"/htmx/files/"+_fileTestID+"/move",
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if folders.allCalls != 1 {
		t.Errorf("ListAll calls = %d, want 1", folders.allCalls)
	}

	if !strings.Contains(body, _fileTestDest) {
		t.Errorf("body = %q, want the destination option", body)
	}
}

func TestFileActionsRejectMalformedForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		path       string
		form       url.Values
		wantStatus int
		check      func(*fakeFileActions) bool
	}{
		{
			name:       "a rename without a name",
			path:       "/htmx/files/" + _fileTestID + "/rename",
			form:       url.Values{},
			wantStatus: http.StatusUnprocessableEntity,
			check:      func(f *fakeFileActions) bool { return f.renameCalls == 0 },
		},
		{
			name:       "a move without a destination",
			path:       "/htmx/files/" + _fileTestID + "/move",
			form:       url.Values{},
			wantStatus: http.StatusUnprocessableEntity,
			check:      func(f *fakeFileActions) bool { return f.moveCalls == 0 },
		},
		{
			name:       "a duplicate with a bad destination",
			path:       "/htmx/files/" + _fileTestID + "/duplicate",
			form:       url.Values{"folder": {"not-a-uuid"}},
			wantStatus: http.StatusUnprocessableEntity,
			check:      func(f *fakeFileActions) bool { return f.dupCalls == 0 },
		},
		{
			name:       "a bad file ID",
			path:       "/htmx/files/not-a-uuid/rename",
			form:       url.Values{"name": {"new.txt"}},
			wantStatus: http.StatusNotFound,
			check:      func(f *fakeFileActions) bool { return f.getCalls == 0 && f.renameCalls == 0 },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			files := &fakeFileActions{file: fileForTest()}

			status, _, _ := fileActionResponse(
				t,
				fileActionApp(files, &fakeFileFolders{}),
				http.MethodPost,
				tc.path,
				tc.form,
			)

			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}

			if !tc.check(files) {
				t.Errorf("mutation ran for a malformed request: %+v", files)
			}
		})
	}
}
