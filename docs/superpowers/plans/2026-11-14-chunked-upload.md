# Chunked Upload Through Cloudflare Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single-request upload with a chunked upload that passes Cloudflare's 100 MB body cap, streams parts into a MinIO multipart upload, and enforces one Redis-backed upload rate and quota reservation per account.

**Architecture:** The browser slices one file into parts at or below 50 MiB. `POST /htmx/uploads` reserves the whole size against quota and creates a MinIO multipart upload. Each `PUT .../parts/:n` is throttled by a per-account Redis pacer and forwarded with `UploadPart`. `POST .../complete` assembles the object and writes the existing file row. A Redis session, a sliding expiry, an abort beacon and a sweeper release abandoned reservations.

**Tech Stack:** Go 1.27, Fiber v2, minio-go v7, go-redis v9, templ, Alpine.js, htmx, PostgreSQL, MinIO (S3), Redis, `miniredis` for repository tests.

**Spec:** `docs/superpowers/specs/2026-11-14-chunked-upload-design.md`

## Global Constraints

- Module path is `github.com/minhhoccode111/fileuploader`.
- `pkg/` must never import `internal/`. Shared multipart part type lives in `pkg/multipart`.
- Whole-file ceiling: `10737418240` bytes (10 GiB).
- Preferred part size: `52428800` bytes (50 MiB). Absolute minimum: `5242880` bytes (5 MiB). Maximum parts: `10000`.
- Part size target: `min(preferred, max(5 MiB, rate * 90s))`. A rate at or below 0 means unlimited and uses the preferred size.
- No request body may exceed one part plus overhead. The server body limit becomes about 51 MiB; the general web limit becomes about 1 MiB.
- One upload rate per account, shared by every part and every upload. Zero means unlimited.
- Upload session idle TTL: `1h` since the last accepted part.
- Quota is reserved at start, released on complete, abort, failure or sweep.
- No resume. No database migration. MinIO stays private. Downloads keep their per-request throttle.
- Use `entity.Err*` sentinels; wrap infrastructure errors with the layer path.
- After any `.templ` edit run `make templ-generate`; after class changes run `make css-build`.
- Every task ends with `make format` and the listed test command. Final task runs `make linter-golangci`.
- The working tree may contain unrelated user changes. Stage only the files named in each task; never use `git add -A` or revert unrelated files.

## Review Focus

- A second upload starts when the first has already reserved most of the quota. Expected: admission rejects it before any bytes move, and abort/sweep returns the headroom.
- A part has the wrong `Content-Length`, an out-of-range number, or arrives after its session expired. Expected: a specific 4xx, no partial ETag stored, no reservation lost.
- The user navigates from folder A to folder B while an upload runs. Expected: B's rows are never replaced by A's; the user gets a success toast.
- The browser closes, crashes or sends no abort beacon. Expected: quota is released by the idle sweep within one TTL plus one sweep interval, not held forever.
- The account has a very low upload rate or an admin changes it mid-upload. Expected: parts share one bucket, no parallel bypass, and a part that cannot fit the proxy timeout is diagnosed rather than retried forever.

---

### Task 1: Config Defaults And Upload Session Math

**Files:**
- Modify: `config/config.go`
- Modify: `config/config_test.go`
- Modify: `.env.example`
- Modify: `docker-compose.yml`
- Modify: `internal/entity/errors.go`
- Create: `internal/entity/upload.go`
- Test: `internal/entity/upload_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `config.Config.Upload.MaxBytes` default `10737418240`
  - `config.Config.Upload.PartBytes` default `52428800`
  - `config.Config.Upload.SessionIdleTTL` default `1h`
  - `entity.UploadSession` with fields `ID, UserID, FolderID, FileID, Name, ContentType, StorageKey, MultipartID string; Size, PartSize int64; PartCount int; CreatedAt, ExpiresAt time.Time`
  - `entity.UploadPartCount(size, partSize int64) (int, error)`
  - `entity.UploadSession.ExpectedPartSize(partNumber int) (int64, error)`
  - `entity.ErrUploadNotFound`, `entity.ErrUploadPartMismatch`, `entity.ErrUploadIncomplete`, `entity.ErrUploadQuotaBusy`

- [ ] **Step 1: Write the failing entity tests**

Create `internal/entity/upload_test.go`:

```go
package entity

import (
	"errors"
	"testing"
)

func TestUploadPartCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		size    int64
		part    int64
		want    int
		wantErr error
	}{
		{name: "exact multiple", size: 100 << 20, part: 50 << 20, want: 2},
		{name: "remainder", size: 10<<20 + 1, part: 5 << 20, want: 3},
		{name: "ten gibibytes", size: 10 << 30, part: 50 << 20, want: 205},
		{name: "zero bytes", size: 0, part: 50 << 20, want: 0},
		{name: "below minimum part", size: 10, part: 1, wantErr: ErrInvalidUpload},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := UploadPartCount(tt.size, tt.part)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("UploadPartCount() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("UploadPartCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestUploadSessionExpectedPartSize(t *testing.T) {
	t.Parallel()

	session := UploadSession{Size: 10<<20 + 7, PartSize: 5 << 20, PartCount: 3}

	first, err := session.ExpectedPartSize(1)
	if err != nil || first != 5<<20 {
		t.Fatalf("part 1 = %d, %v; want %d, nil", first, err, 5<<20)
	}

	last, err := session.ExpectedPartSize(3)
	if err != nil || last != 7 {
		t.Fatalf("part 3 = %d, %v; want 7, nil", last, err)
	}

	if _, err := session.ExpectedPartSize(0); !errors.Is(err, ErrUploadPartMismatch) {
		t.Errorf("part 0 error = %v, want %v", err, ErrUploadPartMismatch)
	}

	if _, err := session.ExpectedPartSize(4); !errors.Is(err, ErrUploadPartMismatch) {
		t.Errorf("part 4 error = %v, want %v", err, ErrUploadPartMismatch)
	}
}
```

- [ ] **Step 2: Run the entity tests to verify they fail**

Run: `go test ./internal/entity -run 'TestUpload(PartCount|SessionExpectedPartSize)' -v`
Expected: FAIL because `UploadPartCount`, `UploadSession`, the sentinels and `ExpectedPartSize` do not exist.

- [ ] **Step 3: Implement the entity model**

In `internal/entity/errors.go`, add:

```go
ErrUploadNotFound     = errors.New("upload not found")
ErrUploadPartMismatch = errors.New("upload part mismatch")
ErrUploadIncomplete   = errors.New("upload incomplete")
ErrUploadQuotaBusy    = errors.New("upload quota busy")
```

Create `internal/entity/upload.go` with the `UploadSession` struct, these constants and helpers:

```go
const (
	MinUploadPartBytes int64 = 5 << 20
	MaxUploadParts           = 10000
)

func UploadPartCount(size, partSize int64) (int, error)
func (s UploadSession) ExpectedPartSize(partNumber int) (int64, error)
```

`UploadPartCount` rejects `size < 0`, `partSize < MinUploadPartBytes`, and a count above `MaxUploadParts` with `ErrInvalidUpload`. `ExpectedPartSize` treats numbers outside `1..PartCount` as `ErrUploadPartMismatch`.

- [ ] **Step 4: Write the failing config test**

Add to `config/config_test.go`:

```go
//nolint:paralleltest // t.Setenv cannot run in a parallel test
func TestUploadDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error = %v, want nil", err)
	}

	if cfg.Upload.MaxBytes != 10737418240 {
		t.Errorf("upload max = %d, want 10737418240", cfg.Upload.MaxBytes)
	}
	if cfg.Upload.PartBytes != 52428800 {
		t.Errorf("upload part = %d, want 52428800", cfg.Upload.PartBytes)
	}
	if cfg.Upload.SessionIdleTTL != time.Hour {
		t.Errorf("upload session idle TTL = %v, want 1h", cfg.Upload.SessionIdleTTL)
	}
}
```

Add `"time"` to the test imports.

- [ ] **Step 5: Run the config test to verify it fails**

Run: `go test ./config -run TestUploadDefaults -v`
Expected: FAIL because `PartBytes` and `SessionIdleTTL` do not exist, and `MaxBytes` still defaults to 104857600.

- [ ] **Step 6: Add the config fields and env examples**

In `config/config.go`'s `upload` struct:

```go
MaxBytes        int64         `env:"UPLOAD_MAX_BYTES"         envDefault:"10737418240"`
PartBytes       int64         `env:"UPLOAD_PART_BYTES"        envDefault:"52428800"`
SessionIdleTTL  time.Duration `env:"UPLOAD_SESSION_IDLE_TTL"  envDefault:"1h"`
Timeout         time.Duration `env:"UPLOAD_TIMEOUT"           envDefault:"5m"`
```

Update `.env.example`:

```dotenv
# Largest accepted file in bytes (default 10 GiB), part size, idle time before
# an abandoned upload is swept, and the request body timeout.
UPLOAD_MAX_BYTES=10737418240
UPLOAD_PART_BYTES=52428800
UPLOAD_SESSION_IDLE_TTL=1h
UPLOAD_TIMEOUT=5m
```

Update the app `environment` block in `docker-compose.yml` with the same values.

- [ ] **Step 7: Run the tests and format**

Run: `go test ./internal/entity ./config -v`
Expected: PASS.

Run: `make format`
Expected: no diff from the formatters.

- [ ] **Step 8: Commit**

```bash
git add config .env.example docker-compose.yml internal/entity
git commit -m "feat: add upload session model and chunk config"
```

---

### Task 2: Multipart Storage Interface And MinIO Implementation

**Files:**
- Create: `pkg/multipart/part.go`
- Modify: `internal/repo/contracts.go`
- Modify: `pkg/minio/storage.go`
- Create: `pkg/minio/multipart_test.go`
- Modify: `internal/usecase/file/file_test.go`
- Modify: `internal/usecase/folder/folder_test.go`

**Interfaces:**
- Consumes: `entity` sentinels from Task 1.
- Produces:
  - `multipart.Part{Number int; ETag string}`
  - `repo.ObjectStorage.CreateMultipartUpload(ctx context.Context, bucket, key, contentType string) (string, error)`
  - `repo.ObjectStorage.UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, reader io.Reader, size int64) (string, error)`
  - `repo.ObjectStorage.CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []multipart.Part) error`
  - `repo.ObjectStorage.AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error`

- [ ] **Step 1: Write the failing MinIO conversion test**

Create `pkg/minio/multipart_test.go`:

```go
package minio

import (
	"testing"

	"github.com/minhhoccode111/fileuploader/pkg/multipart"
)

