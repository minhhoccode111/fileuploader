package folder

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/multipart"
)

// errStorage is the failure the fake bucket reports.
var errStorage = errors.New("storage failed")

// fakeFolderRepo answers from the folders the test set up and records what the
// use case asked it to store or delete.
type fakeFolderRepo struct {
	byID       map[string]entity.Folder
	root       entity.Folder
	rootErr    error
	getErr     error
	items      []entity.FolderItem
	refs       []entity.FolderRef
	taken      bool
	nameErr    error
	storeErr   error
	renameErr  error
	moveErr    error
	deleteErr  error
	listAllErr error
	all        []entity.Folder
	stored     *entity.Folder
	renamed    *entity.Folder
	deletedID  string
}

func (f *fakeFolderRepo) Store(_ context.Context, folder *entity.Folder) error {
	if f.storeErr != nil {
		return f.storeErr
	}

	f.stored = folder

	return nil
}

func (f *fakeFolderRepo) Get(_ context.Context, _, folderID string) (entity.Folder, error) {
	if f.getErr != nil {
		return entity.Folder{}, f.getErr
	}

	folder, ok := f.byID[folderID]
	if !ok {
		return entity.Folder{}, entity.ErrFolderNotFound
	}

	return folder, nil
}

func (f *fakeFolderRepo) GetRoot(_ context.Context, _ string) (entity.Folder, error) {
	return f.root, f.rootErr
}

func (f *fakeFolderRepo) Lock(ctx context.Context, userID, folderID string) (entity.Folder, error) {
	return f.Get(ctx, userID, folderID)
}

func (f *fakeFolderRepo) LockUser(_ context.Context, _ string) error {
	return nil
}

func (f *fakeFolderRepo) LockSubtree(ctx context.Context, userID, folderID string) error {
	_, err := f.Get(ctx, userID, folderID)

	return err
}

func (f *fakeFolderRepo) List(_ context.Context, _, _ string) ([]entity.FolderItem, error) {
	return f.items, nil
}

func (f *fakeFolderRepo) Breadcrumb(_ context.Context, _, _ string) ([]entity.FolderRef, error) {
	return f.refs, nil
}

func (f *fakeFolderRepo) NameTaken(_ context.Context, _, _, _ string) (bool, error) {
	return f.taken, f.nameErr
}

func (f *fakeFolderRepo) Rename(_ context.Context, folder *entity.Folder) error {
	if f.renameErr != nil {
		return f.renameErr
	}

	f.renamed = folder

	return nil
}

func (f *fakeFolderRepo) Move(_ context.Context, _, _, _ string) error {
	return f.moveErr
}

func (f *fakeFolderRepo) Delete(_ context.Context, _, folderID string) error {
	f.deletedID = folderID

	return f.deleteErr
}

func (f *fakeFolderRepo) ListAll(_ context.Context, _ string) ([]entity.Folder, error) {
	if f.listAllErr != nil {
		return nil, f.listAllErr
	}

	if f.all != nil {
		return f.all, nil
	}

	out := make([]entity.Folder, 0, len(f.byID))
	for _, folder := range f.byID {
		out = append(out, folder)
	}

	return out, nil
}

// fakeFileRepo supplies the file side of the folder checks.
type fakeFileRepo struct {
	items     []entity.FileItem
	nameTaken bool
	nameErr   error
	keys      []string
	keysErr   error
	files     []entity.File
	filesErr  error
	file      entity.File
	fileErr   error
}

func (f *fakeFileRepo) Store(_ context.Context, _ *entity.File) error {
	return nil
}

func (f *fakeFileRepo) Get(_ context.Context, _, _ string) (entity.File, error) {
	if f.file.ID != "" {
		return f.file, f.fileErr
	}

	return entity.File{}, entity.ErrFileNotFound
}

func (f *fakeFileRepo) List(_ context.Context, _, _ string) ([]entity.FileItem, error) {
	return f.items, nil
}

func (f *fakeFileRepo) Rename(_ context.Context, _ *entity.File) error {
	return nil
}

func (f *fakeFileRepo) Move(_ context.Context, _, _, _ string) error {
	return nil
}

func (f *fakeFileRepo) Delete(_ context.Context, _, _ string) (entity.File, error) {
	return entity.File{}, entity.ErrFileNotFound
}

func (f *fakeFileRepo) NameTaken(_ context.Context, _, _, _ string) (bool, error) {
	return f.nameTaken, f.nameErr
}

