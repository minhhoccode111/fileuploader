# Meaningful Test Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace broad, shallow test coverage with tests that protect the public share boundary, auth guards, admin mutations and real persistence behavior.

**Architecture:** Unit and HTTP tests stay in their existing packages and use hand-written fakes. New controller tests are split by domain. Repository and storage tests live in one build-tagged integration package that uses the Compose Postgres and MinIO services. CI runs the unit gate with `-race`; integration is opt-in.

**Tech Stack:** Go 1.27, `testing`, Fiber `app.Test`, Postgres 18, MinIO, `golang-migrate` library, miniredis, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-22-meaningful-test-suite-design.md`

## Global Constraints

- Go 1.27 and the standard `testing` package. No test framework, no assertion library, no gomock or mockery.
- Every test that can run in parallel uses `t.Parallel()`; env-mutating tests are exempt and carry `//nolint:paralleltest`.
- New tests must pass `make format` and `make linter-golangci`.
- Integration tests use `//go:build integration` and never run under plain `go test ./...`.
- Integration tests use `fileuploader_test`, never the development database.
- Integration tests use the Compose `db` and `minio` services through `TEST_*` environment variables.
- No test may assert a wall-clock upper bound.
- Fakes must record the `userID` and folder/file identifiers passed to them.
- Test files follow the existing style: table-driven cases, `t.Run` names, `t.Helper()` helpers, exact `errors.Is` assertions.
- The linter rules apply: `funlen` 65 lines/40 statements, `gocyclo` 10, `gocognit` 15, `mnd`, `err113`, `wsl_v5`, `nlreturn`, `dupl` threshold 100.

## Review Focus

- **Share subtree escape:** a token for one folder must never open a sibling, ancestor or another account’s folder. Tested in Task 2 and Task 10.
- **Expired or revoked share:** an expired token must render an error, not cached content. Tested in Tasks 2 and 8.
- **Guard regression:** `RequireAuth` must redirect anonymous visitors and `RequireGuest` must redirect signed-in visitors, including htmx requests. Tested in Task 3.
- **Admin session revocation:** a role, password or non-active status change must end the account’s live sessions and set the update trigger. Tested in Task 5.
- **Keyset stability:** paging with duplicate lower-case names must not repeat or skip a row. Tested in Task 10.

---

### Task 1: Test Commands And CI Gate

**Files:**
- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`
- Modify: `README.md`

**Interfaces:**
- Produces:
  - `make test`
  - `make test-race`
  - `make test-shuffle`
  - `make test-cover`
  - `make test-integration`
  - `make test-fuzz`
- Consumes: no Go interfaces; only the existing Makefile variables `BASE_STACK` and the module path.

- [ ] **Step 1: Add the test targets to the Makefile**

Add these targets after `deps-audit`:

```make
test: ### run the unit and HTTP tests
	go test ./...
.PHONY: test

test-race: ### run every test with the race detector
	go test -race ./...
.PHONY: test-race

test-shuffle: ### run every test shuffled ten times
	go test -shuffle=on -count=10 ./...
.PHONY: test-shuffle

test-cover: ### write a coverage profile and print the function summary
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
.PHONY: test-cover

test-fuzz: ### fuzz the cursor decoder for ten seconds
	go test -fuzz=FuzzDecodeEntryCursor -fuzztime=10s ./internal/entity
.PHONY: test-fuzz

test-integration: ### start Postgres and MinIO, then run tagged integration tests
	$(BASE_STACK) up -d db minio
	TEST_PG_ADMIN_URL='postgres://user:password@localhost:5433/postgres?sslmode=disable' \
	TEST_PG_URL='postgres://user:password@localhost:5433/fileuploader_test?sslmode=disable' \
	TEST_MINIO_ENDPOINT='localhost:9000' \
	TEST_MINIO_ACCESS_KEY='minioadmin' \
	TEST_MINIO_SECRET_KEY='P@55w0rd' \
	TEST_MINIO_USE_SSL='false' \
	go test -tags=integration ./internal/repo/persistent/integration/...
.PHONY: test-integration
```

Add `test` to `pre-commit` before `linter-golangci`:

```make
pre-commit: deps templ-generate assets css-build format schema test linter-golangci ### run pre-commit
```

- [ ] **Step 2: Run the fast commands**

Run: `make test`

Expected: every package passes or reports `no test files`.

Run: `make test-race`

Expected: every package passes under `-race`.

Do not run `make test-shuffle` yet; the existing slow stream test makes it take too long. Task 10 removes that test.

- [ ] **Step 3: Add the unit-test CI job**

Append this job to `.github/workflows/ci.yml`:

```yaml
  test:
    name: runner / go test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version: '1.27'
      - name: Run tests
        run: go test -race ./...
      - name: Run shuffled tests
        run: go test -shuffle=on -count=5 ./...
```

- [ ] **Step 4: Document the commands**

In `README.md`, under `Development`, replace the bare `go test ./...` line with:

```sh
make test
make test-race
make test-shuffle
make test-cover
make test-integration
```

State that `make test-integration` needs Docker and starts `db` and `minio`.

- [ ] **Step 5: Commit**

```bash
git add Makefile .github/workflows/ci.yml README.md
git commit -m "test: add test commands and CI gate"
```

---

### Task 2: Share Use Case Security Tests

**Files:**
- Create: `internal/usecase/share/share_test.go`
- Modify: no production files unless a test exposes a real bug.

**Interfaces:**
- Consumes: `share.New(args ...any) usecase.Share`, `entity.Share`, `entity.Folder`, `entity.File`, `repo.ShareRepo`, `repo.FolderRepo`, `repo.FileRepo`, `repo.UnitOfWork`.
- Produces: fakes and helpers local to the package test file.

- [ ] **Step 1: Write the fake repositories**

Create `internal/usecase/share/share_test.go` in package `share`. Use embedded interfaces so an unexpected call panics instead of silently returning zero values:

```go
type fakeShareRepo struct {
    repo.ShareRepo
    share       entity.Share
    active      entity.Share
    byFolder    entity.Share
    activeErr   error
    byFolderErr error
    storeErr    error
    updateErr   error
    deleteErr   error
    getErr      error
    stored      *entity.Share
    updated     *entity.Share
    deletedID   string
    lastUserID  string
}

