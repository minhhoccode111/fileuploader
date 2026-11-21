package folder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/internal/usecase/pace"
	"github.com/minhhoccode111/fileuploader/pkg/multipart"
)

// errStorage is the failure the fake bucket reports.
var errStorage = errors.New("storage failed")

// fakeFolderRepo answers from the folders the test set up and records what the
// use case asked it to store, delete or page through.
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

	folderPage       func(parentID, search string, after *entity.EntryCursor, limit int) ([]entity.FolderItem, error)
	pageErr          error
	lastFolderUserID string
	lastParentID     string
	lastFolderSearch string
	lastFolderAfter  *entity.EntryCursor
	lastFolderLimit  int
	folderCalls      int

	paths       map[string][]entity.FolderRef
	pathsErr    error
	lastPathsID string
	pathIDs     []string
	pathCalls   int
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

func (f *fakeFolderRepo) ListPage(
	_ context.Context,
	userID, parentID, search string,
	after *entity.EntryCursor,
	limit int,
) ([]entity.FolderItem, error) {
	f.folderCalls++
	f.lastFolderUserID = userID
	f.lastParentID = parentID
	f.lastFolderSearch = search
	f.lastFolderAfter = after
	f.lastFolderLimit = limit

	if f.pageErr != nil {
		return nil, f.pageErr
	}

	if f.folderPage != nil {
		return f.folderPage(parentID, search, after, limit)
	}

	return f.items, nil
}

