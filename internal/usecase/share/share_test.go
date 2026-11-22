package share

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
)

// errStore is the failure the fake share repository reports for a write.
var errStore = errors.New("share store failed")

type fakeShareRepo struct {
	repo.ShareRepo

	share       entity.Share
	active      entity.Share
	byFolder    entity.Share
	shares      []entity.Share
	activeErr   error
	byFolderErr error
	storeErr    error
	updateErr   error
	deleteErr   error
	getErr      error

	stored         *entity.Share
	updated        *entity.Share
	deletedID      string
	lastUserID     string
	lastFolderID   string
	lastShareID    string
	lastListUserID string
	updateCalls    int
	deleteCalls    int
	storeCalls     int
}

func (f *fakeShareRepo) Store(_ context.Context, share *entity.Share) error {
	f.storeCalls++

	if f.storeErr != nil {
		return f.storeErr
	}

	f.stored = share

	return nil
}

func (f *fakeShareRepo) Get(_ context.Context, shareID string) (entity.Share, error) {
	f.lastShareID = shareID

	return f.share, f.getErr
}

func (f *fakeShareRepo) GetActiveByUserAndFolder(
	_ context.Context,
	userID, folderID string,
) (entity.Share, error) {
	f.lastUserID = userID
	f.lastFolderID = folderID

	if f.activeErr != nil {
		return entity.Share{}, f.activeErr
	}

	if f.active.ID == "" {
		return entity.Share{}, entity.ErrShareNotFound
	}

	return f.active, nil
}

func (f *fakeShareRepo) GetByUserAndFolder(
	_ context.Context,
	userID, folderID string,
) (entity.Share, error) {
	f.lastUserID = userID
	f.lastFolderID = folderID

	if f.byFolderErr != nil {
		return entity.Share{}, f.byFolderErr
	}

	if f.byFolder.ID == "" {
		return entity.Share{}, entity.ErrShareNotFound
	}

	return f.byFolder, nil
}

func (f *fakeShareRepo) ListByUser(_ context.Context, userID string) ([]entity.Share, error) {
	f.lastListUserID = userID

	return f.shares, nil
}

func (f *fakeShareRepo) UpdateExpiry(
	_ context.Context,
	userID, shareID string,
	expiresAt *time.Time,
) error {
	f.updateCalls++
	f.lastUserID = userID
	f.lastShareID = shareID

	if f.updateErr != nil {
		return f.updateErr
	}

	f.updated = &entity.Share{ID: shareID, UserID: userID, ExpiresAt: expiresAt}

	return nil
}

func (f *fakeShareRepo) Delete(_ context.Context, userID, shareID string) error {
	f.deleteCalls++
	f.lastUserID = userID
	f.deletedID = shareID

	return f.deleteErr
}

type fakeFolderRepo struct {
	repo.FolderRepo

	folder           entity.Folder
	crumbs           []entity.FolderRef
	lockErr          error
	getErr           error
	crumbErr         error
	lastUserID       string
	lastFolder       string
	getUserID        string
	breadcrumbUserID string
	getCalls         int
	crumbCalls       int
}

func (f *fakeFolderRepo) LockUser(_ context.Context, _ string) error {
	return f.lockErr
}

func (f *fakeFolderRepo) Get(_ context.Context, userID, folderID string) (entity.Folder, error) {
	f.getCalls++
	f.lastUserID = userID
	f.getUserID = userID
	f.lastFolder = folderID

	if f.getErr != nil {
		return entity.Folder{}, f.getErr
	}

	return f.folder, nil
}

func (f *fakeFolderRepo) Breadcrumb(
	_ context.Context,
	userID, folderID string,
) ([]entity.FolderRef, error) {
	f.crumbCalls++
	f.lastUserID = userID
	f.breadcrumbUserID = userID
	f.lastFolder = folderID

	if f.crumbErr != nil {
		return nil, f.crumbErr
	}

	return f.crumbs, nil
}

type fakeFileRepo struct {
	repo.FileRepo

	file       entity.File
	getErr     error
	lastUserID string
	lastFile   string
}

func (f *fakeFileRepo) Get(_ context.Context, userID, fileID string) (entity.File, error) {
	f.lastUserID = userID
	f.lastFile = fileID

	if f.getErr != nil {
		return entity.File{}, f.getErr
	}

	return f.file, nil
}

type fakeUnitOfWork struct{ called bool }

