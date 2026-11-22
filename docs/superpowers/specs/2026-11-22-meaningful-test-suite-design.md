# Meaningful Test Suite Design

## Intent

The suite has 280 test functions and passes `-race` and shuffled runs, but its
coverage is uneven. Cross-package statement coverage is 43.2%. Public share
access, the auth guards, most admin mutations, and most file and folder actions
are untested. Several tests assert generated SQL strings or template classes
instead of behavior, so they can pass while the application is broken.

This design changes the goal from "more tests" to "tests that fail for the
right reason." It covers the security boundaries, the mutation handlers, the
persistence layer, and the test-runner setup. It also removes or rewrites tests
that create false confidence.

## Goals

- Test every public share decision: expiry, revocation, subtree escape, owner
  scoping and file access.
- Test `RequireAuth` and `RequireGuest`, not only `RequireAdmin`.
- Test the login policy: wrong password, unknown email, pending account,
  disabled account, session-store failure, `Authenticate` and `Logout`.
- Test admin mutations: account edit, limits, status, role change and access
  request approval.
- Test file and folder actions through the HTTP layer.
- Test share management and the public share file download.
- Make fakes assert the tenant and folder identifiers passed to repositories.
- Add real Postgres integration tests for repository behavior.
- Add real MinIO integration tests for object and multipart behavior.
- Add a CI test gate with the race detector.
- Replace wall-clock timing assertions with deterministic checks.
- Add a fuzz target for cursor decoding.

## Non-Goals

- Chasing 100% statement coverage.
- Covering generated `*_templ.go` files.
- Browser automation with Playwright, chromedp or a JavaScript test runner.
- Replacing the existing use-case/repository architecture.
- Testing the REST transport that is not part of this repository today.
- Adding testcontainers-go. Integration tests use the existing Compose
  services through `TEST_*` environment variables.
- Changing product behavior. Production changes are allowed only where a
  testability seam is required, and each must be justified in its task.

## Constraints

- Go 1.27 and the standard `testing` package. Do not add a test framework or
  assertion library.
- Keep hand-written fakes. Do not introduce gomock or mockery.
- `t.Parallel()` is required by `paralleltest` unless the test mutates process
  environment, starts a shared server, or runs a fuzz corpus setup.
- New tests must pass `make format` and `make linter-golangci`.
- Integration tests use the `integration` build tag and do not run during
  `go test ./...`.
- Integration tests use the Compose `db` and `minio` services. They must not
  touch the development database.
- The linter rules in `.golangci.yml` apply, including `funlen`, `gocyclo`,
  `gocognit`, `mnd`, `err113`, `wsl_v5`, `nlreturn`, `dupl` and
  `gochecknoglobals`.
- Tests must not depend on wall-clock upper bounds. A test may assert a lower
  bound or a fake clock, never "this must finish in under N milliseconds."
- No new production package-level state.

## Decisions Made Here

- **Share security comes first.** The public share path is the highest-risk
  untested area and the first implementation task after the CI gate.
- **HTTP tests use `app.Test`.** They exercise routing, middleware, handlers,
  validation and rendering together. Direct handler calls are reserved for
  helpers such as error mapping.
- **Controller test files are split by domain.** New tests go into
  `admin_mutation_test.go`, `file_action_test.go`, `folder_action_test.go` and
  `share_manage_test.go` instead of growing the existing mixed files.
- **Fakes record arguments.** A fake that ignores `userID` or `folderID`
  cannot prove tenant scoping. Add fields such as `lastUserID`,
  `lastFolderID`, `lastFileID`, and assert them.
- **Integration tests use one Compose stack.** `make test-integration` starts
  `db` and `minio`, creates a dedicated `fileuploader_test` database, applies
  migrations, and runs the tagged tests.
- **Integration tests use unique identifiers.** They do not truncate shared
  tables between tests. Each test creates its own users, folders, files and
  buckets or keys, so tests can run in parallel.
- **MinIO tests use a dedicated bucket.** The bucket name is
  `fileuploader-test-<run-id>` and is created in `TestMain`. Object keys are
  unique per test.
- **The slow five-second stream test is replaced.** The server writes with no
  deadline by default; that default is asserted directly. A short configurable
  timeout test may prove the option is wired, but it must not sleep for five
  seconds.
- **The latency upper-bound tests are removed.** Keep the deterministic
  `randomDelay` range test and add a test that a non-positive maximum returns
  zero. Do not measure scheduler time.

## Test Layers

