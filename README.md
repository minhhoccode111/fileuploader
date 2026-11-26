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

The deployment can seed both accounts at startup (see `.env.example`):
an administrator from `ADMIN_EMAIL` / `ADMIN_USERNAME` / `ADMIN_PASSWORD`,
and a try-out account from `DEMO_EMAIL` / `DEMO_USERNAME` / `DEMO_PASSWORD`
— the login page offers the demo credentials to a visitor. An account whose
email is already taken is skipped, so a restart never rewrites one.

Without the seed, open `/register` and create an account. It will be
pending. The first administrator has to be promoted by hand:

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

Things that cost me time, in no particular order.

### Uploads

- An upload is a protocol, not a form field. Start opens a multipart session and
  reserves the declared size; parts carry ETags; complete joins them and writes
  the row; abort releases both. The defaults are 10 GiB per file, 50 MiB parts
  and a 1 h idle window.
- S3 rejects any part under 5 MiB except the last, and one upload carries at most
  10,000 parts. A 10 GiB file at 50 MiB is 205 parts, not 200; at the 5 MiB floor
  it is 2,048.
- The requested part size is `rate * 90s`, clamped to 5–50 MiB, not always the
  configured 50 MiB. Below about 57 KB/s the 5 MiB floor wins; below about
  17 KB/s even that part cannot finish inside the 5-minute server read timeout,
  and the browser gives up after 120 s. The comment says the floor protects the
  timeout; for very slow accounts it does the opposite.
- The browser sends one part at a time with XHR, because only XHR reports upload
  progress. It retries three times with exponential backoff and jitter on a
  dropped connection or timeout, and on HTTP 429 or 5xx other than 524; a 4xx is
  final. Start is a JSON `fetch`, complete is a `fetch`, abort is a keepalive
  `fetch`.
- Abort cannot be a beacon: `navigator.sendBeacon` cannot send the
  `X-CSRF-Token` header, so the pagehide path uses
  `fetch(..., { keepalive: true })`. Closing the drawer and `pagehide` both
  abort; the sweeper is the backstop.
- An upload belongs to the page's JS, not to the page lifecycle. An htmx folder
  swap does not unload the document, so the loop keeps sending and `activeUpload`
  stays set; the next upload says another is running, and the old flow writes
  progress and errors into the new drawer by global id. Only closing the drawer
  or `pagehide` bumps the run and aborts.
- The part PUT has no auth-redirect check. If the session expires mid-upload,
  `RequireAuth` answers 302 to `/login`; XHR follows it and reports 200 with the
  login HTML, so the bar reaches 100% and only complete fails. Start checks
  `response.redirected` and content type, complete checks `X-Upload-Folder`; the
  part path checks neither.
- The upload id lives only in JS memory. A reload loses it; the Redis session
  slides on each part and the reservation lasts until the idle TTL and sweeper,
  so a 10 GiB upload restarts from zero and the old reservation can block new
  ones for up to an hour.
- Start reserves the whole declared size in Redis, not just the bytes received.
  The quota lock is a 30 s lease renewed every 10 s; a caller waits at most 10 s
  before getting a 503. Expired sessions still count in `uploadReserved` until
  the sweeper deletes them, so a dead upload can block new ones.
- Complete is idempotent for an hour: `upload-completed:<id>` remembers the
  file, so a retry after a lost response returns the same row. The browser never
  retries complete, though, so a lost response shows "Upload failed" while the
  file exists.
- Complete and abort race under the same quota lock. Complete re-reads the
  session after taking the lock, so the loser either returns the winner's file
  or 404s. Abort after a successful complete is a 204 no-op; it does not delete
  the finished file.
- A failed join aborts the multipart and ends the session, so a transient S3
  error at 99% throws away every part; a full re-upload is the only recovery.
- An empty file never opens a multipart upload: zero parts, `PutObject` at
  complete, no abort, and a download without a length, so it goes out chunked.