func (f *fakeUnitOfWork) Do(ctx context.Context, fn func(context.Context) error) error {
	f.called = true

	return fn(ctx)
}

func newShareUseCase(
	shares repo.ShareRepo,
	folders repo.FolderRepo,
	files repo.FileRepo,
	uow repo.UnitOfWork,
) usecase.Share {
	return New(shares, folders, files, uow)
}

func TestCreateStoresAShare(t *testing.T) {
	t.Parallel()

	folderID := "folder-1"
	shares := &fakeShareRepo{}
	folders := &fakeFolderRepo{folder: entity.Folder{ID: folderID, UserID: "user-1"}}
	uow := &fakeUnitOfWork{}
	uc := newShareUseCase(shares, folders, &fakeFileRepo{}, uow)

	share, err := uc.Create(context.Background(), "user-1", folderID, nil)
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if share.ID == "" {
		t.Fatal("Create() returned an empty share ID")
	}

	if shares.stored == nil {
		t.Fatal("Create() did not store the share")
	}

	if shares.stored.UserID != "user-1" || shares.stored.FolderID != folderID {
		t.Errorf("stored share = %+v, want owner and folder", shares.stored)
	}

	if !uow.called {
		t.Error("Create() did not run inside the unit of work")
	}

	if shares.lastUserID != "user-1" || shares.lastFolderID != folderID {
		t.Errorf("lookup = %q/%q, want user-1/folder-1", shares.lastUserID, shares.lastFolderID)
	}
}

func TestCreateReusesAnActiveShare(t *testing.T) {
	t.Parallel()

	expiresAt := time.Now().UTC().Add(time.Hour)
	shares := &fakeShareRepo{active: entity.Share{
		ID:       "share-1",
		UserID:   "user-1",
		FolderID: "folder-1",
	}}
	folders := &fakeFolderRepo{folder: entity.Folder{ID: "folder-1", UserID: "user-1"}}
	uc := newShareUseCase(shares, folders, &fakeFileRepo{}, &fakeUnitOfWork{})

	share, err := uc.Create(context.Background(), "user-1", "folder-1", &expiresAt)
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if share.ID != "share-1" {
		t.Errorf("share ID = %q, want share-1", share.ID)
	}

	if shares.updateCalls != 1 {
		t.Fatalf("UpdateExpiry calls = %d, want 1", shares.updateCalls)
	}

	if shares.storeCalls != 0 {
		t.Errorf("Store calls = %d, want 0 for an active share", shares.storeCalls)
	}

	if share.ExpiresAt == nil || !share.ExpiresAt.Equal(expiresAt) {
		t.Errorf("expiry = %v, want %v", share.ExpiresAt, expiresAt)
	}
}

func TestCreateRevivesAnExpiredShare(t *testing.T) {
	t.Parallel()

	expired := time.Now().UTC().Add(-time.Hour)
	old := entity.Share{ID: "share-1", UserID: "user-1", FolderID: "folder-1", ExpiresAt: &expired}
	shares := &fakeShareRepo{byFolder: old}
	folders := &fakeFolderRepo{folder: entity.Folder{ID: "folder-1", UserID: "user-1"}}
	uc := newShareUseCase(shares, folders, &fakeFileRepo{}, &fakeUnitOfWork{})

	share, err := uc.Create(context.Background(), "user-1", "folder-1", nil)
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if share.ID != old.ID {
		t.Errorf("share ID = %q, want the revoked row %q", share.ID, old.ID)
	}

	if shares.updateCalls != 1 {
		t.Errorf("UpdateExpiry calls = %d, want 1", shares.updateCalls)
	}

	if shares.storeCalls != 0 {
		t.Errorf("Store calls = %d, want 0 for a revived share", shares.storeCalls)
	}
}

func TestCreateRejectsAnUnknownFolder(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{}
	folders := &fakeFolderRepo{getErr: entity.ErrFolderNotFound}
	uc := newShareUseCase(shares, folders, &fakeFileRepo{}, &fakeUnitOfWork{})

	_, err := uc.Create(context.Background(), "user-1", "missing", nil)
	if !errors.Is(err, entity.ErrFolderNotFound) {
		t.Fatalf("Create() error = %v, want ErrFolderNotFound", err)
	}

	if shares.storeCalls != 0 {
		t.Errorf("Store calls = %d, want 0", shares.storeCalls)
	}
}

