package file

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
)

// errStore is the failure the fake file repository reports for a write.
var errStore = errors.New("store failed")

// fakeFileRepo answers from the file the test set up and records the writes
// the use case asked for.
type fakeFileRepo struct {
	file      entity.File
	getErr    error
	nameTaken bool
	nameErr   error
	storeErr  error
	renameErr error
	deleteErr error
	stored    *entity.File
	renamed   *entity.File
	deleted   bool
}

func (f *fakeFileRepo) Store(_ context.Context, file *entity.File) error {
	if f.storeErr != nil {
		return f.storeErr
	}

	f.stored = file

	return nil
}

func (f *fakeFileRepo) Get(_ context.Context, _, _ string) (entity.File, error) {
	return f.file, f.getErr
}

func (f *fakeFileRepo) List(_ context.Context, _, _ string) ([]entity.FileItem, error) {
	return nil, nil
}

func (f *fakeFileRepo) Rename(_ context.Context, file *entity.File) error {
	if f.renameErr != nil {
		return f.renameErr
	}

	f.renamed = file

	return nil
}

func (f *fakeFileRepo) Move(_ context.Context, _, _, _ string) error {
	return nil
}

func (f *fakeFileRepo) Delete(_ context.Context, _, _ string) (entity.File, error) {
	f.deleted = true

	return f.file, f.deleteErr
}

func (f *fakeFileRepo) NameTaken(_ context.Context, _, _, _ string) (bool, error) {
	return f.nameTaken, f.nameErr
}

func (f *fakeFileRepo) DeleteByFolderSubtree(_ context.Context, _, _ string) ([]entity.File, error) {
	return nil, nil
}

// fakeFolderRepo supplies the folder lookup and the folder side of the name
// check.
type fakeFolderRepo struct {
	folder    entity.Folder
	getErr    error
	nameTaken bool
	nameErr   error
}

func (f *fakeFolderRepo) Store(_ context.Context, _ *entity.Folder) error {
	return nil
}

func (f *fakeFolderRepo) Get(_ context.Context, _, _ string) (entity.Folder, error) {
	return f.folder, f.getErr
}

func (f *fakeFolderRepo) GetRoot(_ context.Context, _ string) (entity.Folder, error) {
	return entity.Folder{}, nil
}

func (f *fakeFolderRepo) Lock(_ context.Context, _, _ string) (entity.Folder, error) {
	return f.folder, f.getErr
}

func (f *fakeFolderRepo) LockUser(_ context.Context, _ string) error {
	return nil
}

func (f *fakeFolderRepo) LockSubtree(_ context.Context, _, _ string) error {
	return f.getErr
}

func (f *fakeFolderRepo) List(_ context.Context, _, _ string) ([]entity.FolderItem, error) {
	return nil, nil
}

func (f *fakeFolderRepo) Breadcrumb(_ context.Context, _, _ string) ([]entity.FolderRef, error) {
	return nil, nil
}

func (f *fakeFolderRepo) NameTaken(_ context.Context, _, _, _ string) (bool, error) {
	return f.nameTaken, f.nameErr
}

func (f *fakeFolderRepo) Rename(_ context.Context, _ *entity.Folder) error {
	return nil
}

func (f *fakeFolderRepo) Move(_ context.Context, _, _, _ string) error {
	return nil
}

func (f *fakeFolderRepo) Delete(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeFolderRepo) ListAll(_ context.Context, _ string) ([]entity.Folder, error) {
	return nil, nil
}

// fakeUnitOfWork runs the callback and records that it was used.
type fakeUnitOfWork struct {
	called bool
}

func (f *fakeUnitOfWork) Do(ctx context.Context, fn func(context.Context) error) error {
	f.called = true

	return fn(ctx)
}

// fakeStorage records the object the use case wrote, removed or signed.
type fakeStorage struct {
	putKey     string
	putSize    int64
	putType    string
	putErr     error
	copyKey    string
	copySrcKey string
	copyErr    error
	removeKey  string
	removeErr  error
	presign    string
	presignErr error
}

func (f *fakeStorage) PutObject(
	_ context.Context,
	_, key string,
	_ io.Reader,
	size int64,
	contentType string,
) error {
	f.putKey = key
	f.putSize = size
	f.putType = contentType

	return f.putErr
}

func (f *fakeStorage) PresignedGetObject(
	_ context.Context,
	_, _ string,
	_ time.Duration,
	_ string,
) (string, error) {
	return f.presign, f.presignErr
}