type fakeFolderRepo struct {
    repo.FolderRepo
    folder     entity.Folder
    getErr     error
    crumbs     []entity.FolderRef
    crumbErr   error
    lastUserID string
    lastFolder string
}

type fakeFileRepo struct {
    repo.FileRepo
    file       entity.File
    getErr     error
    lastUserID string
    lastFile   string
}

type fakeUnitOfWork struct{ called bool }

func (f *fakeUnitOfWork) Do(ctx context.Context, fn func(context.Context) error) error {
    f.called = true

    return fn(ctx)
}
```

Every fake method must capture the `userID` it receives. The tests assert these fields; a test that only checks the returned error is not enough.

Implement these methods explicitly; embedding the interface only supplies the
ones the use case never calls:

- `fakeShareRepo`: `Store`, `Get`, `GetActiveByUserAndFolder`,
  `GetByUserAndFolder`, `ListByUser`, `UpdateExpiry`, `Delete`.
- `fakeFolderRepo`: `LockUser`, `Get`, `Breadcrumb`.
- `fakeFileRepo`: `Get`.

- [ ] **Step 2: Write the `Create` tests**

Add these test functions:

- `TestCreateStoresAShare`: folder lookup succeeds, no existing share; assert `stored.UserID`, `stored.FolderID`, non-empty ID, and `uow.called`.
- `TestCreateReusesAnActiveShare`: `active` returns a share; assert `UpdateExpiry` was called, `Store` was not, and the returned expiry is the requested one.
- `TestCreateRevivesAnExpiredShare`: `GetActive` returns `ErrShareNotFound`, `GetByUserAndFolder` returns an expired share; assert the same row ID is reused and `Store` is not called.
- `TestCreateRejectsAnUnknownFolder`: `folders.Get` returns `entity.ErrFolderNotFound`; assert the sentinel survives wrapping and nothing was stored.
- `TestCreateReportsAStoreFailure`: `Store` returns `errStore`; assert `errors.Is(err, errStore)` and the operation is wrapped with `ShareUseCase - Create`.

- [ ] **Step 3: Write the `Get` tests**

Use `time.Now().UTC().Add(...)` for the expiry values.

- `TestGetReturnsAShareWithoutExpiry`: assert the share is returned.
- `TestGetRejectsAnExpiredShare`: expiry one minute in the past; assert `entity.ErrShareExpired`.
- `TestGetRejectsAShareAtTheExpiryInstant`: expiry exactly now; assert `entity.ErrShareExpired`.
- `TestGetRejectsAMissingShare`: repository returns `entity.ErrShareNotFound`; assert the sentinel.

- [ ] **Step 4: Write the `ResolveFolder` tests**

- `TestResolveFolderUsesTheShareRootForAnEmptyID`: empty folder ID; assert `folders.Get` received `share.FolderID`.
- `TestResolveFolderAllowsTheShareRootAndADescendant`: table-driven; crumbs include the share root; assert the folder is returned.
- `TestResolveFolderRejectsASiblingOrAncestor`: crumbs do not include the share root; assert `entity.ErrFolderNotFound`.
- `TestResolveFolderRejectsAnotherOwnersFolder`: `folders.Get` returns `entity.ErrFolderNotFound`; assert the sentinel and that `Breadcrumb` was not called.
- `TestResolveFolderScopesEveryReadToTheOwner`: assert `folders.lastUserID` equals `share.UserID` in both `Get` and `Breadcrumb`.

- [ ] **Step 5: Write the `ResolveFile` tests**

- `TestResolveFileAllowsAFileInsideTheSubtree`: file folder is the share root; assert the file is returned.
- `TestResolveFileRejectsAFileOutsideTheSubtree`: crumbs do not include the root; assert `entity.ErrFileNotFound`.
- `TestResolveFileScopesTheReadToTheOwner`: assert `files.lastUserID` equals `share.UserID`.
- `TestResolveFileRejectsAnUnknownFile`: repository returns `entity.ErrFileNotFound`; assert the sentinel.

- [ ] **Step 6: Write the management tests**

- `TestDeleteScopesTheShareToTheOwner`: assert `shares.lastUserID` equals the caller and `deletedID` equals the share.
- `TestUpdateExpiryRejectsAnotherOwnersShare`: `UpdateExpiry` succeeds, `Get` returns another owner; assert `entity.ErrShareNotFound`.
- `TestListDelegatesToTheRepository`: assert the owner reaches `ListByUser`.

- [ ] **Step 7: Run the package tests**

Run: `go test ./internal/usecase/share -count=1`

Expected: PASS. If a test fails, decide first whether the test or the production code is wrong. Do not weaken the assertion to make it green. If production is wrong, fix it in the smallest change and rerun this task’s tests.

- [ ] **Step 8: Commit**

```bash
git add internal/usecase/share/share_test.go internal/usecase/share/share.go
git commit -m "test(share): cover expiry, subtree and ownership rules"
```

Only add `share.go` if the task found and fixed a production bug.

---

### Task 3: Auth Guard Matrix

**Files:**
- Modify: `internal/controller/web/middleware/guard_test.go`

**Interfaces:**
- Consumes: `RequireAuth`, `RequireGuest`, `RequireAdmin`, `fakeAuthenticator`, `call`, `result`.
- Produces: `guardApp(guard fiber.Handler) *fiber.App` and `guardCall(...)` if the existing helper cannot be reused.

- [ ] **Step 1: Add a generic guard app helper**

Refactor the existing `adminApp` helper into:

```go
func guardApp(guard fiber.Handler) *fiber.App {
    app := fiber.New()
    app.Get("/guarded", guard, func(ctx *fiber.Ctx) error {
        return ctx.SendString("guarded page")
    })

    return app
}
```

Change the existing `call` helper to take the path:

```go
func call(t *testing.T, app *fiber.App, path string, cookie bool, headers map[string]string) result
```

Update every existing `call(t, adminApp(...), ...)` to pass `/admin`. Keep `adminApp` as a thin wrapper if existing tests call it.

- [ ] **Step 2: Add `RequireAuth` tests**

- `TestRequireAuthLetsASignedInVisitorThrough`: cookie plus `fakeAuthenticator` session; assert 200, body, and that `ctx.Locals("session")` and `ctx.Locals("userID")` are set.
- `TestRequireAuthSendsAnonymousVisitorsToLogin`: no cookie; assert 302 to `/login`.
- `TestRequireAuthSendsAnUnknownSessionToLogin`: cookie plus authenticator error; assert 302 to `/login` and no session local.
- `TestRequireAuthRedirectsAnHtmxRequestInPlace`: no cookie plus `HX-Request: true`; assert 200 and `HX-Location` contains `/login`.

- [ ] **Step 3: Add `RequireGuest` tests**

- `TestRequireGuestLetsAnonymousVisitorsThrough`: no cookie; assert 200.
- `TestRequireGuestTreatsAnUnknownSessionAsAnonymous`: cookie plus authenticator error; assert 200.
- `TestRequireGuestSendsASignedInVisitorHome`: valid session; assert 302 to `/`.
- `TestRequireGuestRedirectsAnHtmxRequestInPlace`: valid session plus `HX-Request: true`; assert 200 and `HX-Location` contains `/`.

- [ ] **Step 4: Run the middleware tests**

Run: `go test ./internal/controller/web/middleware -run 'TestRequire' -count=1 -v`

Expected: PASS. Each test must fail if the guard is changed to always pass or always redirect.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/web/middleware/guard_test.go
git commit -m "test(middleware): cover auth and guest guards"
```