func TestCreateReportsAStoreFailure(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{storeErr: errStore}
	folders := &fakeFolderRepo{folder: entity.Folder{ID: "folder-1", UserID: "user-1"}}
	uc := newShareUseCase(shares, folders, &fakeFileRepo{}, &fakeUnitOfWork{})

	_, err := uc.Create(context.Background(), "user-1", "folder-1", nil)
	if !errors.Is(err, errStore) {
		t.Fatalf("Create() error = %v, want %v", err, errStore)
	}

	if !strings.Contains(err.Error(), "ShareUseCase - Create") {
		t.Errorf("Create() error = %v, want the use-case step", err)
	}
}

func TestGetReturnsAShareWithoutExpiry(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{share: entity.Share{ID: "share-1"}}
	uc := newShareUseCase(shares, &fakeFolderRepo{}, &fakeFileRepo{}, &fakeUnitOfWork{})

	share, err := uc.Get(context.Background(), "share-1")
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}

	if share.ID != "share-1" {
		t.Errorf("Get() = %+v, want share-1", share)
	}
}

func TestGetRejectsAnExpiredShare(t *testing.T) {
	t.Parallel()

	expired := time.Now().UTC().Add(-time.Minute)
	shares := &fakeShareRepo{share: entity.Share{ID: "share-1", ExpiresAt: &expired}}
	uc := newShareUseCase(shares, &fakeFolderRepo{}, &fakeFileRepo{}, &fakeUnitOfWork{})

	_, err := uc.Get(context.Background(), "share-1")
	if !errors.Is(err, entity.ErrShareExpired) {
		t.Fatalf("Get() error = %v, want ErrShareExpired", err)
	}
}

func TestGetRejectsAShareAtTheExpiryInstant(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	shares := &fakeShareRepo{share: entity.Share{ID: "share-1", ExpiresAt: &now}}
	uc := newShareUseCase(shares, &fakeFolderRepo{}, &fakeFileRepo{}, &fakeUnitOfWork{})

	_, err := uc.Get(context.Background(), "share-1")
	if !errors.Is(err, entity.ErrShareExpired) {
		t.Fatalf("Get() error = %v, want ErrShareExpired", err)
	}
}

func TestGetRejectsAMissingShare(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{getErr: entity.ErrShareNotFound}
	uc := newShareUseCase(shares, &fakeFolderRepo{}, &fakeFileRepo{}, &fakeUnitOfWork{})

	_, err := uc.Get(context.Background(), "missing")
	if !errors.Is(err, entity.ErrShareNotFound) {
		t.Fatalf("Get() error = %v, want ErrShareNotFound", err)
	}
}

func TestResolveFolderUsesTheShareRootForAnEmptyID(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{share: entity.Share{ID: "share-1", UserID: "user-1", FolderID: "root"}}
	folders := &fakeFolderRepo{folder: entity.Folder{ID: "root", UserID: "user-1"}}
	uc := newShareUseCase(shares, folders, &fakeFileRepo{}, &fakeUnitOfWork{})

	_, folder, err := uc.ResolveFolder(context.Background(), "share-1", "")
	if err != nil {
		t.Fatalf("ResolveFolder() error = %v, want nil", err)
	}

	if folder.ID != "root" {
		t.Errorf("folder = %q, want root", folder.ID)
	}

	if folders.lastFolder != "root" {
		t.Errorf("lookup folder = %q, want root", folders.lastFolder)
	}
}