func (f *fakeFolderRepo) Paths(
	_ context.Context,
	userID string,
	folderIDs []string,
) (map[string][]entity.FolderRef, error) {
	f.pathCalls++
	f.lastPathsID = userID
	f.pathIDs = append(f.pathIDs, folderIDs...)

	if f.pathsErr != nil {
		return nil, f.pathsErr
	}

	if f.paths != nil {
		return f.paths, nil
	}

	return map[string][]entity.FolderRef{}, nil
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

	filePage       func(folderID, search string, after *entity.EntryCursor, limit int) ([]entity.FileItem, error)
	pageErr        error
	lastFileUserID string
	lastFileFolder string
	lastFileSearch string
	lastFileAfter  *entity.EntryCursor
	lastFileLimit  int
	fileCalls      int
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

func (f *fakeFileRepo) ListPage(
	_ context.Context,
	userID, folderID, search string,
	after *entity.EntryCursor,
	limit int,
) ([]entity.FileItem, error) {
	f.fileCalls++
	f.lastFileUserID = userID
	f.lastFileFolder = folderID
	f.lastFileSearch = search
	f.lastFileAfter = after
	f.lastFileLimit = limit

	if f.pageErr != nil {
		return nil, f.pageErr
	}

	if f.filePage != nil {
		return f.filePage(folderID, search, after, limit)
	}

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
	removed    []string
	objectBody []byte
	err        error
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
	return io.NopCloser(bytes.NewReader(f.objectBody)), nil
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

func (f *fakeStorage) ListIncompleteUploads(
	_ context.Context,
	_ string,
) ([]multipart.IncompleteUpload, error) {
	return nil, nil
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

	view, err := uc.List(context.Background(), "user", "", "", nil)
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

// pageFolders builds the folder page a fake returns: name-ordered items, as
// many as the repository was asked for.
func pageFolders(limit int) []entity.FolderItem {
	items := make([]entity.FolderItem, 0, limit)

	for i := range limit {
		items = append(items, entity.FolderItem{
			ID:   fmt.Sprintf("folder-%03d", i),
			Name: fmt.Sprintf("folder %03d", i),
		})
	}

	return items
}

// pageFiles builds the file page a fake returns.
func pageFiles(limit int) []entity.FileItem {
	items := make([]entity.FileItem, 0, limit)

	for i := range limit {
		items = append(items, entity.FileItem{
			ID:       fmt.Sprintf("file-%03d", i),
			FolderID: "folder",
			Name:     fmt.Sprintf("file %03d", i),
		})
	}

	return items
}

func TestListMergesFoldersBeforeFiles(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{
		byID: map[string]entity.Folder{"folder": {ID: "folder", UserID: "user"}},
		refs: []entity.FolderRef{{ID: "folder", Name: "Docs"}},
		folderPage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FolderItem, error) {
			return []entity.FolderItem{
				{ID: "folder-b", Name: "Beta"},
				{ID: "folder-a", Name: "Alpha"},
			}, nil
		},
	}
	files := &fakeFileRepo{filePage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FileItem, error) {
		return []entity.FileItem{
			{ID: "file-b", Name: "b.txt"},
			{ID: "file-a", Name: "a.txt"},
		}, nil
	}}
	uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	view, err := uc.List(context.Background(), "user", "folder", "note", nil)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if len(view.Folders) != 2 || view.Folders[0].Name != "Beta" || view.Folders[1].Name != "Alpha" {
		t.Errorf("List() folders = %+v, want Beta then Alpha", view.Folders)
	}

	if len(view.Files) != 2 || view.Files[0].Name != "b.txt" || view.Files[1].Name != "a.txt" {
		t.Errorf("List() files = %+v, want b.txt then a.txt", view.Files)
	}

	if view.Search != "note" {
		t.Errorf("List() search = %q, want %q", view.Search, "note")
	}

	if view.Next != nil {
		t.Errorf("List() next = %+v, want nil", view.Next)
	}
}

func TestListStopsAtTheFolderLimit(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{
		byID: map[string]entity.Folder{"folder": {ID: "folder", UserID: "user"}},
		folderPage: func(_, _ string, _ *entity.EntryCursor, limit int) ([]entity.FolderItem, error) {
			return pageFolders(limit), nil
		},
	}
	files := &fakeFileRepo{}
	uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	view, err := uc.List(context.Background(), "user", "folder", "", nil)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if len(view.Folders) != 100 {
		t.Fatalf("List() returned %d folders, want 100", len(view.Folders))
	}

	if files.fileCalls != 0 {
		t.Errorf("List() asked for files %d times, want 0", files.fileCalls)
	}

	if view.Next == nil {
		t.Fatal("List() next = nil, want a folder cursor")
	}

	if view.Next.Kind != entity.EntryKindFolder {
		t.Errorf("next kind = %q, want %q", view.Next.Kind, entity.EntryKindFolder)
	}

	if view.Next.ID != "folder-099" {
		t.Errorf("next id = %q, want %q", view.Next.ID, "folder-099")
	}

	if view.Next.FoldersDone {
		t.Error("next FoldersDone = true, want false")
	}
}

func TestListFillsTheRestWithFiles(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{
		byID: map[string]entity.Folder{"folder": {ID: "folder", UserID: "user"}},
		folderPage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FolderItem, error) {
			return pageFolders(3), nil
		},
	}
	files := &fakeFileRepo{filePage: func(_, _ string, _ *entity.EntryCursor, limit int) ([]entity.FileItem, error) {
		return pageFiles(limit), nil
	}}
	uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	view, err := uc.List(context.Background(), "user", "folder", "", nil)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if len(view.Folders) != 3 {
		t.Errorf("List() returned %d folders, want 3", len(view.Folders))
	}

	if len(view.Files) != 97 {
		t.Fatalf("List() returned %d files, want 97", len(view.Files))
	}

	if files.lastFileLimit != 98 {
		t.Errorf("file page limit = %d, want 98", files.lastFileLimit)
	}

	if files.lastFileAfter != nil {
		t.Errorf("file page after = %+v, want the first file", files.lastFileAfter)
	}

	if view.Next == nil || view.Next.Kind != entity.EntryKindFile {
		t.Fatalf("List() next = %+v, want a file cursor", view.Next)
	}

	if view.Next.ID != "file-096" {
		t.Errorf("next id = %q, want %q", view.Next.ID, "file-096")
	}
}

func TestListDetectsFilesBehindAFullFolderPage(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{
		byID: map[string]entity.Folder{"folder": {ID: "folder", UserID: "user"}},
		folderPage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FolderItem, error) {
			return pageFolders(100), nil
		},
	}
	files := &fakeFileRepo{filePage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FileItem, error) {
		return pageFiles(1), nil
	}}
	uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	view, err := uc.List(context.Background(), "user", "folder", "", nil)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if len(view.Folders) != 100 {
		t.Errorf("List() returned %d folders, want 100", len(view.Folders))
	}

	if len(view.Files) != 0 {
		t.Errorf("List() returned %d files, want 0", len(view.Files))
	}

	if files.lastFileLimit != 1 {
		t.Errorf("file page limit = %d, want 1", files.lastFileLimit)
	}

	if view.Next == nil {
		t.Fatal("List() next = nil, want a folder cursor")
	}

	if view.Next.Kind != entity.EntryKindFolder || !view.Next.FoldersDone {
		t.Errorf("next = %+v, want a folder cursor with FoldersDone", view.Next)
	}

	if view.Next.ID != "folder-099" {
		t.Errorf("next id = %q, want %q", view.Next.ID, "folder-099")
	}
}