---

### Task 4: Login Policy Tests

**Files:**
- Modify: `internal/usecase/user/user_test.go`

**Interfaces:**
- Consumes: `fakeUserRepo`, `fakeSessionRepo`, `entity.User`, `entity.UserStatus`, `entity.ErrInvalidCredentials`, `entity.ErrUserPending`, `entity.ErrUserDisabled`.
- Produces: `fakeSessionRepo.storeErr` and `fakeSessionRepo.getErr` fields.

- [ ] **Step 1: Extend the session fake**

Add to `fakeSessionRepo`:

```go
storeErr error
getErr   error
deleteErr error
lastID   string
lastTTL  time.Duration
```

Make `Store` record `id` and the TTL, and return `storeErr`. Make `Get` return `getErr` when it is set. Make `Delete` return `deleteErr`. Do not break existing tests: zero values keep the old behavior.

- [ ] **Step 2: Add the login matrix**

- `TestLoginRejectsAWrongPassword`: active user with a bcrypt hash for another password; assert `entity.ErrInvalidCredentials`, no session stored.
- `TestLoginRejectsAnUnknownEmail`: repo returns `entity.ErrUserNotFound`; assert `entity.ErrInvalidCredentials`.
- `TestLoginRejectsAPendingAccount`: correct password, status `pending`; assert `entity.ErrUserPending`, no session.
- `TestLoginRejectsADisabledAccount`: correct password, status `disabled`; assert `entity.ErrUserDisabled`, no session.
- `TestLoginReportsASessionStoreFailure`: correct password, active account, `storeErr` set; assert the wrapped error and no session returned.
- `TestLoginStoresTheTTL`: assert `Store` received the configured `sessionTTL`.

- [ ] **Step 3: Add authentication and logout tests**

- `TestAuthenticatePassesTheSessionID`: fake `Get` records the ID; assert it equals the caller’s value and the session is returned.
- `TestAuthenticateReportsAMissingSession`: fake `getErr` is `entity.ErrSessionNotFound`; assert the sentinel survives wrapping.
- `TestLogoutDeletesTheSession`: fake records the deleted ID; assert `Delete` was called once with the caller’s session ID.
- `TestLogoutReportsADeleteFailure`: add `deleteErr` to the fake and assert the wrapped error.

- [ ] **Step 4: Run the user tests**

Run: `go test ./internal/usecase/user -count=1`

Expected: PASS. A login that skips the status check must fail at least one of the new tests.

- [ ] **Step 5: Commit**

```bash
git add internal/usecase/user/user_test.go
git commit -m "test(user): cover login policy and session lifecycle"
```

---

### Task 5: Admin Mutation Tests

**Files:**
- Create: `internal/controller/web/admin_mutation_test.go`
- Modify: `internal/controller/web/admin_test.go` only if a shared helper is needed.

**Interfaces:**
- Consumes: `Controller`, `usecase.User`, `entity.User`, `entity.UserFilter`, `entity.UserList`, `request.UserUpdate`, `request.Role`, `validatorx.New()`, `logger.New("error")`.
- Produces: `fakeAdminUser` and `adminMutationApp(users usecase.User) *fiber.App`.

- [ ] **Step 1: Write the admin fake**

Create `internal/controller/web/admin_mutation_test.go` in package `web`:

```go
type fakeAdminUser struct {
    usecase.User
    user       entity.User
    list       entity.UserList
    listErr    error
    updateErr  error
    limitsErr  error
    statusErr  error
    roleErr    error
    approveErr error

    lastFilter   entity.UserFilter
    lastUserID   string
    lastUsername string
    lastEmail    string
    lastRole     entity.UserRole
    lastPassword string
    lastStatus   entity.UserStatus
    lastQuota    int64
    lastUpload   int64
    lastDownload int64
    lastApproved string
    sessionsEnded bool
}
```

Implement only the methods the handlers call. Record every argument. Return canned values.

Add a helper that wires the handlers behind a middleware parking `entity.Session{UserID: "admin-1", Role: entity.UserRoleAdmin}`:

```go
func adminMutationApp(users usecase.User) *fiber.App
```

Register:

```text
GET  /htmx/admin/users
GET  /htmx/admin/users/edit
POST /htmx/admin/users/edit
POST /htmx/admin/role
GET  /htmx/admin/access-requests
POST /htmx/admin/access-requests/approve
```

- [ ] **Step 2: Write the account table tests**

- `TestAdminUsersTablePassesTheFilter`: request `?q=john&role=admin&sort=email&dir=asc&limit=10&page=2`; assert the fake received the normalized filter and the page renders rows.
- `TestAdminUsersTableRejectsAMalformedQuery`: `?sort=password_hash`; assert 422 and `ListUsers` was not called.
- `TestAdminUsersTablePushesAChangedURL`: request with filters; assert `HX-Push-Url` contains `/admin/users?` and the encoded filter.
- `TestAdminUsersTableSkipsThePushWhenTheURLIsUnchanged`: set `HX-Current-URL` to the same filtered path; assert no `HX-Push-Url`.
- `TestAdminUsersTableReportsAListFailure`: `listErr` set; assert 500.

- [ ] **Step 3: Write the edit tests**