func TestCompletePartsSortsByNumber(t *testing.T) {
	t.Parallel()

	got := completeParts([]multipart.Part{
		{Number: 2, ETag: "two"},
		{Number: 1, ETag: "one"},
	})

	if len(got) != 2 || got[0].PartNumber != 1 || got[1].PartNumber != 2 {
		t.Fatalf("completeParts() = %#v, want parts ordered 1 then 2", got)
	}
	if got[0].ETag != "one" || got[1].ETag != "two" {
		t.Errorf("completeParts() ETags = %q, %q", got[0].ETag, got[1].ETag)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./pkg/minio -run TestCompletePartsSortsByNumber -v`
Expected: FAIL because `multipart.Part` and `completeParts` do not exist.

- [ ] **Step 3: Add the neutral part type and the storage methods**

Create `pkg/multipart/part.go`:

```go
// Package multipart carries the storage-neutral description of one part of a
// multipart upload.
package multipart

// Part is one uploaded part and the ETag the storage returned for it.
type Part struct {
	Number int
	ETag   string
}
```

Add the four methods to `repo.ObjectStorage` in `internal/repo/contracts.go`. Import `pkg/multipart`.

In `pkg/minio/storage.go`, add the methods using the minio-go client:

- `CreateMultipartUpload` calls `m.Client.NewMultipartUpload(ctx, bucket, key, miniogo.PutObjectOptions{ContentType: contentType})`.
- `UploadPart` calls `m.Client.PutObjectPart(ctx, bucket, key, uploadID, partNumber, reader, size, miniogo.PutObjectPartOptions{})` and returns `part.ETag`.
- `CompleteMultipartUpload` calls `m.Client.CompleteMultipartUpload(ctx, bucket, key, uploadID, completeParts(parts), miniogo.PutObjectOptions{})`.
- `AbortMultipartUpload` calls `m.Client.AbortMultipartUpload`; if the error response code is `NoSuchUpload`, return nil, so a sweep can run twice.

Add `completeParts(parts []multipart.Part) []miniogo.CompletePart`, sorting a copy by `Number`.

- [ ] **Step 4: Update every test fake that implements `ObjectStorage`**

In `internal/usecase/file/file_test.go`, add methods to `fakeStorage`:

```go
func (f *fakeStorage) CreateMultipartUpload(_ context.Context, _, _, _ string) (string, error) {
	return "multipart-id", nil
}

func (f *fakeStorage) UploadPart(_ context.Context, _, _, _ string, partNumber int, r io.Reader, size int64) (string, error) {
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
```

Add the fields the fake needs and the `pkg/multipart` import. Add matching stubs to the `fakeStorage` in `internal/usecase/folder/folder_test.go`.

- [ ] **Step 5: Run the tests**

Run: `go test ./pkg/minio ./internal/usecase/file ./internal/usecase/folder -v`
Expected: PASS.

- [ ] **Step 6: Format and commit**

```bash
make format
git add pkg/multipart pkg/minio internal/repo/contracts.go internal/usecase/file/file_test.go internal/usecase/folder/folder_test.go
git commit -m "feat: add S3 multipart storage operations"
```

---

### Task 3: Redis Upload Session Repository

**Files:**
- Modify: `internal/repo/contracts.go`
- Create: `internal/repo/upload/upload.go`
- Test: `internal/repo/upload/upload_test.go`
- Modify: `go.mod`, `go.sum` (test dependency)

**Interfaces:**
- Consumes: `entity.UploadSession`, `entity.ErrUploadNotFound`, `entity.ErrUploadQuotaBusy`, `pkg/redis`.
- Produces: `repo.UploadSessionRepo` and `upload.New(rdb *redis.Redis) repo.UploadSessionRepo` with:

```go
Create(ctx context.Context, session entity.UploadSession, idleTTL time.Duration) error
Get(ctx context.Context, uploadID string) (entity.UploadSession, error)
SetPart(ctx context.Context, uploadID string, partNumber int, etag string, expiresAt time.Time) error
Parts(ctx context.Context, uploadID string) (map[int]string, error)
ListByUser(ctx context.Context, userID string) ([]entity.UploadSession, error)
Expired(ctx context.Context, now time.Time, limit int) ([]entity.UploadSession, error)
Delete(ctx context.Context, uploadID string) error
LockQuota(ctx context.Context, userID string) (func(), error)
```

- [ ] **Step 1: Add the Redis test dependency**

Run: `go get github.com/alicebob/miniredis/v2@latest`
Expected: `go.mod` and `go.sum` updated. This dependency is used only by tests.

- [ ] **Step 2: Write the failing repository tests**

Create `internal/repo/upload/upload_test.go` with a `newTestRepo(t)` helper using `miniredis.RunT(t)` and `redis.New(mr.Addr())`. Cover:

```go
func TestCreateAndGetRoundTrip(t *testing.T)
func TestSetPartStoresETagAndSlidesExpiry(t *testing.T)
func TestListByUserSumsOnlyLiveSessions(t *testing.T)
func TestExpiredReturnsDueSessions(t *testing.T)
func TestDeleteIsIdempotent(t *testing.T)
func TestLockQuotaSerializes(t *testing.T)
```

Assertions to pin:

- `Create` then `Get` returns every field and a zero `ErrUploadNotFound` only for missing IDs.
- `SetPart` stores `parts[2] == "etag-two"` and the expiry score increases by the new expiry.
- `ListByUser("user")` returns two sessions and skips a session owned by `"other"`; a stale ID left in the user set does not fail the call.
- `Expired(now.Add(2*idleTTL), 100)` returns the session created earlier; a fresh session is not returned.
- `Delete` twice returns nil both times, and `Get` after it returns `entity.ErrUploadNotFound`.
- `LockQuota` gives the first caller the lock; a second caller with a 50ms context returns `entity.ErrUploadQuotaBusy`; after release the second caller succeeds.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/repo/upload -v`
Expected: FAIL because the interface and repository do not exist.

- [ ] **Step 4: Add the interface and implement the repository**

Add `UploadSessionRepo` to `internal/repo/contracts.go` with the exact signatures above.

Create `internal/repo/upload/upload.go`, package `upload`, with constructor `New(rdb *redis.Redis) repo.UploadSessionRepo`. Use these keys:

```go
_uploadPrefix    = "upload:"
_partsPrefix     = "upload-parts:"
_userPrefix      = "user-uploads:"
_expiryKey       = "upload-expiry"
_quotaLockPrefix = "upload-quota-lock:"
```

Implementation rules:

- Session hash fields: `id, user_id, folder_id, file_id, name, content_type, size, part_size, part_count, storage_key, multipart_id, created_at` as Unix seconds.
- `Create` uses a pipeline: `HSET` the session, `SADD` the user set, `ZADD` `upload-expiry` with `now.Add(idleTTL).Unix()`. Set a fallback TTL of `idleTTL + 24h` on the session hash and part hash.
- `Get` uses `HGETALL`; missing key returns `entity.ErrUploadNotFound`; parse integers with `strconv.ParseInt`.
- `SetPart` uses a pipeline: `HSET upload-parts:<id>` field part number, `ZADD` the expiry score, `EXPIRE` both keys to `expiresAt.Sub(now)`. It must not create a session that does not exist.
- `Parts` uses `HGETALL` and returns a `map[int]string`.
- `ListByUser` uses `SMEMBERS`; call `Get` for each and drop members whose key is gone with `SREM`.
- `Expired` uses `ZRangeByScore` with `Max: strconv.FormatInt(now.Unix(), 10)` and `Offset: 0, Count: int64(limit)`; skip and remove missing members.
- `Delete` uses a pipeline with `DEL` on both hashes, `SREM` on the user set and `ZREM` on the expiry set.
- `LockQuota` uses `SET key token NX PX 30000` in a retry loop with a 25ms ticker and the request context. The release function deletes the key only when its value is still the token; otherwise it does nothing. A context deadline before acquisition returns `entity.ErrUploadQuotaBusy`.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/repo/upload -v`
Expected: PASS.

- [ ] **Step 6: Format, tidy and commit**

```bash
make format
make deps
git add internal/repo/contracts.go internal/repo/upload go.mod go.sum
git commit -m "feat: add Redis upload session repository"
```

---

### Task 4: Redis Per-Account Upload Pacer

**Files:**
- Modify: `internal/repo/contracts.go`
- Create: `internal/repo/transfer/transfer.go`
- Test: `internal/repo/transfer/transfer_test.go`

**Interfaces:**
- Consumes: `pkg/redis`.
- Produces:

```go
type TransferLimiter interface {
	Pace(ctx context.Context, userID string, bytes, rate int64) error
}
```

and `transfer.New(rdb *redis.Redis) repo.TransferLimiter`.

- [ ] **Step 1: Write the failing pacer tests**

Create `internal/repo/transfer/transfer_test.go` with a test constructor that sets the unexported clock and wait function:

```go
func newTestRepo(t *testing.T, now time.Time) (*Repo, *[]time.Duration) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb, err := redis.New(mr.Addr())
	if err != nil { t.Fatalf("redis.New() error = %v", err) }
	t.Cleanup(rdb.Close)

	var waits []time.Duration
	return &Repo{
		rdb: rdb,
		now: func() time.Time { return now },
		wait: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			return nil
		},
	}, &waits
}
```

Cover:

```go
func TestPaceSkipsUnlimitedAndZeroBytes(t *testing.T)
func TestPaceDelaysTheSecondReservation(t *testing.T)
func TestPaceSharesOneBucketAcrossCallers(t *testing.T)
func TestPaceReturnsRedisFailure(t *testing.T)
```

Assertions:

- `Pace(ctx, "user", 1<<20, 0)` records no wait and leaves the key absent.
- Two calls of `1<<20` at `1<<20` bytes/sec record waits `0` and `1s`.
- Two goroutines reserves `1<<20` each at `1<<20`; the recorded waits, sorted, are `0` and `1s` (mutex serializes the read-modify-write).
- Closing miniredis makes `Pace` return an error wrapping the Redis failure.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/repo/transfer -v`
Expected: FAIL because the package does not exist.

- [ ] **Step 3: Add the interface and implementation**

Add `TransferLimiter` to `internal/repo/contracts.go`.

Create `internal/repo/transfer/transfer.go`:

- Key: `upload-rate:<userID>`.
- `Repo` fields: `rdb *redis.Redis`, `mu sync.Mutex`, `now func() time.Time`, `wait func(context.Context, time.Duration) error`.
- `New` sets `now` to `time.Now` and `wait` to a context-aware timer:
```go
func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 { return nil }
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
```
- `Pace` returns nil when `rate <= 0` or `bytes <= 0`.
- Under `mu`, `GET` the stored next-send time in microseconds; default to `now.UnixMicro()`. Compute `sendAt = max(next, nowMicros)`, `newNext = sendAt + bytes*1_000_000/rate`, store `newNext` with `SET key newNext PX 3600000` (one hour). Release the mutex before waiting.
- Wait for `sendAt - now` outside the mutex.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/repo/transfer -v`
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
make format
git add internal/repo/contracts.go internal/repo/transfer
git commit -m "feat: add per-account Redis upload pacer"
```

---

### Task 5: Use Case Settings And StartUpload

**Files:**
- Modify: `internal/usecase/contracts.go`
- Modify: `internal/usecase/file/file.go`
- Create: `internal/usecase/file/upload.go`
- Test: `internal/usecase/file/upload_test.go`

**Interfaces:**
- Consumes: `repo.UploadSessionRepo`, `repo.TransferLimiter`, `repo.ObjectStorage` from Tasks 2-4.
- Produces:
  - `usecase.UploadSettings{MaxBytes, PartBytes int64; SessionIdleTTL time.Duration; DefaultQuotaBytes, DefaultUploadRateBytesPerSec int64}`
  - `usecase.File.StartUpload(ctx context.Context, userID, folderID, name, contentType string, size int64) (entity.UploadSession, error)`
  - The remaining upload methods arrive in Tasks 6-8.

- [ ] **Step 1: Write the failing StartUpload tests**

Create `internal/usecase/file/upload_test.go` in package `file`. Add a `fakeUploadRepo` implementing `repo.UploadSessionRepo` with fields for stored sessions, reserved sessions, parts, expired sessions, lock behavior and recorded calls. Add a `fakePacer` implementing `repo.TransferLimiter`.

Cover:

```go
func TestStartUploadCreatesSessionAndReservesQuota(t *testing.T)
func TestStartUploadRejectsWhenReservedHeadroomIsGone(t *testing.T)
func TestStartUploadShrinksPartsForASlowAccount(t *testing.T)
func TestStartUploadRejectsAFileOverTheLimit(t *testing.T)
func TestStartUploadAbortsMultipartWhenSessionStoreFails(t *testing.T)
```

Assertions:

- A 10 GiB upload with no quota gets `PartSize == 50<<20` and `PartCount == 205`; the fake session repo receives one session whose `Size` equals the request; `file_id`, `storage_key` and `multipart_id` are non-empty.
- With `user.StorageQuotaBytes = 100`, `StorageUsedBytes = 0` and a live session of 60 bytes, a 50-byte upload returns `entity.ErrStorageQuotaExceeded`; no session is created.
- With an effective rate of `100 * 1024` bytes/sec, `PartSize == 9_216_000` (100 KiB × 90s) and the count follows from it.
- A size equal to `10<<30` is accepted; `10<<30 + 1` returns `entity.ErrFileTooLarge`.
- If `uploadRepo.Create` returns an error, the fake storage records `AbortMultipartUpload` for the created multipart ID.

Use `New(users, files, folders, storage, &fakeUnitOfWork{}, "bucket", uploadRepo, pacer, usecase.UploadSettings{...})` in the tests.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/usecase/file -run TestStartUpload -v`
Expected: FAIL because the settings, interface methods and implementation do not exist.

- [ ] **Step 3: Add settings, dependencies and StartUpload**

In `internal/usecase/contracts.go`, add `UploadSettings` and `StartUpload` to the `File` interface with the exact signature from the design. Keep the existing `Upload` method for now so the old controller still compiles.

In `internal/usecase/file/file.go`, add fields:

```go
uploads       repo.UploadSessionRepo
pacer         repo.TransferLimiter
partBytes     int64
sessionIdleTTL time.Duration
defaultUploadRate int64
```

Extend the variadic `New` dispatch with `case repo.UploadSessionRepo`, `case repo.TransferLimiter` and `case usecase.UploadSettings`. Default `partBytes` to `50 << 20`, `sessionIdleTTL` to `time.Hour` and `maxBytes` to `10 << 30` when zero. Keep the existing `int64` max-bytes case for old tests.

Create `internal/usecase/file/upload.go` with:

```go
const (
	_uploadPartTargetSeconds = 90
	_uploadPartFloor         = int64(5 << 20)
	_uploadPartCeiling       = int64(50 << 20)
	_maxUploadParts          = 10000
)

func partSizeForRate(rate, preferred int64) int64
```

`partSizeForRate` returns `preferred` when rate <= 0, otherwise `min(preferred, max(5 MiB, rate*90))`. If `preferred < 5 MiB`, use 5 MiB.

`StartUpload` order:

1. `entity.CleanName(name)`.
2. Reject `size < 0` with `ErrInvalidUpload` and `size > uc.maxBytes` with `ErrFileTooLarge`.
3. `uc.folders.Get(ctx, userID, folderID)`; `uc.checkName(...)`.
4. Read the user when `uc.users != nil`; compute effective quota and rate.
5. Take `uc.uploads.LockQuota(ctx, userID)` when quota > 0. Sum `ListByUser` sizes, reject when `used + reserved + size > quota`.
6. Generate `fileID` and `uploadID` with `uuid.NewString()`, build `storageKey(userID, fileID)`, normalize content type.
7. Call `uc.storage.CreateMultipartUpload`.
8. Build `entity.UploadSession`, call `uc.uploads.Create(ctx, session, uc.sessionIdleTTL)`. On failure, call `AbortMultipartUpload` and return the create error wrapped.
9. Release the quota lock on every return path after acquisition.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/usecase/file -run TestStartUpload -v`
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
make format
git add internal/usecase/contracts.go internal/usecase/file
git commit -m "feat: start chunked uploads with quota reservation"
```

---

### Task 6: Use Case UploadPart

**Files:**
- Modify: `internal/usecase/file/upload.go`
- Test: `internal/usecase/file/upload_test.go`

**Interfaces:**
- Consumes: `repo.TransferLimiter.Pace`, `repo.UploadSessionRepo.SetPart`, `repo.ObjectStorage.UploadPart`.
- Produces: `File.UploadPart(ctx context.Context, userID, uploadID string, partNumber int, body io.Reader, size int64) error`, added to the `usecase.File` interface in this task.

- [ ] **Step 1: Write the failing tests**

Add to `internal/usecase/file/upload_test.go`:

```go
func TestUploadPartStoresETagAndSlidesExpiry(t *testing.T)
func TestUploadPartRejectsWrongLength(t *testing.T)
func TestUploadPartRejectsAnotherUsersSession(t *testing.T)
func TestUploadPartPacesTheBody(t *testing.T)
func TestUploadPartDoesNotStoreETagOnStorageFailure(t *testing.T)
```

Assertions:

- A session with `PartSize=5<<20`, `PartCount=2`, `Size=5<<20+7`; part 1 with size `5<<20` stores ETag `"etag"` and a `SetPart` expiry later than `time.Now()`.
- Part 2 with size `8` is rejected with `ErrUploadPartMismatch`; nothing reaches storage.
- Part 1 with size `5<<20+1` is rejected with `ErrUploadPartMismatch`.
- Another user's upload ID returns `ErrUploadNotFound`.
- A 1 MiB body at rate `1<<20` records a `Pace` call for every read batch; the sum of paced bytes equals the body length. Use a fake pacer that records `bytes`.
- A storage error returns the wrapped error and leaves `SetPart` uncalled.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/usecase/file -run TestUploadPart -v`
Expected: FAIL because `UploadPart` is missing.

- [ ] **Step 3: Implement UploadPart**

In `internal/usecase/file/upload.go`:

1. `session, err := uc.uploads.Get(ctx, uploadID)`; if missing return `fmt.Errorf("FileUseCase - UploadPart - uc.uploads.Get: %w", err)`.
2. If `session.UserID != userID`, return `entity.ErrUploadNotFound`.
3. `expected, err := session.ExpectedPartSize(partNumber)`; if `err != nil`, return it.
4. If `size != expected`, return `entity.ErrUploadPartMismatch`.
5. Resolve the user's effective rate when `uc.users != nil`; rate 0 means unlimited.
6. Wrap `body` with `pacedReader` when rate > 0:

```go
type pacedReader struct {
	r       io.Reader
	limiter repo.TransferLimiter
	ctx     context.Context
	userID  string
	rate    int64
	batch   int
}
```

`Read` caps `p` at `batch`; after a successful read it calls `limiter.Pace(ctx, userID, int64(n), rate)`. Batch is `clamp(rate/4, 64 KiB, 1 MiB)`.
7. Call `uc.storage.UploadPart(ctx, uc.bucket, session.StorageKey, session.MultipartID, partNumber, reader, size)`.
8. On success call `uc.uploads.SetPart(ctx, uploadID, partNumber, etag, time.Now().UTC().Add(uc.sessionIdleTTL))`.
9. Return wrapped errors with the `FileUseCase - UploadPart - ...` path.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/usecase/file -run TestUploadPart -v`
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
make format
git add internal/usecase/file
git commit -m "feat: upload one chunk through the shared pacer"
```

---

### Task 7: Use Case CompleteUpload

**Files:**
- Modify: `internal/usecase/file/upload.go`
- Test: `internal/usecase/file/upload_test.go`

**Interfaces:**
- Consumes: `repo.UploadSessionRepo.Parts`, `LockQuota`, `Delete`; `repo.ObjectStorage.CompleteMultipartUpload`, `PutObject`, `RemoveObject`; existing `storeRow`.
- Produces: `File.CompleteUpload(ctx context.Context, userID, uploadID string) (entity.File, error)`, added to the `usecase.File` interface in this task.

- [ ] **Step 1: Write the failing tests**

Add:

```go
func TestCompleteUploadAssemblesPartsAndStoresRow(t *testing.T)
func TestCompleteUploadRejectsMissingParts(t *testing.T)
func TestCompleteUploadCleansUpOnNameConflict(t *testing.T)
func TestCompleteUploadUsesPutObjectForAnEmptyFile(t *testing.T)
func TestCompleteUploadReleasesTheReservation(t *testing.T)
```

Assertions:

- With two parts `{1:"one", 2:"two"}`, the fake storage receives `CompleteMultipartUpload` with sorted `multipart.Part` values; the fake file repo receives the row; the session repo records `Delete`; the quota is incremented by `File.Size`.
- Missing part 2 returns `entity.ErrUploadIncomplete` and does not call `CompleteMultipartUpload`.
- A `storeRow` name conflict returns `entity.ErrEntryAlreadyExists`, calls `RemoveObject` for the completed key and still deletes the session.
- A zero-byte session calls `PutObject` with size 0 and never calls `CompleteMultipartUpload`.
- A completion with the quota recheck failing does not increment `StorageUsedBytes` and deletes the session.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/usecase/file -run TestCompleteUpload -v`
Expected: FAIL because `CompleteUpload` is missing.

- [ ] **Step 3: Implement CompleteUpload**

Order:

1. `uc.uploads.Get`; wrong owner -> `ErrUploadNotFound`.
2. `uc.uploads.Parts`; require exactly `PartCount` entries and every number `1..PartCount`; else `ErrUploadIncomplete`.
3. Acquire `LockQuota` and defer release.
4. Build `[]multipart.Part` sorted.
5. For `Size == 0`, call `PutObject` with `bytes.NewReader(nil)`. Otherwise call `CompleteMultipartUpload`.
6. Build `entity.File` from the session (`ID=session.FileID`, `UserID`, `FolderID`, `Name`, `ContentType`, `StorageKey`, `Size`, `CreatedAt`, `UpdatedAt`).
7. Call `uc.storeRow(ctx, session.UserID, session.FolderID, session.Name, &file)`.
8. On success, call `uc.uploads.Delete`; a delete failure is ignored because the sweeper will release the reservation later. Return the file.
9. On `storeRow` failure, call `uc.finalizeError(ctx, &file, err)`, then `uc.uploads.Delete`, then return the finalize error.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/usecase/file -run TestCompleteUpload -v`
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
make format
git add internal/usecase/file
git commit -m "feat: complete chunked uploads"
```

---

### Task 8: Use Case AbortUpload And SweepUploads

**Files:**
- Modify: `internal/usecase/file/upload.go`
- Test: `internal/usecase/file/upload_test.go`

**Interfaces:**
- Consumes: `repo.UploadSessionRepo.Expired`, `LockQuota`, `Delete`; `repo.ObjectStorage.AbortMultipartUpload`.
- Produces:
  - `File.AbortUpload(ctx context.Context, userID, uploadID string) error`
  - `File.SweepUploads(ctx context.Context) (int, error)`
  - Both methods added to the `usecase.File` interface in this task.

- [ ] **Step 1: Write the failing tests**

Add:

```go
func TestAbortUploadDeletesSessionAndMultipart(t *testing.T)
func TestAbortUploadIsIdempotentForMissingSession(t *testing.T)
func TestAbortUploadRejectsAnotherUser(t *testing.T)
func TestSweepUploadsAbortsExpiredSessions(t *testing.T)
func TestSweepUploadsKeepsSessionsWhoseAbortFails(t *testing.T)
```

Assertions:

- Abort calls `AbortMultipartUpload` and `uploads.Delete`; a missing session returns nil without touching storage.
- Another user's session returns `ErrUploadNotFound`.
- Sweep with two expired sessions returns `2`, calls abort twice, deletes twice, and takes the quota lock once per session.
- A storage abort failure returns an error and leaves that session undeleted; the next sweep can retry it.
- Running sweep on an empty list returns `0, nil`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/usecase/file -run 'Test(AbortUpload|SweepUploads)' -v`
Expected: FAIL because the methods are missing.

- [ ] **Step 3: Implement both methods**

`AbortUpload`:

1. `Get`; if `ErrUploadNotFound`, return nil.
2. Wrong owner -> `ErrUploadNotFound`.
3. Acquire `LockQuota`, defer release.
4. `AbortMultipartUpload`; wrap failures.
5. `Delete`; wrap failures.

`SweepUploads`:

1. Loop `Expired(ctx, time.Now().UTC(), 100)` until it returns none.
2. For each session, acquire the user's quota lock, call `AbortMultipartUpload`, then `Delete`.
3. Count successful deletions. On an abort or delete error, keep the session and collect the wrapped error.
4. If one full batch deletes nothing, stop and return the count with the joined errors; otherwise the same expired session would be fetched forever.
4. Sleep is not part of the use case; the app owns the ticker.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/usecase/file -run 'Test(AbortUpload|SweepUploads)' -v`
Expected: PASS.

- [ ] **Step 5: Format and commit**

```bash
make format
git add internal/usecase/file
git commit -m "feat: abort and sweep chunked uploads"
```

---

### Task 9: HTTP Handlers, DTOs, Routes And Body Limits

**Files:**
- Create: `internal/controller/web/request/upload.go`
- Create: `internal/controller/web/response/upload.go`
- Replace: `internal/controller/web/upload.go`
- Modify: `internal/controller/web/file.go`
- Modify: `internal/controller/web/router.go`
- Modify: `internal/controller/web/middleware/bodylimit.go`
- Modify: `internal/controller/web/middleware/bodylimit_test.go`
- Replace: `internal/controller/web/upload_test.go`
- Modify: `internal/controller/web/request/drive.go`

**Interfaces:**
- Consumes: `usecase.File.StartUpload`, `UploadPart`, `CompleteUpload`, `AbortUpload`.
- Produces:
  - `request.UploadStart{FolderID string `json:"folder"`; Name string `json:"name"`; Size int64 `json:"size"`; ContentType string `json:"content_type"`}`
  - `response.UploadStart{UploadID string `json:"upload_id"`; PartSize int64 `json:"part_size"`; PartCount int `json:"part_count"`}`
  - Routes `POST /htmx/uploads`, `PUT /htmx/uploads/:upload/parts/:part`, `POST /htmx/uploads/:upload/complete`, `POST /htmx/uploads/:upload/abort`.
  - Response header `X-Upload-Folder`.

- [ ] **Step 1: Write the failing handler tests**

Replace `internal/controller/web/upload_test.go`. Build a Fiber app with a test middleware that parks `entity.Session{UserID: "user"}` in `ctx.Locals(_sessionLocal, ...)`, a `fakeUploadFile` embedding `usecase.File`, and a fake folder usecase. Cover:

```go
func TestUploadStartReturnsPartPlan(t *testing.T)
func TestUploadStartRejectsAnInvalidBody(t *testing.T)
func TestUploadPartRejectsAWrongContentLength(t *testing.T)
func TestUploadPartPassesTheExactSize(t *testing.T)
func TestUploadCompleteSetsTheFolderHeader(t *testing.T)
func TestUploadAbortReturnsNoContent(t *testing.T)
func TestUploadErrorMapsQuotaAndMissingUpload(t *testing.T)
```

Assertions:

- Start with JSON `{"folder":"...","name":"note.txt","size":104857600,"content_type":"text/plain"}` returns 200 and the JSON fields from the fake `StartUpload`.
- Missing name returns 422.
- A part request with `Content-Length: 0` returns 411; with a length above the configured part limit returns 413; the handler never calls the use case.
- A valid part request calls `UploadPart` with the same part number and size.
- Complete calls `CompleteUpload`, sets `X-Upload-Folder: folder-id`, and returns the entries HTML.
- Abort returns 204 and calls `AbortUpload`.
- `entity.ErrStorageQuotaExceeded` maps to 413 with text; `entity.ErrUploadNotFound` maps to 404.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/controller/web -run TestUpload -v`
Expected: FAIL because the handlers and DTOs do not exist.

- [ ] **Step 3: Add the DTOs**

`request/upload.go`:

```go
type UploadStart struct {
	FolderID    string `json:"folder"       validate:"required,uuid4"`
	Name        string `json:"name"         validate:"required,max=255"`
	Size        int64  `json:"size"         validate:"gte=0"`
	ContentType string `json:"content_type" validate:"max=255"`
}

func (r *UploadStart) Sanitize() { r.Name = strings.TrimSpace(r.Name) }
```

`response/upload.go`:

```go
type UploadStart struct {
	UploadID  string `json:"upload_id"`
	PartSize  int64  `json:"part_size"`
	PartCount int    `json:"part_count"`
}
```

- [ ] **Step 4: Implement the four handlers**

Replace `internal/controller/web/upload.go` with the new handlers and delete the old multipart parser. `file.go` loses `fileUploadSubmit`, `uploadParseError`, `uploadError` and `uploadRate`; keep `downloadRate` and `newThrottledReader` for downloads.

- `uploadStart`: authenticate via `c.userID`; `ctx.BodyParser(&req)`; `Sanitize`; `c.v.Struct`; call `StartUpload`; return `ctx.JSON(response.UploadStart{...})`.
- `uploadPart`: validate the upload ID with `c.validID`; parse part number with `strconv.Atoi` and require `> 0`; read `ctx.Request().Header.ContentLength()`; if `< 0` return 411; if `0` or above `c.cfg.Upload.PartBytes` return 413; call `UploadPart(ctx.UserContext(), userID, uploadID, partNumber, ctx.Request().BodyStream(), length)`; return 204.
- `uploadComplete`: call `CompleteUpload`; set `ctx.Set("X-Upload-Folder", file.FolderID)`; set `_hxTriggerHeader`; `renderEntries(ctx, userID, file.FolderID)`.
- `uploadAbort`: call `AbortUpload`; return 204.
- Add `uploadFailure(ctx, err, action)` mapping the sentinels to plain-text statuses:
  - `ErrInvalidName`, `ErrInvalidUpload`, `ErrUploadPartMismatch` -> 422
  - `ErrFileTooLarge`, `ErrStorageQuotaExceeded` -> 413
  - `ErrFolderNotFound`, `ErrUploadNotFound` -> 404
  - `ErrEntryAlreadyExists`, `ErrUploadIncomplete` -> 409
  - `ErrUploadQuotaBusy` -> 503
  - default -> log and 500 generic text

- [ ] **Step 5: Register routes and change body limits**

In `router.go`:

```go
app.Use(middleware.BodyLimit(
	1<<20,
	cfg.Upload.PartBytes+config.MultipartOverheadBytes,
	"/htmx/uploads/",
))
```

Add the four routes behind `middleware.RequireAuth(u)`. Change the middleware signature to:

```go
func BodyLimit(general, upload int64, uploadPrefix string) fiber.Handler
```

Behavior:

- If the path starts with `uploadPrefix`, use `upload` as the known-length limit and allow unknown-length bodies.
- Otherwise use `general` and reject an unknown-length body with 411.
- Reject a known length above the chosen limit with 413.

Update `bodylimit_test.go` to cover: a 1 MiB general request, a part-sized request under `/htmx/uploads/`, an oversized part, and a chunked body outside the prefix.

Remove the old `app.Post("/htmx/files", ...)` route and `request.FileUpload` from `request/drive.go`.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/controller/web -run TestUpload -v`
Expected: PASS.

Run: `go test ./internal/controller/web/middleware -run TestBodyLimit -v`
Expected: PASS.

- [ ] **Step 7: Format and commit**

```bash
make format
git add internal/controller/web
git commit -m "feat: add chunked upload HTTP endpoints"
```

---

### Task 10: Browser Uploader, Folder Guard And Toast

**Files:**
- Modify: `web/templates/drive.templ`
- Modify: `web/templates/layout.templ`
- Modify: `web/static/js/app.js`
- Generated: `web/templates/*_templ.go`
- Generated: `web/static/css/app.css` if classes change

**Interfaces:**
- Consumes: routes and JSON from Task 9.
- Produces: browser behavior only.

- [ ] **Step 1: Add the template hooks**

In `drive.templ`:

- Keep the hidden `folder` input and `name` input.
- Remove `enctype="multipart/form-data"`.
- Keep `onsubmit="return uploadFileWithProgress(this)"`.
- Change `#drive-entries` to:

```templ
<div id="drive-entries" class="overflow-x-auto" data-folder-id={ page.FolderID }>
```

In `layout.templ`, before the closing `</body>`:

```templ
<div id="app-toast" class="toast toast-end toast-bottom z-50 hidden"></div>
```

- [ ] **Step 2: Regenerate templ**

Run: `make templ-generate`
Expected: `*_templ.go` updated; no stale HTML.

- [ ] **Step 3: Rewrite the uploader in `web/static/js/app.js`**

Replace `uploadFileWithProgress` with an async flow. Keep the global `hadUpload`/active state in module scope:

```js
let activeUpload = null;

function uploadFileWithProgress(form) { /* start async, return false */ }
async function startUpload(file, folder, name) { /* POST JSON */ }
function uploadPart(url, blob, onProgress) { /* XHR PUT, retries */ }
async function completeUpload(id) { /* POST, header check, swap or toast */ }
function abortActiveUpload() { /* sendBeacon */ }
function showToast(message) { /* fill #app-toast, remove hidden */ }
```

Rules:

- Read the file, folder and name from the form; capture `document.getElementById("drive-entries")?.dataset.folderId`.
- `POST /htmx/uploads` with JSON `{folder, name, size: file.size, content_type: file.type}`. On failure, show the response text in `#upload-error`.
- Send parts sequentially. Each part is `file.slice(offset, offset + partSize)`; use XHR `PUT` to `/htmx/uploads/${id}/parts/${number}`. Set `Content-Type: application/octet-stream`.
- Retry a part up to three times on network errors or 5xx except 524. Reset the per-part progress base on retry.
- Progress = `(completedBytes + event.loaded) / file.size * 100`.
- On complete, read `X-Upload-Folder`. If it equals the current `data-folder-id`, replace `#drive-entries.innerHTML`, call `htmx.process` and `truncateAllMiddle`, and dispatch `entrySaved`. Otherwise call `showToast("Upload complete")` and dispatch `entrySaved` so the drawer closes.
- Clear `activeUpload` before dispatching `entrySaved` on success.
- On an unrecoverable error, try `abortActiveUpload()` once, show the error, re-enable the submit button.
- Add `change` listener on `#entry-drawer`: when it becomes unchecked and `activeUpload` exists, send the abort beacon.
- Add `window.addEventListener("pagehide", abortActiveUpload)`.
- `abortActiveUpload` uses `navigator.sendBeacon("/htmx/uploads/" + activeUpload.id + "/abort")` and clears the active upload after the call.
- `showToast` puts `<div class="alert alert-success">` into `#app-toast`, removes `hidden`, and re-hides after 4 seconds.

