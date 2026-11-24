# File Uploader — The Odin Project

Server-rendered personal drive. Fiber serves HTML, templ renders it, htmx swaps fragments, and
Alpine drives the upload drawer. Postgres holds metadata, Redis holds sessions and transfer
counters, and object bytes live in a private S3-compatible bucket. Module path:
`github.com/minhhoccode111/fileuploader`. The Go version is declared in `go.mod`.

There is one transport: `internal/controller/web`. The `usecase` layer holds the rules and the
`repo` layer holds persistence. Pages and partials are HTML; the chunked-upload driver is the one
JSON corner (a part plan at start, status codes for the part and abort calls). None of it is a
public JSON API.

## Commands — drive everything through the Makefile

The `Makefile` loads `.env` (falling back to `.env.example`) and exports it into every target, and
several targets carry flags that matter. Running the underlying tool directly means running it with
different settings than CI. Use the target, not the tool.

| Task                                       | Use this                                | Not this                                                                                 |
| ------------------------------------------ | --------------------------------------- | ---------------------------------------------------------------------------------------- |
| Install tool binaries (`migrate`, linters) | `make bin-deps`                         | `go install ...`                                                                         |
| Lint                                       | `make linter-golangci`                  | `golangci-lint run`                                                                      |
| Format                                     | `make format`                           | `gofmt` — the target runs `go fix`, `gofumpt`, and `gci` with the repo's import grouping |
| Tidy / verify modules                      | `make deps`                             | `go mod tidy`                                                                            |
| Vulnerability scan                         | `make deps-audit`                       | `govulncheck ./...`                                                                      |
| Start Postgres                             | `make compose-up`                       | `docker compose up`                                                                      |
| Start the whole stack including the app    | `make compose-up-all`                   | `docker compose up`                                                                      |
| Tear down                                  | `make compose-down`                     | `docker compose down`                                                                    |
| Run the app locally                        | `make run`                              | `go run ./cmd/app` — the target builds with `-tags migrate`                              |
| Run the dev stack (hot reload)             | `make dev`                              | running air + tailwind by hand                                                           |
| Regenerate templ                           | `make templ-generate`                   | `templ generate`                                                                         |
| Build CSS                                  | `make css-build`                        | invoking the tailwind binary by hand                                                     |
| Download frontend assets                   | `make assets`                           | `curl`-ing htmx/alpine/daisyUI by hand                                                   |
| Create a migration                         | `make migrate-create <name>`            | `migrate create ...`                                                                     |
| Apply the next / all migrations            | `make migrate-up`, `migrate-up-all`     | `migrate -path ... up 1` / `up`                                                          |
| Revert the last / all migrations           | `make migrate-down`, `migrate-down-all` | `migrate -path ... down 1` / `down -all`                                                 |
| Redo the last migration                    | `make migrate-redo`                     | `migrate ... down 1 && ... up 1`                                                         |
| Inspect migrations                         | `make migrate-status`, `migrate-list`   | `migrate ... version` / `ls migrations`                                                  |
| Dump the current schema                    | `make schema`                           | `atlas schema inspect` by hand                                                           |
| Full check before pushing                  | `make pre-commit`                       | running the steps by hand                                                                |

`make help` lists every target.

Never claim a change is done without `make format` and `make linter-golangci` passing.

## Dependency rule

```text
cmd/app
  └─ internal/app                 wiring and lifecycle
       ├─ internal/controller/web
       │    └─ internal/usecase
       │         └─ internal/repo
       └─ pkg                     infrastructure with no domain knowledge
```

- `internal/entity` — domain types and sentinel errors. Imports nothing from this module. Every other
  layer may import it.
- `internal/usecase/contracts.go` — the interfaces controllers call. `internal/usecase/<domain>/`
  implements them.
- `internal/repo/contracts.go` — the interfaces use cases call. `internal/repo/persistent/<domain>/`
  (Postgres), `internal/repo/session/` (Redis), `internal/repo/transfer/` (Redis rate buckets) and
  `internal/repo/upload/` (Redis multipart sessions) implement them.
- `internal/controller/web/` — the Fiber transport: handlers, middleware, and its own `request/` and
  `response/` DTOs.
- `pkg/` — transport-agnostic infrastructure (`httpserver`, `logger`, `minio`, `multipart`,
  `postgres`, `redis`, `validatorx`). Must not import `internal/`.