- `TestAdminUsersEditRendersTheAccount`: `GetUser` returns a user; assert 200 and the form fields.
- `TestAdminUsersEditRejectsABadID`: `?id=not-a-uuid`; assert 422 and no `GetUser`.
- `TestAdminUsersEditRejectsAnUnknownAccount`: `ErrUserNotFound`; assert 404.

- [ ] **Step 4: Write the edit-submit tests**

Use `url.Values` and `application/x-www-form-urlencoded`.

- `TestAdminUsersEditSubmitUpdatesTheAccount`: valid form; assert `UpdateUser` received ID, username, email, role and password, `SetUserLimits` received the parsed byte values, `SetStatus` was or was not called according to the status, response is 200, and `HX-Trigger` is `usersUpdated`.
- `TestAdminUsersEditSubmitRejectsBadLimits`: `storage_quota=1XB`; assert 422 and no `UpdateUser`.
- `TestAdminUsersEditSubmitRejectsNegativeLimits`: `storage_quota=-1`; assert 422 and no write.
- `TestAdminUsersEditSubmitEndsSessionsForANonActiveStatus`: valid form with `status=disabled`; assert `SetStatus` called and the response says sessions ended.
- `TestAdminUsersEditSubmitReportsAConflict`: `updateErr` is `entity.ErrEmailAlreadyExists`; assert 409.

- [ ] **Step 5: Write the role and access-request tests**

- `TestRoleSubmitUpdatesTheRole`: valid email and role; assert `SetRole` received the lower-cased email and role, 200, and `HX-Trigger: usersUpdated`.
- `TestRoleSubmitRejectsAnUnknownAccount`: `roleErr` is `entity.ErrUserNotFound`; assert 404.
- `TestRoleSubmitRejectsAnInvalidRole`: form role `root`; assert 422 and no `SetRole`.
- `TestAccessRequestsTableForcesPending`: request with filters; assert `lastFilter.Status == entity.UserStatusPending` and newest-first order.
- `TestAccessRequestApproveReturnsNoContent`: valid ID; assert 204 and `HX-Trigger: accessRequestsUpdated`.
- `TestAccessRequestApproveRejectsABadID`: no ID; assert 400.

- [ ] **Step 6: Run the controller tests**

Run: `go test ./internal/controller/web -run 'TestAdmin|TestRole|TestAccess' -count=1 -v`

Expected: PASS. The existing template-only tests stay untouched.

- [ ] **Step 7: Commit**

```bash
git add internal/controller/web/admin_mutation_test.go
git commit -m "test(web): cover admin mutations and access requests"
```

---

### Task 6: File Action Controller Tests

**Files:**
- Create: `internal/controller/web/file_action_test.go`

**Interfaces:**
- Consumes: `usecase.File`, `usecase.Folder`, `entity.File`, `request.FileRename`, `request.FileMove`, `request.FileDuplicate`.
- Produces: `fakeFileActions` and `fileActionApp(files usecase.File, folders usecase.Folder) *fiber.App`.

- [ ] **Step 1: Write the file fake**

```go
type fakeFileActions struct {
    usecase.File
    file       entity.File
    getErr     error
    renameErr  error
    moveErr    error
    duplicateErr error
    deleteErr  error
    renamed    string
    movedTo    string
    duplicatedTo string
    deletedID  string
    lastUserID string
    lastFileID string
    getCalls   int
}
```

Record every argument. Return canned values. Add a fake folder use case that returns a canned `FolderView` for `List` and `ListAll`.

- [ ] **Step 2: Wire the file routes**

Register these handlers on a Fiber app behind a middleware parking `UserID: "user-1"`:

```text
GET  /htmx/files/new
GET  /htmx/files/:file
GET  /htmx/files/:file/move
GET  /htmx/files/:file/duplicate
POST /htmx/files/:file/rename
POST /htmx/files/:file/move
POST /htmx/files/:file/duplicate
POST /htmx/files/:file/delete
```

- [ ] **Step 3: Write the tests**

- `TestFileNewRendersTheUploadForm`: request `?folder=<uuid>`; assert 200 and `name="file"`.
- `TestFileDrawerRendersTheFile`: valid file ID; assert 200, file name, download link.
- `TestFileDrawerRejectsABadID`: assert 404 and no `Get`.
- `TestFileRenameRefreshesTheFolder`: valid form; assert `Rename` received the file ID and trimmed name, `List` received `file.FolderID`, and `HX-Trigger` is `entrySaved`.
- `TestFileMoveRefreshesTheSourceFolder`: assert `Get` ran before `Move`, `Move` received the destination, `List` received the source folder.
- `TestFileDuplicateRefreshesTheSourceFolder`: same shape for `Duplicate`.
- `TestFileDeleteRefreshesTheFolder`: assert `Delete` and `List`; assert 200 and `HX-Trigger`.
- `TestFileDeleteTreatsCleanupFailureAsSuccess`: return `entity.ErrStorageCleanup`; assert 200 and `HX-Trigger`, not 500.
- `TestFileActionsRejectMalformedForms`: table-driven; missing name, bad destination UUID, bad file ID; assert the documented status and no mutation call.

- [ ] **Step 4: Run the file controller tests**

Run: `go test ./internal/controller/web -run 'TestFile' -count=1 -v`