- [ ] **Step 4: Build CSS**

Run: `make css-build`
Expected: `web/static/css/app.css` includes the toast classes; no hand edits.

- [ ] **Step 5: Manual browser check**

Run `make compose-up`, `make dev`, then:
1. Upload a small file; rows appear and the drawer closes.
2. Upload a file larger than 100 MB; parts are visible in the dev network panel, each below 50 MiB.
3. Start an upload, browse to another folder; completion shows the toast and does not replace the new folder's rows.
4. Close the drawer mid-upload; inspect Redis with `redis-cli KEYS 'upload:*'` and confirm the session is gone after the beacon (or within the sweep interval).
5. Set an upload rate in the admin UI, upload two files at once, and confirm the combined speed stays near the limit.

- [ ] **Step 6: Commit**

```bash
git add web/templates web/static/js/app.js web/static/css/app.css
git commit -m "feat: send uploads in chunks from the drawer"
```

---

### Task 11: Wire The Repositories, Limits And Sweeper

**Files:**
- Modify: `internal/app/app.go`
- Create: `internal/app/sweeper.go`
- Modify: `pkg/httpserver` usage in `internal/app/app.go`
- Modify: `config/config.go` (`UploadOverheadBytes`, replacing `MultipartOverheadBytes`)
- Modify: `internal/controller/web/router.go`