func (f *fakeStorage) RemoveObject(_ context.Context, _, key string) error {
	f.removeKey = key

	return f.removeErr
}

func (f *fakeStorage) CopyObject(_ context.Context, _, dstKey, srcKey string) error {
	f.copyKey = dstKey
	f.copySrcKey = srcKey

	return f.copyErr
}

func (f *fakeStorage) GetObject(_ context.Context, _, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func TestUploadStoresTheBytesAndTheRow(t *testing.T) {
	t.Parallel()

	files := &fakeFileRepo{}
	folders := &fakeFolderRepo{folder: entity.Folder{ID: "folder", UserID: "user"}}
	storage := &fakeStorage{}
	uc := New(files, folders, storage, &fakeUnitOfWork{}, "bucket", 1024)

	file, err := uc.Upload(
		context.Background(),
		"user",
		"folder",
		"  note.txt  ",
		"text/plain; charset=utf-8",
		4,
		strings.NewReader("data"),
	)
	if err != nil {
		t.Fatalf("Upload() error = %v, want nil", err)
	}

	if files.stored == nil {
		t.Fatal("Upload() did not store the row")
	}

	if file.Name != "note.txt" {
		t.Errorf("file name = %q, want %q", file.Name, "note.txt")
	}

	if storage.putSize != 4 || storage.putType != "text/plain" {
		t.Errorf("stored %d bytes as %q, want 4 as text/plain", storage.putSize, storage.putType)
	}

	if storage.putKey != "users/user/files/"+file.ID {
		t.Errorf("object key = %q, want it built from the identifiers", storage.putKey)
	}
}

func TestUploadRejectsAFileOverTheLimit(t *testing.T) {
	t.Parallel()

	storage := &fakeStorage{}
	uc := New(&fakeFileRepo{}, &fakeFolderRepo{}, storage, &fakeUnitOfWork{}, "bucket", 4)

	_, err := uc.Upload(
		context.Background(),
		"user",
		"folder",
		"note.txt",
		"text/plain",
		5,
		strings.NewReader("data!"),
	)
	if !errors.Is(err, entity.ErrFileTooLarge) {
		t.Fatalf("Upload() error = %v, want %v", err, entity.ErrFileTooLarge)
	}

	if storage.putKey != "" {
		t.Error("Upload() wrote an object over the limit")
	}
}

func TestUploadRejectsANameAnotherEntryHolds(t *testing.T) {
	t.Parallel()

	storage := &fakeStorage{}
	uc := New(
		&fakeFileRepo{},
		&fakeFolderRepo{folder: entity.Folder{ID: "folder"}, nameTaken: true},
		storage,
		&fakeUnitOfWork{},
		"bucket",
		1024,
	)

	_, err := uc.Upload(
		context.Background(),
		"user",
		"folder",
		"note.txt",
		"text/plain",
		4,
		strings.NewReader("data"),
	)
	if !errors.Is(err, entity.ErrEntryAlreadyExists) {
		t.Fatalf("Upload() error = %v, want %v", err, entity.ErrEntryAlreadyExists)
	}

	if storage.putKey != "" {
		t.Error("Upload() wrote an object for a taken name")
	}
}

func TestUploadRemovesTheObjectWhenTheRowFails(t *testing.T) {
	t.Parallel()

	files := &fakeFileRepo{storeErr: errStore}
	folders := &fakeFolderRepo{folder: entity.Folder{ID: "folder"}}
	storage := &fakeStorage{}
	uc := New(files, folders, storage, &fakeUnitOfWork{}, "bucket", 1024)

	_, err := uc.Upload(
		context.Background(),
		"user",
		"folder",
		"note.txt",
		"text/plain",
		4,
		strings.NewReader("data"),
	)
	if !errors.Is(err, errStore) {
		t.Fatalf("Upload() error = %v, want %v", err, errStore)
	}

	if storage.removeKey != storage.putKey || storage.removeKey == "" {
		t.Errorf("removed key = %q, want the key just written %q", storage.removeKey, storage.putKey)
	}
}

func TestUploadReportsAnUnknownFolder(t *testing.T) {
	t.Parallel()

	folders := &fakeFolderRepo{getErr: entity.ErrFolderNotFound}
	uc := New(&fakeFileRepo{}, folders, &fakeStorage{}, &fakeUnitOfWork{}, "bucket", 1024)

	_, err := uc.Upload(
		context.Background(),
		"user",
		"missing",
		"note.txt",
		"text/plain",
		4,
		strings.NewReader("data"),
	)
	if !errors.Is(err, entity.ErrFolderNotFound) {
		t.Errorf("Upload() error = %v, want %v", err, entity.ErrFolderNotFound)
	}
}

func TestRenameChecksTheFolderSideOfTheName(t *testing.T) {
	t.Parallel()

	files := &fakeFileRepo{file: entity.File{
		ID:       "file",
		UserID:   "user",
		FolderID: "folder",
		Name:     "old.txt",
	}}
	folders := &fakeFolderRepo{nameTaken: true}
	uc := New(files, folders, &fakeStorage{}, &fakeUnitOfWork{}, "bucket", 1024)

	_, err := uc.Rename(context.Background(), "user", "file", "new.txt")
	if !errors.Is(err, entity.ErrEntryAlreadyExists) {
		t.Fatalf("Rename() error = %v, want %v", err, entity.ErrEntryAlreadyExists)
	}

	if files.renamed != nil {
		t.Error("Rename() stored a name another entry holds")
	}
}

func TestRenameStoresTheNewName(t *testing.T) {
	t.Parallel()

	files := &fakeFileRepo{file: entity.File{
		ID:       "file",
		UserID:   "user",
		FolderID: "folder",
		Name:     "old.txt",
	}}
	uc := New(files, &fakeFolderRepo{}, &fakeStorage{}, &fakeUnitOfWork{}, "bucket", 1024)

	file, err := uc.Rename(context.Background(), "user", "file", "new.txt")
	if err != nil {
		t.Fatalf("Rename() error = %v, want nil", err)
	}

	if file.Name != "new.txt" || files.renamed == nil || files.renamed.Name != "new.txt" {
		t.Errorf("renamed file = %q, want %q", file.Name, "new.txt")
	}
}

func TestDeleteRemovesTheRowAndTheObject(t *testing.T) {
	t.Parallel()

	files := &fakeFileRepo{file: entity.File{
		ID:         "file",
		UserID:     "user",
		FolderID:   "folder",
		StorageKey: "users/user/files/file",
	}}
	storage := &fakeStorage{}
	uc := New(files, &fakeFolderRepo{}, storage, &fakeUnitOfWork{}, "bucket", 1024)

	file, err := uc.Delete(context.Background(), "user", "file")
	if err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}

	if !files.deleted {
		t.Error("Delete() kept the row")
	}

	if storage.removeKey != file.StorageKey {
		t.Errorf("removed key = %q, want %q", storage.removeKey, file.StorageKey)
	}
}