Expected: PASS. The existing download tests remain separate.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/web/file_action_test.go
git commit -m "test(web): cover file actions"
```

---

### Task 7: Folder Action Controller Tests

**Files:**
- Create: `internal/controller/web/folder_action_test.go`

**Interfaces:**
- Consumes: `usecase.Folder`, `entity.Folder`, `entity.FolderView`, `request.FolderCreate`, `request.FolderMove`, `request.FolderDuplicate`.
- Produces: `fakeFolderActions`, `folderActionApp(folders usecase.Folder) *fiber.App`.

- [ ] **Step 1: Write the folder fake**

Record `Get`, `Create`, `Rename`, `Move`, `Duplicate`, `Delete`, `ListAll`, `DownloadZip` calls and their arguments. Include:

```go
view       entity.FolderView
folder     entity.Folder
parent     entity.Folder
archive    io.ReadCloser
filename   string
getErr     error
createErr  error
moveErr    error
duplicateErr error
deleteErr  error
zipErr     error
lastUserID string
lastFolder string
lastParent string
lastName   string
lastDest   string
```

- [ ] **Step 2: Wire the folder routes**

Register the page and action routes behind the session middleware:

```text
GET  /htmx/folders/new
GET  /htmx/folders/:folder/edit
GET  /htmx/folders/:folder/move
GET  /htmx/folders/:folder/duplicate
GET  /htmx/folders/tree
POST /htmx/folders
POST /htmx/folders/:folder/rename
POST /htmx/folders/:folder/move
POST /htmx/folders/:folder/duplicate
POST /htmx/folders/:folder/delete
GET  /folders/:folder/download
```

- [ ] **Step 3: Write the tests**

- `TestFolderNewRendersTheCreateForm`: assert 200 and the parent field.
- `TestFolderEditRejectsTheRoot`: `folder.IsRoot()`; assert 422.
- `TestFolderCreateRefreshesTheParent`: assert `Create` received `parentID` and trimmed name; `List` received the parent; `HX-Trigger`.
- `TestFolderMoveRefreshesTheSourceParent`: assert `Get` before `Move`, destination passed, source parent refreshed.
- `TestFolderDuplicateRefreshesTheSourceParent`: same for `Duplicate`.
- `TestFolderDeleteRedirectsWithTheSearch`: set `HX-Current-URL` with `q=note`; assert redirect to the parent with `?q=note`.
- `TestFolderDeleteFallsBackHomeWhenTheParentIsGone`: `Get` returns `ErrFolderNotFound`; assert redirect to `/?q=note`.
- `TestFolderDownloadZipStreamsTheArchive`: assert zip content type, `Content-Disposition`, and the body bytes.
- `TestFolderDownloadZipRedirectsOnFailure`: `zipErr`; assert redirect to `/`.
- `TestFolderActionsRejectMalformedForms`: bad parent UUID, empty name, bad destination; assert status and no mutation.

- [ ] **Step 4: Run the folder controller tests**

Run: `go test ./internal/controller/web -run 'TestFolder(New|Edit|Create|Move|Duplicate|Delete|Download)' -count=1 -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/web/folder_action_test.go
git commit -m "test(web): cover folder actions and zip download"
```

---

### Task 8: Share Management And Public Download Tests

**Files:**
- Create: `internal/controller/web/share_manage_test.go`

**Interfaces:**
- Consumes: `usecase.Share`, `usecase.Folder`, `usecase.File`, `entity.Share`, `request.ShareCreate`, `request.ShareUpdateExpiry`.
- Produces: `fakeShareManage`, `fakeSharedFile`, `shareManageApp`.

- [ ] **Step 1: Write the fakes**

Reuse the existing `shareStub` and `sharedFolderStub` from `share_test.go` where
they fit. Extend `shareStub` to record `List`, `Create`, `UpdateExpiry` and
`Delete`; it already records `Get` and `ResolveFolder`. Add a `fakeSharedFile`
that records `OpenDownload` and returns a canned reader/file. Add a tiny
`fakeShareUser` that implements `Authenticate` so the owner-redirect test can
park a session behind a cookie. Record every `userID`, `folderID`, `shareID`
and expiry the handlers pass.

- [ ] **Step 2: Wire the routes**

```text
GET  /share
GET  /htmx/folders/:folder/share
GET  /htmx/shares/:share
POST /htmx/shares
POST /htmx/shares/:share/time
POST /htmx/shares/:share/delete
GET  /share/:share
GET  /share/:share/folders/:folder
GET  /share/:share/files/:file/download
```

Park a session `UserID: "user-1"` for owner routes. Leave public routes without the session middleware.

- [ ] **Step 3: Write the management tests**

- `TestShareIndexListsTheAccount`: assert `List` received `user-1` and rows render.
- `TestShareFormRendersTheFolder`: assert the folder ID is in the form.
- `TestShareDrawerRendersTheShare`: assert the share link and expiry.
- `TestShareCreateStoresTheShare`: valid form with an expiry; assert `Create` received the owner, folder and parsed UTC expiry; assert redirect to `/share`.
- `TestShareCreateRejectsAnInvalidExpiry`: `expires_at=not-a-date`; assert 422 and no `Create`.
- `TestShareCreateWithUnlimitedSendsNilExpiry`: assert `expiresAt == nil`.
- `TestShareUpdateTimeRefreshesTheRows`: valid share; assert `UpdateExpiry` and `List`; assert `HX-Trigger`.
- `TestShareUpdateTimeRejectsAnotherOwnersShare`: `ErrShareNotFound`; assert 404.
- `TestShareDeleteRefreshesTheRows`: assert `Delete` and `List`.
- `TestShareDeleteRejectsABadID`: assert 404 and no `Delete`.

- [ ] **Step 4: Write the public access tests**

- `TestSharedFileDownloadStreamsAFile`: valid token; assert 200, bytes, `Content-Disposition`, and that `ResolveFile` received the share token and file ID.
- `TestSharedFileDownloadRejectsAnExpiredShare`: `ResolveFile` returns `ErrShareExpired`; assert the error page and no bytes.
- `TestSharedFileDownloadRejectsAFileOutsideTheShare`: `ErrFileNotFound`; assert the error page.
- `TestSharedFileDownloadRejectsAnUnknownToken`: `ErrShareNotFound`; assert the error page.
- `TestSharedRootOpensTheShareRoot`: assert `ResolveFolder` received `folderID == ""`.
- `TestSharedRootRedirectsTheOwnerToTheirDrive`: valid owner session; assert the drive redirect, not a duplicate public page.

- [ ] **Step 5: Run the share controller tests**

Run: `go test ./internal/controller/web -run 'TestShare|TestShared' -count=1 -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/controller/web/share_manage_test.go
git commit -m "test(web): cover share management and public download"
```

---

### Task 9: Fakes Assert Tenant Scope

**Files:**
- Modify: `internal/usecase/file/file_test.go`
- Modify: `internal/usecase/folder/folder_test.go`
- Modify: `internal/usecase/file/upload_test.go` if a fake needs the same fields.

**Interfaces:**
- Consumes: existing fake types.
- Produces: fields `lastUserID`, `lastFolderID`, `lastFileID`, `lastIDs []string` on the relevant fakes.

- [ ] **Step 1: Record file repo arguments**

Change `fakeFileRepo.Get` to:

```go
func (f *fakeFileRepo) Get(_ context.Context, userID, fileID string) (entity.File, error) {
    f.lastUserID = userID
    f.lastFileID = fileID

    return f.file, f.getErr
}
```

Do the same for `Store`, `Rename`, `Move`, `Delete`, `NameTaken`, `DeleteByFolderSubtree`, and `ListPage`. Add the fields to the struct. Do not change their return behavior.

Also record `userID` in `fakeUserRepoForFile.GetByID` and `AdjustStorageUsed`, and assert it in `TestDeleteFreesQuota` and the duplicate quota tests.

- [ ] **Step 2: Record folder repo arguments**

Record `userID`, `folderID`, `parentID`, and the `folderIDs` slice on `Get`, `Lock`, `ListPage`, `Paths`, `Breadcrumb`, `NameTaken`, `Rename`, `Move`, `Delete`, `ListAll`.

- [ ] **Step 3: Assert the scope in existing tests**

Add assertions to the tests that exercise a repository call:

- `TestRenameStoresTheNewName`: assert `files.lastUserID == "user"` and `files.lastFileID == "file"`.
- `TestDeleteRemovesTheRowAndTheObject`: assert the file and user IDs.
- `TestDownloadSignsTheObject`: assert the file and user IDs.
- `TestSearchFillsPathsForFoldersAndFiles`: assert `folders.pathIDs` contains `folder-1` and `folder-2`, not only that `Paths` was called.
- `TestListPassesTheFolderAndSearchToBothRepos`: assert `folders.lastFolderUserID` and `files.lastFileUserID` equal the caller.

- [ ] **Step 4: Strengthen the quota tests**

In `TestDuplicateChargesQuota`, after the error assertion, add:

```go
if storage.copyKey != "" {
    t.Errorf("CopyObject key = %q, want no copy over quota", storage.copyKey)
}