Inner layers never import outer ones. Wiring happens exactly once, in `internal/app/app.go`
(`initUseCases`, `initServers`).

## Where code goes

Decide by asking what the code knows about:

| The code knows about…                                         | It belongs in                                   | Shape                                              |
| ------------------------------------------------------------- | ----------------------------------------------- | -------------------------------------------------- |
| nothing but the domain                                        | `internal/entity/<name>.go`                     | struct + methods (`User.IsValid`, …)               |
| a domain rule that spans repositories                         | `internal/usecase/<domain>/<domain>.go`         | method on `UseCase`                                |
| SQL, a table, a driver error code                             | `internal/repo/persistent/<domain>/<domain>.go` | method on `Repo`                                   |
| a Redis session, rate bucket or multipart session             | `internal/repo/<session\|transfer\|upload>/`     | method on the repo type                            |
| an HTTP status or a message envelope                          | `internal/controller/web/<domain>.go`           | handler                                            |
| a browser page or fragment                                    | `web/templates/<name>.templ`                    | templ component                                    |
| a server, pool, client or middleware with no domain knowledge | `pkg/<name>/`                                   | reusable package                                   |
| a transaction boundary over several repositories              | `internal/repo/contracts.go` + `pkg/postgres`   | `UnitOfWork.Do`                                    |

Layout rules:

- One file per domain per layer, named after the domain (`user.go`).
- Package name matches the directory name.
- Constructors are always `New`, return the _interface_ from `contracts.go`, and take their
  dependencies as arguments. No package-level state — `gochecknoglobals` and `gochecknoinits` are on.
- Unexported package constants are prefixed with an underscore (`_defaultEntityCap`). `mnd` rejects
  bare numeric literals, so add a constant rather than inlining one.

## Frontend

`web/` holds everything the browser gets. It is plain static output — no Node at runtime.

- `web/templates/*.templ` — pages, compiled to Go by templ. `*_templ.go` is generated and
  **committed**; run `make templ-generate` after every `.templ` edit or the build serves stale HTML.
- `web/app.css` — the Tailwind input. `@plugin "./plugins/daisyui.mjs"` pulls in daisyUI.
- `web/static/css/app.css` — the Tailwind output. **Committed**, so the Docker image needs no Node.
  Never hand-edit it: change classes or `web/app.css`, then `make css-build`.
- `web/plugins/daisyui.mjs` and `web/static/js/{htmx,alpine}.min.js` — pinned vendor files,
  fetched by `make assets`. All versions live in the `Makefile` (and `tool` in `go.mod`); bump them
  there, never in the file.
- `web/static/js/app.js` — hand-written browser code (upload driver, drawers). It is not a vendor
  file: edit it directly.

The app serves `web/static` at `/static` (`app.Static`) and renders `web/templates` through the web
controller.

Pages take a `templates.View` (`web/templates/view.go`) that carries the per-request state: whether
the visitor is signed in and whether they hold the admin role. The web controller builds it from
the account the guard parked, so a template never asks who the visitor is. A role-gated link (the
items of the admin section in the sidebar) is only a hint — the route behind it carries its own
guard (`middleware.RequireAdmin`), so a direct request is refused too.

The admin area is a namespace: `/admin` holds no page of its own and sends the visitor to
`/admin/users`, the landing page of the area. The sidebar shows the area as a toggle, not a link:
it opens the links of the pages under it, and it starts open when the page on screen lives in the
area. Every redirect goes through `middleware.Redirect`, which answers an htmx request with
`HX-Location` and every other request with a 302, so both navigation styles land on the same page.

The account list on `/admin/users` loads itself: the page renders a skeleton, then htmx asks
`/htmx/admin/users` for the rows and swaps them in. The same element reloads the rows when a role
changes, because the role form answers with `HX-Trigger: usersUpdated`. The list is a read model
(`entity.UserListItem`), so the password hash never leaves the database. Every column title sorts
the list; the order rides in the URL as `sort` and `dir`, so a view stays bookmarkable. The first
click on an inactive column sorts it ascending, and a click on the active column turns the order
around. Access requests use the same shape with `accessRequestsUpdated`.

`make dev` runs air (rebuilds Go and templ on save) together with the Tailwind watcher. It needs
Postgres, Redis and MinIO up first: `make compose-up`. air watches `.go`, `.templ` and `.env`; CSS
changes are handled by the Tailwind watcher, so a class edit does not restart the server.