| Layer | Package | Purpose |
| --- | --- | --- |
| Entity | `internal/entity` | Pure validation, cursor codec, name and search rules |
| Use case | `internal/usecase/*` | Rules, ownership, quota, cleanup and error mapping |
| Controller | `internal/controller/web` | Routes, middleware, forms, status codes and htmx headers |
| Repository integration | `internal/repo/persistent/integration` | Real SQL, constraints, transactions and keyset paging |
| Storage integration | `internal/repo/persistent/integration` | Real S3 object and multipart behavior |
| Fuzz | `internal/entity` | Cursor decoder invariants |

## Required Security Matrix

### Share use case

`internal/usecase/share/share.go`:

| Case | Expected |
| --- | --- |
| `Create` on an owned folder | stores a share |
| `Create` on an unknown folder | `ErrFolderNotFound` |
| `Create` with an active share for the folder | reuses the row and updates expiry |
| `Create` with an expired share for the folder | revives the row |
| `Create` when `UpdateExpiry` fails | wrapped error, no new row |
| `Get` on a missing token | `ErrShareNotFound` |
| `Get` on an expired token | `ErrShareExpired` |
| `Get` with nil expiry | returns the share |
| `Get` exactly at expiry | `ErrShareExpired` |
| `ResolveFolder` with empty folder ID | uses the share root |
| `ResolveFolder` on the share root | allowed |
| `ResolveFolder` on a descendant | allowed |
| `ResolveFolder` on a sibling or ancestor | `ErrFolderNotFound` |
| `ResolveFolder` with another owner’s folder | `ErrFolderNotFound` |
| `ResolveFile` inside the subtree | allowed |
| `ResolveFile` outside the subtree | `ErrFileNotFound` |
| `Delete` with the wrong owner | repository error surfaced |
| `UpdateExpiry` with the wrong owner | `ErrShareNotFound` |

The fakes must record the `userID` passed to `folders.Get`, `files.Get`,
`folders.Breadcrumb`, `shares.Delete` and `shares.UpdateExpiry`. A test that
only checks the returned error does not prove the scope.

### Auth guards

`internal/controller/web/middleware/guard.go`:

| Guard | Request | Expected |
| --- | --- | --- |
| `RequireAuth` | no cookie | redirect to `/login` |
| `RequireAuth` | unknown session | redirect to `/login` |
| `RequireAuth` | valid session | 200, locals set |
| `RequireAuth` | htmx request without session | `HX-Location` to `/login` |
| `RequireGuest` | no cookie | 200 |
| `RequireGuest` | unknown session | 200 |
| `RequireGuest` | valid session | redirect to `/` |
| `RequireGuest` | htmx request with session | `HX-Location` to `/` |

### Login policy

`internal/usecase/user/user.go`:

| Case | Expected |
| --- | --- |
| wrong password | `ErrInvalidCredentials` |
| unknown email | `ErrInvalidCredentials` |
| pending account | `ErrUserPending` |
| disabled account | `ErrUserDisabled` |
| active account | session stored with user ID and role |
| session store failure | wrapped error, no session returned |
| `Authenticate` missing session | repository error preserved |
| `Logout` missing session | repository error preserved |

### Admin mutations

`internal/controller/web/admin.go`:

| Handler | Must test |
| --- | --- |
| `adminUsersTable` | filters reach `ListUsers`; malformed query is 422; list error is 500; `HX-Push-Url` set only when the URL changes |
| `adminUsersEdit` | bad ID is 422; unknown user is 404; success renders the form |
| `adminUsersEditSubmit` | validation is 422; bad limits are 422; success calls `UpdateUser`, `SetUserLimits`, `SetStatus`; `HX-Trigger: usersUpdated` |
| `validateUserLimits` | suffix parse failure and negative values are 422, no write |
| `roleSubmit` | invalid role is 422; unknown email is 404; success sets `HX-Trigger` |
| `adminAccessRequestsTable` | forces `Status=pending`, newest first |
| `adminAccessRequestApprove` | bad ID is 400; success is 204 with `HX-Trigger: accessRequestsUpdated` |

### File and folder actions

File handlers must cover:

- `fileNew` and `fileDrawer` render the expected fragment.
- `fileRename` calls `Rename` with the submitted name and refreshes the file’s
  folder.
- `fileMove` and `fileDuplicate` call `Get` first, then the mutation, then
  refresh the source folder.
- `fileDelete` refreshes the file’s folder and treats `ErrStorageCleanup` as a
  logged success, not a 500.
