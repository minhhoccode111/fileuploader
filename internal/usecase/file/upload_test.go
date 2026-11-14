package file

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
)

// errUploadStore is the failure the fake upload repository reports for a write.
var errUploadStore = errors.New("upload store failed")

const (
	_testGiB        = int64(1) << 30
	_testMaxBytes   = int64(10) << 30
	_testPartBytes  = int64(50) << 20
	_testUploadTTL  = time.Hour
	_testSlowRate   = int64(100) * 1024
	_testSlowPartSz = int64(9_216_000)
)

// fakeUploadRepo keeps the sessions the use case stores and answers the
// queries a start makes, so a test can set the reserved headroom it sees.
type fakeUploadRepo struct {
	sessions  []entity.UploadSession
	reserved  []entity.UploadSession
	parts     map[string]map[int]string
	expired   []entity.UploadSession
	createErr error
	getErr    error
	listErr   error
	lockErr   error
	created   []entity.UploadSession
	lastTTL   time.Duration
	lockCalls int
	releases  int
	deleted   []string
}

func (f *fakeUploadRepo) Create(
	_ context.Context,
	//nolint:gocritic // the repository interface passes the session by value
	session entity.UploadSession,
	idleTTL time.Duration,
) error {
	f.created = append(f.created, session)
	f.lastTTL = idleTTL

	return f.createErr
}

func (f *fakeUploadRepo) Get(_ context.Context, uploadID string) (entity.UploadSession, error) {
	if f.getErr != nil {
		return entity.UploadSession{}, f.getErr
	}

	for i := range f.sessions {
		if f.sessions[i].ID == uploadID {
			return f.sessions[i], nil
		}
	}

	return entity.UploadSession{}, entity.ErrUploadNotFound
}

func (f *fakeUploadRepo) SetPart(
	_ context.Context,
	uploadID string,
	partNumber int,
	etag string,
	_ time.Time,
) error {
	if f.parts == nil {
		f.parts = make(map[string]map[int]string)
	}

	if f.parts[uploadID] == nil {
		f.parts[uploadID] = make(map[int]string)
	}

	f.parts[uploadID][partNumber] = etag

	return nil
}

func (f *fakeUploadRepo) Parts(_ context.Context, uploadID string) (map[int]string, error) {
	return f.parts[uploadID], nil
}

func (f *fakeUploadRepo) ListByUser(_ context.Context, _ string) ([]entity.UploadSession, error) {
	return f.reserved, f.listErr
}

func (f *fakeUploadRepo) Expired(_ context.Context, _ time.Time, _ int) ([]entity.UploadSession, error) {
	return f.expired, nil
}

func (f *fakeUploadRepo) Delete(_ context.Context, uploadID string) error {
	f.deleted = append(f.deleted, uploadID)

	return nil
}

func (f *fakeUploadRepo) LockQuota(_ context.Context, _ string) (func(), error) {
	f.lockCalls++

	if f.lockErr != nil {
		return nil, f.lockErr
	}

	return func() { f.releases++ }, nil
}

// fakePacer records the pace calls the use case asks for.
type fakePacer struct {
	calls []fakePace
	err   error
}

type fakePace struct {
	userID string
	bytes  int64
	rate   int64
}

func (f *fakePacer) Pace(_ context.Context, userID string, bytes, rate int64) error {
	f.calls = append(f.calls, fakePace{userID: userID, bytes: bytes, rate: rate})

	return f.err
}

// fakeUploadStorage records the multipart calls a start makes. The embedded
// fakeStorage answers the plain object calls.
type fakeUploadStorage struct {
	fakeStorage
	createID   string
	createKey  string
	createType string
	createErr  error
	abortID    string
	abortKey   string
	abortErr   error
	aborted    bool
}

func (f *fakeUploadStorage) CreateMultipartUpload(
	_ context.Context,
	_, key, contentType string,
) (string, error) {
	f.createKey = key
	f.createType = contentType

	if f.createErr != nil {
		return "", f.createErr
	}

	if f.createID == "" {
		return "multipart-id", nil
	}

	return f.createID, nil
}