if files.stored != nil {
    t.Error("Duplicate stored a row over quota")
}
```

In `TestStartUploadRejectsWhenReservedHeadroomIsGone`, assert `storage.createCalls == 0` and `uploads.lockCalls == 1` (already present; keep it).

- [ ] **Step 5: Run the affected use-case tests**

Run: `go test ./internal/usecase/file ./internal/usecase/folder -count=1`

Expected: PASS. If a new assertion fails, treat it as a production bug until proven otherwise.

- [ ] **Step 6: Commit**

```bash
git add internal/usecase/file/file_test.go internal/usecase/folder/folder_test.go internal/usecase/file/upload_test.go
git commit -m "test: assert tenant scope through repository fakes"
```

Only add `upload_test.go` if its fake needs the new fields.

---

### Task 10: Remove Flaky And Low-Value Tests

**Files:**
- Modify: `internal/controller/web/middleware/latency_test.go`
- Modify: `pkg/httpserver/server_test.go`
- Modify: `internal/controller/web/upload_test.go`
- Modify: `internal/controller/web/admin_test.go`
- Modify: `internal/usecase/folder/folder_test.go`

**Interfaces:**
- Consumes: existing helpers.
- Produces: deterministic tests only.

- [ ] **Step 1: Replace the latency wall-clock tests**

Delete `TestLatencyStaysUnderTheCeiling` and `TestLatencyStaysOffWithoutAMaximum`. Keep `TestRandomDelayLandsInsideTheRange`.

Add:

```go
func TestRandomDelayIsZeroWhenTheRangeIsOff(t *testing.T) {
    t.Parallel()

    for _, maxDelay := range []time.Duration{0, -time.Second} {
        if got := randomDelay(time.Second, maxDelay); got != 0 {
            t.Errorf("randomDelay(max=%s) = %s, want 0", maxDelay, got)
        }
    }
}
```

Do not add a test that measures `Latency` with `time.Since`.

- [ ] **Step 2: Replace the five-second stream test**

In `pkg/httpserver/server_test.go`, delete `TestStreamedDownloadIsNotCutByAWriteTimeout` and its `slowReader`. Add a package-internal test in `pkg/httpserver/server_internal_test.go`:

```go
package httpserver

import (
    "testing"
    "time"

    "github.com/minhhoccode111/fileuploader/pkg/logger"
)

func TestNewUsesNoWriteTimeoutByDefault(t *testing.T) {
    t.Parallel()

    srv := New(logger.New("error"))
    if srv.writeTimeout != 0 {
        t.Errorf("writeTimeout = %s, want 0", srv.writeTimeout)
    }
}