- The body limit has two layers: 1 MiB for every route, and `PartBytes + 1 MiB`
  on one part path. The middleware accepts a chunked body on the part path, but
  the handler always answers 411 without a Content-Length, so that allowance is
  dead. The extra MiB only turns a body between the two limits into a 413 from
  the handler instead of the server.
- The rate limiter is a virtual next-send pointer. The first reservation starts
  at `now` and sleeps zero, so a transfer gets one free batch (64 KiB–1 MiB)
  before the rate applies; a client that disconnects during the sleep still
  spends the reservation.
- The part plan is a flat JSON object, not presigned part URLs: every byte goes
  through `/htmx/uploads/.../parts/...`, CSRF, the body limit and the pacer. XHR
  upload progress is the only progress source, and the plan carries no ETags or
  checksums.
- Parts are sequential. At 50 MiB a 10 GiB file is 205 requests, and the
  100–200 ms fake latency applies to each, adding roughly 20–41 s of pure sleep.
- The 120 s XHR timeout is longer than the 90 s part target, but below about
  44 KB/s a 5 MiB part cannot finish in 120 s. The client retries the same part
  without waiting for the server to finish the previous attempt, so the
  attempts can overlap.
- Progress rewinds on a retry because each attempt reports from zero, and it
  reaches 100% after the last part while complete is still joining all of them.

### Downloads and archives

- Downloads stream through the app so the per-account limiter sees the bytes;
  `WriteTimeout` is 0 so a large download is not cut. The pacer reserves a
  quarter second of rate per batch, clamped to 64 KiB–1 MiB.
- Folder zip is collected and probed before the first byte: one `ListAll`, one
  `List` per folder, one `Get` and one `GetObject`+`Stat` per file, then another
  `GetObject` per file while writing. A 5,000-file folder does roughly 10,000
  bucket round trips before the headers.
- Empty folders are not entries in the archive, so they vanish. A folder named
  `x.zip` downloads as `x.zip.zip`. A failure after the headers cannot change
  the status; the pipe closes with an error and the client gets a truncated
  archive that looks like a corrupt download.
- Content-Disposition carries the name twice: a quoted ASCII fallback and an
  RFC 5987 `filename*`. A name with no ASCII becomes `download`; a quote or
  backslash in the fallback is replaced so it cannot break the header.
  `X-Content-Type-Options: nosniff` goes with it.
- `Download` (presigned, 15 min) still exists and passes its integration test,
  but no route calls it. Both live download routes stream instead; a presigned
  route would bypass `download-rate:<user>` entirely. `MINIO_PUBLIC_ENDPOINT`
  only matters to that unused helper.
- Shared file downloads are public and still paced against the owner's
  `download-rate:<owner>` bucket. A popular link can slow the owner's own drive;
  the visitor never pays.

### Sessions and security

- A session ID is 32 random bytes in base64, stored in an HttpOnly,
  SameSite=Lax cookie. Redis holds `{user_id, role}` and the account's session
  index. The TTL is absolute, so activity does not extend a 24 h session.
- The role is a snapshot. A role change ends every live session; a password
  change keeps the current one and ends the others; disabling an account ends
  all of them. That is why the session can carry the role and a guard never has
  to read the database.
- Login throttling is two buckets: 5 per email and 30 per client address in
  15 minutes. Every attempt takes a slot before the password is checked, and a
  blocked attempt still increments. A successful login resets the email bucket
  only, so the client bucket counts successful logins too; 30 people behind one
  NAT can lock out the 31st for 15 minutes. If `X-Real-IP` is missing, every
  visitor shares the proxy address.
- The email bucket is charged before the IP bucket is consulted, so a blocked
  address can still fill a victim's five email slots and deny that account.
- The message for an unknown email and a wrong password is the same, but the
  path is not constant-time: an unknown email returns before bcrypt. The message
  hides it from the page, not from a stopwatch.
- CSRF is double-submit: a 32-byte random cookie (not HttpOnly, because the page
  must read it) and the same value in `X-CSRF-Token`. The comparison is
  constant-time. The Origin check compares `host:port` and ignores the scheme;
  an absent Origin still has to pass the token. Safe methods are not checked, so
  the cookie is issued on the first GET. The token is not rotated at login or
  logout.