Alpine shorthands (`@click`, `:class`) pass through templ unchanged; use `x-on:` / `x-bind:` only if
you prefer the long form. To feed Go data into `x-data`, use a templ expression:
`<div x-data={ templ.JSONString(data) }>`. See `.opencode/skills/alpinejs/SKILL.md`.

`middleware.Latency` (`internal/controller/web/middleware/latency.go`) sleeps before every request
for a random duration between `HTTP_FAKE_LATENCY_MIN` and `HTTP_FAKE_LATENCY_MAX` (100ms and 200ms
by default), so the pages can be checked against a slow network. A non-positive max turns the delay
off. The liveness probe at `/healthz` waits for it too, so keep the range small in production.

## The handler contract

Every web handler does the same six things in the same order. Copy the shape from a neighbouring
handler.

1. **Get the caller.** The auth guard parks the session it resolved in `ctx.Locals("session")` and
   the web controller reads the user ID from it with `c.userID(ctx)`. A missing session is a
   redirect to `/login`, not a 500.
2. **Decode into a DTO** from `internal/controller/web/request/` (`ctx.BodyParser` or
   `ctx.Query`). Never decode into an `entity` type, and never pass a `request.*` type into a use
   case.
3. **Validate the DTO** with the injected `*validator.Validate` (`c.v.Struct(req)`), after calling
   its `Sanitize` when it has one. Structural validation (required, min, max, oneof) lives here.
   Domain validation — anything that needs to know the rules, like a status transition or a name
   clash — lives on the entity or the use case and must not be duplicated in the controller.
4. **Call the use case** with the request context: `ctx.UserContext()`. `context.Context` is the
   first parameter of every method that crosses a layer.
5. **Map the error.** `errors.Is` against the `entity.Err*` sentinels through the domain's error
   helper (`driveError`, `accountError`, `userUpdateError`, `shareError`) → an HTTP status and a
   message the visitor may see. Log unexpected errors; never leak them.
6. **Render the response.** A partial route returns a fragment with `c.renderPage`. A page route
   returns the full page for a normal request and the fragment plus a title update for an htmx
   partial with `c.render`. Mutations set `HX-Trigger` when another element must reload, or redirect
   through `middleware.Redirect`. The upload endpoints are the exception: `uploadStart` answers the
   JSON part plan, `uploadPart` and `uploadAbort` answer `204`, and `uploadFailure` answers plain
   text the upload driver shows in its error box.

## Conventions that will bite you

**Error style.** Domain failures are sentinel errors in `internal/entity/errors.go`
(`ErrUserNotFound`, `ErrFileNotFound`, `ErrRootFolder`). Every other error is wrapped with the
layer path: `fmt.Errorf("FolderUseCase - Create - uc.repo.Store: %w", err)`. The `err113` linter
rejects `errors.New`/`fmt.Errorf` for new dynamic errors — add a sentinel to `entity` instead. The
controller is the only place that translates a sentinel into an HTTP status.

**Config is environment-only.** `config/config.go` parses env vars with `caarlos0/env`. A new
setting needs the struct field, an entry in `.env.example`, and — if the container needs it — the
`environment` block in `docker-compose.yml`. Fields tagged `required` make the app fail at startup.

**The linter is strict and non-obvious.** `.golangci.yml` enables ~45 linters. The ones that
reject otherwise-fine code: `wsl_v5` and `nlreturn` (blank line required before `return` and
around blocks), `funlen` (65 lines / 40 statements), `gocyclo` (10), `gocognit` (15), `mnd`,
`gochecknoglobals`, `exhaustive` (switches over enum types need every case or a `default`), `dupl`
(threshold 100 — copy-pasting a handler trips it).

**htmx is a contract.** A handler that mutates state answers a fragment or a redirect, never a
bare 200 with no body unless htmx expects none. Events another element listens to are set with
`HX-Trigger`; navigation goes through `middleware.Redirect` so htmx and a plain browser both land on
the same page.

**CSRF is a contract.** `middleware.CSRF` gives every visitor a `csrf_token` cookie and refuses a
POST, PUT, PATCH or DELETE whose `X-CSRF-Token` header does not match it with a 403. htmx adds the
header on every request through the `htmx:config:request` listener in `web/static/js/app.js`. A new
`fetch` or `XMLHttpRequest` call must add it by hand, and `navigator.sendBeacon` cannot send a
header at all, so browser code that must outlive the page uses a keepalive fetch instead. An unsafe
request whose `Origin` names another host (including another port) is refused even with a matching
token, so a cookie planted by a related subdomain cannot pass.

