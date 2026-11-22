package web

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
)

const (
	_shareManageShare  = "8c1f3e2a-4b5d-4c6e-9f70-1a2b3c4d5e6f"
	_shareManageFolder = "b6a1d2c3-4e5f-4a6b-8c7d-9e0f1a2b3c4e"
	_shareManageFile   = "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e45"
	_shareManageUser   = "user-1"
)

func (s *shareStub) List(_ context.Context, userID string) ([]entity.Share, error) {
	s.listCalls++
	s.lastListUserID = userID

	return s.shares, s.err
}

func (s *shareStub) Create(
	_ context.Context,
	userID, folderID string,
	expiresAt *time.Time,
) (entity.Share, error) {
	s.createCalls++
	s.createUserID = userID
	s.createFolderID = folderID
	s.createExpiry = expiresAt

	if s.createErr != nil {
		return entity.Share{}, s.createErr
	}

	return entity.Share{ID: _shareManageShare, UserID: userID, FolderID: folderID, ExpiresAt: expiresAt}, nil
}

func (s *shareStub) UpdateExpiry(
	_ context.Context,
	userID, shareID string,
	expiresAt *time.Time,
) (entity.Share, error) {
	s.updateCalls++
	s.updateUserID = userID
	s.updateShareID = shareID
	s.updateExpiry = expiresAt

	if s.updateErr != nil {
		return entity.Share{}, s.updateErr
	}

	return entity.Share{ID: shareID, UserID: userID, ExpiresAt: expiresAt}, nil
}

func (s *shareStub) Delete(_ context.Context, userID, shareID string) error {
	s.deleteCalls++
	s.deleteUserID = userID
	s.deleteShareID = shareID

	return s.deleteErr
}

func (s *shareStub) ResolveFile(
	_ context.Context,
	shareID, fileID string,
) (entity.Share, entity.File, error) {
	s.resolveFileCalls++
	s.resolveFileShare = shareID
	s.resolveFileID = fileID

	if s.resolveFileErr != nil {
		return entity.Share{}, entity.File{}, s.resolveFileErr
	}

	return s.share, s.file, nil
}

type fakeSharedFile struct {
	usecase.File

	reader     io.ReadCloser
	file       entity.File
	err        error
	lastUserID string
	lastFileID string
	calls      int
}

func (f *fakeSharedFile) OpenDownload(
	_ context.Context,
	userID, fileID string,
) (io.ReadCloser, entity.File, error) {
	f.calls++
	f.lastUserID = userID
	f.lastFileID = fileID

	if f.err != nil {
		return nil, entity.File{}, f.err
	}

	return f.reader, f.file, nil
}

type fakeShareUser struct {
	usecase.User

	session entity.Session
	err     error
}

func (f *fakeShareUser) Authenticate(_ context.Context, _ string) (entity.Session, error) {
	if f.err != nil {
		return entity.Session{}, f.err
	}

	return f.session, nil
}

type fakeShareFolders struct {
	usecase.Folder

	folder  entity.Folder
	view    entity.FolderView
	getErr  error
	listErr error

	lastUserID   string
	lastFolderID string
	getCalls     int
	listCalls    int
}

func (f *fakeShareFolders) Get(_ context.Context, userID, folderID string) (entity.Folder, error) {
	f.getCalls++
	f.lastUserID = userID
	f.lastFolderID = folderID

	if f.getErr != nil {
		return entity.Folder{}, f.getErr
	}

	return f.folder, nil
}

