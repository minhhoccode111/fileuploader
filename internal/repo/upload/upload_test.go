package upload

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/repo"
	"github.com/minhhoccode111/fileuploader/pkg/redis"
	goredis "github.com/redis/go-redis/v9"
)

const _testIdleTTL = time.Hour

// newTestRepo starts an in-memory Redis and returns the repository under test
// together with the client, so a test can look at the keys the repository
// leaves behind.
func newTestRepo(t *testing.T) (repo.UploadSessionRepo, *redis.Redis) {
	t.Helper()

	mr := miniredis.RunT(t)

	rdb, err := redis.New(mr.Addr())
	if err != nil {
		t.Fatalf("redis.New() error = %v, want nil", err)
	}

	t.Cleanup(rdb.Close)

	return New(rdb), rdb
}

// testSession builds a session that differs in every field, so a lost field
// shows up as a mismatch.
func testSession() entity.UploadSession {
	return entity.UploadSession{
		ID:          "upload-one",
		UserID:      "user",
		FolderID:    "folder",
		FileID:      "file",
		Name:        "holiday.mp4",
		ContentType: "video/mp4",
		StorageKey:  "user/folder/file/holiday.mp4",
		MultipartID: "multipart-1",
		Size:        10 << 20,
		PartSize:    5 << 20,
		PartCount:   3,
		CreatedAt:   time.Unix(1_760_000_000, 0).UTC(),
	}
}

// A session written by Create must come back field for field. The expiry is
// not part of the hash: it lives in the expiry index as a score.
func TestCreateAndGetRoundTrip(t *testing.T) {
	t.Parallel()

	sessions, rdb := newTestRepo(t)
	ctx := context.Background()
	session := testSession()

	before := time.Now().Add(_testIdleTTL).Unix()

	err := sessions.Create(ctx, session, _testIdleTTL)
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	after := time.Now().Add(_testIdleTTL).Unix()

	got, err := sessions.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}

	if got != session {
		t.Errorf("Get() = %+v, want %+v", got, session)
	}

	score, err := rdb.Client.ZScore(ctx, _expiryKey, session.ID).Result()
	if err != nil {
		t.Fatalf("ZScore() error = %v, want nil", err)
	}

	if int64(score) < before || int64(score) > after {
		t.Errorf("expiry score = %d, want between %d and %d", int64(score), before, after)
	}

	_, err = sessions.Get(ctx, "missing")
	if !errors.Is(err, entity.ErrUploadNotFound) {
		t.Errorf("Get(missing) error = %v, want %v", err, entity.ErrUploadNotFound)
	}
}

// A part lands in the parts hash and pushes the expiry of the upload forward,
// so an upload that keeps receiving parts is not swept.
func TestSetPartStoresETagAndSlidesExpiry(t *testing.T) {
	t.Parallel()

	sessions, rdb := newTestRepo(t)
	ctx := context.Background()
	session := testSession()

	err := sessions.Create(ctx, session, _testIdleTTL)
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	expiresAt := time.Now().Add(3 * _testIdleTTL).Truncate(time.Second)

	err = sessions.SetPart(ctx, session.ID, 2, "etag-two", expiresAt)
	if err != nil {
		t.Fatalf("SetPart() error = %v, want nil", err)
	}

	parts, err := sessions.Parts(ctx, session.ID)
	if err != nil {
		t.Fatalf("Parts() error = %v, want nil", err)
	}

	if parts[2] != "etag-two" {
		t.Errorf("parts[2] = %q, want %q", parts[2], "etag-two")
	}

	score, err := rdb.Client.ZScore(ctx, _expiryKey, session.ID).Result()
	if err != nil {
		t.Fatalf("ZScore() error = %v, want nil", err)
	}

	if int64(score) != expiresAt.Unix() {
		t.Errorf("expiry score = %d, want %d", int64(score), expiresAt.Unix())
	}

	err = sessions.SetPart(ctx, "missing", 1, "etag", expiresAt)
	if !errors.Is(err, entity.ErrUploadNotFound) {
		t.Errorf("SetPart(missing) error = %v, want %v", err, entity.ErrUploadNotFound)
	}
}