**Login is throttled.** `repo.LoginGuard` counts sign-in attempts in Redis: every attempt takes one
slot from the email bucket and the client bucket, five per email address and thirty per client
address in fifteen minutes. Once either bucket is full the password is not checked. The email
bucket clears on a successful sign-in; the client bucket does not, so one working account cannot
reset the budget of the address behind it. `clientIP` trusts `X-Real-IP` because nginx sets it, and
falls back to the connection address.

## Observability

Every request gets an identifier. `middleware.RequestID` keeps an inbound `X-Request-ID` only when it
is 8–64 characters of `[A-Za-z0-9._-]`, otherwise it generates a UUID, and writes the value on the
response. nginx sets it from `$request_id`, so the proxy log and the app log name the same request.
The access line carries the identifier, method, path, status, size and latency. A handler logs
through `c.requestLogger(ctx)`, so its error line carries the same identifier. `/healthz` and
`/static/` are counted in metrics but not logged.

`pkg/metrics` owns an explicit Prometheus registry: request count, duration histogram, response
bytes, in-flight requests, build info, and Postgres pool states. Labels are route templates
(`/folders/:folder`), never raw paths or account data; a request that matched no route is
`route="unmatched"`.

Metrics and `net/http/pprof` share one private listener, `OBSERVE_ADDR`, `127.0.0.1:9090` by
default. It is off when `OBSERVE_ENABLED=false` or when `HTTP_USE_PREFORK_MODE=true`: prefork starts
one process per worker and every copy would race for the port. The listener bypasses Fiber, so it
also bypasses the fake latency, CSRF and the body limit. Never publish it on `0.0.0.0`; a heap
profile can contain secrets. In Docker the app binds `0.0.0.0:9090` inside the container and compose
publishes `127.0.0.1:9090:9090`, so the host reaches it and the public listener does not. The other
containers on `app_network` can reach it too; they are the app's own services.

## Testing

`make test` runs the unit and controller tests. They use fakes for repositories and `miniredis` for
Redis, so they need no Docker. `make test-race` adds the race detector, `make test-shuffle` runs the
suite shuffled ten times, and `make test-cover` writes `coverage.out` and prints the function
summary.

`make test-integration` needs Docker: it starts the Compose `db` and `minio` services, recreates the
`fileuploader_test` database, applies the migrations, and uses a fresh `fileuploader-test-*` bucket
that it removes afterwards. Those tests live behind the `integration` build tag in
`internal/repo/persistent/integration/`, so a plain `go test ./...` skips them.

`make test-fuzz` fuzzes the folder-page cursor decoder in `internal/entity` for ten seconds.

CI (`.github/workflows/ci.yml`) runs the linters, `go test -race ./...`, a shuffled run, and the
integration suite against the same Compose services.

## Adding a use case to an existing domain

1. Add the method to `internal/usecase/contracts.go`.
2. Implement it in `internal/usecase/<domain>/<domain>.go`.
3. If it needs new persistence, repeat the same steps in `internal/repo/contracts.go` and the
   matching `internal/repo/` implementation.
4. If it writes to more than one table, wrap the writes in the unit of work — see below.
5. Expose it on the web: handler in `internal/controller/web/<domain>.go`, route in `router.go`, and
   a templ fragment or page in `web/templates/` when the browser must show it.

## Adding a whole domain

Work outward from the entity. Every step has a counterpart in the existing `folder` and `share`
domains — read one before writing.

1. **Entity** — `internal/entity/<domain>.go` with the type and its invariants; new sentinel errors
   go in `internal/entity/errors.go`.
2. **Migration** — `make migrate-create create_<table>`, then fill both the `.up.sql` and the
   `.down.sql`. A missing down-migration is a review failure.
3. **Repo contract** — add the interface to `internal/repo/contracts.go`.
4. **Repo implementation** — `internal/repo/persistent/<domain>/<domain>.go` (`package <domain>`,
   Squirrel queries, driver errors mapped to sentinels) and a `New` returning the interface.
5. **Use-case contract** — add the interface to `internal/usecase/contracts.go`.
6. **Use-case implementation** — `internal/usecase/<domain>/<domain>.go`, same `New` pattern.
7. **Wiring** — add a field to `useCases` in `internal/app/app.go`, construct repo and use case in
   `initUseCases`, and thread the use case through `initServers` into the web router.