func TestWriteTimeoutOptionIsApplied(t *testing.T) {
    t.Parallel()

    srv := New(logger.New("error"), WriteTimeout(50*time.Millisecond))
    if srv.writeTimeout != 50*time.Millisecond {
        t.Errorf("writeTimeout = %s, want 50ms", srv.writeTimeout)
    }
}
```

Fix the import path to the real module path. This is a white-box test of the default that caused the original bug.

- [ ] **Step 3: Fix the upload content-length test**

Rename `TestUploadPartRejectsAWrongContentLength` to
`TestUploadPartMapsALengthMismatch`. The controller contract is: it rejects a
negative or oversized `Content-Length`, but a zero-length body reaches the use
case, which decides whether the part size is valid. Keep the fake
`partErr = entity.ErrUploadPartMismatch` in the empty-body case, assert
`files.partCalls == 1`, and assert the 422 body. Do not change the handler.
The old test name and comment claimed rejection happened before the use case,
which was false. Keep the oversized-part case unchanged because that one is
rejected before the use case.

- [ ] **Step 4: Remove markup assertions that cannot fail behavior**

In `internal/controller/web/admin_test.go`:

- Delete `TestAdminUsersTableKeepsTheColumnsFixed`.
- Delete `TestAdminUsersTableCarriesTheSort`.
- Keep `TestAdminUsersTableShowsTheStorage`; it asserts the formatted values.
- Keep `TestAdminUsersSortHeader`; it asserts the generated `sort`/`dir` link values. Do not add more class-name assertions.

The behavior the deleted tests pretended to cover is now in Task 5 (`TestAdminUsersTablePassesTheFilter`, `TestAdminUsersTablePushesAChangedURL`).

- [ ] **Step 5: Use `_pageSize` in folder tests**

In `internal/usecase/folder/folder_test.go`, replace the hardcoded page numbers:

- `100` -> `_pageSize`
- `97` -> `_pageSize - 3`
- `98` -> `_pageSize - 2`
- `101` -> `_pageSize + 1`
- `"folder-099"` -> the last item from `pageFolders(_pageSize)`
- `"file-096"` -> the last item from `pageFiles(_pageSize - 3)`

Do not replace `_pageSize` with a literal in the production code.

- [ ] **Step 6: Run the fast suite**

Run: `make test-race`

Expected: PASS.

Run: `make test-shuffle`

Expected: PASS, now without the five-second sleep.

- [ ] **Step 7: Commit**

```bash
git add internal/controller/web/middleware/latency_test.go pkg/httpserver/server_test.go pkg/httpserver/server_internal_test.go internal/controller/web/upload_test.go internal/controller/web/admin_test.go internal/usecase/folder/folder_test.go
git commit -m "test: remove flaky and markup-only assertions"
```

---

### Task 11: Postgres Integration Harness And Repository Tests

**Files:**
- Create: `internal/repo/persistent/integration/doc.go`
- Create: `internal/repo/persistent/integration/main_test.go`
- Create: `internal/repo/persistent/integration/postgres_test.go`
- Create: `internal/repo/persistent/integration/file_test.go`
- Create: `internal/repo/persistent/integration/folder_test.go`
- Create: `internal/repo/persistent/integration/share_test.go`
- Create: `internal/repo/persistent/integration/user_test.go`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `postgres.New`, `file.New`, `folder.New`, `share.New`, `user.New`, `entity` types, `golang-migrate`.
- Produces:
  - `func testPostgres(t *testing.T) *postgres.Postgres`
  - `func seedUser(t *testing.T, pg *postgres.Postgres, username, email string) entity.User`
  - `func seedRoot(t *testing.T, pg *postgres.Postgres, userID string) entity.Folder`
  - `func seedFolder(t *testing.T, pg *postgres.Postgres, userID, parentID, name string) entity.Folder`
  - `func seedFile(t *testing.T, pg *postgres.Postgres, userID, folderID, name, key string, size int64) entity.File`

- [ ] **Step 1: Write the integration bootstrap**

Create `internal/repo/persistent/integration/doc.go` with no build tag:

```go
// Package integration holds the build-tagged repository and storage tests.
package integration
```

This file keeps the package visible to `go test ./...`, which would otherwise
report "matched no packages" when every other file in the directory is
integration-tagged.

At the top of `main_test.go`:

```go
//go:build integration

package integration
```

`TestMain` must:

1. Read `TEST_PG_ADMIN_URL` and `TEST_PG_URL`.
2. Connect to the admin URL with `pgx.Connect`.
3. `DROP DATABASE IF EXISTS fileuploader_test WITH (FORCE)`.
4. `CREATE DATABASE fileuploader_test`.
5. Run migrations with `migrate.New("file://"+migrationsDir, testPGURL)`, `m.Up()`, and ignore `migrate.ErrNoChange`.
6. Run `m.Run()`.
7. Close the pool and drop the test database again.

Resolve `migrationsDir` from `runtime.Caller(0)` plus `../../../../migrations`.

If an env var is missing, fail with a message naming `make test-integration`.

- [ ] **Step 2: Write the Postgres lifecycle tests**

- `TestUnitOfWorkCommits`: `pg.Do` inserts a user; after return, the row exists.
- `TestUnitOfWorkRollsBack`: insert a user, return an error; the row does not exist.
- `TestNestedUnitOfWorkReusesTheTransaction`: outer `Do` inserts a user, calls inner `Do`, and the inner inserts a folder; both rows commit.
- `TestNestedUnitOfWorkErrorRollsBackTheOuter`: the inner returns an error, the outer returns it, and neither row exists.
- `TestMigrationsApplied`: `schema_migrations` is non-empty and `users` exists.

- [ ] **Step 3: Write the file repository tests**

- `TestFileRepoScopesReadsToTheOwner`: two users; `Get`/`ListPage` with the wrong user returns `ErrFileNotFound` or no rows.
- `TestFilePageIsStableAcrossTies`: seed files with the same lower-case name in different folders of one owner, page the whole drive with limit 1, follow the cursor, and assert no duplicates or omissions. A same-folder duplicate is already rejected by the unique index.
- `TestFileSearchEscapesWildcards`: names `100%_x`, `100ax`, `100_x`; search `100%` returns only the literal.
- `TestFileRepoEnforcesUniqueNamePerFolder`: insert a duplicate lower-case name; assert the repository maps the unique violation to `ErrEntryAlreadyExists`.
- `TestDeleteByFolderSubtreeReturnsEveryFile`: seed a subtree; assert all files are returned.

- [ ] **Step 4: Write the folder repository tests**

- `TestFolderBreadcrumbIsRootFirst`: nested folders; assert root, middle, leaf order.
- `TestFolderPathsResolvesManyFolders`: request two folder IDs; assert each map entry and root-first order.
- `TestFolderPathsOmitsAnotherOwnersFolder`: assert the other owner’s ID is absent.
- `TestFolderPageScopesToParentAndSearch`: assert rows from another parent or owner are absent.
- `TestFolderLocksRejectUnknownFolders`: `Lock` and `LockSubtree` return `ErrFolderNotFound`.
- `TestFolderDeleteCascadesFiles`: delete a folder with a file; assert the file row is gone.

- [ ] **Step 5: Write the share repository tests**

- `TestShareStoreAndGetRoundTrip`: insert, get by token, assert all fields.
- `TestActiveShareFilter`: an expired share is absent from `GetActiveByUserAndFolder` and `ListByUser`, but present in `GetByUserAndFolder`.
- `TestShareUpdateAndDeleteScopeToOwner`: wrong owner returns `ErrShareNotFound`; correct owner succeeds.
- `TestShareListIsNewestFirst`: insert two shares; assert order.

- [ ] **Step 6: Write the user repository tests**

- `TestUserListFiltersEscapedSearch`: create users with a unique per-test prefix and names/emails containing `%` and `_`; assert literal matching. Never assert over the whole users table; another parallel test may own other rows.
- `TestUserListOrdersAndPages`: create a unique set of users for this test and filter by its prefix; assert every supported sort, direction, limit and page; the identifier tie-breaker keeps the page stable.
- `TestUserListTotalCountsTheWholeFilter`: assert `Total` counts the filtered prefix, independent of the page.
- `TestUserUniqueConstraints`: duplicate email and username map to the sentinels.

- [ ] **Step 7: Add the integration CI job**

Append this job to `.github/workflows/ci.yml`:

```yaml
  integration:
    name: runner / integration tests
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version: '1.27'
      - name: Start Postgres and MinIO
        run: docker compose up -d db minio
      - name: Wait for Postgres
        run: docker compose exec -T db pg_isready -U user
      - name: Run integration tests
        run: make test-integration
      - name: Stop services
        if: always()
        run: docker compose down -v