- Invalid IDs return 404 or redirect as the handler specifies.

Folder handlers must cover:

- `folderNew`, `folderEdit`, `folderMoveForm`, `folderDuplicateForm` and
  `folderTree` render.
- `folderCreate` calls `Create` and refreshes the parent.
- `folderMove` and `folderDuplicate` refresh the source parent.
- `folderDelete` redirects to the parent with the active search, or Home when
  the parent is gone.
- `folderDownloadZip` sets the zip headers and streams the body.
- Root-folder actions are refused.

### Share management and public access

- `shareIndex` lists the account’s shares.
- `shareForm` and `shareDrawer` render.
- `shareCreate` validates the folder, parses the expiry and redirects to
  `/share`.
- Invalid or malformed expiry is 422.
- `shareUpdateTime` and `shareDelete` are owner-scoped and refresh the rows.
- `sharedRoot` opens the share root; an owner opening their own share is sent
  to the matching drive folder.
- `sharedFileDownload` streams only a file inside the share subtree.
- Expired, revoked and unknown tokens return the share error page.
- A visitor’s session is not used as the owner identifier.

## Integration Test Design

### Database

- `internal/repo/persistent/integration/main_test.go` starts the shared
  Postgres connection.
- It creates `fileuploader_test` from `TEST_PG_ADMIN_URL` and drops it first
  with `WITH (FORCE)` so a stale run cannot poison the next one.
- It applies `migrations/*.up.sql` with `golang-migrate`’s library API.
- It closes the pool and drops the database in `TestMain` when the run ends.
- Tests use unique user, folder, file and share UUIDs. No global truncation.

### Object storage

- The same integration package starts a `pkg/minio` client from
  `TEST_MINIO_ENDPOINT`, `TEST_MINIO_ACCESS_KEY`, `TEST_MINIO_SECRET_KEY`,
  `TEST_MINIO_USE_SSL`.
- `TestMain` creates a unique bucket. Tests share the bucket but use unique
  object keys.
- Tests cover put, get, presigned get, copy, remove, multipart create, upload
  part, complete, abort and incomplete-upload listing.

### Commands

```sh
make test                 # go test ./...
make test-race            # go test -race ./...
make test-shuffle         # go test -shuffle=on -count=10 ./...
make test-cover           # coverage profile and function summary
make test-integration     # docker compose up -d db minio + tagged tests
make test-fuzz            # go test -fuzz=FuzzDecodeEntryCursor -fuzztime=10s ./internal/entity
```

`make test-integration` exports:

```sh
TEST_PG_ADMIN_URL=postgres://user:password@localhost:5433/postgres?sslmode=disable
TEST_PG_URL=postgres://user:password@localhost:5433/fileuploader_test?sslmode=disable
TEST_MINIO_ENDPOINT=localhost:9000
TEST_MINIO_ACCESS_KEY=minioadmin
TEST_MINIO_SECRET_KEY=P@55w0rd
TEST_MINIO_USE_SSL=false
```

## CI

Add a `test` job to `.github/workflows/ci.yml`:

- checkout and Go 1.27
- `go test -race ./...`
- `go test -shuffle=on -count=5 ./...`

Add an `integration` job:

- checkout and Go 1.27
- `docker compose up -d db minio`
- wait for Postgres with `docker compose exec -T db pg_isready -U user`
- `make test-integration`
- `docker compose down -v`

The integration job may be a separate workflow so a Docker outage does not
block the unit-test gate.

## Acceptance Criteria

- New tests fail if the corresponding guard, handler or use case is reverted.
- `go test -race ./...` passes.
- `go test -shuffle=on -count=10 ./...` passes.
- `make test-integration` passes with the Compose stack up.
- `go test -fuzz=FuzzDecodeEntryCursor -fuzztime=10s ./internal/entity` does
  not crash.
- `make format` and `make linter-golangci` pass.
- The share use case has at least one test per decision in the security
  matrix.
- `RequireAuth` and `RequireGuest` each have success, failure and htmx tests.
- Every admin mutation handler listed above has a success and a rejection
  test.
- No test asserts a wall-clock upper bound.
- No new test depends on the developer’s `.env` values.

## Rollout

The tests are additive. The only production changes are testability seams for
the latency and server-timeout tests, if the implementer keeps those tests.
Integration tests require Docker and are opt-in. The CI test job can land
before the integration job so the unit gate starts protecting the branch
immediately.

## Open Questions

None. The scope is deliberately limited to trust in the existing behavior, not
new product features.