func TestListContinuesAFilePageWithoutFolders(t *testing.T) {
	t.Parallel()

	cursor := &entity.EntryCursor{Kind: entity.EntryKindFile, Name: "file 050", ID: "file-050"}

	folders := &fakeFolderRepo{
		byID: map[string]entity.Folder{"folder": {ID: "folder", UserID: "user"}},
	}
	files := &fakeFileRepo{filePage: func(_, _ string, after *entity.EntryCursor, limit int) ([]entity.FileItem, error) {
		if after != cursor {
			t.Errorf("file page after = %+v, want the cursor handed in", after)
		}

		return pageFiles(limit), nil
	}}
	uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	view, err := uc.List(context.Background(), "user", "folder", "", cursor)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if folders.folderCalls != 0 {
		t.Errorf("List() asked for folders %d times, want 0", folders.folderCalls)
	}

	if len(view.Folders) != 0 {
		t.Errorf("List() returned %d folders, want 0", len(view.Folders))
	}

	if len(view.Files) != 100 {
		t.Errorf("List() returned %d files, want 100", len(view.Files))
	}

	if files.lastFileLimit != 101 {
		t.Errorf("file page limit = %d, want 101", files.lastFileLimit)
	}

	if view.Next == nil || view.Next.Kind != entity.EntryKindFile {
		t.Fatalf("List() next = %+v, want a file cursor", view.Next)
	}
}

// A folder cursor with FoldersDone means the folder pass already ended: the
// next page skips it and starts the files at the first row behind the folder
// batch instead of restarting the listing.
func TestListContinuesAfterAFullFolderPage(t *testing.T) {
	t.Parallel()

	cursor := &entity.EntryCursor{
		Kind:        entity.EntryKindFolder,
		Name:        "folder 099",
		ID:          "folder-099",
		FoldersDone: true,
	}

	folders := &fakeFolderRepo{
		byID: map[string]entity.Folder{"folder": {ID: "folder", UserID: "user"}},
	}
	files := &fakeFileRepo{filePage: func(_, _ string, after *entity.EntryCursor, limit int) ([]entity.FileItem, error) {
		if after != nil {
			t.Errorf("file page after = %+v, want the first file", after)
		}

		if limit != _pageSize+1 {
			t.Errorf("file page limit = %d, want %d", limit, _pageSize+1)
		}

		return pageFiles(limit), nil
	}}
	uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	view, err := uc.List(context.Background(), "user", "folder", "", cursor)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if folders.folderCalls != 0 {
		t.Errorf("List() asked for folders %d times, want 0", folders.folderCalls)
	}

	if len(view.Folders) != 0 {
		t.Errorf("List() returned %d folders, want 0", len(view.Folders))
	}

	if len(view.Files) != _pageSize {
		t.Errorf("List() returned %d files, want %d", len(view.Files), _pageSize)
	}
}

// A page that ends exactly at the last row carries no cursor: the merge learns
// a next page exists only from the extra row each repository is asked for.
func TestListOmitsTheCursorAtTheDataBoundary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		folders []entity.FolderItem
		files   []entity.FileItem
	}{
		{name: "a full folder page and no files", folders: pageFolders(_pageSize)},
		{name: "a full file page and no folders", files: pageFiles(_pageSize)},
		{name: "three folders and the rest files", folders: pageFolders(3), files: pageFiles(_pageSize - 3)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			folders := &fakeFolderRepo{
				byID: map[string]entity.Folder{"folder": {ID: "folder", UserID: "user"}},
				folderPage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FolderItem, error) {
					return tc.folders, nil
				},
			}
			files := &fakeFileRepo{filePage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FileItem, error) {
				return tc.files, nil
			}}
			uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

			view, err := uc.List(context.Background(), "user", "folder", "", nil)
			if err != nil {
				t.Fatalf("List() error = %v, want nil", err)
			}

			if view.Next != nil {
				t.Errorf("List() next = %+v, want no next page", view.Next)
			}
		})
	}
}

func TestListPassesTheFolderAndSearchToBothRepos(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{
		byID:       map[string]entity.Folder{"folder": {ID: "folder", UserID: "user"}},
		folderPage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FolderItem, error) { return nil, nil },
	}
	files := &fakeFileRepo{filePage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FileItem, error) {
		return nil, nil
	}}
	uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	_, err := uc.List(context.Background(), "user", "folder", "note", nil)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if folders.lastParentID != "folder" {
		t.Errorf("folder parent = %q, want %q", folders.lastParentID, "folder")
	}

	if folders.lastFolderSearch != "note" {
		t.Errorf("folder search = %q, want %q", folders.lastFolderSearch, "note")
	}

	if files.lastFileFolder != "folder" {
		t.Errorf("file folder = %q, want %q", files.lastFileFolder, "folder")
	}

	if files.lastFileSearch != "note" {
		t.Errorf("file search = %q, want %q", files.lastFileSearch, "note")
	}
}