func (f *fakeShareFolders) List(
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

func shareManageApp(
	shares usecase.Share,
	folders usecase.Folder,
	files usecase.File,
	users usecase.User,
) *fiber.App {
	c := &Controller{
		shares:  shares,
		folders: folders,
		files:   files,
		u:       users,
		l:       logger.New("error"),
		v:       validatorx.New(),
	}

	app := fiber.New()
	owner := func(ctx *fiber.Ctx) error {
		ctx.Locals(_sessionLocal, entity.Session{UserID: _shareManageUser})

		return ctx.Next()
	}

	app.Get("/share", owner, c.shareIndex)
	app.Get("/htmx/folders/:folder/share", owner, c.shareForm)
	app.Get("/htmx/shares/:share", owner, c.shareDrawer)
	app.Post("/htmx/shares", owner, c.shareCreate)
	app.Post("/htmx/shares/:share/time", owner, c.shareUpdateTime)
	app.Post("/htmx/shares/:share/delete", owner, c.shareDelete)
	app.Get("/share/:share", c.sharedRoot)
	app.Get("/share/:share/folders/:folder", c.sharedFolderRoute)
	app.Get("/share/:share/files/:file/download", c.sharedFileDownload)

	return app
}

func TestShareIndexListsTheAccount(t *testing.T) {
	t.Parallel()

	shares := &shareStub{shares: []entity.Share{{ID: _shareManageShare, UserID: _shareManageUser, FolderID: _shareManageFolder}}}
	folders := &fakeShareFolders{folder: entity.Folder{ID: _shareManageFolder, Name: "docs"}}

	status, _, body := folderActionResponse(
		t,
		shareManageApp(shares, folders, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodGet,
		"/share",
		nil,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if shares.listCalls != 1 || shares.lastListUserID != _shareManageUser {
		t.Errorf("List = %d/%q, want 1/%q", shares.listCalls, shares.lastListUserID, _shareManageUser)
	}

	if !strings.Contains(body, "/share/"+_shareManageShare) || !strings.Contains(body, "docs") {
		t.Errorf("body = %q, want the share row", body)
	}
}

func TestShareFormRendersTheFolder(t *testing.T) {
	t.Parallel()

	folders := &fakeShareFolders{folder: entity.Folder{ID: _shareManageFolder, Name: "docs"}}

	status, _, body := folderActionResponse(
		t,
		shareManageApp(&shareStub{}, folders, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodGet,
		"/htmx/folders/"+_shareManageFolder+"/share",
		nil,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if !strings.Contains(body, _shareManageFolder) {
		t.Errorf("body = %q, want the folder ID", body)
	}
}

func TestShareDrawerRendersTheShare(t *testing.T) {
	t.Parallel()

	expires := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	shares := &shareStub{share: entity.Share{
		ID:        _shareManageShare,
		UserID:    _shareManageUser,
		FolderID:  _shareManageFolder,
		ExpiresAt: &expires,
	}}
	folders := &fakeShareFolders{folder: entity.Folder{ID: _shareManageFolder, Name: "docs"}}

	status, _, body := folderActionResponse(
		t,
		shareManageApp(shares, folders, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodGet,
		"/htmx/shares/"+_shareManageShare,
		nil,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if !strings.Contains(body, "/share/"+_shareManageShare) || !strings.Contains(body, "2026-12-01 10:00") {
		t.Errorf("body = %q, want the share link and expiry", body)
	}
}

func TestShareCreateStoresTheShare(t *testing.T) {
	t.Parallel()

	shares := &shareStub{}

	status, headers, _ := folderActionResponse(
		t,
		shareManageApp(shares, &fakeShareFolders{}, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodPost,
		"/htmx/shares",
		url.Values{"folder": {_shareManageFolder}, "expires_at": {"2026-12-01T10:00"}},
		nil,
	)

	if status != http.StatusFound {
		t.Fatalf("status = %d, want %d", status, http.StatusFound)
	}

	if got := headers.Get(fiber.HeaderLocation); got != "/share" {
		t.Errorf("Location = %q, want /share", got)
	}

	if shares.createCalls != 1 || shares.createUserID != _shareManageUser || shares.createFolderID != _shareManageFolder {
		t.Errorf("Create = %d/%q/%q, want 1/%q/%q", shares.createCalls, shares.createUserID, shares.createFolderID, _shareManageUser, _shareManageFolder)
	}

	want := time.Date(2026, 12, 1, 10, 0, 0, 0, time.Local).UTC()
	if shares.createExpiry == nil || !shares.createExpiry.Equal(want) {
		t.Errorf("expiry = %v, want %v", shares.createExpiry, want)
	}
}

func TestShareCreateRejectsAnInvalidExpiry(t *testing.T) {
	t.Parallel()

	shares := &shareStub{}

	status, _, _ := folderActionResponse(
		t,
		shareManageApp(shares, &fakeShareFolders{}, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodPost,
		"/htmx/shares",
		url.Values{"folder": {_shareManageFolder}, "expires_at": {"not-a-date"}},
		nil,
	)

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
	}

	if shares.createCalls != 0 {
		t.Errorf("Create calls = %d, want 0", shares.createCalls)
	}
}

func TestShareCreateWithUnlimitedSendsNilExpiry(t *testing.T) {
	t.Parallel()

	shares := &shareStub{}

	status, _, _ := folderActionResponse(
		t,
		shareManageApp(shares, &fakeShareFolders{}, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodPost,
		"/htmx/shares",
		url.Values{"folder": {_shareManageFolder}, "unlimited": {"true"}},
		nil,
	)

	if status != http.StatusFound {
		t.Fatalf("status = %d, want %d", status, http.StatusFound)
	}

	if shares.createExpiry != nil {
		t.Errorf("expiry = %v, want nil", shares.createExpiry)
	}
}

func TestShareUpdateTimeRefreshesTheRows(t *testing.T) {
	t.Parallel()

	shares := &shareStub{shares: []entity.Share{{ID: _shareManageShare, UserID: _shareManageUser, FolderID: _shareManageFolder}}}
	folders := &fakeShareFolders{folder: entity.Folder{ID: _shareManageFolder, Name: "docs"}}

	status, headers, _ := folderActionResponse(
		t,
		shareManageApp(shares, folders, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodPost,
		"/htmx/shares/"+_shareManageShare+"/time",
		url.Values{"expires_at": {"2026-12-01T10:00"}},
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if shares.updateCalls != 1 || shares.updateShareID != _shareManageShare || shares.updateUserID != _shareManageUser {
		t.Errorf("UpdateExpiry = %d/%q/%q, want 1/%q/%q", shares.updateCalls, shares.updateShareID, shares.updateUserID, _shareManageShare, _shareManageUser)
	}

	if shares.listCalls != 1 {
		t.Errorf("List calls = %d, want 1", shares.listCalls)
	}

	if got := headers.Get(_hxTriggerHeader); got != _entrySavedEvent {
		t.Errorf("HX-Trigger = %q, want %q", got, _entrySavedEvent)
	}
}

func TestShareUpdateTimeRejectsAnotherOwnersShare(t *testing.T) {
	t.Parallel()

	shares := &shareStub{updateErr: entity.ErrShareNotFound}

	status, _, _ := folderActionResponse(
		t,
		shareManageApp(shares, &fakeShareFolders{}, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodPost,
		"/htmx/shares/"+_shareManageShare+"/time",
		url.Values{"unlimited": {"true"}},
		nil,
	)

	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
	}
}

func TestShareDeleteRefreshesTheRows(t *testing.T) {
	t.Parallel()

	shares := &shareStub{shares: []entity.Share{{ID: _shareManageShare, UserID: _shareManageUser, FolderID: _shareManageFolder}}}
	folders := &fakeShareFolders{folder: entity.Folder{ID: _shareManageFolder, Name: "docs"}}

	status, headers, _ := folderActionResponse(
		t,
		shareManageApp(shares, folders, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodPost,
		"/htmx/shares/"+_shareManageShare+"/delete",
		nil,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if shares.deleteCalls != 1 || shares.deleteShareID != _shareManageShare || shares.deleteUserID != _shareManageUser {
		t.Errorf("Delete = %d/%q/%q, want 1/%q/%q", shares.deleteCalls, shares.deleteShareID, shares.deleteUserID, _shareManageShare, _shareManageUser)
	}

	if got := headers.Get(_hxTriggerHeader); got != _entrySavedEvent {
		t.Errorf("HX-Trigger = %q, want %q", got, _entrySavedEvent)
	}
}

func TestShareDeleteRejectsABadID(t *testing.T) {
	t.Parallel()

	shares := &shareStub{}

	status, _, _ := folderActionResponse(
		t,
		shareManageApp(shares, &fakeShareFolders{}, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodPost,
		"/htmx/shares/not-a-uuid/delete",
		nil,
		nil,
	)

	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
	}

	if shares.deleteCalls != 0 {
		t.Errorf("Delete calls = %d, want 0", shares.deleteCalls)
	}
}

func TestSharedFileDownloadStreamsAFile(t *testing.T) {
	t.Parallel()

	shares := &shareStub{
		share: entity.Share{ID: _shareManageShare, UserID: _shareManageUser, FolderID: _shareManageFolder},
		file:  entity.File{ID: _shareManageFile, Name: "note.txt", ContentType: "text/plain", Size: 5},
	}
	files := &fakeSharedFile{
		reader: io.NopCloser(strings.NewReader("hello")),
		file:   entity.File{ID: _shareManageFile, Name: "note.txt", ContentType: "text/plain", Size: 5},
	}

	status, headers, body := folderActionResponse(
		t,
		shareManageApp(shares, &fakeShareFolders{}, files, &fakeShareUser{}),
		http.MethodGet,
		"/share/"+_shareManageShare+"/files/"+_shareManageFile+"/download",
		nil,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if body != "hello" {
		t.Errorf("body = %q, want hello", body)
	}

	if !strings.Contains(headers.Get(fiber.HeaderContentDisposition), "note.txt") {
		t.Errorf("Content-Disposition = %q, want note.txt", headers.Get(fiber.HeaderContentDisposition))
	}

	if shares.resolveFileCalls != 1 || shares.resolveFileShare != _shareManageShare || shares.resolveFileID != _shareManageFile {
		t.Errorf("ResolveFile = %d/%q/%q, want 1/%q/%q", shares.resolveFileCalls, shares.resolveFileShare, shares.resolveFileID, _shareManageShare, _shareManageFile)
	}
}

func TestSharedFileDownloadRejectsAnExpiredShare(t *testing.T) {
	t.Parallel()

	shares := &shareStub{resolveFileErr: entity.ErrShareExpired}

	status, _, body := folderActionResponse(
		t,
		shareManageApp(shares, &fakeShareFolders{}, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodGet,
		"/share/"+_shareManageShare+"/files/"+_shareManageFile+"/download",
		nil,
		nil,
	)

	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
	}

	if strings.Contains(body, "hello") {
		t.Errorf("body = %q, want no file bytes", body)
	}
}

func TestSharedFileDownloadRejectsAFileOutsideTheShare(t *testing.T) {
	t.Parallel()

	shares := &shareStub{resolveFileErr: entity.ErrFileNotFound}

	status, headers, _ := folderActionResponse(
		t,
		shareManageApp(shares, &fakeShareFolders{}, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodGet,
		"/share/"+_shareManageShare+"/files/"+_shareManageFile+"/download",
		nil,
		nil,
	)

	if status != http.StatusFound {
		t.Fatalf("status = %d, want %d", status, http.StatusFound)
	}

	if got := headers.Get(fiber.HeaderLocation); got != "/share/"+_shareManageShare {
		t.Errorf("Location = %q, want the share root", got)
	}
}

func TestSharedFileDownloadRejectsAnUnknownToken(t *testing.T) {
	t.Parallel()

	shares := &shareStub{resolveFileErr: entity.ErrShareNotFound}

	status, _, _ := folderActionResponse(
		t,
		shareManageApp(shares, &fakeShareFolders{}, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodGet,
		"/share/"+_shareManageShare+"/files/"+_shareManageFile+"/download",
		nil,
		nil,
	)

	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
	}
}

func TestSharedRootOpensTheShareRoot(t *testing.T) {
	t.Parallel()

	shares := &shareStub{
		share:  entity.Share{ID: _shareManageShare, UserID: _shareManageUser, FolderID: _shareManageFolder},
		folder: entity.Folder{ID: _shareManageFolder, Name: "docs"},
	}
	folders := &fakeShareFolders{view: entity.FolderView{
		Folder:     entity.Folder{ID: _shareManageFolder, Name: "docs"},
		Breadcrumb: []entity.FolderRef{{ID: _shareManageFolder, Name: "docs"}},
	}}

	status, _, body := folderActionResponse(
		t,
		shareManageApp(shares, folders, &fakeSharedFile{}, &fakeShareUser{}),
		http.MethodGet,
		"/share/"+_shareManageShare,
		nil,
		nil,
	)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if shares.resolves != 1 {
		t.Errorf("ResolveFolder calls = %d, want 1", shares.resolves)
	}

	if folders.lastUserID != _shareManageUser {
		t.Errorf("listing user = %q, want the share owner", folders.lastUserID)
	}

	if !strings.Contains(body, "docs") {
		t.Errorf("body = %q, want the shared folder", body)
	}
}

func TestSharedRootRedirectsTheOwnerToTheirDrive(t *testing.T) {
	t.Parallel()

	shares := &shareStub{
		share:  entity.Share{ID: _shareManageShare, UserID: _shareManageUser, FolderID: _shareManageFolder},
		folder: entity.Folder{ID: _shareManageFolder, ParentID: new("root-id"), Name: "docs"},
	}
	folders := &fakeShareFolders{folder: entity.Folder{ID: _shareManageFolder, ParentID: new("root-id"), Name: "docs"}}
	users := &fakeShareUser{session: entity.Session{UserID: _shareManageUser}}

	status, headers, _ := folderActionResponse(
		t,
		shareManageApp(shares, folders, &fakeSharedFile{}, users),
		http.MethodGet,
		"/share/"+_shareManageShare,
		nil,
		map[string]string{fiber.HeaderCookie: _authCookieName + "=session-1"},
	)

	if status != http.StatusFound {
		t.Fatalf("status = %d, want %d", status, http.StatusFound)
	}

	want := "/folders/" + _shareManageFolder
	if got := headers.Get(fiber.HeaderLocation); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}
