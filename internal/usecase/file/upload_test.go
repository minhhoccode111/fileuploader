package file

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/internal/usecase/pace"
	"github.com/minhhoccode111/fileuploader/pkg/multipart"
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
	_testFiveMiB    = int64(5) << 20
)

// fakeUploadRepo keeps the sessions the use case stores and answers the
// queries a start makes, so a test can set the reserved headroom it sees.
type fakeUploadRepo struct {
	sessions     []entity.UploadSession
	reserved     []entity.UploadSession
	parts        map[string]map[int]string
	expired      []entity.UploadSession
	createErr    error
	getErr       error
	listErr      error
	lockErr      error
	deleteErr    error
	created      []entity.UploadSession
	lastTTL      time.Duration
	partExpiry   time.Time
	setPartCalls int
	lockCalls    int
	expiredCalls int
	releases     int
	deleted      []string
	onLock       func()
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
	expiresAt time.Time,
) error {
	f.setPartCalls++
	f.partExpiry = expiresAt

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

// Expired hands back the sessions the sweep tests expired, minus the ones the
// use case already deleted, so a sweep sees the list shrink the way the store
// does.
func (f *fakeUploadRepo) Expired(_ context.Context, _ time.Time, _ int) ([]entity.UploadSession, error) {
	f.expiredCalls++

	live := make([]entity.UploadSession, 0, len(f.expired))

	for i := range f.expired {
		if !slices.Contains(f.deleted, f.expired[i].ID) {
			live = append(live, f.expired[i])
		}
	}

	return live, nil
}

func (f *fakeUploadRepo) Delete(_ context.Context, uploadID string) error {
	f.deleted = append(f.deleted, uploadID)

	return f.deleteErr
}

func (f *fakeUploadRepo) LockQuota(_ context.Context, _ string) (func(), error) {
	f.lockCalls++

	if f.lockErr != nil {
		return nil, f.lockErr
	}

	if f.onLock != nil {
		f.onLock()
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
	scope  repo.TransferScope
	bytes  int64
	rate   int64
}

func (f *fakePacer) Pace(
	_ context.Context,
	userID string,
	scope repo.TransferScope,
	bytes, rate int64,
) error {
	f.calls = append(f.calls, fakePace{userID: userID, scope: scope, bytes: bytes, rate: rate})

	return f.err
}

// fakeUploadStorage records the multipart calls a start makes. The embedded
// fakeStorage answers the plain object calls.
type fakeUploadStorage struct {
	fakeStorage
	createID      string
	createKey     string
	createType    string
	createErr     error
	createCalls   int
	abortID       string
	abortKey      string
	abortErr      error
	abortCalls    int
	partID        string
	partKey       string
	partNumber    int
	partSize      int64
	partBody      []byte
	partErr       error
	partCalls     int
	aborted       bool
	completeKey   string
	completeID    string
	completeParts []multipart.Part
	completeErr   error
	completeCalls int
}

func (f *fakeUploadStorage) CreateMultipartUpload(
	_ context.Context,
	_, key, contentType string,
) (string, error) {
	f.createCalls++
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
	f.abortCalls++
	f.abortKey = key
	f.abortID = uploadID

	return f.abortErr
}

// CompleteMultipartUpload records the parts the use case joins and the key it
// joins them under, so a test sees the order a completion sends.
func (f *fakeUploadStorage) CompleteMultipartUpload(
	_ context.Context,
	_, key, uploadID string,
	parts []multipart.Part,
) error {
	f.completeCalls++
	f.completeKey = key
	f.completeID = uploadID
	f.completeParts = parts

	return f.completeErr
}

// UploadPart consumes the reader the use case paced, so a test sees the bytes
// the limiter throttled and the way a failed write stops the part.
func (f *fakeUploadStorage) UploadPart(
	_ context.Context,
	_, key, uploadID string,
	partNumber int,
	reader io.Reader,
	size int64,
) (string, error) {
	f.partCalls++
	f.partID = uploadID
	f.partKey = key
	f.partNumber = partNumber
	f.partSize = size

	body, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	f.partBody = body

	if f.partErr != nil {
		return "", f.partErr
	}

	return "etag", nil
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

// An empty file needs no multipart upload: it is written in one request at
// completion, so a start opens nothing to abort or sweep.
func TestStartUploadSkipsMultipartForAnEmptyFile(t *testing.T) {
	t.Parallel()

	uploads := &fakeUploadRepo{}
	storage := &fakeUploadStorage{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{
			MaxBytes:       _testMaxBytes,
			PartBytes:      _testPartBytes,
			SessionIdleTTL: _testUploadTTL,
		},
	)

	session, err := uc.StartUpload(context.Background(), "user", "folder", "empty.txt", "text/plain", 0)
	if err != nil {
		t.Fatalf("StartUpload() error = %v, want nil", err)
	}

	if storage.createCalls != 0 {
		t.Errorf("multipart creates = %d, want 0 for an empty file", storage.createCalls)
	}

	if session.MultipartID != "" {
		t.Errorf("multipart id = %q, want empty", session.MultipartID)
	}

	if session.PartCount != 0 {
		t.Errorf("part count = %d, want 0", session.PartCount)
	}

	if len(uploads.created) != 1 {
		t.Fatalf("created sessions = %d, want 1", len(uploads.created))
	}

	if uploads.created[0].MultipartID != "" {
		t.Errorf("stored multipart id = %q, want empty", uploads.created[0].MultipartID)
	}
}

// partSession is the two-part session the upload part tests send into.
func partSession() entity.UploadSession {
	return entity.UploadSession{
		ID:          "upload",
		UserID:      "user",
		FolderID:    "folder",
		FileID:      "file",
		StorageKey:  "users/user/files/file",
		MultipartID: "multipart-id",
		Size:        _testFiveMiB + 7,
		PartSize:    _testFiveMiB,
		PartCount:   2,
	}
}

func TestUploadPartStoresETagAndSlidesExpiry(t *testing.T) {
	t.Parallel()

	uploads := &fakeUploadRepo{sessions: []entity.UploadSession{partSession()}}
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

	before := time.Now()

	err := uc.UploadPart(
		context.Background(),
		"user",
		"upload",
		1,
		strings.NewReader(strings.Repeat("a", int(_testFiveMiB))),
		_testFiveMiB,
	)
	if err != nil {
		t.Fatalf("UploadPart() error = %v, want nil", err)
	}

	if got := uploads.parts["upload"][1]; got != "etag" {
		t.Errorf("stored etag = %q, want %q", got, "etag")
	}

	if uploads.setPartCalls != 1 {
		t.Fatalf("SetPart calls = %d, want 1", uploads.setPartCalls)
	}

	if !uploads.partExpiry.After(before) {
		t.Errorf("part expiry = %v, want after %v", uploads.partExpiry, before)
	}

	if storage.partID != "multipart-id" || storage.partKey != "users/user/files/file" {
		t.Errorf(
			"uploaded to %q/%q, want multipart-id/users/user/files/file",
			storage.partID,
			storage.partKey,
		)
	}

	if storage.partNumber != 1 || storage.partSize != _testFiveMiB {
		t.Errorf(
			"uploaded part %d of %d bytes, want part 1 of %d",
			storage.partNumber,
			storage.partSize,
			_testFiveMiB,
		)
	}

	if len(pacer.calls) != 0 {
		t.Errorf("pacer calls = %d, want 0 without a rate", len(pacer.calls))
	}
}

func TestUploadPartRejectsWrongLength(t *testing.T) {
	t.Parallel()

	uploads := &fakeUploadRepo{sessions: []entity.UploadSession{partSession()}}
	storage := &fakeUploadStorage{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{MaxBytes: _testMaxBytes, PartBytes: _testPartBytes},
	)

	err := uc.UploadPart(context.Background(), "user", "upload", 2, strings.NewReader("12345678"), 8)
	if !errors.Is(err, entity.ErrUploadPartMismatch) {
		t.Fatalf("UploadPart() short last part error = %v, want %v", err, entity.ErrUploadPartMismatch)
	}

	err = uc.UploadPart(
		context.Background(),
		"user",
		"upload",
		1,
		strings.NewReader(strings.Repeat("a", int(_testFiveMiB+1))),
		_testFiveMiB+1,
	)
	if !errors.Is(err, entity.ErrUploadPartMismatch) {
		t.Fatalf("UploadPart() long part error = %v, want %v", err, entity.ErrUploadPartMismatch)
	}

	if storage.partCalls != 0 {
		t.Errorf("storage calls = %d, want 0 for a mismatched part", storage.partCalls)
	}

	if uploads.setPartCalls != 0 {
		t.Errorf("SetPart calls = %d, want 0 for a mismatched part", uploads.setPartCalls)
	}
}

func TestUploadPartRejectsAnotherUsersSession(t *testing.T) {
	t.Parallel()

	uploads := &fakeUploadRepo{sessions: []entity.UploadSession{partSession()}}
	storage := &fakeUploadStorage{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{MaxBytes: _testMaxBytes, PartBytes: _testPartBytes},
	)

	err := uc.UploadPart(
		context.Background(),
		"intruder",
		"upload",
		1,
		strings.NewReader(strings.Repeat("a", int(_testFiveMiB))),
		_testFiveMiB,
	)
	if !errors.Is(err, entity.ErrUploadNotFound) {
		t.Fatalf("UploadPart() error = %v, want %v", err, entity.ErrUploadNotFound)
	}

	if storage.partCalls != 0 {
		t.Error("UploadPart() stored a part of another account")
	}
}

// A session whose idle window has passed is gone as far as a part is concerned,
// even when its hash is still readable, so the bytes never reach the bucket and
// the expiry does not slide.
func TestUploadPartRejectsAnExpiredSession(t *testing.T) {
	t.Parallel()

	session := partSession()
	session.ExpiresAt = time.Now().Add(-time.Minute)

	uploads := &fakeUploadRepo{sessions: []entity.UploadSession{session}}
	storage := &fakeUploadStorage{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{MaxBytes: _testMaxBytes, PartBytes: _testPartBytes},
	)

	err := uc.UploadPart(
		context.Background(),
		"user",
		"upload",
		1,
		strings.NewReader(strings.Repeat("a", int(_testFiveMiB))),
		_testFiveMiB,
	)
	if !errors.Is(err, entity.ErrUploadNotFound) {
		t.Fatalf("UploadPart() error = %v, want %v", err, entity.ErrUploadNotFound)
	}

	if storage.partCalls != 0 {
		t.Errorf("storage calls = %d, want 0 for an expired session", storage.partCalls)
	}

	if uploads.setPartCalls != 0 {
		t.Errorf("SetPart calls = %d, want 0 for an expired session", uploads.setPartCalls)
	}
}

func TestUploadPartPacesTheBody(t *testing.T) {
	t.Parallel()

	const (
		rate    = int64(1) << 20
		bodyLen = int64(1) << 20
		batch   = int64(256) << 10
	)

	session := partSession()
	session.Size = bodyLen
	session.PartSize = bodyLen
	session.PartCount = 1

	uploads := &fakeUploadRepo{sessions: []entity.UploadSession{session}}
	storage := &fakeUploadStorage{}
	pacer := &fakePacer{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		pacer,
		usecase.UploadSettings{
			MaxBytes:                     _testMaxBytes,
			PartBytes:                    _testPartBytes,
			DefaultUploadRateBytesPerSec: rate,
		},
	)

	err := uc.UploadPart(
		context.Background(),
		"user",
		"upload",
		1,
		strings.NewReader(strings.Repeat("a", int(bodyLen))),
		bodyLen,
	)
	if err != nil {
		t.Fatalf("UploadPart() error = %v, want nil", err)
	}

	if int64(len(storage.partBody)) != bodyLen {
		t.Fatalf("stored bytes = %d, want %d", len(storage.partBody), bodyLen)
	}

	checkPartPaces(t, pacer.calls, rate, bodyLen, batch)
}

// checkPartPaces asserts each pace call carries one batch at the account rate
// and that the calls add up to the whole body.
func checkPartPaces(t *testing.T, calls []fakePace, rate, bodyLen, batch int64) {
	t.Helper()

	if len(calls) < 2 {
		t.Fatalf("pacer calls = %d, want one per read batch", len(calls))
	}

	var paced int64

	for _, call := range calls {
		if call.userID != "user" {
			t.Errorf("paced user = %q, want %q", call.userID, "user")
		}

		if call.scope != repo.TransferUpload {
			t.Errorf("paced scope = %q, want %q", call.scope, repo.TransferUpload)
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

// stuckReader hands over nothing and reports no error, which a paced read must
// not pass on as if it had made progress.
type stuckReader struct{}

func (stuckReader) Read([]byte) (int, error) { return 0, nil }

func TestPacedReaderReportsNoProgress(t *testing.T) {
	t.Parallel()

	pacer := &fakePacer{}

	reader := pace.NewReader(
		context.Background(),
		stuckReader{},
		pacer,
		"user",
		repo.TransferUpload,
		1<<20,
	)

	n, err := reader.Read(make([]byte, 16))
	if n != 0 || !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("Read() = %d, %v; want 0, %v", n, err, io.ErrNoProgress)
	}

	if len(pacer.calls) != 0 {
		t.Errorf("pacer calls = %d, want none without bytes", len(pacer.calls))
	}
}

func TestUploadPartDoesNotStoreETagOnStorageFailure(t *testing.T) {
	t.Parallel()

	uploads := &fakeUploadRepo{sessions: []entity.UploadSession{partSession()}}
	storage := &fakeUploadStorage{partErr: errUploadStore}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{MaxBytes: _testMaxBytes, PartBytes: _testPartBytes},
	)

	err := uc.UploadPart(
		context.Background(),
		"user",
		"upload",
		1,
		strings.NewReader(strings.Repeat("a", int(_testFiveMiB))),
		_testFiveMiB,
	)
	if !errors.Is(err, errUploadStore) {
		t.Fatalf("UploadPart() error = %v, want %v", err, errUploadStore)
	}

	if !strings.Contains(err.Error(), "FileUseCase - UploadPart - uc.storage.UploadPart") {
		t.Errorf("UploadPart() error = %v, want the storage path in the message", err)
	}

	if uploads.setPartCalls != 0 {
		t.Errorf("SetPart calls = %d, want 0 when the store rejects the part", uploads.setPartCalls)
	}

	if uploads.parts != nil {
		t.Errorf("stored parts = %v, want none", uploads.parts)
	}
}

// completeSession is the two-part session the completion tests finish.
func completeSession() entity.UploadSession {
	return entity.UploadSession{
		ID:          "upload",
		UserID:      "user",
		FolderID:    "folder",
		FileID:      "file",
		Name:        "big.iso",
		ContentType: "application/octet-stream",
		StorageKey:  "users/user/files/file",
		MultipartID: "multipart-id",
		Size:        _testFiveMiB + 7,
		PartSize:    _testFiveMiB,
		PartCount:   2,
		CreatedAt:   time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
	}
}

// completeUseCase wires a completion the way the app does, onto fakes the test
// can inspect afterwards.
func completeUseCase(
	users *fakeUserRepoForFile,
	files *fakeFileRepo,
	storage *fakeUploadStorage,
	uploads *fakeUploadRepo,
) usecase.File {
	return New(
		users,
		files,
		&fakeFolderRepo{folder: entity.Folder{ID: "folder", UserID: "user"}},
		storage,
		&fakeUnitOfWork{},
		"bucket",
		uploads,
		&fakePacer{},
		usecase.UploadSettings{
			MaxBytes:       _testMaxBytes,
			PartBytes:      _testPartBytes,
			SessionIdleTTL: _testUploadTTL,
		},
	)
}

func TestCompleteUploadAssemblesPartsAndStoresRow(t *testing.T) {
	t.Parallel()

	session := completeSession()
	users := &fakeUserRepoForFile{user: entity.User{StorageQuotaBytes: _testGiB}}
	files := &fakeFileRepo{}
	storage := &fakeUploadStorage{}
	uploads := &fakeUploadRepo{
		sessions: []entity.UploadSession{session},
		parts: map[string]map[int]string{
			"upload": {2: "two", 1: "one"},
		},
	}
	uc := completeUseCase(users, files, storage, uploads)

	file, err := uc.CompleteUpload(context.Background(), "user", "upload")
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v, want nil", err)
	}

	wantParts := []multipart.Part{{Number: 1, ETag: "one"}, {Number: 2, ETag: "two"}}
	if !slices.Equal(storage.completeParts, wantParts) {
		t.Errorf("completed parts = %v, want %v", storage.completeParts, wantParts)
	}

	if storage.completeKey != session.StorageKey || storage.completeID != session.MultipartID {
		t.Errorf(
			"completed %q/%q, want %q/%q",
			storage.completeID,
			storage.completeKey,
			session.MultipartID,
			session.StorageKey,
		)
	}

	checkCompletedFile(t, &file, &session)
	checkCompletedRow(t, files, uploads, users, &session)
}

func TestCompleteUploadRechecksTheSessionAfterWinningTheQuotaLock(t *testing.T) {
	t.Parallel()

	session := completeSession()
	users := &fakeUserRepoForFile{user: entity.User{StorageQuotaBytes: _testGiB}}
	files := &fakeFileRepo{}
	storage := &fakeUploadStorage{}
	uploads := &fakeUploadRepo{
		sessions: []entity.UploadSession{session},
		parts: map[string]map[int]string{
			"upload": {1: "one", 2: "two"},
		},
	}

	// A concurrent completion won the race while this request was waiting for
	// the quota lock and already deleted the session.
	uploads.onLock = func() { uploads.sessions = nil }

	uc := completeUseCase(users, files, storage, uploads)

	_, err := uc.CompleteUpload(context.Background(), "user", "upload")
	if !errors.Is(err, entity.ErrUploadNotFound) {
		t.Fatalf("CompleteUpload() error = %v, want %v", err, entity.ErrUploadNotFound)
	}

	if storage.completeCalls != 0 {
		t.Errorf("CompleteMultipartUpload calls = %d, want 0", storage.completeCalls)
	}

	if files.stored != nil {
		t.Error("CompleteUpload() stored a row after losing the session race")
	}

	if storage.removeKey != "" {
		t.Errorf("removed object %q after losing the race, want no removal", storage.removeKey)
	}
}

// checkCompletedRow asserts a finished upload left the row, the account usage
// and the session behind as expected.
func checkCompletedRow(
	t *testing.T,
	files *fakeFileRepo,
	uploads *fakeUploadRepo,
	users *fakeUserRepoForFile,
	session *entity.UploadSession,
) {
	t.Helper()

	if files.stored == nil {
		t.Fatal("CompleteUpload() did not store the row")
	}

	if files.stored.ID != "file" || files.stored.Size != session.Size {
		t.Errorf(
			"stored row = %q of %d bytes, want %q of %d bytes",
			files.stored.ID,
			files.stored.Size,
			"file",
			session.Size,
		)
	}

	if !slices.Equal(uploads.deleted, []string{"upload"}) {
		t.Errorf("deleted sessions = %v, want [upload]", uploads.deleted)
	}

	if users.user.StorageUsedBytes != session.Size {
		t.Errorf(
			"storage used = %d, want %d",
			users.user.StorageUsedBytes,
			session.Size,
		)
	}

	if uploads.lockCalls != 1 || uploads.releases != 1 {
		t.Errorf(
			"quota lock calls/releases = %d/%d, want 1/1",
			uploads.lockCalls,
			uploads.releases,
		)
	}
}

// checkCompletedFile asserts the row carries what the session held.
func checkCompletedFile(t *testing.T, file *entity.File, session *entity.UploadSession) {
	t.Helper()

	if file.UserID != session.UserID || file.FolderID != session.FolderID {
		t.Errorf("file owner = %q/%q, want %q/%q", file.UserID, file.FolderID, session.UserID, session.FolderID)
	}

	if file.Name != session.Name || file.ContentType != session.ContentType {
		t.Errorf("file name/type = %q/%q, want %q/%q", file.Name, file.ContentType, session.Name, session.ContentType)
	}

	if file.StorageKey != session.StorageKey {
		t.Errorf("file key = %q, want %q", file.StorageKey, session.StorageKey)
	}

	if !file.CreatedAt.Equal(session.CreatedAt) {
		t.Errorf("file created at = %v, want %v", file.CreatedAt, session.CreatedAt)
	}

	if file.UpdatedAt.Before(session.CreatedAt) {
		t.Errorf("file updated at = %v, want at or after %v", file.UpdatedAt, session.CreatedAt)
	}
}

func TestCompleteUploadRejectsMissingParts(t *testing.T) {
	t.Parallel()

	storage := &fakeUploadStorage{}
	uploads := &fakeUploadRepo{
		sessions: []entity.UploadSession{completeSession()},
		parts: map[string]map[int]string{
			"upload": {1: "one"},
		},
	}
	uc := completeUseCase(&fakeUserRepoForFile{}, &fakeFileRepo{}, storage, uploads)

	_, err := uc.CompleteUpload(context.Background(), "user", "upload")
	if !errors.Is(err, entity.ErrUploadIncomplete) {
		t.Fatalf("CompleteUpload() error = %v, want %v", err, entity.ErrUploadIncomplete)
	}

	if storage.completeCalls != 0 {
		t.Errorf("completion calls = %d, want 0 for an incomplete upload", storage.completeCalls)
	}

	if len(uploads.deleted) != 0 {
		t.Errorf("deleted sessions = %v, want none for an upload that can still finish", uploads.deleted)
	}

	if uploads.lockCalls != 0 {
		t.Errorf("quota lock calls = %d, want 0 for an incomplete upload", uploads.lockCalls)
	}
}

// A session past its idle window is gone for completion too, so the join never
// runs and no row is stored.
func TestCompleteUploadRejectsAnExpiredSession(t *testing.T) {
	t.Parallel()

	session := completeSession()
	session.ExpiresAt = time.Now().Add(-time.Minute)

	files := &fakeFileRepo{}
	storage := &fakeUploadStorage{}
	uploads := &fakeUploadRepo{
		sessions: []entity.UploadSession{session},
		parts: map[string]map[int]string{
			"upload": {1: "one", 2: "two"},
		},
	}
	uc := completeUseCase(&fakeUserRepoForFile{}, files, storage, uploads)

	_, err := uc.CompleteUpload(context.Background(), "user", "upload")
	if !errors.Is(err, entity.ErrUploadNotFound) {
		t.Fatalf("CompleteUpload() error = %v, want %v", err, entity.ErrUploadNotFound)
	}

	if storage.completeCalls != 0 {
		t.Errorf("completion calls = %d, want 0 for an expired session", storage.completeCalls)
	}

	if files.stored != nil {
		t.Error("CompleteUpload() stored a row for an expired session")
	}

	if len(uploads.deleted) != 0 {
		t.Errorf("deleted sessions = %v, want none for an expired session", uploads.deleted)
	}
}

// A join that fails must not leave the parts of the multipart upload in the
// bucket: the session is ended and the upload is aborted once, so the bucket
// holds nothing for a file that never became a row.
func TestCompleteUploadReleasesReservationOnStorageFailure(t *testing.T) {
	t.Parallel()

	session := completeSession()
	files := &fakeFileRepo{}
	storage := &fakeUploadStorage{completeErr: errUploadStore}
	uploads := &fakeUploadRepo{
		sessions: []entity.UploadSession{session},
		parts: map[string]map[int]string{
			"upload": {1: "one", 2: "two"},
		},
	}
	uc := completeUseCase(&fakeUserRepoForFile{}, files, storage, uploads)

	_, err := uc.CompleteUpload(context.Background(), "user", "upload")
	if !errors.Is(err, errUploadStore) {
		t.Fatalf("CompleteUpload() error = %v, want %v", err, errUploadStore)
	}

	if !strings.Contains(err.Error(), "FileUseCase - CompleteUpload - uc.storage.CompleteMultipartUpload") {
		t.Errorf("CompleteUpload() error = %v, want the storage path in the message", err)
	}

	if files.stored != nil {
		t.Error("CompleteUpload() stored a row for an object it never joined")
	}

	if storage.abortCalls != 1 {
		t.Fatalf("abort calls = %d, want 1 so no parts are orphaned", storage.abortCalls)
	}

	if storage.abortID != session.MultipartID || storage.abortKey != session.StorageKey {
		t.Errorf(
			"aborted %q/%q, want %q/%q",
			storage.abortID,
			storage.abortKey,
			session.MultipartID,
			session.StorageKey,
		)
	}

	if !slices.Equal(uploads.deleted, []string{"upload"}) {
		t.Errorf("deleted sessions = %v, want the reservation released", uploads.deleted)
	}

	if uploads.releases != 1 {
		t.Errorf("quota releases = %d, want 1", uploads.releases)
	}
}

// A failed abort on top of a failed join is reported with the cleanup sentinel,
// so a caller can tell the bucket may still hold the parts.
func TestCompleteUploadJoinsCleanupErrorWhenAbortFails(t *testing.T) {
	t.Parallel()

	files := &fakeFileRepo{}
	storage := &fakeUploadStorage{completeErr: errUploadStore, abortErr: errUploadStore}
	uploads := &fakeUploadRepo{
		sessions: []entity.UploadSession{completeSession()},
		parts: map[string]map[int]string{
			"upload": {1: "one", 2: "two"},
		},
	}
	uc := completeUseCase(&fakeUserRepoForFile{}, files, storage, uploads)

	_, err := uc.CompleteUpload(context.Background(), "user", "upload")
	if !errors.Is(err, errUploadStore) {
		t.Fatalf("CompleteUpload() error = %v, want %v", err, errUploadStore)
	}

	if !errors.Is(err, entity.ErrStorageCleanup) {
		t.Errorf("CompleteUpload() error = %v, want %v", err, entity.ErrStorageCleanup)
	}

	if storage.abortCalls != 1 {
		t.Errorf("abort calls = %d, want 1", storage.abortCalls)
	}

	if !slices.Equal(uploads.deleted, []string{"upload"}) {
		t.Errorf("deleted sessions = %v, want the reservation released", uploads.deleted)
	}
}

func TestCompleteUploadCleansUpOnNameConflict(t *testing.T) {
	t.Parallel()

	session := completeSession()
	files := &fakeFileRepo{nameTaken: true}
	storage := &fakeUploadStorage{}
	uploads := &fakeUploadRepo{
		sessions: []entity.UploadSession{session},
		parts: map[string]map[int]string{
			"upload": {1: "one", 2: "two"},
		},
	}
	uc := completeUseCase(&fakeUserRepoForFile{}, files, storage, uploads)

	_, err := uc.CompleteUpload(context.Background(), "user", "upload")
	if !errors.Is(err, entity.ErrEntryAlreadyExists) {
		t.Fatalf("CompleteUpload() error = %v, want %v", err, entity.ErrEntryAlreadyExists)
	}

	if storage.removeKey != session.StorageKey {
		t.Errorf("removed %q, want %q", storage.removeKey, session.StorageKey)
	}

	if !slices.Equal(uploads.deleted, []string{"upload"}) {
		t.Errorf("deleted sessions = %v, want [upload]", uploads.deleted)
	}

	if uploads.releases != 1 {
		t.Errorf("quota releases = %d, want 1", uploads.releases)
	}
}

func TestCompleteUploadUsesPutObjectForAnEmptyFile(t *testing.T) {
	t.Parallel()

	session := completeSession()
	session.Size = 0
	session.PartCount = 0
	session.MultipartID = ""

	users := &fakeUserRepoForFile{}
	files := &fakeFileRepo{}
	storage := &fakeUploadStorage{}
	uploads := &fakeUploadRepo{sessions: []entity.UploadSession{session}}
	uc := completeUseCase(users, files, storage, uploads)

	file, err := uc.CompleteUpload(context.Background(), "user", "upload")
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v, want nil", err)
	}

	if storage.completeCalls != 0 {
		t.Errorf("completion calls = %d, want 0 for an empty file", storage.completeCalls)
	}

	if storage.abortCalls != 0 {
		t.Errorf("abort calls = %d, want 0 for an empty file that never opened a multipart", storage.abortCalls)
	}

	if storage.putKey != session.StorageKey || storage.putSize != 0 {
		t.Errorf("put %q of %d bytes, want %q of 0 bytes", storage.putKey, storage.putSize, session.StorageKey)
	}

	if file.Size != 0 {
		t.Errorf("file size = %d, want 0", file.Size)
	}

	if users.user.StorageUsedBytes != 0 {
		t.Errorf("storage used = %d, want 0", users.user.StorageUsedBytes)
	}

	if !slices.Equal(uploads.deleted, []string{"upload"}) {
		t.Errorf("deleted sessions = %v, want [upload]", uploads.deleted)
	}
}

// An empty file whose write fails leaves no multipart upload to abort, so the
// cleanup only ends the session and reports the write failure.
func TestCompleteUploadOfAnEmptyFileSkipsAbortOnWriteFailure(t *testing.T) {
	t.Parallel()

	session := completeSession()
	session.Size = 0
	session.PartCount = 0
	session.MultipartID = ""

	storage := &fakeUploadStorage{}
	storage.putErr = errUploadStore

	uploads := &fakeUploadRepo{sessions: []entity.UploadSession{session}}
	uc := completeUseCase(&fakeUserRepoForFile{}, &fakeFileRepo{}, storage, uploads)

	_, err := uc.CompleteUpload(context.Background(), "user", "upload")
	if !errors.Is(err, errUploadStore) {
		t.Fatalf("CompleteUpload() error = %v, want %v", err, errUploadStore)
	}

	if storage.abortCalls != 0 {
		t.Errorf("abort calls = %d, want 0 for a session without a multipart", storage.abortCalls)
	}

	if !slices.Equal(uploads.deleted, []string{"upload"}) {
		t.Errorf("deleted sessions = %v, want the reservation released", uploads.deleted)
	}
}

func TestCompleteUploadReleasesTheReservation(t *testing.T) {
	t.Parallel()

	users := &fakeUserRepoForFile{user: entity.User{StorageQuotaBytes: 100, StorageUsedBytes: 100}}
	files := &fakeFileRepo{}
	storage := &fakeUploadStorage{}
	uploads := &fakeUploadRepo{
		sessions: []entity.UploadSession{completeSession()},
		parts: map[string]map[int]string{
			"upload": {1: "one", 2: "two"},
		},
	}
	uc := completeUseCase(users, files, storage, uploads)

	_, err := uc.CompleteUpload(context.Background(), "user", "upload")
	if !errors.Is(err, entity.ErrStorageQuotaExceeded) {
		t.Fatalf("CompleteUpload() error = %v, want %v", err, entity.ErrStorageQuotaExceeded)
	}

	if users.user.StorageUsedBytes != 100 {
		t.Errorf("storage used = %d, want the unchanged 100", users.user.StorageUsedBytes)
	}

	if files.stored != nil {
		t.Error("CompleteUpload() stored a row over the quota")
	}

	if !slices.Equal(uploads.deleted, []string{"upload"}) {
		t.Errorf("deleted sessions = %v, want [upload]", uploads.deleted)
	}

	if storage.removeKey == "" {
		t.Error("CompleteUpload() kept the object of the failed completion")
	}

	if uploads.lockCalls != 1 || uploads.releases != 1 {
		t.Errorf(
			"quota lock calls/releases = %d/%d, want 1/1",
			uploads.lockCalls,
			uploads.releases,
		)
	}
}

// expiredSession is one session the sweep tests let the store hand back. A
// swept session carries every field the abort needs, because it is built the
// way a live session is.
func expiredSession(id, userID string) entity.UploadSession {
	return entity.UploadSession{
		ID:          id,
		UserID:      userID,
		FolderID:    "folder",
		FileID:      "file-" + id,
		Name:        id + ".iso",
		StorageKey:  "users/" + userID + "/files/file-" + id,
		MultipartID: "multipart-" + id,
		Size:        _testFiveMiB,
		PartSize:    _testFiveMiB,
		PartCount:   1,
	}
}

func TestAbortUploadDeletesSessionAndMultipart(t *testing.T) {
	t.Parallel()

	session := expiredSession("upload", "user")
	uploads := &fakeUploadRepo{sessions: []entity.UploadSession{session}}
	storage := &fakeUploadStorage{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{MaxBytes: _testMaxBytes, PartBytes: _testPartBytes},
	)

	err := uc.AbortUpload(context.Background(), "user", "upload")
	if err != nil {
		t.Fatalf("AbortUpload() error = %v, want nil", err)
	}

	if storage.abortCalls != 1 {
		t.Fatalf("abort calls = %d, want 1", storage.abortCalls)
	}

	if storage.abortKey != session.StorageKey || storage.abortID != session.MultipartID {
		t.Errorf(
			"aborted %q/%q, want %q/%q",
			storage.abortID,
			storage.abortKey,
			session.MultipartID,
			session.StorageKey,
		)
	}

	if !slices.Equal(uploads.deleted, []string{"upload"}) {
		t.Errorf("deleted sessions = %v, want [upload]", uploads.deleted)
	}

	if uploads.lockCalls != 1 || uploads.releases != 1 {
		t.Errorf(
			"quota lock calls/releases = %d/%d, want 1/1",
			uploads.lockCalls,
			uploads.releases,
		)
	}
}

func TestAbortUploadIsIdempotentForMissingSession(t *testing.T) {
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

	err := uc.AbortUpload(context.Background(), "user", "gone")
	if err != nil {
		t.Fatalf("AbortUpload() error = %v, want nil for a missing session", err)
	}

	if storage.abortCalls != 0 {
		t.Errorf("abort calls = %d, want 0 for a missing session", storage.abortCalls)
	}

	if len(uploads.deleted) != 0 {
		t.Errorf("deleted sessions = %v, want none for a missing session", uploads.deleted)
	}

	if uploads.lockCalls != 0 {
		t.Errorf("quota lock calls = %d, want 0 for a missing session", uploads.lockCalls)
	}
}

func TestAbortUploadRejectsAnotherUser(t *testing.T) {
	t.Parallel()

	session := expiredSession("upload", "user")
	uploads := &fakeUploadRepo{sessions: []entity.UploadSession{session}}
	storage := &fakeUploadStorage{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{MaxBytes: _testMaxBytes, PartBytes: _testPartBytes},
	)

	err := uc.AbortUpload(context.Background(), "intruder", "upload")
	if !errors.Is(err, entity.ErrUploadNotFound) {
		t.Fatalf("AbortUpload() error = %v, want %v", err, entity.ErrUploadNotFound)
	}

	if storage.abortCalls != 0 {
		t.Error("AbortUpload() aborted another account's upload")
	}

	if len(uploads.deleted) != 0 {
		t.Errorf("deleted sessions = %v, want none for another account", uploads.deleted)
	}
}

// Aborting an empty file ends its session without touching storage, because the
// start of an empty file never opened a multipart upload.
func TestAbortUploadOfAnEmptyFileSkipsStorage(t *testing.T) {
	t.Parallel()

	session := expiredSession("upload", "user")
	session.Size = 0
	session.PartCount = 0
	session.MultipartID = ""

	uploads := &fakeUploadRepo{sessions: []entity.UploadSession{session}}
	storage := &fakeUploadStorage{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{MaxBytes: _testMaxBytes, PartBytes: _testPartBytes},
	)

	err := uc.AbortUpload(context.Background(), "user", "upload")
	if err != nil {
		t.Fatalf("AbortUpload() error = %v, want nil", err)
	}

	if storage.abortCalls != 0 {
		t.Errorf("abort calls = %d, want 0 for a session without a multipart", storage.abortCalls)
	}

	if !slices.Equal(uploads.deleted, []string{"upload"}) {
		t.Errorf("deleted sessions = %v, want [upload]", uploads.deleted)
	}
}

func TestSweepUploadsAbortsExpiredSessions(t *testing.T) {
	t.Parallel()

	first := expiredSession("upload-1", "user")
	second := expiredSession("upload-2", "other")

	uploads := &fakeUploadRepo{expired: []entity.UploadSession{first, second}}
	storage := &fakeUploadStorage{}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{MaxBytes: _testMaxBytes, PartBytes: _testPartBytes},
	)

	count, err := uc.SweepUploads(context.Background())
	if err != nil {
		t.Fatalf("SweepUploads() error = %v, want nil", err)
	}

	if count != 2 {
		t.Errorf("swept sessions = %d, want 2", count)
	}

	if storage.abortCalls != 2 {
		t.Errorf("abort calls = %d, want 2", storage.abortCalls)
	}

	if len(uploads.deleted) != 2 {
		t.Errorf("deleted sessions = %v, want both expired sessions", uploads.deleted)
	}

	for _, session := range []entity.UploadSession{first, second} {
		if !slices.Contains(uploads.deleted, session.ID) {
			t.Errorf("deleted sessions = %v, want %q", uploads.deleted, session.ID)
		}
	}

	if uploads.lockCalls != 2 || uploads.releases != 2 {
		t.Errorf(
			"quota lock calls/releases = %d/%d, want one pair per session",
			uploads.lockCalls,
			uploads.releases,
		)
	}

	if uploads.expiredCalls != 2 {
		t.Errorf("expired queries = %d, want 2 (one page, then an empty page)", uploads.expiredCalls)
	}
}

func TestSweepUploadsKeepsSessionsWhoseAbortFails(t *testing.T) {
	t.Parallel()

	session := expiredSession("upload", "user")
	uploads := &fakeUploadRepo{expired: []entity.UploadSession{session}}
	storage := &fakeUploadStorage{abortErr: errUploadStore}
	uc := newUploadUseCase(
		&fakeUserRepoForFile{},
		storage,
		uploads,
		&fakePacer{},
		usecase.UploadSettings{MaxBytes: _testMaxBytes, PartBytes: _testPartBytes},
	)

	count, err := uc.SweepUploads(context.Background())
	if !errors.Is(err, errUploadStore) {
		t.Fatalf("SweepUploads() error = %v, want %v", err, errUploadStore)
	}

	if !strings.Contains(err.Error(), "FileUseCase - SweepUploads - uc.storage.AbortMultipartUpload") {
		t.Errorf("SweepUploads() error = %v, want the storage path in the message", err)
	}

	if count != 0 {
		t.Errorf("swept sessions = %d, want 0", count)
	}

	if len(uploads.deleted) != 0 {
		t.Errorf("deleted sessions = %v, want the session kept for a retry", uploads.deleted)
	}

	if uploads.expiredCalls != 1 {
		t.Errorf("expired queries = %d, want 1 so a stuck session is not refetched", uploads.expiredCalls)
	}

	storage.abortErr = nil

	count, err = uc.SweepUploads(context.Background())
	if err != nil {
		t.Fatalf("SweepUploads() retry error = %v, want nil", err)
	}

	if count != 1 {
		t.Errorf("swept sessions on retry = %d, want 1", count)
	}

	if !slices.Equal(uploads.deleted, []string{"upload"}) {
		t.Errorf("deleted sessions = %v, want [upload]", uploads.deleted)
	}
}

func TestSweepUploadsWithoutExpiredSessions(t *testing.T) {
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

	count, err := uc.SweepUploads(context.Background())
	if err != nil {
		t.Fatalf("SweepUploads() error = %v, want nil", err)
	}

	if count != 0 {
		t.Errorf("swept sessions = %d, want 0", count)
	}

	if storage.abortCalls != 0 || len(uploads.deleted) != 0 {
		t.Errorf(
			"abort calls/deleted = %d/%d, want 0/0",
			storage.abortCalls,
			len(uploads.deleted),
		)
	}
}