func TestDownloadSignsTheObject(t *testing.T) {
	t.Parallel()

	files := &fakeFileRepo{file: entity.File{
		ID:         "file",
		Name:       "note.txt",
		StorageKey: "users/user/files/file",
	}}
	storage := &fakeStorage{presign: "https://bucket.example/object"}
	uc := New(files, &fakeFolderRepo{}, storage, &fakeUnitOfWork{}, "bucket", 1024)

	link, err := uc.Download(context.Background(), "user", "file")
	if err != nil {
		t.Fatalf("Download() error = %v, want nil", err)
	}

	if link != storage.presign {
		t.Errorf("Download() link = %q, want %q", link, storage.presign)
	}
}

// fakeUserRepoForFile is a minimal user repository for quota tests.
type fakeUserRepoForFile struct {
	user    entity.User
	updated *entity.User
	getErr  error
}

func (f *fakeUserRepoForFile) Store(_ context.Context, _ *entity.User) error { return nil }
func (f *fakeUserRepoForFile) GetByID(_ context.Context, _ string) (entity.User, error) {
	return f.user, f.getErr
}

func (f *fakeUserRepoForFile) GetByEmail(_ context.Context, _ string) (entity.User, error) {
	return f.user, f.getErr
}

func (f *fakeUserRepoForFile) List(_ context.Context, _ *entity.UserFilter) (entity.UserList, error) {
	return entity.UserList{}, nil
}

func (f *fakeUserRepoForFile) UpdateRole(_ context.Context, _ string, _ entity.UserRole) error {
	return nil
}

func (f *fakeUserRepoForFile) Update(_ context.Context, user *entity.User) error {
	f.updated = user

	return nil
}

func (f *fakeUserRepoForFile) SetUserLimits(_ context.Context, _ string, _, _, _ int64) error {
	return nil
}

func (f *fakeUserRepoForFile) AdjustStorageUsed(_ context.Context, _ string, delta int64) error {
	f.user.StorageUsedBytes += delta
	updated := f.user
	f.updated = &updated

	return nil
}