- Registration is pending by default; the root folder is created in the same
  unit of work. The success response is 202 with the error fragment, so "account
  requested" appears in the red error box.
- Password rules are min 6, at least one upper, lower, digit and special; bcrypt
  stops at 72 bytes. The validator's `max=72` counts runes, so a short CJK
  password can pass validation and still fail `bcrypt.ErrPasswordTooLong`
  (mapped to a 422). The registration form has no `maxlength`.
- Usernames are Unicode letters/digits only, 3–255, and case-sensitive in the
  unique index; emails are trimmed and lower-cased before storage. Changing the
  email needs only the session, no password and no verification, and it does not
  end other sessions.

### The drive

- Every drive mutation takes a `FOR UPDATE` lock on the account's root folder
  row. That is the whole concurrency story: one account writes one drive
  operation at a time. It is simple and makes name checks safe, but a folder
  duplicate holds the lock across N `CopyObject` calls inside the Postgres
  transaction, and `Create` lists the whole tree inside it just to compute
  depth.
- Names are compared with `lower(name)` and unique per table. Folders:
  `(user_id, parent_id, lower(name))`; files: `(folder_id, lower(name))`; the
  root needs a separate partial index because `parent_id IS NULL` never
  collides. The cross-table rule "a folder and a file cannot share a name" has
  no database constraint; it lives in `checkName`, under `LockUser`.
- That check reads a row loaded before the lock. `file.Move` and `folder.Move`
  can therefore check the old name while a concurrent rename commits. The
  same-table unique index catches a file/file or folder/folder clash, but a
  folder/file clash can slip through one listing with two identical names.
- `CleanName` rejects slash, backslash, Windows-forbidden characters, controls,
  bidi/format characters, a trailing dot, and Windows device names (`NUL.txt`,
  `Lpt9.log`; `COM10.txt` passes). It deliberately keeps U+200C/U+200D for emoji
  and living scripts, so a name made only of a zero-width joiner, a soft hyphen,
  or a variation selector is accepted and renders blank. Two names that look
  the same can still be different rows.
- Folder depth is 20 with the root at 0. Create checks `depth(parent)+1`; move
  and duplicate check `depth(destination)+1+height(subtree)`. The move/duplicate
  forms pre-filter invalid destinations with the same formula, so the UI and the
  use case agree. The 255 and 20 constants are repeated in several packages; one
  edit can leave them out of step.
- Cycles are prevented only by the app. The DB has no `CHECK (parent_id <> id)`,
  `FolderSubtreeHeight` has no `seen` set, and the recursive CTEs use `UNION ALL`
  with no cycle clause. A hand-made cycle can overflow the Go stack or spin a
  query.
- A folder page is 100 entries, folders first and then files. The merge asks
  each repo for `limit+1`; if the folder pass fills the page, page 1 has zero
  files and the cursor says `FoldersDone`. A page is not a merged sort, it is two
  passes.
- The cursor is base64 JSON, not an opaque signed token. It holds only
  `{kind,name,id,foldersDone}`, with no folder or query binding; it is validated
  for kind, a bounded valid-UTF-8 name, and a parseable UUID, but a hand-edited
  cursor can jump anywhere or hide every folder. Full page routes ignore `after` entirely
  while the load-more partials 400 on a missing one; the same query param has two
  contracts.