func (f *fakeUploadStorage) AbortMultipartUpload(_ context.Context, _, key, uploadID string) error {
	f.aborted = true
	f.abortKey = key
	f.abortID = uploadID

	return f.abortErr
}

// newUploadUseCase wires the use case the way the app does.
func newUploadUseCase(
	users repo.UserRepo,
	storage repo.ObjectStorage,
	uploads repo.UploadSessionRepo,
	pacer repo.TransferLimiter,
	settings usecase.UploadSettings,
) usecase.File {
	return New(
		users,
		&fakeFileRepo{},
		&fakeFolderRepo{folder: entity.Folder{ID: "folder", UserID: "user"}},
		storage,
		&fakeUnitOfWork{},
		"bucket",
		uploads,
		pacer,
		settings,
	)
}

func TestStartUploadCreatesSessionAndReservesQuota(t *testing.T) {
	t.Parallel()

	uploads := &fakeUploadRepo{}
	storage := &fakeUploadStorage{}
	pacer := &fakePacer{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		pacer,
		usecase.UploadSettings{
			MaxBytes:       _testMaxBytes,
			PartBytes:      _testPartBytes,
			SessionIdleTTL: _testUploadTTL,
		},
	)

	session, err := uc.StartUpload(
		context.Background(),
		"user",
		"folder",
		"  big.iso  ",
		"application/x-iso9660-image",
		10*_testGiB,
	)
	if err != nil {
		t.Fatalf("StartUpload() error = %v, want nil", err)
	}

	checkBigUploadSession(t, &session)

	if len(uploads.created) != 1 {
		t.Fatalf("created sessions = %d, want 1", len(uploads.created))
	}

	if uploads.created[0].Size != 10*_testGiB {
		t.Errorf("stored size = %d, want %d", uploads.created[0].Size, 10*_testGiB)
	}

	if uploads.lastTTL != _testUploadTTL {
		t.Errorf("stored TTL = %v, want %v", uploads.lastTTL, _testUploadTTL)
	}

	if uploads.lockCalls != 0 {
		t.Errorf("quota lock calls = %d, want 0 without a quota", uploads.lockCalls)
	}

	if len(pacer.calls) != 0 {
		t.Errorf("pacer calls = %d, want 0 at start", len(pacer.calls))
	}
}

// checkBigUploadSession asserts the fields of the session a 10 GiB start
// returns.
func checkBigUploadSession(t *testing.T, session *entity.UploadSession) {
	t.Helper()

	if session.PartSize != _testPartBytes {
		t.Errorf("part size = %d, want %d", session.PartSize, _testPartBytes)
	}

	if session.PartCount != 205 {
		t.Errorf("part count = %d, want 205", session.PartCount)
	}

	if session.Size != 10*_testGiB {
		t.Errorf("session size = %d, want %d", session.Size, 10*_testGiB)
	}

	if session.Name != "big.iso" {
		t.Errorf("session name = %q, want %q", session.Name, "big.iso")
	}

	if session.UserID != "user" || session.FolderID != "folder" {
		t.Errorf("session owner = %q/%q, want user/folder", session.UserID, session.FolderID)
	}

	if session.FileID == "" || session.StorageKey == "" || session.MultipartID == "" {
		t.Errorf(
			"session identifiers = %q/%q/%q, want all set",
			session.FileID,
			session.StorageKey,
			session.MultipartID,
		)
	}
}

func TestStartUploadRejectsWhenReservedHeadroomIsGone(t *testing.T) {
	t.Parallel()

	uploads := &fakeUploadRepo{
		reserved: []entity.UploadSession{{ID: "live", UserID: "user", Size: 60}},
	}
	storage := &fakeUploadStorage{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{user: entity.User{StorageQuotaBytes: 100}},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{MaxBytes: _testMaxBytes, PartBytes: _testPartBytes},
	)

	_, err := uc.StartUpload(context.Background(), "user", "folder", "big.iso", "text/plain", 50)
	if !errors.Is(err, entity.ErrStorageQuotaExceeded) {
		t.Fatalf("StartUpload() error = %v, want %v", err, entity.ErrStorageQuotaExceeded)
	}

	if len(uploads.created) != 0 {
		t.Errorf("created sessions = %d, want 0", len(uploads.created))
	}

	if storage.createKey != "" {
		t.Error("StartUpload() opened a multipart upload over quota")
	}

	if uploads.lockCalls != 1 {
		t.Errorf("quota lock calls = %d, want 1", uploads.lockCalls)
	}

	if uploads.releases != 1 {
		t.Errorf("quota releases = %d, want 1", uploads.releases)
	}
}

