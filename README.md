# File Uploader

A small personal drive built as my take on [The Odin Project's file uploader
project](https://www.theodinproject.com/lessons/nodejs-file-uploader). The
assignment asks for Express and Prisma; I built the same product in Go to
learn the server side of an upload flow properly.

The UI is server-rendered with templ and htmx. Postgres holds metadata, Redis
holds sessions and counters, and the bytes live in an S3-compatible bucket
(MinIO locally). There is no SPA and no Node at runtime.

It is a learning project, not a production service. It is meant to be easy to
run, read and extend.

## What it does

- **Accounts.** Registration is a request: anyone can fill the form, but the
  account stays `pending` until an administrator approves it. Disabling an
  account ends its live sessions immediately. A signed-in account can change
  its own username, email and password from `/account`; a password change keeps
  the current browser signed in and ends every other session.
- **Drive.** Nested folders and files per account: upload, download, rename,
  move, duplicate, delete. A folder downloads as a zip. The default ceiling is
  10 GiB per file.
- **Browse and search.** A folder page lists 100 entries at a time and loads
  the next batch with a keyset cursor. `q` filters the current folder; `/search`
  searches the whole drive and shows the path to each result.
- **Chunked uploads.** The browser sends one part at a time (50 MiB by
  default), with progress, retries and abort. The object is assembled in the
  bucket before the database row appears, so a failed upload cannot leave a
  half-file in the drive.
- **Sharing.** Read-only public links to folders, with an optional expiry, plus
  a page to list and revoke them.
- **Admin.** Search, filter, sort and page through accounts; edit an account;
  reset a password; approve access requests; set per-account storage quota and
  upload/download limits.
- **Limits.** Quota and transfer rates can be set per account, with global
  defaults from the environment.

## Stack

| Part                  | Choice                                  |
| --------------------- | --------------------------------------- |
| HTTP                  | Go 1.27 + Fiber                         |
| Pages                 | templ + htmx 4 + Alpine 3               |
| Styles                | Tailwind 4 + daisyUI 5                  |
| Data                  | Postgres 18 + Squirrel + golang-migrate |
| Sessions and counters | Redis 8                                 |
| File bytes            | MinIO (S3-compatible)                   |
| Dev loop              | air + Tailwind watcher                  |

## Run it

You need Go 1.27, Docker with Compose, and make, on Linux or macOS. The
Makefile downloads platform binaries for those systems; on Windows, use WSL.

```sh
cp .env.example .env
make compose-up     # Postgres, Redis and MinIO
make run            # http://localhost:8090
```

`make compose-up` blocks on the service logs. Press Ctrl-C to stop the log
view — the containers keep running — then start `make run`, or run it in a
second terminal. `make run` builds with the `migrate` tag, so startup applies
pending migrations; the app also creates the bucket on startup.

| Service            | Address                                       |
| ------------------ | --------------------------------------------- |
| App                | http://localhost:8090                         |
| Postgres           | `localhost:5433`                              |
| Redis              | `localhost:6379`                              |
| MinIO S3 / console | http://localhost:9000 / http://localhost:9001 |

If the app exits during startup, make sure Postgres, Redis and MinIO are up. If
you started it with `go run` instead of `make run`, the settings from `.env`
were not loaded; the Makefile is what exports them.

### The first account

Open `/register` and create an account. It will be pending. The first
administrator has to be promoted by hand:

```sh
docker compose -p fileuploader exec db psql -U user -d fileuploader -c \
  "UPDATE users SET role = 'admin', status = 'active' WHERE email = 'you@example.com';"
```

Sign in, then approve everyone else from **Admin → Access requests**. The admin
page cannot create the first admin, because the page itself needs an admin to
open.

### Everything in Docker

`make compose-up-all` builds the app image and starts it behind nginx at
http://app.lvh.me (the hostname resolves to `127.0.0.1`). That path takes its
settings from `docker-compose.yml`, not from `.env`. `make compose-down` stops
the stack; the database and bucket volumes stay.

## Configuration

`.env.example` is the full list. The ones that change behavior the most:

| Variable                              | Default                | What it does                                               |
| ------------------------------------- | ---------------------- | ---------------------------------------------------------- |
| `UPLOAD_MAX_BYTES`                    | `10737418240` (10 GiB) | Largest file accepted.                                     |
| `UPLOAD_PART_BYTES`                   | `52428800` (50 MiB)    | Part size. Must be between 5 and 50 MiB.                   |
| `UPLOAD_SESSION_IDLE_TTL`             | `1h`                   | How long an unfinished upload survives without a new part. |
| `UPLOAD_TIMEOUT`                      | `5m`                   | Read deadline for a part; `0` disables it.                 |
| `STORAGE_DEFAULT_QUOTA_BYTES`         | `0`                    | Default quota; `0` means unlimited.                        |
| `UPLOAD_DEFAULT_RATE_BYTES_PER_SEC`   | `0`                    | Default upload rate; `0` means unlimited.                  |
| `DOWNLOAD_DEFAULT_RATE_BYTES_PER_SEC` | `0`                    | Default download rate; `0` means unlimited.                |
| `HTTP_FAKE_LATENCY_MIN` / `MAX`       | `100ms` / `200ms`      | Artificial delay. Set max to `0` to turn it off.           |
| `OBSERVE_ENABLED`                     | `true`                 | Metrics and pprof listener.                                |
| `OBSERVE_ADDR`                        | `127.0.0.1:9090`       | Where it binds. Loopback only, never a public interface.   |
| `SESSION_TTL`                         | `24h`                  | Redis session lifetime.                                    |

A per-account value of `0` inherits the global default.

`/metrics` serves Prometheus metrics and `/debug/pprof/` serves Go profiles on `OBSERVE_ADDR`.
Neither goes through nginx or the app's middleware. The listener is off when `OBSERVE_ENABLED=false`
or when `HTTP_USE_PREFORK_MODE=true`.

## Development

```sh
make bin-deps        # air, templ, migrate and the pinned build tools
make dev             # air + Tailwind watch; needs the services up
make format          # go fix, gofumpt, gci
make linter-golangci
make test
make test-race
make test-shuffle
make test-cover
make test-integration
make test-fuzz
make pre-commit      # deps, generate, assets, CSS, format, schema, test, lint
```

`make dev` needs Postgres, Redis and MinIO already running (`make compose-up`).
air watches `.go`, `.templ` and `.env`; the Tailwind watcher handles CSS. Run
`make bin-deps` once first: the other targets expect air, templ and the pinned
tools on `PATH`.

Unit and HTTP tests use fakes and `miniredis`, so they do not need Docker.
`make test-integration` needs Docker: it starts the Compose `db` and `minio`
services, creates the `fileuploader_test` database, applies the migrations,
and uses a dedicated `fileuploader-test-*` bucket. `make test-fuzz` runs the
cursor decoder fuzzer for ten seconds.

Generated files and pinned vendor assets are committed, so a fresh clone and
the Docker image need no Node step:

- `web/templates/*_templ.go` — run `make templ-generate` after editing a
  `.templ` file.
- `web/static/css/app.css` — run `make css-build` after changing classes or
  `web/app.css`.
- `web/plugins/daisyui.mjs` and `web/static/js/{htmx,alpine}.min.js` — pinned
  vendor files; `make assets` fetches them when missing, and after a version
  bump in the `Makefile` use `make -B assets`.
- `web/static/js/app.js` — hand-written upload and drawer code. Edit it
  directly; it is not replaced by `make assets`.

Migrations are golang-migrate pairs in `migrations/`. Use
`make migrate-create add_user_avatar`, `make migrate-up` and `make migrate-down`.
`make schema` writes the live schema to `docs/schema.sql`; it skips if no
database is reachable.

## How it is put together

```text
cmd/app                   entry point
internal/app              wiring, startup, upload sweeper
internal/controller/web   Fiber handlers, middleware, request/response DTOs
internal/usecase          rules: accounts, folders, files, shares, pacing
internal/repo             contracts plus Postgres and Redis implementations
internal/entity           domain types and sentinel errors
pkg                       server, logger, Postgres, Redis, MinIO, validators
web/templates             templ pages
migrations                SQL up/down pairs
```

The dependency direction is inward. A controller calls a use case, a use case
calls repository interfaces, and `internal/app` is the only place that wires
the concrete implementations together. Repositories never import controllers,
and entities import nothing from this module.

A request starts in a Fiber handler. The auth guard resolves the session cookie
in Redis and leaves `{user_id, role}` in the request locals, so a handler never
queries the database just to learn who is asking. The handler decodes the form,
validates its shape, calls a use case and maps `entity.Err*` sentinels to HTTP
statuses. Domain rules — ownership, name clashes, quota, share expiry — live in
the use case, inside a transaction when more than one row changes.

File metadata is in Postgres. Bytes are in a private bucket, and the storage key
is derived from the file ID, so rename and move never touch the object.
Downloads stream through the app so the per-account rate limiter can pace them;
shared links go through the same path and use the owner's limits.

An upload has four steps: start creates the multipart upload and reserves the
declared size against the quota, each part is written and its ETag stored,
complete assembles the object and creates the row, abort cancels both. A
sweeper aborts sessions that went idle and never finished. The session lives in
Redis with a sliding TTL, so a closed tab does not leave an assembly running
forever.

## What I learned

A few things that stuck:

- **An upload is a protocol, not a form field.** One POST with a file field is
  the obvious design. The version here opens a session, sends numbered parts,
  completes, and aborts. Each part comes back with an ETag, the object is
  assembled before the row exists, and a tab that never sends the abort is left
  to the sweeper.
- **Quota checks need to include work in progress.** Reading
  `storage_used_bytes` at upload start is not enough: two tabs can both see the
  same free space and both take it. The start takes a per-account lock, sums the
  live sessions, and admits the declared size only if the total still fits.
- **A cached role is fast until the role changes.** Keeping `{user_id, role}`
  in Redis keeps the guard off the database. The cost is that a session is a
  snapshot: changing a role has to delete the sessions that still carry the old
  one, or a demotion waits until the next sign-in.
- **Offset pagination fights a list built from two tables.** Folders and files
  share one listing, so `LIMIT/OFFSET` starts skipping or repeating rows as the
  folder table changes. The cursor records the last `(name, id)` of each pass
  and resumes from there, so the next page does not depend on an offset.
- **Search needs the index and the UI to agree.** `pg_trgm` makes substring
  search useful at three characters and does almost nothing below that, so the
  global search refuses shorter terms instead of scanning every row.
- **Sending bytes through the app is sometimes the point.** A presigned URL is
  less code, but it also leaves the app, so the per-account rate limiter never
  sees the bytes. Streaming through one paced reader lets file, share and zip
  downloads share the same budget.
- **htmx turns response headers into UI state.** A form post returns the
  fragment to swap, `HX-Trigger` reloads another element, and `HX-Location`
  redirects an htmx request the way a 302 redirects a browser. The handler stays
  small, but the response becomes part of the UI contract.
- **A transaction ends at the database.** Postgres writes can share one unit of
  work; Redis sessions and MinIO objects cannot join it. The upload flow
  compensates explicitly: release the quota on abort, remove the object if the
  row fails, and let a sweeper clean up what the browser never finished.
- **Tests that pin HTML break on every design change.** Asserting exact class
  names or full markup makes a Tailwind tweak look like a regression. The useful
  tests check ownership, status codes, redirects and stored state; the Postgres
  and MinIO cases sit behind an integration tag.

## Notes and rough edges

- There is no email verification or self-service password reset; a signed-in
  account changes its own password from `/account`, and an admin can reset one
  for it.
- The app speaks HTML. Most `/htmx` endpoints are fragments for the page; the
  upload driver's start call returns a small JSON part plan and its part/abort
  calls answer status codes. None of it is a public JSON API.
- Global search needs Postgres's `pg_trgm` extension. The folder-browse
  migration installs it, so the database user needs permission to create an
  extension.
- The credentials in `.env.example` and `docker-compose.yml` are local
  development defaults. Change them before exposing the stack to a network.
- The admin settings page (`/admin/settings`) is still a placeholder.
- The 100–200 ms fake latency is deliberate. It makes loading and htmx swap
  states visible while developing; set `HTTP_FAKE_LATENCY_MAX=0` to remove it.