func (f *fakeFileRepo) DeleteByFolderSubtree(_ context.Context, _, _ string) ([]entity.File, error) {
	if f.files != nil {
		return f.files, f.filesErr
	}

	if f.keysErr != nil {
		return nil, f.keysErr
	}

	out := make([]entity.File, 0, len(f.keys))
	for _, key := range f.keys {
		out = append(out, entity.File{StorageKey: key})
	}

	return out, nil
}

// fakeUnitOfWork runs the callback and records that it was used.
type fakeUnitOfWork struct {
	called bool
}

func (f *fakeUnitOfWork) Do(ctx context.Context, fn func(context.Context) error) error {
	f.called = true

	return fn(ctx)
}

// fakeStorage records the objects a delete removed.
type fakeStorage struct {
	removed []string
	err     error
}

func (f *fakeStorage) PutObject(
	_ context.Context,
	_, _ string,
	_ io.Reader,
	_ int64,
	_ string,
) error {
	return nil
}

func (f *fakeStorage) PresignedGetObject(
	_ context.Context,
	_, _ string,
	_ time.Duration,
	_ string,
) (string, error) {
	return "", nil
}

func (f *fakeStorage) RemoveObject(_ context.Context, _, key string) error {
	f.removed = append(f.removed, key)

	return f.err
}

func (f *fakeStorage) CopyObject(_ context.Context, _, _, _ string) error {
	return nil
}