func TestUploadRejectsWhenQuotaExceeded(t *testing.T) {
	t.Parallel()

	users := &fakeUserRepoForFile{user: entity.User{StorageQuotaBytes: 4, StorageUsedBytes: 2}}
	files := &fakeFileRepo{}
	folders := &fakeFolderRepo{folder: entity.Folder{ID: "folder", UserID: "user"}}
	storage := &fakeStorage{}
	uc := New(users, files, folders, storage, &fakeUnitOfWork{}, "bucket", 1024)

	_, err := uc.Upload(context.Background(), "user", "folder", "note.txt", "text/plain", 4, strings.NewReader("data"))
	if !errors.Is(err, entity.ErrStorageQuotaExceeded) {
		t.Fatalf("Upload() error = %v, want %v", err, entity.ErrStorageQuotaExceeded)
	}

	if storage.putKey != "" {
		t.Error("Upload() wrote an object over quota")
	}
}

func TestUploadUsesGlobalDefaultQuota(t *testing.T) {
	t.Parallel()

	users := &fakeUserRepoForFile{user: entity.User{StorageQuotaBytes: 0, StorageUsedBytes: 2}}
	files := &fakeFileRepo{}
	folders := &fakeFolderRepo{folder: entity.Folder{ID: "folder", UserID: "user"}}
	storage := &fakeStorage{}
	uc := New(
		users,
		files,
		folders,
		storage,
		&fakeUnitOfWork{},
		"bucket",
		1024,
		usecase.Limits{StorageQuotaBytes: 4},
	)

	_, err := uc.Upload(context.Background(), "user", "folder", "note.txt", "text/plain", 4, strings.NewReader("data"))
	if !errors.Is(err, entity.ErrStorageQuotaExceeded) {
		t.Fatalf("Upload() error = %v, want %v", err, entity.ErrStorageQuotaExceeded)
	}
}

func TestUploadIncrementsUsage(t *testing.T) {
	t.Parallel()

	users := &fakeUserRepoForFile{user: entity.User{StorageQuotaBytes: 10, StorageUsedBytes: 0}}
	files := &fakeFileRepo{}
	folders := &fakeFolderRepo{folder: entity.Folder{ID: "folder", UserID: "user"}}
	storage := &fakeStorage{}
	uc := New(users, files, folders, storage, &fakeUnitOfWork{}, "bucket", 1024)

	_, err := uc.Upload(context.Background(), "user", "folder", "note.txt", "text/plain", 4, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload() error = %v, want nil", err)
	}

	if users.updated == nil || users.updated.StorageUsedBytes != 4 {
		t.Errorf("usage = %v, want 4", users.updated)
	}
}

func TestDeleteFreesQuota(t *testing.T) {
	t.Parallel()

	users := &fakeUserRepoForFile{user: entity.User{StorageQuotaBytes: 10, StorageUsedBytes: 4}}
	files := &fakeFileRepo{file: entity.File{ID: "file", UserID: "user", FolderID: "folder", StorageKey: "users/user/files/file", Size: 4}}
	folders := &fakeFolderRepo{}
	storage := &fakeStorage{}
	uc := New(users, files, folders, storage, &fakeUnitOfWork{}, "bucket", 1024)

	_, err := uc.Delete(context.Background(), "user", "file")
	if err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}

	if users.updated == nil || users.updated.StorageUsedBytes != 0 {
		t.Errorf("usage = %v, want 0", users.updated)
	}
}

func TestDuplicateChargesQuota(t *testing.T) {
	t.Parallel()

	users := &fakeUserRepoForFile{user: entity.User{StorageQuotaBytes: 4, StorageUsedBytes: 2}}
	files := &fakeFileRepo{file: entity.File{ID: "file", UserID: "user", FolderID: "folder", StorageKey: "users/user/files/file", Size: 4, ContentType: "text/plain", Name: "note.txt"}}
	folders := &fakeFolderRepo{folder: entity.Folder{ID: "folder"}}
	storage := &fakeStorage{}
	uc := New(users, files, folders, storage, &fakeUnitOfWork{}, "bucket", 1024)

	_, err := uc.Duplicate(context.Background(), "user", "file", "folder")
	if !errors.Is(err, entity.ErrStorageQuotaExceeded) {
		t.Fatalf("Duplicate() error = %v, want %v", err, entity.ErrStorageQuotaExceeded)
	}
}