**Interfaces:**
- Consumes: `upload.New`, `transfer.New`, `File.SweepUploads`, `usecase.UploadSettings`.
- Produces: a running app with the chunked flow wired end to end.

- [ ] **Step 1: Add the sweeper loop**

Create `internal/app/sweeper.go`:

```go
const _uploadSweepInterval = 5 * time.Minute

type uploadSweeper interface {
	SweepUploads(ctx context.Context) (int, error)
}

func startUploadSweeper(ctx context.Context, files uploadSweeper, l logger.Interface)
```

It runs one sweep immediately, then on a `time.NewTicker(_uploadSweepInterval)`. Each run logs the count and any error. The goroutine exits on `ctx.Done()`.

- [ ] **Step 2: Wire the new repositories**

In `internal/app/app.go`:

```go
uploads := uploadRepo.New(rdb)
pacer := transferRepo.New(rdb)

file: usecaseFile.New(
	users, files, folders, storage, pg, cfg.MinIO.Bucket,
	uploads, pacer,
	usecase.UploadSettings{
		MaxBytes:                     cfg.Upload.MaxBytes,
		PartBytes:                    cfg.Upload.PartBytes,
		SessionIdleTTL:               cfg.Upload.SessionIdleTTL,
		DefaultQuotaBytes:            cfg.Limits.StorageQuotaBytes,
		DefaultUploadRateBytesPerSec: cfg.Limits.UploadRateBytesPerSec,
	},
),
```