func (f *fakeStorage) GetObject(_ context.Context, _, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (f *fakeStorage) CreateMultipartUpload(_ context.Context, _, _, _ string) (string, error) {
	return "multipart-id", nil
}

func (f *fakeStorage) UploadPart(_ context.Context, _, _, _ string, _ int, _ io.Reader, _ int64) (string, error) {
	return "etag", nil
}

func (f *fakeStorage) CompleteMultipartUpload(_ context.Context, _, _, _ string, _ []multipart.Part) error {
	return nil
}

func (f *fakeStorage) AbortMultipartUpload(_ context.Context, _, _, _ string) error {
	return nil
}

func TestCreateStoresTheFolder(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{byID: map[string]entity.Folder{
		"parent": {ID: "parent", UserID: "user"},
	}}
	uc := New(folders, &fakeFileRepo{}, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	folder, err := uc.Create(context.Background(), "user", "parent", "  Reports  ")
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if folders.stored == nil {
		t.Fatal("Create() did not store the folder")
	}

	if folder.Name != "Reports" || folders.stored.Name != "Reports" {
		t.Errorf("folder name = %q, want %q", folder.Name, "Reports")
	}

	if folder.ParentID == nil || *folder.ParentID != "parent" {
		t.Errorf("folder parent = %v, want parent", folder.ParentID)
	}
}

func TestCreateRejectsANameAnotherEntryHolds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		folders *fakeFolderRepo
		files   *fakeFileRepo
	}{
		{
			name:    "a folder holds it",
			folders: &fakeFolderRepo{taken: true},
			files:   &fakeFileRepo{},
		},
		{
			name:    "a file holds it",
			folders: &fakeFolderRepo{},
			files:   &fakeFileRepo{nameTaken: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.folders.byID = map[string]entity.Folder{"parent": {ID: "parent"}}

			uc := New(tc.folders, tc.files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

			_, err := uc.Create(context.Background(), "user", "parent", "Reports")
			if !errors.Is(err, entity.ErrEntryAlreadyExists) {
				t.Errorf("Create() error = %v, want %v", err, entity.ErrEntryAlreadyExists)
			}

			if tc.folders.stored != nil {
				t.Error("Create() stored a folder with a taken name")
			}
		})
	}
}

func TestCreateRejectsAnUnknownParent(t *testing.T) {
	t.Parallel()

	uc := New(&fakeFolderRepo{}, &fakeFileRepo{}, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	_, err := uc.Create(context.Background(), "user", "missing", "Reports")
	if !errors.Is(err, entity.ErrFolderNotFound) {
		t.Errorf("Create() error = %v, want %v", err, entity.ErrFolderNotFound)
	}
}

func TestRenameStoresTheNewName(t *testing.T) {
	t.Parallel()

	parentID := "parent"

	folders := &fakeFolderRepo{byID: map[string]entity.Folder{
		"child":  {ID: "child", UserID: "user", ParentID: &parentID, Name: "Old"},
		"parent": {ID: parentID, UserID: "user"},
	}}
	uc := New(folders, &fakeFileRepo{}, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	folder, err := uc.Rename(context.Background(), "user", "child", "  New  ")
	if err != nil {
		t.Fatalf("Rename() error = %v, want nil", err)
	}

	if folder.Name != "New" {
		t.Errorf("Rename() name = %q, want %q", folder.Name, "New")
	}

	if folders.renamed == nil || folders.renamed.Name != "New" {
		t.Errorf("Rename() stored %v, want the name New", folders.renamed)
	}
}

func TestRenameRejectsTheRootFolder(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{byID: map[string]entity.Folder{
		"root": {ID: "root", UserID: "user", Name: "/"},
	}}
	uc := New(folders, &fakeFileRepo{}, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	_, err := uc.Rename(context.Background(), "user", "root", "Home")
	if !errors.Is(err, entity.ErrRootFolder) {
		t.Fatalf("Rename() error = %v, want %v", err, entity.ErrRootFolder)
	}

	if folders.renamed != nil {
		t.Error("Rename() stored a new name for the root folder")
	}
}

func TestRenameRejectsANameAnotherEntryHolds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		folders *fakeFolderRepo
		files   *fakeFileRepo
	}{
		{
			name:    "a folder holds it",
			folders: &fakeFolderRepo{taken: true},
			files:   &fakeFileRepo{},
		},
		{
			name:    "a file holds it",
			folders: &fakeFolderRepo{},
			files:   &fakeFileRepo{nameTaken: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parentID := "parent"

			tc.folders.byID = map[string]entity.Folder{
				"child":  {ID: "child", UserID: "user", ParentID: &parentID, Name: "Old"},
				"parent": {ID: parentID, UserID: "user"},
			}

			uc := New(tc.folders, tc.files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

			_, err := uc.Rename(context.Background(), "user", "child", "New")
			if !errors.Is(err, entity.ErrEntryAlreadyExists) {
				t.Errorf("Rename() error = %v, want %v", err, entity.ErrEntryAlreadyExists)
			}

			if tc.folders.renamed != nil {
				t.Error("Rename() stored a name another entry holds")
			}
		})
	}
}

func TestDeleteRejectsTheRootFolder(t *testing.T) {
	t.Parallel()

	root := entity.Folder{ID: "root", UserID: "user"}
	folders := &fakeFolderRepo{byID: map[string]entity.Folder{"root": root}}
	uc := New(folders, &fakeFileRepo{}, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	_, err := uc.Delete(context.Background(), "user", "root")
	if !errors.Is(err, entity.ErrRootFolder) {
		t.Fatalf("Delete() error = %v, want %v", err, entity.ErrRootFolder)
	}

	if folders.deletedID != "" {
		t.Errorf("Delete() removed %q, want nothing", folders.deletedID)
	}
}

func TestDeleteRemovesTheSubtreeAndItsObjects(t *testing.T) {
	t.Parallel()

	parentID := "parent"

	child := entity.Folder{ID: "child", UserID: "user", ParentID: &parentID}
	parent := entity.Folder{ID: parentID, UserID: "user"}

	folders := &fakeFolderRepo{byID: map[string]entity.Folder{
		child.ID:  child,
		parent.ID: parent,
	}}
	files := &fakeFileRepo{keys: []string{"one", "two"}}
	storage := &fakeStorage{}
	uc := New(folders, files, storage, &fakeUnitOfWork{}, "bucket")

	got, err := uc.Delete(context.Background(), "user", child.ID)
	if err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}

	if folders.deletedID != child.ID {
		t.Errorf("Delete() removed %q, want %q", folders.deletedID, child.ID)
	}

	if got.ID != parent.ID {
		t.Errorf("Delete() parent = %q, want %q", got.ID, parent.ID)
	}

	if len(storage.removed) != 2 || storage.removed[0] != "one" || storage.removed[1] != "two" {
		t.Errorf("removed objects = %v, want [one two]", storage.removed)
	}
}

func TestDeleteReportsACleanupFailure(t *testing.T) {
	t.Parallel()

	parentID := "parent"

	folders := &fakeFolderRepo{byID: map[string]entity.Folder{
		"child":  {ID: "child", UserID: "user", ParentID: &parentID},
		"parent": {ID: parentID, UserID: "user"},
	}}
	files := &fakeFileRepo{keys: []string{"one"}}
	uc := New(folders, files, &fakeStorage{err: errStorage}, &fakeUnitOfWork{}, "bucket")

	_, err := uc.Delete(context.Background(), "user", "child")
	if !errors.Is(err, errStorage) {
		t.Errorf("Delete() error = %v, want %v", err, errStorage)
	}
}

func TestListReadsTheRootWhenNoFolderCameIn(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{
		root:  entity.Folder{ID: "root", UserID: "user"},
		refs:  []entity.FolderRef{{ID: "root", Name: "/"}},
		items: []entity.FolderItem{{ID: "child", Name: "Reports"}},
	}
	files := &fakeFileRepo{items: []entity.FileItem{{ID: "file", Name: "note.txt"}}}
	uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	view, err := uc.List(context.Background(), "user", "")
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if view.Folder.ID != "root" {
		t.Errorf("List() folder = %q, want root", view.Folder.ID)
	}

	if len(view.Folders) != 1 || len(view.Files) != 1 {
		t.Errorf("List() returned %d folders and %d files, want 1 and 1", len(view.Folders), len(view.Files))
	}
}

func TestMoveChangesParent(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{byID: map[string]entity.Folder{
		"source":      {ID: "source", UserID: "user", ParentID: new("old"), Name: "Reports"},
		"old":         {ID: "old", UserID: "user", Name: "Old"},
		"destination": {ID: "destination", UserID: "user", Name: "Destination"},
	}}
	uc := New(folders, &fakeFileRepo{}, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	moved, err := uc.Move(context.Background(), "user", "source", "destination")
	if err != nil {
		t.Fatalf("Move() error = %v, want nil", err)
	}

	if moved.ParentID == nil || *moved.ParentID != "destination" {
		t.Errorf("moved parent = %v, want destination", moved.ParentID)
	}
}

func TestMoveRejectsRootSelfAndDescendant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		folderID      string
		destinationID string
		folders       map[string]entity.Folder
		wantErr       error
	}{
		{
			name:          "root",
			folderID:      "root",
			destinationID: "other",
			folders: map[string]entity.Folder{
				"root":  {ID: "root", UserID: "user", Name: "/"},
				"other": {ID: "other", UserID: "user", ParentID: new("root"), Name: "Other"},
			},
			wantErr: entity.ErrRootFolder,
		},
		{
			name:          "self",
			folderID:      "source",
			destinationID: "source",
			folders: map[string]entity.Folder{
				"source": {ID: "source", UserID: "user", ParentID: new("root"), Name: "Source"},
				"root":   {ID: "root", UserID: "user", Name: "/"},
			},
			wantErr: entity.ErrFolderCycle,
		},
		{
			name:          "descendant",
			folderID:      "parent",
			destinationID: "child",
			folders: map[string]entity.Folder{
				"root":   {ID: "root", UserID: "user", Name: "/"},
				"parent": {ID: "parent", UserID: "user", ParentID: new("root"), Name: "Parent"},
				"child":  {ID: "child", UserID: "user", ParentID: new("parent"), Name: "Child"},
			},
			wantErr: entity.ErrFolderCycle,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			folders := &fakeFolderRepo{byID: tc.folders}
			uc := New(folders, &fakeFileRepo{}, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

			_, err := uc.Move(context.Background(), "user", tc.folderID, tc.destinationID)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Move() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestMoveRejectsNameCollision(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{byID: map[string]entity.Folder{
		"source":      {ID: "source", UserID: "user", ParentID: new("old"), Name: "Reports"},
		"old":         {ID: "old", UserID: "user", Name: "Old"},
		"destination": {ID: "destination", UserID: "user", Name: "Destination"},
	}}
	files := &fakeFileRepo{nameTaken: true}
	uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	_, err := uc.Move(context.Background(), "user", "source", "destination")
	if !errors.Is(err, entity.ErrEntryAlreadyExists) {
		t.Errorf("Move() error = %v, want %v", err, entity.ErrEntryAlreadyExists)
	}
}

// fakeUserRepoForFolder is a minimal user repository for quota tests.
type fakeUserRepoForFolder struct {
	user    entity.User
	updated *entity.User
	getErr  error
}

func (f *fakeUserRepoForFolder) Store(_ context.Context, _ *entity.User) error { return nil }
func (f *fakeUserRepoForFolder) GetByID(_ context.Context, _ string) (entity.User, error) {
	return f.user, f.getErr
}

func (f *fakeUserRepoForFolder) GetByEmail(_ context.Context, _ string) (entity.User, error) {
	return f.user, f.getErr
}

func (f *fakeUserRepoForFolder) List(_ context.Context, _ *entity.UserFilter) (entity.UserList, error) {
	return entity.UserList{}, nil
}

func (f *fakeUserRepoForFolder) UpdateRole(_ context.Context, _ string, _ entity.UserRole) error {
	return nil
}

func (f *fakeUserRepoForFolder) Update(_ context.Context, user *entity.User) error {
	f.updated = user

	return nil
}

func (f *fakeUserRepoForFolder) SetUserLimits(_ context.Context, _ string, _, _, _ int64) error {
	return nil
}

func (f *fakeUserRepoForFolder) AdjustStorageUsed(_ context.Context, _ string, delta int64) error {
	f.user.StorageUsedBytes += delta
	updated := f.user
	f.updated = &updated

	return nil
}

func TestDeleteFreesQuotaFolder(t *testing.T) {
	t.Parallel()

	parentID := "parent"

	folders := &fakeFolderRepo{byID: map[string]entity.Folder{
		"child":  {ID: "child", UserID: "user", ParentID: &parentID},
		"parent": {ID: parentID, UserID: "user"},
	}}
	files := &fakeFileRepo{files: []entity.File{{StorageKey: "one", Size: 4}}}
	users := &fakeUserRepoForFolder{user: entity.User{StorageQuotaBytes: 10, StorageUsedBytes: 4}}
	storage := &fakeStorage{}
	uc := New(users, folders, files, storage, &fakeUnitOfWork{}, "bucket")

	_, err := uc.Delete(context.Background(), "user", "child")
	if err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}

	if users.updated == nil || users.updated.StorageUsedBytes != 0 {
		t.Errorf("usage = %v, want 0", users.updated)
	}
}

func TestDuplicateChargesQuotaFolder(t *testing.T) {
	t.Parallel()

	rootID := "root"

	folders := &fakeFolderRepo{byID: map[string]entity.Folder{
		"root":   {ID: rootID, UserID: "user", Name: "/"},
		"source": {ID: "source", UserID: "user", ParentID: &rootID, Name: "Source"},
		"dest":   {ID: "dest", UserID: "user", ParentID: &rootID, Name: "Dest"},
	}}
	files := &fakeFileRepo{
		items: []entity.FileItem{{ID: "file1", Name: "note.txt"}},
		file:  entity.File{ID: "file1", UserID: "user", FolderID: "source", StorageKey: "users/user/files/file1", Size: 4, ContentType: "text/plain", Name: "note.txt"},
	}
	users := &fakeUserRepoForFolder{user: entity.User{StorageQuotaBytes: 4, StorageUsedBytes: 2}}
	storage := &fakeStorage{}
	uc := New(users, folders, files, storage, &fakeUnitOfWork{}, "bucket")

	_, err := uc.Duplicate(context.Background(), "user", "source", "dest")
	if !errors.Is(err, entity.ErrStorageQuotaExceeded) {
		t.Fatalf("Duplicate() error = %v, want %v", err, entity.ErrStorageQuotaExceeded)
	}
}

func TestDuplicateUsesGlobalDefaultQuota(t *testing.T) {
	t.Parallel()

	rootID := "root"

	folders := &fakeFolderRepo{byID: map[string]entity.Folder{
		"root":   {ID: rootID, UserID: "user", Name: "/"},
		"source": {ID: "source", UserID: "user", ParentID: &rootID, Name: "Source"},
		"dest":   {ID: "dest", UserID: "user", ParentID: &rootID, Name: "Dest"},
	}}
	files := &fakeFileRepo{
		items: []entity.FileItem{{ID: "file1", Name: "note.txt"}},
		file:  entity.File{ID: "file1", UserID: "user", FolderID: "source", StorageKey: "users/user/files/file1", Size: 4, ContentType: "text/plain", Name: "note.txt"},
	}
	users := &fakeUserRepoForFolder{user: entity.User{StorageQuotaBytes: 0, StorageUsedBytes: 2}}
	storage := &fakeStorage{}
	uc := New(
		users,
		folders,
		files,
		storage,
		&fakeUnitOfWork{},
		"bucket",
		usecase.Limits{StorageQuotaBytes: 4},
	)

	_, err := uc.Duplicate(context.Background(), "user", "source", "dest")
	if !errors.Is(err, entity.ErrStorageQuotaExceeded) {
		t.Fatalf("Duplicate() error = %v, want %v", err, entity.ErrStorageQuotaExceeded)
	}
}