- Local `q` has no minimum and runs against direct children. Global search needs
  three characters because a shorter substring cannot use `pg_trgm`, caps the
  term at 100 runes, drops control characters (Postgres rejects NUL), and
  escapes `\`, `%` and `_` so the term is literal. Nothing is ranked: results
  are `lower(name)`, then id, folders first.
- Global search resolves every result's path in one recursive query and marks
  the page read-only: a folder opens on its page, a file downloads, and a
  location link opens the containing folder. A folder result's path includes the
  folder itself, so its location reads `Home / Docs` next to a Name cell that
  already says `Docs`.
- `storage_used_bytes` is maintained by the app, not a trigger. Delete
  decrements it before removing bytes; the duplicate quota check sees the row
  but not the Redis reservations of in-flight uploads.
- Folder deletion deletes file rows first, returns their storage keys, deletes
  the folder row, lets the FK cascade remove the subtree, then removes objects.
  The row order is deliberate: an object left behind is invisible, while a row
  without an object could still be listed.
- Moving a folder or file to the folder it is already in is a silent success
  with no `updated_at` change. Duplicate copies into the destination but
  re-renders the source folder, so the new row is not on screen unless the
  destination is the current folder. Create, rename and duplicate reload under
  the active `q`, so a new name that does not match the filter disappears.
- Every drive mutation renders the first page again: `renderEntries` passes no
  cursor, so after loading 500 rows a rename goes back to 100 and a fresh
  sentinel. The cursor is not in the URL, so a reload does the same.

### Sharing and admin

- There is one share per folder (`unique (user_id, folder_id)`). Creating again
  updates the expiry and keeps the same token; creating after expiry revives the
  expired row, so a leaked old URL starts working again.
- Expiry is a zone problem. The `datetime-local` input has no zone, so the
  server parses it in `time.Local` and stores UTC; the drawer formats the stored
  UTC instant back without converting to `time.Local`. On any server whose `TZ`
  is not UTC, saving the same form shifts the expiry by the offset, and the owner
  sees a time they never typed. The scratch image has no tzdata, so in Docker it
  is UTC; on a laptop it is not.
- A blank expiry means unlimited, not "keep the current one". Clearing the date
  and saving turns a limited link permanent. A past date is accepted, stored,
  then hidden from `/share` because the list only shows active shares; the owner
  sees an empty page and thinks creation failed. The expiry instant itself is
  already expired (`!After` in Go, `> now()` in SQL).
- Expired and deleted look the same to a visitor (404) but not to the owner: a
  deleted row is gone forever, an expired row is revived with the same token
  when the folder is shared again. The drawer and the index both hide expired
  shares, so the only recovery is the folder's share form.
- Opening your own share while signed in redirects you to the folder in your
  drive. To see what a visitor sees, use a private window or sign out. Shared
  folders have local `q`, no global search, no zip download, and downloads use
  the owner's rate budget.
- A share of a deep folder shows the owner's full ancestor breadcrumb. A share
  of `/A/B/C` renders `/`, `A`, `B`, `C`; the ancestor names are visible even
  though clicking one is rejected by the subtree check and bounces to the share
  root.
- The admin list is offset paginated, unlike the drive's keyset. `?page=999`
  queries offset 9980 and returns no rows, while the pager clamps the label to
  the last page, so the table can be empty under "Page 3 of 3". The day filter
  compares `created_at::date` or `updated_at::date` in the DB zone, and the date
  was parsed at UTC midnight.
- The admin filter form has no 4xx target. A `q` over 100 characters fails
  validation with a 422, and the error list swaps into `#admin-users`, taking the
  hidden sort, direction, page and limit inputs with it; the table cannot
  recover without a reload.
- Clear is a `type=reset`, not a link to an empty filter. The inputs are rendered
  at page load outside the swapped table, so on a bookmarked `?q=foo`, Clear puts
  `foo` back and fetches the same view again.
- Zero means "inherit the global default" for quota and rates; the row labels an
  effective `<= 0` as "Unlimited" and the editor says "Zero inherits the
  default". With both defaults at 0, "inherit" and "no limit" are the same value
  and an explicit zero quota cannot be expressed.

### Running it

- `make run` builds with `-tags migrate`; `internal/app/migrate.go` has an
  `init()` that retries the database 20 times at one second and applies all
  migrations before `main`. A plain `go run ./cmd/app` has no migration step, and
  `.env` is loaded by the Makefile, not by the app.
- Postgres 18 wants the volume at `/var/lib/postgresql`, not
  `/var/lib/postgresql/data`. The compose file uses the former.