Add the imports with aliases `uploadRepo` and `transferRepo`.

- [ ] **Step 3: Start and stop the sweeper**

In `Run`, after `initUseCases`:

```go
sweepCtx, stopSweep := context.WithCancel(context.Background())
defer stopSweep()

startUploadSweeper(sweepCtx, uc.file, l)
```

Keep the existing signal and server shutdown behavior.

- [ ] **Step 4: Change the HTTP body limit**

In `config/config.go`, rename `MultipartOverheadBytes` to `UploadOverheadBytes` and update references. In `initServers` set:

```go
httpserver.BodyLimit(int(cfg.Upload.PartBytes + config.UploadOverheadBytes))
```

In `router.go` use the same overhead for the web middleware. No route may accept more than one part plus overhead.

- [ ] **Step 5: Build and run the full test suite**

Run: `go build ./...`
Expected: success.

Run: `go test ./...`
Expected: PASS.

Run: `make format`
Expected: no formatter diff.

- [ ] **Step 6: Commit**

```bash
git add internal/app config internal/controller/web/router.go
git commit -m "feat: wire chunked uploads and the sweep loop"
```

---

### Task 12: Remove The Old Upload Path

**Files:**
- Modify: `internal/usecase/contracts.go`
- Modify: `internal/usecase/file/file.go`
- Modify: `internal/usecase/file/file_test.go`
- Modify: `internal/controller/web/request/drive.go`
- Modify: `internal/controller/web/file.go`
- Delete/replace: any remaining old upload tests
- Modify: `AGENTS.md` if it still describes the one-request upload