```

Add `.github/workflows/ci.yml` to this task's file list.

- [ ] **Step 8: Run the integration tests**

Run: `make test-integration`

Expected: PASS with the Compose stack up. If Docker is unavailable, the failure message must say which `TEST_*` variable or service is missing.

- [ ] **Step 9: Commit**

```bash
git add internal/repo/persistent/integration Makefile .github/workflows/ci.yml
git commit -m "test(repo): add Postgres integration coverage"
```

---

### Task 12: MinIO Integration Tests

**Files:**
- Create: `internal/repo/persistent/integration/minio_test.go`
- Modify: `internal/repo/persistent/integration/main_test.go`

**Interfaces:**
- Consumes: `minio.New`, `ObjectStorage` methods, `multipart.Part`.
- Produces: `func testStorage(t *testing.T) *minio.MinIO` and `func testBucket() string`.

- [ ] **Step 1: Start MinIO in `TestMain`**

Read:

```text
TEST_MINIO_ENDPOINT
TEST_MINIO_ACCESS_KEY
TEST_MINIO_SECRET_KEY
TEST_MINIO_USE_SSL
```

Create a client with `minio.New`, call `EnsureBucket` for `fileuploader-test-<run-id>`, and store the bucket name in a package-level test variable. After `m.Run`, remove the test bucket and close the client. The bucket is unique to the run, so cleanup cannot touch a developer bucket.

- [ ] **Step 2: Write the object tests**

- `TestObjectStorageRoundTrip`: `PutObject` a small body; `GetObject`; assert the bytes and close the reader.
- `TestPresignedGetReturnsTheObject`: generate a presigned URL; `http.Get` it; assert 200 and the bytes.
- `TestCopyAndRemoveObject`: copy, assert both objects exist, remove the copy, assert only the source remains.
- `TestObjectStorageRejectsAMissingObject`: call `GetObject` on an unknown key, then read the stream; assert the read or the open returns an error. MinIO may defer the missing-key error until the first read, so do not assert on the open call alone.

- [ ] **Step 3: Write the multipart tests**

- `TestMultipartUploadCompletes`: create, upload two 5 MiB parts plus a small final part, complete with sorted part numbers, get the object, assert the concatenated bytes. MinIO rejects a non-final part below 5 MiB.
- `TestMultipartAbortLeavesNoObject`: create, upload one part, abort, then attempt to read the key and assert it fails or returns no bytes.
- `TestListIncompleteUploadsReportsAnOpenUpload`: create an upload without completing; assert the key and upload ID appear; abort it in cleanup.

- [ ] **Step 4: Run the integration tests**

Run: `make test-integration`

Expected: PASS. The tests use unique object keys, so they can run in parallel.

- [ ] **Step 5: Commit**

```bash
git add internal/repo/persistent/integration/minio_test.go internal/repo/persistent/integration/main_test.go
git commit -m "test(storage): add MinIO integration coverage"
```

---

### Task 13: Cursor Fuzz And Final Verification

**Files:**
- Create: `internal/entity/fuzz_test.go`
- Modify: `README.md` if the fuzz command is not documented yet.

**Interfaces:**
- Consumes: `DecodeEntryCursor`, `EntryCursor.Encode`.
- Produces: `FuzzDecodeEntryCursor`.

- [ ] **Step 1: Write the fuzz test**

```go
func FuzzDecodeEntryCursor(f *testing.F) {
    f.Add("")
    f.Add("not-base64")
    f.Add(EntryCursor{
        Kind: EntryKindFile,
        Name: "notes.txt",
        ID:   "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44",
    }.Encode())

    f.Fuzz(func(t *testing.T, encoded string) {
        cursor, err := DecodeEntryCursor(encoded)
        if err != nil {
            return
        }

        if cursor == nil {
            return
        }

        if cursor.Kind != EntryKindFolder && cursor.Kind != EntryKindFile {
            t.Fatalf("accepted kind %q", cursor.Kind)
        }

        if cursor.Name == "" || cursor.ID == "" {
            t.Fatalf("accepted empty field: %+v", cursor)
        }
    })
}
```

- [ ] **Step 2: Run the fuzz target**

Run: `make test-fuzz`

Expected: no crash and no failing input during the ten-second run. If a crash is found, minimize it with `go test -run=^$ -fuzz=FuzzDecodeEntryCursor ./internal/entity`, add the minimized value to `testdata/fuzz/FuzzDecodeEntryCursor/`, fix the decoder, and rerun.

- [ ] **Step 3: Run the complete verification**

Run each command and record the result:

```sh
make format
make linter-golangci
make test
make test-race
make test-shuffle
make test-integration
make test-fuzz
```

Expected:

- `make format` changes nothing after it finishes.
- `make linter-golangci` reports no issues.
- both test commands pass.
- integration tests pass with the stack up.
- fuzz target reports no failure.

- [ ] **Step 4: Update the testing notes**

In `README.md`, state:

- unit and HTTP tests need no Docker;
- integration tests use `fileuploader_test` and a dedicated MinIO bucket;
- `make test-integration` starts the Compose services;
- `make test-fuzz` runs the cursor fuzzer.

- [ ] **Step 5: Commit**

```bash
git add internal/entity/fuzz_test.go README.md
git commit -m "test(entity): fuzz the cursor decoder"
```

---

## Self-Review

**Spec coverage:** The plan covers every Required Security Matrix item in Tasks 2–8, the integration design in Tasks 11–12, CI and commands in Task 1, and the flake/quality cleanup in Tasks 9–10. Fuzz is Task 13.

**Step scan:** Every test task names the test and its assertions. Every infrastructure task names the command and expected result. No step says "handle edge cases" or "add tests for the above."

**Type consistency:** The fakes use the existing `repo.*` and `usecase.*` interfaces. The integration helpers use `*postgres.Postgres`, `*minio.MinIO`, `entity.*` and `multipart.Part`, matching the production signatures.

**Review Focus:** Each of the five items has an owning task: share subtree in Task 2/11, expired share in Task 2/8, guards in Task 3, admin revocation in Task 5, keyset stability in Task 11.