8. **Web** — DTOs in `internal/controller/web/request/` and `response/`, handlers in
   `internal/controller/web/<domain>.go`, registration in `router.go`, the use case added to the
   `Controller` struct, and the pages or fragments in `web/templates/`.

## Unit of Work

`repo.UnitOfWork` (`internal/repo/contracts.go`) is the transaction boundary a use case writes
through. `*postgres.Postgres` implements it (`pkg/postgres/tx.go`), so the unit of work _is_ the
connection pool — there is no second object to wire. `internal/app/app.go` passes `pg` as the unit
of work.

```go
err = uc.uow.Do(ctx, func(ctx context.Context) error {
    if err := uc.repo.Store(ctx, &user); err != nil {
        return fmt.Errorf("uc.repo.Store: %w", err)
    }

    return uc.folders.CreateRoot(ctx, rootFolder)
})
if err != nil {
    return fmt.Errorf("UserUseCase - Register - uc.uow.Do: %w", err)
}
```

Rules:

- The context handed to the callback carries the transaction. Repositories pick it up with
  `r.Querier(ctx)`, so a repository joins a transaction without a new interface method and without
  a second constructor.
- The transaction is committed when the callback returns nil and rolled back when it returns an
  error. Sentinel errors survive the wrapping, so `errors.Is` still reaches `entity.Err*`.
- `Do` on a context that already carries a transaction reuses it. Nested use cases therefore do not
  open a second transaction, which would deadlock on a pool with one connection.
- Only Postgres is transactional here. Redis sessions and MinIO blobs stay outside the unit of
  work; compensate for them in the use case if a failure must not leave them behind.

## Routing

All routes are registered in `internal/controller/web/router.go`. Page routes are `GET`; mutations
are `POST` under `/htmx/` and usually answer a fragment the page swaps in. The upload driver is the
exception: its start call answers JSON and its part and abort calls answer status codes. A guard is
attached per route, not to a group, so a new page must opt into `middleware.RequireAuth` or
`middleware.RequireAdmin` explicitly.

There is no versioned JSON transport. A JSON API would be a new transport package with its own
router, wired in `initServers` next to the web router; the use cases stay as they are.

## Database

`migrations/` holds golang-migrate pairs (`<timestamp>_<name>.up.sql` / `.down.sql`). Create with
`make migrate-create <name>`, apply with `make migrate-up`. The app applies them itself at startup
only when built with the `migrate` build tag (`internal/app/migrate.go`) — that is what `make run`
and the Dockerfile do. Queries are built with Squirrel via the embedded `*postgres.Postgres`; there
is no ORM and no raw string concatenation.

`make migrate-up` / `make migrate-down` run exactly one step and print `no change` when there is
nothing to do. golang-migrate's raw `up 1` / `down 1` demand _exactly_ one migration and fail with a
misleading `error: file does not exist` when none is left, so the Makefile wraps them — do not call
the subcommand bare. Never run a bare `migrate down`: with no argument it prompts to revert _all_
migrations.

`make schema` dumps the live schema to `docs/schema.sql` with Atlas
(`atlas schema inspect`). It reads the running database, so it needs one up; when none is
reachable it prints `schema: database not reachable, skipping` and exits 0, which keeps it
safe in scripts that may run without the stack. Regenerate the file after any migration
change and commit it — the diff is the schema change.

`users.role` holds `admin` or `user`, and the database enforces it with a CHECK constraint
(`users_role_check`). `users.status` holds `pending`, `active` or `disabled`; registration creates a
`pending` plain user, and only an admin moves it to `active`. The first admin is granted out of
band with `UPDATE users SET role = 'admin', status = 'active' WHERE email = '...';` — the admin page
cannot do it, because the page itself needs an admin.

The role travels with the session: Redis holds `{user_id, role}` under `session:<id>`, next to the
index of that account's live sessions under `user-sessions:<user_id>`. A guard therefore reads the
role from Redis and never touches the database. The price is that the role in a session is a
snapshot, so changing a role has to end the sessions that carry the old one: `SetRole` updates the
role and then calls `DeleteUserSessions`, and the account picks the new role up on its next sign-in.
Changing a password keeps the current session and ends the rest; disabling an account ends all of
them.