- MinIO's upstream image is no longer published; compose runs
  `docker.io/pgsty/silo`. MinIO rejects the S3 lifecycle action for incomplete
  multipart uploads, so the app sweeps them itself.
- The metrics/pprof listener is a separate `net/http` server on `OBSERVE_ADDR`
  (loopback by default). It bypasses Fiber, so it also bypasses CSRF, the body
  limit and the fake latency. It is off under prefork because one process per
  worker would race for the port; `OBSERVE_ADDR=""` falls back to the default
  rather than disabling it, so use `OBSERVE_ENABLED=false`.
- `make migrate-up` and `make migrate-down` wrap `migrate up 1` / `down 1`
  because golang-migrate answers `error: file does not exist` when no migration
  is left. A bare `migrate down` with no argument offers to revert everything.
  `make schema` writes `docs/schema.sql` and skips with an exit 0 when no
  database is reachable; it adds the `pg_trgm` line by hand.
- Unit and HTTP tests use fakes and `miniredis`; the Postgres and MinIO cases sit
  behind the integration tag. The cursor decoder has a fuzzer because the value
  is hand-editable. Asserting exact HTML classes breaks on every design change;
  tests that check ownership, status codes and stored state survive.
- templ writes committed `*_templ.go` files, and Tailwind writes a committed
  `app.css`. Edit the source, run `make templ-generate` or `make css-build`, or
  the build serves stale HTML/CSS with no warning. `make dev` runs air through
  `dev-run.sh`; the wrapper reloads `.env` and `exec`s the app so air's stop
  signal reaches it.
- The bundled nginx sets `client_max_body_size 110m`, `X-Real-IP`, and
  `X-Request-ID`, but not `X-Forwarded-Proto`; it also forwards `Host $host`,
  which drops a non-default port.

### CI and deploy

- A push to `main` is the whole pipeline: `go test -race` first, then the image
  builds and pushes to Docker Hub as `latest` and the commit sha, then one ssh
  line reaches the VPS. `concurrency` with `cancel-in-progress: false` queues a
  second push instead of killing a deploy mid-run.
- The ssh command ends with the word `deploy`, and that word does nothing. The
  key in the VPS `authorized_keys` is pinned with
  `restrict,command=".../scripts/ci-deploy.sh"`: sshd runs the forced command
  and discards whatever the client asked for — the request lands in
  `SSH_ORIGINAL_COMMAND`, which nothing reads. The word exists only because
  non-interactive ssh wants some remote request; the workflow comment says so.
- The forced command is least privilege. A leaked key or a compromised runner
  can only pull and restart the app, never open a shell; `restrict` also kills
  pty, port forwarding and agent forwarding. No CI command crosses the wire, so
  there is nothing to inject, and the script is version-controlled like any
  other code.
- `ci-deploy.sh` reads `IMAGE_TAG` from the server's `.env` (default `latest`),
  runs `docker compose pull app`, `up -d app`, prunes old images and prints
  `compose ps`. Rollback is pinning `IMAGE_TAG` to a sha in `.env` — every
  build is already on Docker Hub under its commit sha.
- The private key lands in `~/.ssh/vps_key` with `chmod 600`, because ssh
  refuses a group- or world-readable key. The host key comes from
  `VPS_KNOWN_HOSTS`, not first-use trust, so there is no interactive prompt and
  no MITM window on the first connect.

### Browser code

- The browser is not a framework: htmx handles links, forms and partial swaps;
  Alpine only drives the drawers and the file picker; the upload driver is
  hand-written XHR/fetch. The vendored htmx is 4.0.0, so 4xx/5xx responses still
  swap by default; a downgrade to htmx 2 would silently stop showing login and
  register errors.
- A same-URL GET link is cancelled before it fires; after a successful action
  `HX-Trigger: entrySaved` closes the drawer; `HX-Location` navigates the page;
  `HX-Push-Url` keeps filters bookmarkable. A page navigation swaps
  `#page-content` and a head-only title update arrives with
  `HX-Request-Type: partial`.