**Interfaces:**
- Consumes: the new flow from Tasks 5-11.
- Produces: one upload path only.

- [ ] **Step 1: Prove the old path is unused**

Run: `rg -n "FileUpload|fileUploadSubmit|parseUpload|uc.files.Upload|files.Upload" --glob '*.go'`
Expected: only the old use case method, its tests, the old DTO and the old handler remain.

- [ ] **Step 2: Delete the old implementation and DTO**

- Remove `Upload` from the `usecase.File` interface.
- Remove `FileUseCase.Upload` from `internal/usecase/file/file.go`.
- Remove `request.FileUpload` from `request/drive.go`.
- Remove the old parser constants and helpers if any remain.
- Keep `PutObject`, `normalizeContentType`, `storeRow`, `checkName`, `finalizeError`, `storageKey` because the new flow uses them.

- [ ] **Step 3: Repair the use case tests**

Delete the tests that exercise the old `Upload` method. Replace setup calls that used `uc.Upload` with direct fake rows, for example:

```go
files := &fakeFileRepo{file: entity.File{
	ID: "file", UserID: "user", FolderID: "folder",
	StorageKey: "users/user/files/file", Size: 4, Name: "note.txt",
}}
```

Run: `go test ./internal/usecase/file -v`
Expected: PASS with the remaining rename/delete/move/duplicate/download tests.