func TestResolveFolderAllowsTheShareRootAndADescendant(t *testing.T) {
	t.Parallel()

	const rootID = "root"

	cases := []struct {
		name   string
		folder entity.Folder
		crumbs []entity.FolderRef
	}{
		{
			name:   "the share root",
			folder: entity.Folder{ID: rootID, UserID: "user-1"},
			crumbs: []entity.FolderRef{{ID: rootID, Name: "/"}},
		},
		{
			name:   "a descendant",
			folder: entity.Folder{ID: "child", UserID: "user-1"},
			crumbs: []entity.FolderRef{{ID: rootID, Name: "/"}, {ID: "child", Name: "Docs"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			shares := &fakeShareRepo{share: entity.Share{ID: "share-1", UserID: "user-1", FolderID: rootID}}
			folders := &fakeFolderRepo{folder: tc.folder, crumbs: tc.crumbs}
			uc := newShareUseCase(shares, folders, &fakeFileRepo{}, &fakeUnitOfWork{})

			_, folder, err := uc.ResolveFolder(context.Background(), "share-1", tc.folder.ID)
			if err != nil {
				t.Fatalf("ResolveFolder() error = %v, want nil", err)
			}

			if folder.ID != tc.folder.ID {
				t.Errorf("folder = %q, want %q", folder.ID, tc.folder.ID)
			}
		})
	}
}

func TestResolveFolderRejectsASiblingOrAncestor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		crumbs []entity.FolderRef
	}{
		{
			name:   "a sibling",
			crumbs: []entity.FolderRef{{ID: "root", Name: "/"}, {ID: "sibling", Name: "Other"}},
		},
		{
			name:   "an ancestor",
			crumbs: []entity.FolderRef{{ID: "root", Name: "/"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			shares := &fakeShareRepo{share: entity.Share{ID: "share-1", UserID: "user-1", FolderID: "shared"}}
			folders := &fakeFolderRepo{
				folder: entity.Folder{ID: "outside", UserID: "user-1"},
				crumbs: tc.crumbs,
			}
			uc := newShareUseCase(shares, folders, &fakeFileRepo{}, &fakeUnitOfWork{})

			_, _, err := uc.ResolveFolder(context.Background(), "share-1", "outside")
			if !errors.Is(err, entity.ErrFolderNotFound) {
				t.Fatalf("ResolveFolder() error = %v, want ErrFolderNotFound", err)
			}
		})
	}
}

func TestResolveFolderRejectsAnotherOwnersFolder(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{share: entity.Share{ID: "share-1", UserID: "user-1", FolderID: "shared"}}
	folders := &fakeFolderRepo{getErr: entity.ErrFolderNotFound}
	uc := newShareUseCase(shares, folders, &fakeFileRepo{}, &fakeUnitOfWork{})

	_, _, err := uc.ResolveFolder(context.Background(), "share-1", "other")
	if !errors.Is(err, entity.ErrFolderNotFound) {
		t.Fatalf("ResolveFolder() error = %v, want ErrFolderNotFound", err)
	}

	if folders.crumbCalls != 0 {
		t.Errorf("Breadcrumb calls = %d, want 0 after a failed folder read", folders.crumbCalls)
	}
}

func TestResolveFolderScopesEveryReadToTheOwner(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{share: entity.Share{ID: "share-1", UserID: "owner-1", FolderID: "shared"}}
	folders := &fakeFolderRepo{
		folder: entity.Folder{ID: "child", UserID: "owner-1"},
		crumbs: []entity.FolderRef{{ID: "shared", Name: "/"}, {ID: "child", Name: "Docs"}},
	}
	uc := newShareUseCase(shares, folders, &fakeFileRepo{}, &fakeUnitOfWork{})

	_, _, err := uc.ResolveFolder(context.Background(), "share-1", "child")
	if err != nil {
		t.Fatalf("ResolveFolder() error = %v, want nil", err)
	}

	if folders.getUserID != "owner-1" || folders.breadcrumbUserID != "owner-1" {
		t.Errorf("read users = %q/%q, want owner-1/owner-1", folders.getUserID, folders.breadcrumbUserID)
	}
}

func TestResolveFileAllowsAFileInsideTheSubtree(t *testing.T) {
	t.Parallel()

	file, _, err := resolveFileFor(t, "user-1")
	if err != nil {
		t.Fatalf("ResolveFile() error = %v, want nil", err)
	}

	if file.ID != "file-1" {
		t.Errorf("file = %q, want file-1", file.ID)
	}
}

func TestResolveFileRejectsAFileOutsideTheSubtree(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{share: entity.Share{ID: "share-1", UserID: "user-1", FolderID: "root"}}
	folders := &fakeFolderRepo{
		folder: entity.Folder{ID: "outside", UserID: "user-1"},
		crumbs: []entity.FolderRef{{ID: "other-root", Name: "/"}, {ID: "outside", Name: "Other"}},
	}
	files := &fakeFileRepo{file: entity.File{ID: "file-1", UserID: "user-1", FolderID: "outside"}}
	uc := newShareUseCase(shares, folders, files, &fakeUnitOfWork{})

	_, _, err := uc.ResolveFile(context.Background(), "share-1", "file-1")
	if !errors.Is(err, entity.ErrFileNotFound) {
		t.Fatalf("ResolveFile() error = %v, want ErrFileNotFound", err)
	}
}

func TestResolveFileScopesTheReadToTheOwner(t *testing.T) {
	t.Parallel()

	_, files, err := resolveFileFor(t, "owner-1")
	if err != nil {
		t.Fatalf("ResolveFile() error = %v, want nil", err)
	}

	if files.lastUserID != "owner-1" {
		t.Errorf("file user = %q, want owner-1", files.lastUserID)
	}
}

