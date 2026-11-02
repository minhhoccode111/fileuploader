# User Limits and Storage Quota Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-user storage quotas and transfer speed limits, with global defaults and per-user overrides, and keep storage usage consistent across uploads, duplicates, and deletes.

**Architecture:** Store raw limits and usage on `users`, resolve effective values against config defaults, lock the user/root-folder mutation path before changing usage, and enforce quotas in the file and folder use cases. Upload throttling wraps the streamed file reader in the controller.

**Tech Stack:** Go, PostgreSQL, Squirrel, Fiber, golang.org/x/time/rate, templ.

**Spec:** `docs/superpowers/specs/2026-11-02-user-storage-and-transfer-limits-design.md`

## Global Constraints

- Run all commands through the Makefile; `make format` and `make linter-golangci` must pass.
- Follow clean architecture: controllers call use cases, use cases call repos, `context.Context` is first.
- Add both `.up.sql` and `.down.sql` for every migration, then run `make schema`.
- Constructors return interfaces and take dependencies as arguments; no package-level state.
- Unexported constants are prefixed with `_`; avoid bare numeric literals.

## Review Focus

- A quota check that reads usage before locking can let two uploads both pass and exceed the cap.
- Deleting a folder must free the bytes of every file in the subtree, not just the folder row.
- Duplicating a folder must charge the total size of all copied files, and fail before partial storage writes if the quota is exceeded.
- A per-user override of `0` must inherit the global default, while `0` global default means unlimited.
- Upload throttling must not break multipart parsing or the existing size checks.

---

### Task 1: Add user limit columns and usage backfill

**Files:**
- Create: `migrations/<timestamp>_add_user_limits_and_usage.up.sql`
- Create: `migrations/<timestamp>_add_user_limits_and_usage.down.sql`

**Interfaces:**
- Produces: `users.storage_quota_bytes`, `users.storage_used_bytes`, `users.upload_rate_bytes_per_sec`, `users.download_rate_bytes_per_sec`.

- [ ] **Step 1: Create the migration pair**

Run: `make migrate-create add_user_limits_and_usage`

- [ ] **Step 2: Write the up migration**

```sql
ALTER TABLE users
    ADD COLUMN storage_quota_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN storage_used_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN upload_rate_bytes_per_sec BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN download_rate_bytes_per_sec BIGINT NOT NULL DEFAULT 0;

UPDATE users
SET storage_used_bytes = COALESCE((
    SELECT SUM(files.size)
    FROM files
    WHERE files.user_id = users.id
), 0);
```

- [ ] **Step 3: Write the down migration**

```sql
ALTER TABLE users
    DROP COLUMN IF EXISTS storage_quota_bytes,
    DROP COLUMN IF EXISTS storage_used_bytes,
    DROP COLUMN IF EXISTS upload_rate_bytes_per_sec,
    DROP COLUMN IF EXISTS download_rate_bytes_per_sec;
```

- [ ] **Step 4: Apply and regenerate schema**

Run: `make migrate-up && make schema`
Expected: migration applies and `docs/schema.sql` updates.

- [ ] **Step 5: Commit**

```bash
git add migrations docs/schema.sql
git commit -m "feat(user): add per-user storage and transfer limits"
```

---

### Task 2: Add limit fields to the User entity

**Files:**
- Modify: `internal/entity/user.go`

**Interfaces:**
- Produces: `entity.User` fields `StorageQuotaBytes`, `StorageUsedBytes`, `UploadRateBytesPerSec`, `DownloadRateBytesPerSec`.
- Produces: methods `EffectiveStorageQuota`, `EffectiveUploadRate`, `EffectiveDownloadRate`.

- [ ] **Step 1: Write the failing entity test**

Add to `internal/entity/user_test.go`:

```go
func TestEffectiveLimitsInheritDefaults(t *testing.T) {
    user := entity.User{}
    if got := user.EffectiveStorageQuota(100); got != 100 {
        t.Errorf("quota = %d, want 100", got)
    }
    if got := user.EffectiveUploadRate(10); got != 10 {
        t.Errorf("upload rate = %d, want 10", got)
    }
    if got := user.EffectiveDownloadRate(20); got != 20 {
        t.Errorf("download rate = %d, want 20", got)
    }
}

func TestEffectiveLimitsUseOverride(t *testing.T) {
    user := entity.User{
        StorageQuotaBytes:      50,
        UploadRateBytesPerSec:  7,
        DownloadRateBytesPerSec: 9,
    }
    if got := user.EffectiveStorageQuota(100); got != 50 {
        t.Errorf("quota = %d, want 50", got)
    }
    if got := user.EffectiveUploadRate(10); got != 7 {
        t.Errorf("upload rate = %d, want 7", got)
    }
    if got := user.EffectiveDownloadRate(20); got != 9 {
        t.Errorf("download rate = %d, want 9", got)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/entity/... -run TestEffectiveLimits`
Expected: FAIL.

- [ ] **Step 3: Implement the fields and methods**

Add fields to `entity.User` and methods:

```go
func (u User) EffectiveStorageQuota(defaultQuota int64) int64 {
    if u.StorageQuotaBytes != 0 {
        return u.StorageQuotaBytes
    }
    return defaultQuota
}

func (u User) EffectiveUploadRate(defaultRate int64) int64 {
    if u.UploadRateBytesPerSec != 0 {
        return u.UploadRateBytesPerSec
    }
    return defaultRate
}

func (u User) EffectiveDownloadRate(defaultRate int64) int64 {
    if u.DownloadRateBytesPerSec != 0 {
        return u.DownloadRateBytesPerSec
    }
    return defaultRate
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/entity/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/entity/user.go internal/entity/user_test.go
git commit -m "feat(user): add effective limit resolution"
```

