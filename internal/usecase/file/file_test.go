package file

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/internal/usecase/pace"
	"github.com/minhhoccode111/fileuploader/pkg/multipart"
)

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
	putKey           string
	putSize          int64
	putType          string
	putErr           error
	copyKey          string
	copySrcKey       string
	copyErr          error
	removeKey        string
	removeErr        error
	presign          string
	presignErr       error
	objectBody       []byte
	uploadPartNumber int
	uploadPartSize   int64
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
	return io.NopCloser(bytes.NewReader(f.objectBody)), nil
}

func (f *fakeStorage) CreateMultipartUpload(_ context.Context, _, _, _ string) (string, error) {
	return "multipart-id", nil
}

func (f *fakeStorage) UploadPart(_ context.Context, _, _, _ string, partNumber int, _ io.Reader, size int64) (string, error) {
	f.uploadPartNumber = partNumber
	f.uploadPartSize = size

	return "etag", nil
}

func (f *fakeStorage) CompleteMultipartUpload(_ context.Context, _, _, _ string, _ []multipart.Part) error {
	return nil
}

func (f *fakeStorage) AbortMultipartUpload(_ context.Context, _, _, _ string) error {
	return nil
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

// A download is paced at the account rate, on the download budget, before its
// bytes reach the transport.
func TestOpenDownloadPacesTheStream(t *testing.T) {
	t.Parallel()

	const (
		rate    = int64(1) << 20
		bodyLen = int64(1) << 20
	)

	files := &fakeFileRepo{file: entity.File{
		ID:         "file",
		Name:       "note.txt",
		StorageKey: "users/user/files/file",
		Size:       bodyLen,
	}}
	users := &fakeUserRepoForFile{user: entity.User{DownloadRateBytesPerSec: rate}}
	storage := &fakeStorage{objectBody: make([]byte, bodyLen)}
	pacer := &fakePacer{}
	uc := New(users, files, &fakeFolderRepo{}, storage, &fakeUnitOfWork{}, "bucket", pacer)

	reader, _, err := uc.OpenDownload(context.Background(), "user", "file")
	if err != nil {
		t.Fatalf("OpenDownload() error = %v, want nil", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}

	if int64(len(got)) != bodyLen {
		t.Fatalf("ReadAll() bytes = %d, want %d", len(got), bodyLen)
	}

	checkDownloadPaces(t, pacer.calls, rate, bodyLen)
}

// An account with no rate of its own inherits the global download default.
func TestOpenDownloadUsesTheGlobalDefaultRate(t *testing.T) {
	t.Parallel()

	const (
		rate    = int64(1) << 20
		bodyLen = int64(1) << 20
	)

	files := &fakeFileRepo{file: entity.File{ID: "file", StorageKey: "key", Size: bodyLen}}
	users := &fakeUserRepoForFile{user: entity.User{}}
	pacer := &fakePacer{}
	uc := New(
		users,
		files,
		&fakeFolderRepo{},
		&fakeStorage{objectBody: make([]byte, bodyLen)},
		&fakeUnitOfWork{},
		"bucket",
		pacer,
		usecase.Limits{DownloadRateBytesPerSec: rate},
	)

	reader, _, err := uc.OpenDownload(context.Background(), "user", "file")
	if err != nil {
		t.Fatalf("OpenDownload() error = %v, want nil", err)
	}
	defer reader.Close()

	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}

	checkDownloadPaces(t, pacer.calls, rate, bodyLen)
}

// checkDownloadPaces asserts each reservation uses the download budget and the
// calls add up to the whole stream.
func checkDownloadPaces(t *testing.T, calls []fakePace, rate, bodyLen int64) {
	t.Helper()

	if len(calls) == 0 {
		t.Fatal("pacer calls = 0, want at least one")
	}

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

	if paced != bodyLen {
		t.Errorf("paced bytes = %d, want %d", paced, bodyLen)
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

func (f *fakeUserRepoForFile) UpdateStatus(_ context.Context, _ string, _ entity.UserStatus) error {
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