// resolveFileFor resolves one file inside a share owned by owner.
func resolveFileFor(t *testing.T, owner string) (entity.File, *fakeFileRepo, error) {
	t.Helper()

	shares := &fakeShareRepo{share: entity.Share{ID: "share-1", UserID: owner, FolderID: "root"}}
	folders := &fakeFolderRepo{
		folder: entity.Folder{ID: "root", UserID: owner},
		crumbs: []entity.FolderRef{{ID: "root", Name: "/"}},
	}
	files := &fakeFileRepo{file: entity.File{ID: "file-1", UserID: owner, FolderID: "root"}}
	uc := newShareUseCase(shares, folders, files, &fakeUnitOfWork{})

	_, file, err := uc.ResolveFile(context.Background(), "share-1", "file-1")

	return file, files, err
}

func TestResolveFileRejectsAnUnknownFile(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{share: entity.Share{ID: "share-1", UserID: "user-1", FolderID: "root"}}
	files := &fakeFileRepo{getErr: entity.ErrFileNotFound}
	uc := newShareUseCase(shares, &fakeFolderRepo{}, files, &fakeUnitOfWork{})

	_, _, err := uc.ResolveFile(context.Background(), "share-1", "missing")
	if !errors.Is(err, entity.ErrFileNotFound) {
		t.Fatalf("ResolveFile() error = %v, want ErrFileNotFound", err)
	}
}

func TestCreateReportsAnUpdateExpiryFailure(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{
		active:    entity.Share{ID: "share-1", UserID: "user-1", FolderID: "folder-1"},
		updateErr: errStore,
	}
	folders := &fakeFolderRepo{folder: entity.Folder{ID: "folder-1", UserID: "user-1"}}
	uc := newShareUseCase(shares, folders, &fakeFileRepo{}, &fakeUnitOfWork{})

	_, err := uc.Create(context.Background(), "user-1", "folder-1", nil)
	if !errors.Is(err, errStore) {
		t.Fatalf("Create() error = %v, want %v", err, errStore)
	}

	if shares.storeCalls != 0 {
		t.Errorf("Store calls = %d, want 0 after a failed expiry update", shares.storeCalls)
	}
}

func TestDeleteReportsARepositoryFailure(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{deleteErr: errStore}
	uc := newShareUseCase(shares, &fakeFolderRepo{}, &fakeFileRepo{}, &fakeUnitOfWork{})

	err := uc.Delete(context.Background(), "user-1", "share-1")
	if !errors.Is(err, errStore) {
		t.Fatalf("Delete() error = %v, want %v", err, errStore)
	}
}

func TestDeleteScopesTheShareToTheOwner(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{}
	uc := newShareUseCase(shares, &fakeFolderRepo{}, &fakeFileRepo{}, &fakeUnitOfWork{})

	if err := uc.Delete(context.Background(), "user-1", "share-1"); err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}

	if shares.lastUserID != "user-1" || shares.deletedID != "share-1" {
		t.Errorf("delete = %q/%q, want user-1/share-1", shares.lastUserID, shares.deletedID)
	}
}

func TestUpdateExpiryRejectsAnotherOwnersShare(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{share: entity.Share{ID: "share-1", UserID: "other"}}
	uc := newShareUseCase(shares, &fakeFolderRepo{}, &fakeFileRepo{}, &fakeUnitOfWork{})

	_, err := uc.UpdateExpiry(context.Background(), "user-1", "share-1", nil)
	if !errors.Is(err, entity.ErrShareNotFound) {
		t.Fatalf("UpdateExpiry() error = %v, want ErrShareNotFound", err)
	}

	if shares.lastUserID != "user-1" {
		t.Errorf("update user = %q, want user-1", shares.lastUserID)
	}
}

func TestListDelegatesToTheRepository(t *testing.T) {
	t.Parallel()

	shares := &fakeShareRepo{shares: []entity.Share{{ID: "share-1"}}}
	uc := newShareUseCase(shares, &fakeFolderRepo{}, &fakeFileRepo{}, &fakeUnitOfWork{})

	list, err := uc.List(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if len(list) != 1 || list[0].ID != "share-1" {
		t.Errorf("List() = %+v, want share-1", list)
	}

	if shares.lastListUserID != "user-1" {
		t.Errorf("list user = %q, want user-1", shares.lastListUserID)
	}
}