Object storage is S3-compatible, run as the `minio` service (`docker.io/pgsty/silo`, a maintained
MinIO fork — the upstream image is no longer published) on `:9000` (S3) and `:9001` (console).

The bucket name comes from `MINIO_BUCKET` (default `files`) and the app creates the bucket itself at
startup (`pkg/minio.EnsureBucket`), the same way it applies migrations — a fresh volume needs no
console step, and a second start is a no-op. Keep the bucket private: the app streams object bytes
through the per-account download limiter, so there is no public URL or public policy to hand out.
`MINIO_PUBLIC_ENDPOINT` is the host the browser would reach the bucket at if a presigned link were
used; it defaults to `MINIO_ENDPOINT`.

## Browsing and search

Folder pages load one page of 100 entries: folders first, then files, each by `lower(name)` and
`id`. The next page is a keyset cursor, not an offset. The `after` value is the last row of the
batch, encoded by `entity.EntryCursor`, and the "Load more" row carries it to the matching partial
route. The cursor lives in the link, never in the address bar, so a reload starts at the first batch
again. An unusable cursor is a 400 on the partial and never a silent restart.

`q` narrows a folder page to the direct children whose name holds the term, without changing the
breadcrumb or the folder path. `/search` is the own-drive search: it needs at least three characters
(a shorter substring cannot use `pg_trgm`), never includes shared content, and every result carries
the folder path that leads to it. Home is `/` and keeps that URL for its local search; it has no
folder identifier, so a global search started from Home has no `This folder` scope and clearing it
returns Home. Global results are read-only: a folder opens on its page, a file downloads, and a
file's location links to the folder that contains it — actions stay on the folder page.

Shared folder pages get the same page size and local `q`, but no global search: a share grants one
subtree, not a drive. The share token is resolved on every partial request, and the owner identifier
comes from the resolved share, never from the client.

`pg_trgm` is a deployment requirement. The folder-browse migration installs it
(`CREATE EXTENSION IF NOT EXISTS pg_trgm`) and the trigram indexes global search needs; a deployment
without it fails at migration time. Rolling the migration back drops only the indexes it added and
leaves the extension in place, because it may predate this change.

## Uploads

A file arrives in parts, not in one request. The browser asks for a session, sends each part, then
completes or aborts it:

| Step     | Endpoint                                |
| -------- | --------------------------------------- |
| start    | `POST /htmx/uploads`                    |
| part     | `PUT /htmx/uploads/:upload/parts/:part` |
| complete | `POST /htmx/uploads/:upload/complete`   |
| abort    | `POST /htmx/uploads/:upload/abort`      |

The defaults live in `config.Upload`: 10 GiB per file (`UPLOAD_MAX_BYTES`), 50 MiB parts
(`UPLOAD_PART_BYTES`), and a one-hour idle session (`UPLOAD_SESSION_IDLE_TTL`). The part size falls
back to the 5 MiB S3 floor and shrinks for a rate-limited account so each part still finishes well
inside the proxy and server timeouts. The body limit middleware lets the upload paths carry one part
plus `config.UploadOverheadBytes`; every other route stays on the general limit.

`StartUpload` creates the MinIO multipart upload and reserves the whole declared size against the
account quota in Redis, so two sessions cannot both claim the last headroom. Completion, abort and
the sweeper release that reservation. The session itself sits in Redis with the idle TTL, which each
accepted part slides forward. `UploadPart` paces the bytes through `repo.TransferLimiter` before they
reach the bucket and stores the returned ETag. `CompleteUpload` assembles the parts, writes the row
through `storeRow` (folder lock, name check, quota check, usage update) and removes the object again
if the row fails.

Downloads use the same limiter under a separate `download-rate:<user>` bucket. `OpenDownload` and
`DownloadZip` wrap the stream in `internal/usecase/pace`, so concurrent file, share and folder
downloads share one account budget. Keep streamed responses out of `Response().Body()` in middleware:
calling it buffers the whole file and delays every header until the last byte.

A background sweeper (`internal/app/sweeper.go`) runs one sweep at startup and then every five
minutes: it aborts multipart uploads whose session expired and never finished, which is the backstop
for a browser that never sends the abort beacon. It also asks the bucket for multipart uploads older
than 24 hours that no session claims and aborts them, so an upload whose Redis session was lost does
not hold its parts forever. MinIO rejects the S3 lifecycle action for incomplete multipart uploads,
so the app enforces the wait itself.