// The list of an account sums the live sessions only: a session of another
// account is not counted, and an identifier left behind by a session that
// already expired is dropped instead of failing the call.
func TestListByUserSumsOnlyLiveSessions(t *testing.T) {
	t.Parallel()

	sessions, rdb := newTestRepo(t)
	ctx := context.Background()

	first := testSession()

	second := testSession()
	second.ID = "upload-two"
	second.Size = 20 << 20

	other := testSession()
	other.ID = "upload-other"
	other.UserID = "other"

	createSessions(t, sessions, first, second, other)

	err := rdb.Client.SAdd(ctx, _userPrefix+"user", "upload-stale").Err()
	if err != nil {
		t.Fatalf("SAdd() error = %v, want nil", err)
	}

	got, err := sessions.ListByUser(ctx, "user")
	if err != nil {
		t.Fatalf("ListByUser() error = %v, want nil", err)
	}

	assertSessionSet(t, got, first, second)

	stale, err := rdb.Client.SIsMember(ctx, _userPrefix+"user", "upload-stale").Result()
	if err != nil {
		t.Fatalf("SIsMember() error = %v, want nil", err)
	}

	if stale {
		t.Error("ListByUser() kept the stale member")
	}
}

// The sweep asks for the sessions due by a moment: one created earlier is due,
// one that expires much later is not, and a score without a session behind it
// is dropped.
func TestExpiredReturnsDueSessions(t *testing.T) {
	t.Parallel()

	sessions, rdb := newTestRepo(t)
	ctx := context.Background()
	due := testSession()

	err := sessions.Create(ctx, due, _testIdleTTL)
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	fresh := testSession()
	fresh.ID = "upload-fresh"

	err = sessions.Create(ctx, fresh, 10*_testIdleTTL)
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	err = rdb.Client.ZAdd(ctx, _expiryKey, goredis.Z{Score: 1, Member: "upload-ghost"}).Err()
	if err != nil {
		t.Fatalf("ZAdd() error = %v, want nil", err)
	}

	got, err := sessions.Expired(ctx, time.Now().Add(2*_testIdleTTL), 100)
	if err != nil {
		t.Fatalf("Expired() error = %v, want nil", err)
	}

	if len(got) != 1 || got[0].ID != due.ID {
		t.Fatalf("Expired() = %+v, want only %s", got, due.ID)
	}

	ghost, err := rdb.Client.ZScore(ctx, _expiryKey, "upload-ghost").Result()
	if !errors.Is(err, goredis.Nil) {
		t.Errorf("ZScore(ghost) = %v, %v; want the ghost removed", ghost, err)
	}
}

// Deleting an upload that is already gone is not an error, so the sweep and
// the abort path can both call it.
func TestDeleteIsIdempotent(t *testing.T) {
	t.Parallel()

	sessions, rdb := newTestRepo(t)
	ctx := context.Background()
	session := testSession()

	err := sessions.Create(ctx, session, _testIdleTTL)
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	err = sessions.SetPart(ctx, session.ID, 1, "etag-one", time.Now().Add(_testIdleTTL))
	if err != nil {
		t.Fatalf("SetPart() error = %v, want nil", err)
	}

	for range 2 {
		if err := sessions.Delete(ctx, session.ID); err != nil {
			t.Fatalf("Delete() error = %v, want nil", err)
		}
	}

	_, err = sessions.Get(ctx, session.ID)
	if !errors.Is(err, entity.ErrUploadNotFound) {
		t.Errorf("Get() after Delete error = %v, want %v", err, entity.ErrUploadNotFound)
	}

	assertDeleted(t, rdb, &session)
}