---

### Task 3: Persist and read the new user columns

**Files:**
- Modify: `internal/repo/persistent/user/user.go`
- Modify: `internal/repo/contracts.go` if adding limit-update methods
- Modify: `internal/usecase/user/user.go` if adding limit-update methods

**Interfaces:**
- Consumes: `entity.User` limit fields.
- Produces: `UserRepo.SetUserLimits(ctx, userID, quota, uploadRate, downloadRate) error`.
- Produces: `UserUseCase.SetUserLimits(...)`.

- [ ] **Step 1: Update Store and scans to include the four columns**

Update SQL column lists in `Store`, `getUser`, and `List` to read/write the new columns.

- [ ] **Step 2: Add SetUserLimits to UserRepo**

```go
func (r *Repo) SetUserLimits(ctx context.Context, userID string, quota, uploadRate, downloadRate int64) error
```

- [ ] **Step 3: Add SetUserLimits to UserUseCase**

Validate non-negative values, call repo, return updated user.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/repo/persistent/user/... ./internal/usecase/user/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repo internal/usecase/user
git commit -m "feat(user): persist and update per-user limits"
```

---

### Task 4: Adjust storage usage in FileUseCase

**Files:**
- Modify: `internal/usecase/file/file.go`
- Modify: `internal/usecase/file/file_test.go`
- Modify: `internal/app/app.go`

**Interfaces:**
- Consumes: `repo.UserRepo`.
- Produces: quota enforcement in `Upload`, `Delete`, `Duplicate`.

- [ ] **Step 1: Write failing tests for quota enforcement**

Add tests covering upload rejection, delete freeing quota, and duplicate charging quota.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/usecase/file/...`
Expected: FAIL.

- [ ] **Step 3: Add `users repo.UserRepo` to FileUseCase**

Update `New` signature and wiring in `internal/app/app.go`.

- [ ] **Step 4: Implement quota checks and usage updates**

In `Upload`, resolve effective quota and reject when `used + size > quota`. In `storeRow`, increment usage after successful store. In `Delete`, decrement usage. In `Duplicate`, check and increment usage.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/usecase/file/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/usecase/file internal/app
git commit -m "feat(file): enforce per-user storage quota"
```

---

### Task 5: Adjust storage usage in FolderUseCase

**Files:**
- Modify: `internal/usecase/folder/folder.go`
- Modify: `internal/usecase/folder/folder_test.go`
- Modify: `internal/repo/contracts.go`
- Modify: `internal/repo/persistent/folder/folder.go`
- Modify: `internal/app/app.go`

**Interfaces:**
- Consumes: `repo.UserRepo`.
- Produces: folder duplicate charges all copied file bytes; folder delete frees all contained file bytes.

- [ ] **Step 1: Change subtree deletion to return file rows**

Replace `DeleteByFolderSubtree` return type with `[]entity.File` so sizes and keys are available.

- [ ] **Step 2: Write failing tests**

Add tests for folder duplicate quota and folder delete freeing quota.

- [ ] **Step 3: Run tests to verify failure**

Run: `go test ./internal/usecase/folder/...`
Expected: FAIL.

- [ ] **Step 4: Implement quota accounting**

Add `users repo.UserRepo` to `FolderUseCase`. In `Duplicate`, sum copied file sizes and check quota before writes. In `Delete`, sum contained file sizes and decrement usage.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/usecase/folder/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/usecase/folder internal/repo internal/app
git commit -m "feat(folder): account folder copy and delete in quotas"
```

---

### Task 6: Throttle uploads in the controller

**Files:**
- Modify: `internal/controller/web/file.go`
- Modify: `go.mod`

**Interfaces:**
- Consumes: `entity.User` effective upload rate.
- Produces: throttled upload reader.

- [ ] **Step 1: Add `golang.org/x/time` dependency**

Run: `go get golang.org/x/time`

- [ ] **Step 2: Resolve user and effective rate before upload**

Update `fileUploadSubmit` to call `c.u.GetUser` and compute the effective upload rate.

- [ ] **Step 3: Wrap the uploaded file reader with a rate limiter**

Implement a small `throttledReader` using `rate.Limiter.WaitN`, or an equivalent helper, and pass it to `c.files.Upload`.

- [ ] **Step 4: Run tests and build**

Run: `go test ./internal/controller/web/... && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/web/file.go go.mod go.sum
git commit -m "feat(upload): throttle uploads per user"
```

---

### Task 7: Admin UI for per-user limits

**Files:**
- Modify: `internal/controller/web/request/*`
- Modify: `internal/controller/web/response/*`
- Modify: `web/templates/*`
- Modify: `internal/controller/web/admin.go`

**Interfaces:**
- Consumes: `UserUseCase.SetUserLimits`.

- [ ] **Step 1: Add request fields and validation**

Extend the user update request with quota and rate fields.

- [ ] **Step 2: Update controller submit flow**

Call `SetUserLimits` when the edited values change.

- [ ] **Step 3: Update templates**

Add inputs for quota, upload rate, download rate, and read-only used bytes.

- [ ] **Step 4: Regenerate templ and build**

Run: `make templ-generate && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/web web/templates
git commit -m "feat(admin): edit per-user storage and transfer limits"
```

---

### Task 8: Final verification

- [ ] **Step 1: Format and lint**

Run: `make format && make linter-golangci`
Expected: PASS.

- [ ] **Step 2: Run all tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 3: Commit any generated changes**

```bash
git add -A
git commit -m "chore: finalize user limits feature"
```