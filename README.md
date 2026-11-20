# File Uploader

A small personal drive built as my take on [The Odin Project's file uploader
project](https://www.theodinproject.com/lessons/nodejs-file-uploader). The
assignment asks for Express and Prisma; I built the same product in Go to
learn the server side of an upload flow properly.

The UI is server-rendered with templ and htmx. Postgres holds metadata, Redis
holds sessions and counters, and the bytes live in an S3-compatible bucket
(MinIO locally). There is no SPA and no Node at runtime.

It is a learning project, not a production service, but it is meant to be easy
to run, read and extend.

## What it does

- **Accounts.** Registration is a request: anyone can fill the form, but the
  account stays `pending` until an administrator approves it. Disabling an
  account ends its live sessions immediately.
- **Drive.** Nested folders and files per account: upload, download, rename,
  move, duplicate, delete. A folder downloads as a zip. The default ceiling is
  10 GiB per file.
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

You need Go 1.27, Docker with Compose, and make.

```sh
cp .env.example .env
make compose-up     # Postgres, Redis and MinIO
make run            # http://localhost:8090
```

`make compose-up` blocks on the service logs. Press Ctrl-C to stop the log
view — the containers keep running — then start `make run`, or run it in a
second terminal. `make run` builds with the `migrate` tag, so the app applies
pending migrations and creates the bucket on startup.

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
docker compose exec db psql -U user -d fileuploader -c \
  "UPDATE users SET role = 'admin', status = 'active' WHERE email = 'you@example.com';"
```

Sign in, then approve everyone else from **Admin → Access requests**. The admin
page cannot create the first admin, because the page itself needs an admin to
open.

### Everything in Docker

`make compose-up-all` builds the app image and starts it behind nginx at
http://app.lvh.me (the hostname resolves to `127.0.0.1`). That path takes its
settings from `docker-compose.yml`, not from `.env`.

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
| `SESSION_TTL`                         | `24h`                  | Redis session lifetime.                                    |

A per-account value of `0` inherits the global default.

## Development

```sh
make bin-deps        # air, templ, migrate and the pinned build tools
make dev             # air + Tailwind watch; needs the services up
make format          # go fix, gofumpt, gci
make linter-golangci
go test ./...
make pre-commit      # deps, generate, assets, CSS, schema, lint
```

`make dev` needs Postgres, Redis and MinIO already running (`make compose-up`).
air only watches `.go` and `.templ`; the Tailwind watcher handles CSS.

Two kinds of generated files are committed:

- `web/templates/*_templ.go` — run `make templ-generate` after editing a
  `.templ` file.
- `web/static/css/app.css` — run `make css-build` after changing classes or
  `web/app.css`.

Migrations are golang-migrate pairs in `migrations/`. Use
`make migrate-create add_user_avatar`, `make migrate-up` and `make migrate-down`.
`make schema` writes the live schema to `docs/schema.sql`; it skips if no
database is reachable.

The tests use fakes and `miniredis`, so `go test ./...` does not need the
Docker stack.

## How it is put together

```text
cmd/app                   entry point
internal/app              wiring, startup, upload sweeper
internal/controller/web   Fiber handlers, middleware, request/response DTOs
internal/usecase          rules: accounts, folders, files, shares, pacing
internal/repo             contracts plus Postgres, Redis and MinIO implementations
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

## Notes and rough edges

- This is a learning project, not a deployment template. There is no email
  verification or self-service password reset; an admin resets a password.
- The app speaks HTML. The `/htmx` endpoints are fragments for the page, not a
  public JSON API.
- The admin settings page (`/admin/settings`) is still a placeholder.
- The 100–200 ms fake latency is deliberate. It makes loading and htmx swap
  states visible while developing; set `HTTP_FAKE_LATENCY_MAX=0` to remove it.