func TestStartUploadShrinksPartsForASlowAccount(t *testing.T) {
	t.Parallel()

	uploads := &fakeUploadRepo{}
	storage := &fakeUploadStorage{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{
			MaxBytes:                     _testMaxBytes,
			PartBytes:                    _testPartBytes,
			SessionIdleTTL:               _testUploadTTL,
			DefaultUploadRateBytesPerSec: _testSlowRate,
		},
	)

	const size = int64(100) << 20

	session, err := uc.StartUpload(context.Background(), "user", "folder", "big.iso", "text/plain", size)
	if err != nil {
		t.Fatalf("StartUpload() error = %v, want nil", err)
	}

	if session.PartSize != _testSlowPartSz {
		t.Errorf("part size = %d, want %d", session.PartSize, _testSlowPartSz)
	}

	wantCount := int((size + _testSlowPartSz - 1) / _testSlowPartSz)
	if session.PartCount != wantCount {
		t.Errorf("part count = %d, want %d", session.PartCount, wantCount)
	}
}

func TestStartUploadRejectsAFileOverTheLimit(t *testing.T) {
	t.Parallel()

	uploads := &fakeUploadRepo{}
	storage := &fakeUploadStorage{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{MaxBytes: _testMaxBytes, PartBytes: _testPartBytes},
	)

	_, err := uc.StartUpload(context.Background(), "user", "folder", "big.iso", "text/plain", _testMaxBytes)
	if err != nil {
		t.Fatalf("StartUpload() at the limit error = %v, want nil", err)
	}

	_, err = uc.StartUpload(
		context.Background(),
		"user",
		"folder",
		"big.iso",
		"text/plain",
		_testMaxBytes+1,
	)
	if !errors.Is(err, entity.ErrFileTooLarge) {
		t.Fatalf("StartUpload() over the limit error = %v, want %v", err, entity.ErrFileTooLarge)
	}

	_, err = uc.StartUpload(context.Background(), "user", "folder", "big.iso", "text/plain", -1)
	if !errors.Is(err, entity.ErrInvalidUpload) {
		t.Errorf("StartUpload() negative size error = %v, want %v", err, entity.ErrInvalidUpload)
	}

	if len(uploads.created) != 1 {
		t.Errorf("created sessions = %d, want 1", len(uploads.created))
	}
}

func TestStartUploadAbortsMultipartWhenSessionStoreFails(t *testing.T) {
	t.Parallel()

	uploads := &fakeUploadRepo{createErr: errUploadStore}
	storage := &fakeUploadStorage{createID: "multipart-created"}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{MaxBytes: _testMaxBytes, PartBytes: _testPartBytes},
	)

	_, err := uc.StartUpload(context.Background(), "user", "folder", "note.txt", "text/plain", 1024)
	if !errors.Is(err, errUploadStore) {
		t.Fatalf("StartUpload() error = %v, want %v", err, errUploadStore)
	}

	if !storage.aborted {
		t.Fatal("StartUpload() kept the multipart upload")
	}

	if storage.abortID != "multipart-created" {
		t.Errorf("aborted upload = %q, want %q", storage.abortID, "multipart-created")
	}

	if storage.abortKey != storage.createKey || storage.abortKey == "" {
		t.Errorf("aborted key = %q, want the key just created %q", storage.abortKey, storage.createKey)
	}

	if uploads.lockCalls != 0 {
		t.Errorf("quota lock calls = %d, want 0 without a quota", uploads.lockCalls)
	}
}