func TestSearchRequiresThreeCharacters(t *testing.T) {
	t.Parallel()

	for _, search := range []string{"", "a", "ab"} {
		folders := &fakeFolderRepo{}
		files := &fakeFileRepo{}
		uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

		view, err := uc.Search(context.Background(), "user", search, "from", nil)
		if err != nil {
			t.Fatalf("Search(%q) error = %v, want nil", search, err)
		}

		if !view.TooShort {
			t.Errorf("Search(%q) TooShort = false, want true", search)
		}

		if folders.folderCalls != 0 || files.fileCalls != 0 || folders.pathCalls != 0 {
			t.Errorf(
				"Search(%q) made %d folder, %d file and %d path calls, want none",
				search,
				folders.folderCalls,
				files.fileCalls,
				folders.pathCalls,
			)
		}
	}
}

func TestSearchScopesTheWholeDriveToTheUser(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{folderPage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FolderItem, error) {
		return []entity.FolderItem{{ID: "folder-1", Name: "Docs"}}, nil
	}}
	files := &fakeFileRepo{filePage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FileItem, error) {
		return []entity.FileItem{{ID: "file-1", FolderID: "folder-1", Name: "notes.txt"}}, nil
	}}
	uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	_, err := uc.Search(context.Background(), "alice", "nova", "", nil)
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}

	if folders.lastFolderUserID != "alice" {
		t.Errorf("folder user = %q, want %q", folders.lastFolderUserID, "alice")
	}

	if folders.lastParentID != "" {
		t.Errorf("folder parent = %q, want the whole drive", folders.lastParentID)
	}

	if files.lastFileUserID != "alice" {
		t.Errorf("file user = %q, want %q", files.lastFileUserID, "alice")
	}

	if files.lastFileFolder != "" {
		t.Errorf("file folder = %q, want the whole drive", files.lastFileFolder)
	}
}

func TestSearchFillsPathsForFoldersAndFiles(t *testing.T) {
	t.Parallel()

	root := entity.FolderRef{ID: "root", Name: "/"}
	folderPath := []entity.FolderRef{root, {ID: "folder-1", Name: "Docs"}}
	filePath := []entity.FolderRef{root, {ID: "folder-2", Name: "Media"}}

	folders := &fakeFolderRepo{
		folderPage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FolderItem, error) {
			return []entity.FolderItem{{ID: "folder-1", Name: "Docs"}}, nil
		},
		paths: map[string][]entity.FolderRef{
			"folder-1": folderPath,
			"folder-2": filePath,
		},
	}
	files := &fakeFileRepo{filePage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FileItem, error) {
		return []entity.FileItem{{ID: "file-1", FolderID: "folder-2", Name: "clip.mp4"}}, nil
	}}
	uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	view, err := uc.Search(context.Background(), "user", "docs", "", nil)
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}

	if folders.pathCalls != 1 {
		t.Errorf("Paths called %d times, want 1", folders.pathCalls)
	}

	if folders.lastPathsID != "user" {
		t.Errorf("Paths user = %q, want %q", folders.lastPathsID, "user")
	}

	if len(view.Folders) != 1 || !reflect.DeepEqual(view.Folders[0].Path, folderPath) {
		t.Errorf("folder path = %+v, want %+v", view.Folders, folderPath)
	}

	if len(view.Files) != 1 || !reflect.DeepEqual(view.Files[0].Path, filePath) {
		t.Errorf("file path = %+v, want %+v", view.Files, filePath)
	}
}

// A path lookup that fails while filling the results names the step that
// failed, so the log points at the search and not at the listing.
func TestSearchLabelsAPathLookupFailure(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{
		folderPage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FolderItem, error) {
			return []entity.FolderItem{{ID: "folder-1", Name: "Docs"}}, nil
		},
		pathsErr: errStorage,
	}
	uc := New(folders, &fakeFileRepo{}, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	_, err := uc.Search(context.Background(), "user", "docs", "", nil)
	if !errors.Is(err, errStorage) {
		t.Fatalf("Search() error = %v, want %v", err, errStorage)
	}

	if !strings.Contains(err.Error(), "FolderUseCase - Search - uc.fillPaths") {
		t.Errorf("Search() error = %v, want the fillPaths step", err)
	}
}