- Rows are mouse-only: `<tr hx-get>` has no tabindex, role or key handler;
  close/cancel are labels; there is no Escape handler. The collapsed admin
  submenu uses `grid-rows-[0fr]` and `overflow-hidden`, not `hidden` or `inert`,
  so its links stay in the tab order.
- An `autofocus` inside a drawer cannot fire: htmx focuses the swapped content
  before `htmx:after:swap` opens the drawer, while the panel is still hidden.
  The search input also has no id, so htmx cannot restore focus after a search
  swap.
- The admin table loads its own skeleton, then listens for `usersUpdated` or
  `accessRequestsUpdated` to reload. The load-more row is a `<button>` with
  `hx-trigger="click, revealed"` and `hx-sync="this:drop"`, so it loads on click
  or when scrolled into view and ignores a double click.
- Long names keep their head and tail and lose the middle: canvas measurement,
  binary search, re-run after every swap and resize. The full name stays in
  `title` and `data-full-name`.
- Small downloads (≤32 MiB) are fetched into a Blob so the progress bar can
  move; larger files and folder zips go straight to the browser because
  buffering them would hold the whole file in memory and a zip has no size
  anyway.
- The upload start validates the server's part plan (`partCount ==
ceil(size/partSize)`) before sending a byte, and complete swaps rows only if
  the page still shows the folder the file landed in; otherwise it shows a toast
  so a late answer does not rewrite a page the visitor left.

## Known rough edges

- Disabling an account ends its sessions but not its public share links. Share
  resolution never reads the owner's status, so a disabled account keeps serving
  through every `/share/<token>`; there is no delete-user endpoint to clean up.
- A sole admin can demote or disable themselves; there is no last-admin guard.
  Role and status are any-to-any, so an active account can be sent back to the
  access-request list. Approving an account that is not pending is a 500 because
  `ErrInvalidStatus` has no admin error mapping.
- The admin save is three independent writes. A late limits or status failure
  leaves the account update committed, any role/password change already ended
  its sessions, and the table still showing the old row. An empty password field
  still writes the hash the editor read, and `storage_used_bytes` is displayed
  read-only but never written.
- The stale-multipart sweep passes the S3 multipart ID to the Redis session
  lookup, where the key is the app's session UUID. A live upload older than 24 h
  is treated as unclaimed and aborted. The expired-session pass also stops at the
  first batch that deletes nothing, so a run of busy quota locks can delay the
  rest of the due sessions to the next sweep.
- `SetRole` and the admin `UpdateUser` write Postgres before they revoke Redis
  sessions; `DeleteUserSessionsExcept` reads the set and deletes it outside a
  transaction. A Redis failure or a concurrent login can leave an old session
  alive.
- Registration is unthrottled, and a taken email is a distinct 409; the
  password-change endpoint is also unthrottled and answers 422 on a wrong
  current password. Both are oracles, and registration is a bcrypt CPU sink.
- Both cookies derive `Secure` from `ctx.Protocol()`. The bundled nginx never
  sets `X-Forwarded-Proto`, so behind TLS termination they go out without
  `Secure`; the access log uses `ctx.IP()` and records nginx's address, while the
  login throttle trusts `X-Real-IP` and compose publishes the app on `8090`, so a
  direct client can rotate that header.
- A failed object removal after a delete only logs `ErrStorageCleanup`; the row
  and the quota are already gone, no job reconciles complete objects without
  rows, and `storage_used_bytes` can drift down forever.
- The Postgres retry loop wraps a lazy pool constructor, so a server-only build
  starts with Postgres down; `migrate.go` is excluded from ordinary builds and
  CI, and an HTTP bind failure still exits 0.
- `make docker-rm-volume` removes `fileuploader_pg-data`, but the volume is
  `fileuploader_db_data`; the database survives the target.

## Notes

- There is no email verification or self-service password reset; a signed-in
  account changes its own password from `/account`, and an admin can reset one
  for it.
- The first admin is promoted with the `psql` UPDATE from the setup section.
  The admin page cannot do it, because the page needs an admin to open.
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