- [ ] **Step 4: Remove stale references and docs**

Update `AGENTS.md`'s upload description to the chunked flow: 10 GiB max, 50 MiB parts, Redis reservation/pacer, MinIO multipart, four endpoints, sweeper. Do not leave text that claims a multipart form body or a 100 MiB cap.

Run: `rg -n "UPLOAD_MAX_BYTES|MultipartOverheadBytes|/htmx/files\""`
Expected: only valid references remain.

- [ ] **Step 5: Full verification**

Run: `go test ./...`
Expected: PASS.

Run: `make format`
Expected: no diff.

Run: `make linter-golangci`
Expected: PASS.

Run: `make templ-generate`
Expected: no generated diff.

Run: `make css-build`
Expected: no CSS diff, or the expected toast classes only.

- [ ] **Step 6: Commit**

```bash
git add internal/usecase internal/controller/web/request/drive.go internal/controller/web/file.go AGENTS.md
git commit -m "refactor: remove the single-request upload path"
```

---

## Self-Review Notes

- Spec coverage: all four endpoints, quota reservation, idle expiry and abort beacon, Redis pacer, multipart storage, folder-match completion, 10 GiB limit, body limits, sweeper, tests and docs each map to a task.
- Type consistency: `UploadSession`, `multipart.Part`, `UploadSessionRepo`, `TransferLimiter`, `UploadSettings` signatures are repeated in the task that creates them and the task that consumes them.
- Deliberate deviations from the spec text: the pacer uses a process mutex around a Redis read-modify-write instead of Lua because the design already assumes one app instance; `pkg/multipart.Part` replaces the spec's earlier `repo.MultipartPart` so `pkg/minio` does not import `internal/`.
- The one uncovered edge is a browser that ignores or blocks `sendBeacon`; the idle sweep is the backstop and is tested through the use case sweeper tests.