func TestSearchLeavesLocalPathsEmpty(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{
		byID: map[string]entity.Folder{"folder": {ID: "folder", UserID: "user"}},
		folderPage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FolderItem, error) {
			return []entity.FolderItem{{ID: "folder-1", Name: "Docs"}}, nil
		},
	}
	files := &fakeFileRepo{filePage: func(_, _ string, _ *entity.EntryCursor, _ int) ([]entity.FileItem, error) {
		return []entity.FileItem{{ID: "file-1", FolderID: "folder", Name: "notes.txt"}}, nil
	}}
	uc := New(folders, files, &fakeStorage{}, &fakeUnitOfWork{}, "bucket")

	view, err := uc.List(context.Background(), "user", "folder", "", nil)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if folders.pathCalls != 0 {
		t.Errorf("Paths called %d times, want 0", folders.pathCalls)
	}

	if len(view.Folders[0].Path) != 0 || len(view.Files[0].Path) != 0 {
		t.Errorf("local paths = %+v and %+v, want empty", view.Folders[0].Path, view.Files[0].Path)
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

func (f *fakeUserRepoForFolder) UpdateStatus(_ context.Context, _ string, _ entity.UserStatus) error {
	return nil
}

func (f *fakeUserRepoForFolder) Update(_ context.Context, user *entity.User) error {
	f.updated = user

	return nil
}

func (f *fakeUserRepoForFolder) UpdateProfile(_ context.Context, _, _, _ string) error { return nil }

func (f *fakeUserRepoForFolder) UpdatePassword(_ context.Context, _, _ string) error { return nil }

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

type fakePacer struct {
	calls []fakePace
}

type fakePace struct {
	userID string
	scope  repo.TransferScope
	bytes  int64
	rate   int64
}

func (f *fakePacer) Pace(
	_ context.Context,
	userID string,
	scope repo.TransferScope,
	size, rate int64,
) error {
	f.calls = append(f.calls, fakePace{userID: userID, scope: scope, bytes: size, rate: rate})

	return nil
}

// The archive stream is paced on the account download budget, so a folder
// download cannot outrun the same limit as a single file.
func TestDownloadZipPacesTheArchive(t *testing.T) {
	t.Parallel()

	const rate = int64(1) << 20

	rootID := "root"

	folders := &fakeFolderRepo{byID: map[string]entity.Folder{
		rootID: {ID: rootID, UserID: "user", Name: "/"},
	}}
	files := &fakeFileRepo{
		items: []entity.FileItem{{ID: "file1", Name: "note.txt"}},
		file: entity.File{
			ID:         "file1",
			UserID:     "user",
			FolderID:   rootID,
			StorageKey: "users/user/files/file1",
			Size:       1 << 20,
			Name:       "note.txt",
		},
	}
	users := &fakeUserRepoForFolder{user: entity.User{DownloadRateBytesPerSec: rate}}
	pacer := &fakePacer{}
	uc := New(
		users,
		folders,
		files,
		&fakeStorage{objectBody: make([]byte, 1<<20)},
		&fakeUnitOfWork{},
		pacer,
		"bucket",
	)

	reader, filename, err := uc.DownloadZip(context.Background(), "user", rootID)
	if err != nil {
		t.Fatalf("DownloadZip() error = %v, want nil", err)
	}
	defer reader.Close()

	if filename != "root.zip" {
		t.Errorf("filename = %q, want %q", filename, "root.zip")
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}

	if len(got) == 0 {
		t.Fatal("archive is empty")
	}

	if len(pacer.calls) == 0 {
		t.Fatal("pacer calls = 0, want at least one")
	}

	checkArchivePaces(t, pacer.calls, rate, int64(len(got)))
}

// checkArchivePaces asserts each reservation uses the download budget and the
// calls add up to the archive that left the server.
func checkArchivePaces(t *testing.T, calls []fakePace, rate, archiveLen int64) {
	t.Helper()

	batch := int64(pace.BatchSize(rate))

	var paced int64

	for _, call := range calls {
		if call.userID != "user" {
			t.Errorf("paced user = %q, want %q", call.userID, "user")
		}

		if call.scope != repo.TransferDownload {
			t.Errorf("paced scope = %q, want %q", call.scope, repo.TransferDownload)
		}

		if call.rate != rate {
			t.Errorf("paced rate = %d, want %d", call.rate, rate)
		}

		if call.bytes > batch {
			t.Errorf("paced %d bytes in one call, want at most %d", call.bytes, batch)
		}

		paced += call.bytes
	}

	if paced != archiveLen {
		t.Errorf("paced bytes = %d, want the archive size %d", paced, archiveLen)
	}
}