// createSessions stores every session, failing the test on the first error.
func createSessions(t *testing.T, sessions repo.UploadSessionRepo, want ...entity.UploadSession) {
	t.Helper()

	ctx := context.Background()

	for i := range want {
		if err := sessions.Create(ctx, want[i], _testIdleTTL); err != nil {
			t.Fatalf("Create(%s) error = %v, want nil", want[i].ID, err)
		}
	}
}

// assertSessionSet checks that a list carries exactly the wanted sessions and
// that their sizes add up, which is what the quota check reads.
func assertSessionSet(t *testing.T, got []entity.UploadSession, want ...entity.UploadSession) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("ListByUser() = %d sessions, want %d", len(got), len(want))
	}

	ids := make(map[string]bool, len(got))

	var total int64

	for i := range got {
		ids[got[i].ID] = true
		total += got[i].Size
	}

	var wantTotal int64

	for i := range want {
		wantTotal += want[i].Size

		if !ids[want[i].ID] {
			t.Errorf("ListByUser() ids = %v, want %s", ids, want[i].ID)
		}
	}

	if total != wantTotal {
		t.Errorf("ListByUser() total size = %d, want %d", total, wantTotal)
	}
}

// assertDeleted checks that Delete left no session hash, no parts hash and no
// index member behind.
func assertDeleted(t *testing.T, rdb *redis.Redis, session *entity.UploadSession) {
	t.Helper()

	ctx := context.Background()

	for _, key := range []string{sessionKey(session.ID), partsKey(session.ID)} {
		exists, err := rdb.Client.Exists(ctx, key).Result()
		if err != nil {
			t.Fatalf("Exists(%s) error = %v, want nil", key, err)
		}

		if exists != 0 {
			t.Errorf("Exists(%s) = %d, want 0", key, exists)
		}
	}

	member, err := rdb.Client.SIsMember(ctx, userKey(session.UserID), session.ID).Result()
	if err != nil {
		t.Fatalf("SIsMember() error = %v, want nil", err)
	}

	if member {
		t.Error("Delete() kept the user set member")
	}
}

// Only one caller holds the quota lock of an account at a time. A caller that
// cannot have it inside its deadline is told the quota is busy, and the next
// caller gets in once the holder releases.
func TestLockQuotaSerializes(t *testing.T) {
	t.Parallel()

	sessions, _ := newTestRepo(t)

	release, err := sessions.LockQuota(context.Background(), "user")
	if err != nil {
		t.Fatalf("LockQuota() error = %v, want nil", err)
	}

	if release == nil {
		t.Fatal("LockQuota() release = nil, want a function")
	}

	blocked, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = sessions.LockQuota(blocked, "user")
	if !errors.Is(err, entity.ErrUploadQuotaBusy) {
		t.Errorf("LockQuota() while held error = %v, want %v", err, entity.ErrUploadQuotaBusy)
	}

	release()

	waiting, cancelWaiting := context.WithTimeout(context.Background(), time.Second)
	defer cancelWaiting()

	again, err := sessions.LockQuota(waiting, "user")
	if err != nil {
		t.Fatalf("LockQuota() after release error = %v, want nil", err)
	}

	again()

	again()
}

// A release that arrives after the lock expired and another caller took it
// leaves the new holder alone, so an account is not unlocked twice.
func TestReleaseLeavesForeignLock(t *testing.T) {
	t.Parallel()

	sessions, rdb := newTestRepo(t)
	ctx := context.Background()

	release, err := sessions.LockQuota(ctx, "user")
	if err != nil {
		t.Fatalf("LockQuota() error = %v, want nil", err)
	}

	err = rdb.Client.Set(ctx, quotaLockKey("user"), "another-holder", 0).Err()
	if err != nil {
		t.Fatalf("Set() error = %v, want nil", err)
	}

	release()

	holder, err := rdb.Client.Get(ctx, quotaLockKey("user")).Result()
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}

	if holder != "another-holder" {
		t.Errorf("lock holder = %q, want %q", holder, "another-holder")
	}
}
