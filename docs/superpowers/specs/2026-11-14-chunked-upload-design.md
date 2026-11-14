# Chunked Upload Through Cloudflare

## Intent

Cloudflare rejects a proxied request body above 100 MB. The current upload is one
multipart request from the browser to the Go app, so no file above that cap can
reach the app, whatever `UPLOAD_MAX_BYTES` says. This design splits one file into
many requests that each stay below the proxy cap, while the Go app still sees
every byte so the per-account upload speed limit keeps working.

The target maximum file size is 10 GiB. A closed tab starts over; there is no
resume. One account gets one upload rate across every upload it runs at once.

## Goals

- Upload files up to 10 GiB through the existing Cloudflare-proxied hostname.
- Keep MinIO private. No public bucket, no grey-cloud host, no bucket CORS.
- Keep the upload speed limit in the Go app, and make it a single per-account
  total rather than a limit per request.
- Reserve the file's full size against the account quota from the moment the
  upload starts.
- Stream each chunk straight into a MinIO multipart upload. No 10 GiB temp file
  and no second copy on the app server.
- Keep one upload path. The old single-request upload is removed.
- Keep the existing drawer, progress bar and entry refresh.

## Non-Goals

- Resuming after a reload, a lost connection or a different device.
- Direct browser-to-MinIO presigned uploads.
- More than one app replica. Redis is used because it is already part of the
  stack and because the user asked for it, not because horizontal scaling is
  planned.
- Global pacing of downloads. Downloads keep the current per-request throttle.
- Raising Cloudflare's limit, Workers, R2, or a grey-cloud DNS record.
- Changes to the REST API. This repository currently exposes the web
  controller only.
- Antivirus, encryption, deduplication or content inspection.
- Admin UI for in-flight reservations.

## Anti-Goals

- Any single request above roughly 50 MiB.
- Buffering the whole file in memory or on local disk.
- A speed limit that parallel chunks can multiply.
- Uploading 10 GiB and only then discovering the quota is full.
- Orphaned MinIO parts or permanently reserved quota after an abandoned upload.
- Two upload implementations that can drift apart.

## Constraints

- Cloudflare caps one proxied request body at 100 MB on Free, Pro and Business.
  The cap counts the whole body of a request, not the file.
- S3 multipart needs parts of at least 5 MiB, except the last part. It allows at
  most 10,000 parts.
- The current upload writes the file to an `os.CreateTemp` file, then
  `PutObject`. See `internal/controller/web/upload.go`.
- The current rate limiter is built per HTTP request with
  `golang.org/x/time/rate`, so two requests get two buckets. See
  `newThrottledReader` in `internal/controller/web/file.go`.
- Quota is checked against `users.storage_used_bytes` in
  `FileUseCase.Upload` and again inside `storeRow`, which locks the user and the
  folder. See `internal/usecase/file/file.go`.
- The server-wide `ReadTimeout` is `UPLOAD_TIMEOUT`, currently 5m. A part must
  finish inside it.
- The HTTP server's `BodyLimit` and the web `BodyLimit` middleware are currently
  set from `UPLOAD_MAX_BYTES + MultipartOverheadBytes`. Raising the upload
  maximum without changing them would let every route accept a 10 GiB body.
- The linter is strict: `funlen`, `gocyclo`, `gocognit`, `mnd`, `err113`,
  `gochecknoglobals`, `wsl_v5`, `nlreturn`. New code follows the existing
  patterns and sentinels.

## Decisions Made By The User

- Chunked upload through the Go app, not browser-to-MinIO presigned parts.
- No resume. Closing the tab or losing the connection starts over.
- One upload rate per account, shared by all uploads and tabs.
- Target maximum file size is 10 GiB, and the server limit is raised.
- Redis is used for the shared limiter and upload state.
- S3 multipart, not a local temp file.
- The file's size is reserved against the account quota immediately at upload
  start.
- One app instance is expected.
- An abandoned upload releases its reservation quickly: an explicit abort is
  sent when the drawer closes or the page unloads, and a session otherwise
  expires one hour after its last accepted part.
- If the user navigates to another folder while an upload runs, the completion
  does not swap the wrong folder's rows. It shows a success toast instead.

## Decisions Made In This Design

- Part size is 50 MiB by default.
- If an account has a positive upload rate, the part size shrinks so one part
  targets at most 90 seconds, with a floor of 5 MiB:
  `partSize = min(50 MiB, max(5 MiB, rate * 90s))`.
  This keeps a part well inside Cloudflare's 125s proxy read timeout as well as
  the 5m request timeout. A rate below roughly 58 KiB/s cannot fit a 5 MiB part
  in 90 seconds; such a configuration is documented as unsupported for large
  uploads rather than raised above the proxy timeout.
- The old `POST /htmx/files` upload is deleted. The new upload is the only path.
- Four upload endpoints live under `/htmx/uploads`, behind
  `middleware.RequireAuth`: start, part, complete and abort.
- Upload session keys live in Redis. No database migration is needed.
- An app-side sweeper aborts expired multipart uploads. MinIO lifecycle rules
  are not required.
- The completion response is the same entries fragment the old upload returned,
  plus a header carrying the folder id. The drawer swaps only when that id
  matches the folder on screen.
- Zero-byte files use `PutObject` at completion instead of a multipart part.

## File Size And Part Math

- `UPLOAD_MAX_BYTES` default becomes `10737418240` (10 GiB).
- `UPLOAD_PART_BYTES` default is `52428800` (50 MiB).
- Effective part size per session is computed at init from the account's
  effective upload rate, as described above.
- `partCount = max(1, ceil(size / partSize))`.
- The last part carries the remainder. All other parts must be exactly
  `partSize`.
- A 10 GiB file is 205 parts at 50 MiB, or 2048 parts at the 5 MiB floor, both
  far below S3's 10,000-part limit.
- A declared size above `UPLOAD_MAX_BYTES` is rejected at init.
- A declared size below zero is rejected at init.
- A zero-byte file has zero parts and uses `PutObject`.

The browser is told the part size and part count at init. The server never trusts
the browser's later size claims: each part's `Content-Length` and declared index
must match the session's expected size.

## Architecture

### Start

1. The drawer sends `POST /htmx/uploads` with folder, name, size and content
   type.
2. The controller authenticates the visitor, decodes and validates the request,
   then calls `FileUseCase.StartUpload`.
3. The use case cleans the name, checks the folder and the name, checks the
   maximum size, and takes the per-account quota lock.
4. Under the lock it reads the user, sums the sizes of that user's live upload
   sessions, and admits the upload only if
   `used + reserved + size <= quota`, when the quota is positive.
5. It creates a MinIO multipart upload at a fresh storage key and stores the
   session in Redis. The full size is now reserved.
6. The response is `{upload_id, part_size, part_count}`.

### Parts

1. The drawer sends `PUT /htmx/uploads/:upload/parts/:part` with one raw part.
2. The controller loads the session, verifies ownership, checks that the part
   number is in range, and computes the expected size.
3. It rejects a missing or wrong `Content-Length`, and rejects extra bytes while
   streaming.
4. The part body is wrapped in the Redis pacer, then passed to
   `ObjectStorage.UploadPart`.
5. The returned ETag is stored in the session's part hash. Re-sending the same
   part number overwrites its ETag, so a failed part is safe to retry.
6. The response is `204 No Content`.

Parts may arrive out of order. Completion requires every number from 1 to
`partCount`, each with an ETag.

### Complete

1. The drawer sends `POST /htmx/uploads/:upload/complete`.
2. The controller loads the session and verifies ownership.
3. The use case verifies that all parts are present.
4. Under the same per-account quota lock, it calls
   `CompleteMultipartUpload` with parts ordered by number.
5. It writes the file row through the existing `storeRow`: user lock, folder
   lock, quota recheck, name recheck, insert, `storage_used_bytes` increment.
6. On success it deletes the session, removes it from the user's live-upload
   set, and returns the file.
7. If the row write fails, it removes the completed object, releases the
   reservation, deletes the session, and maps the error. The object-first
   invariant of the current upload is kept.

If the name was taken while the bytes were uploading, completion fails with
`ErrEntryAlreadyExists`. The uploaded bytes are discarded; the user retries with
another name. The init-time name check is only early feedback; the definitive
check is the one under the folder lock.

### Abandonment

A session expires after `UPLOAD_SESSION_IDLE_TTL`, default 1h, measured from the
last accepted part. Each accepted part moves the session's score in the expiry
sorted set forward, so a legitimate upload that runs longer than an hour keeps
itself alive. A client that stops sending parts is swept one hour later.

There are two release paths.

**Explicit abort.** `POST /htmx/uploads/:upload/abort` verifies ownership,
calls `AbortMultipartUpload`, deletes the session and releases the reservation.
The client sends it when the drawer closes and on `pagehide`, using
`navigator.sendBeacon` so the request survives the page going away. A beacon
cannot carry a custom method, so the abort route is a POST.

**Sweeper.** The app scans the expiry sorted set and, for each expired session:

1. Calls `AbortMultipartUpload`. A missing upload is ignored.
2. Deletes the session keys and the part hash.
3. Removes the upload from the user's live-upload set, releasing the reserved
   quota.

The sweeper runs at a fixed interval, for example every 5 minutes, and once at
startup. It is safe to run twice. It never removes a completed object; it only
aborts incomplete multipart uploads. A crash between `CompleteMultipartUpload`
and the row insert can therefore leave one orphan object, the same window the
current object-first upload already has. Reconciling orphan objects is out of
scope.

Beacons are best effort. A browser that is killed, loses power or blocks the
beacon falls back to the idle expiry.

## Redis Model

Keys are namespaced. Exact names are a builder choice, but the shapes are:

| Key | Type | Contents |
| --- | --- | --- |
| upload session | hash | id, user id, folder id, name, content type, size, part size, part count, storage key, MinIO upload id, created at, expires at |
| upload parts | hash | field = part number, value = ETag |
| user live uploads | set | upload ids owned by one user |
| upload expiry | sorted set | score = last activity + idle TTL, member = upload id |
| quota lock | string | per-user lock, `SET NX PX` with a retry loop |
| upload rate | string | per-user pacer state |

Session keys get a Redis TTL longer than the idle TTL as a fallback, so a broken
sweeper cannot grow Redis forever. The sorted set is the source of truth for
expiry; if a session key is missing when its turn comes, the sweeper removes the
index entry and logs it. Each accepted part updates the session's score in the
sorted set to `now + idle TTL`. A failed or rejected part does not.

The part hash is separate from the session hash so concurrent part writes do not
read-modify-write one JSON blob.

## Quota Reservation

An in-flight upload counts its full declared size against the account from init
until completion, abort or expiry.

- Admission is serialized per account with the quota lock. Init and completion
  both take it, so an init cannot read `storage_used_bytes` before a completion
  lands and then reserve the same headroom.
- The reserved total is the sum of the sizes of that user's live sessions. No
  separate counter is kept, so there is nothing to drift.
- A zero quota means unlimited. The session is still tracked, but no admission
  check is applied.
- Completion rechecks `used + size <= quota` inside `storeRow` under the user
  and folder locks. The reservation is admission control; the database check
  remains the final authority.
- Aborting, failing completion and sweeping all remove the session from the
  user's live set, which releases the reservation.
- Non-upload usage changes, such as a duplicate or an admin lowering a quota,
  do not take the upload quota lock. A completion can therefore fail at the end
  in a rare race. It never overfills the quota, because the transactional check
  is authoritative.

## Rate Limiting

The existing per-request `rate.Limiter` is replaced for uploads by one logical
bucket per account in Redis.

- Effective rate is resolved from the user with the existing
  `uploadRate` helper. Zero means unlimited, and no pacer work is done.
- The pacer keeps one Redis key per account holding the theoretical next
  send time. Because there is one app instance, a process mutex serializes the
  read-modify-write; the reservation and the stored time are still in Redis, so
  a restart does not hand out a fresh burst. A Lua script is the obvious port
  if replicas are ever added.
- A reservation of `n` bytes at `rate` advances the next send time by
  `n / rate`. The caller waits until the previous send time before sending, so
  concurrent parts queue rather than multiply the rate.
- Every part of every upload uses the same key. Two parallel chunks, two
  simultaneous uploads and two tabs all draw from one account bucket.
- The rate is read per part request, so an admin change applies from the next
  part onward.
- Redis failure fails the part. Authentication and sessions already depend on
  Redis, so failing closed is consistent.
- Downloads keep the current per-request limiter. They are not part of this
  bucket.

`newThrottledReader` and the `golang.org/x/time/rate` dependency for uploads are
replaced by this pacer. The download path can keep the old reader.

## Storage Changes

`repo.ObjectStorage` gains multipart operations:

- `CreateMultipartUpload(ctx, bucket, key, contentType) (uploadID, error)`
- `UploadPart(ctx, bucket, key, uploadID, partNumber, reader, size) (etag, error)`
- `CompleteMultipartUpload(ctx, bucket, key, uploadID, parts) error`
- `AbortMultipartUpload(ctx, bucket, key, uploadID) error`

`parts` is a slice of `multipart.Part{Number int, ETag string}` from a small
`pkg/multipart` package, so the MinIO SDK type does not leak into the use case
and `pkg/` never imports `internal/`. The implementation wraps the minio-go calls
`NewMultipartUpload`, `PutObjectPart`, `CompleteMultipartUpload` and
`AbortMultipartUpload`.

`PutObject` stays for zero-byte files. Nothing else changes in the bucket.
MinIO stays on the private network; `MINIO_PUBLIC_ENDPOINT` is not used by this
flow.

## HTTP Surface

All routes require an authenticated user.

### `POST /htmx/uploads`

Request DTO: `folder`, `name`, `size`, `content_type`.

Response: JSON with `upload_id`, `part_size`, `part_count`.

Errors: invalid body or name, missing folder, too large, quota exceeded, name
taken, storage unavailable.

### `PUT /htmx/uploads/:upload/parts/:part`

Raw body, one part. `Content-Type` is not trusted for storage; it was fixed at
init. `Content-Length` is required. The part number is a positive integer.

Success: `204`.

Errors: unknown upload, upload owned by another user, expired session, bad part
number, wrong size, storage failure, limiter failure.

The body is bounded at the expected part size plus a small allowance. Reading
stops at the limit and returns `413` rather than draining an oversized body.

### `POST /htmx/uploads/:upload/complete`

No body.

Success: the entries fragment for the session's folder, ready to replace
`#drive-entries`, plus a response header carrying the session's folder id. The
controller also sets the existing `entrySaved` trigger.

Errors: missing parts, storage failure, quota exceeded, name conflict.

### `POST /htmx/uploads/:upload/abort`

No body. Verifies ownership, aborts the multipart upload, deletes the session and
releases the reservation. Success is `204`, including when the session is already
gone, so a retry or a late beacon is harmless.

Errors: unknown upload owned by another user. The route is a POST because
`navigator.sendBeacon` cannot send a DELETE.

### Body Limits

- The HTTP server's `BodyLimit` becomes the maximum part size plus a small
  overhead, about 51 MiB. No endpoint accepts more than one part.
- The web `BodyLimit` middleware keeps a small general limit, about 1 MiB, with
  the part route exempted because the part handler enforces its own exact
  limit. The current single `streamPath` parameter is extended to allow the part
  route, or the middleware is mounted per route group.
- `UPLOAD_MAX_BYTES` is no longer used for HTTP body limits. It is only the
  declared whole-file ceiling.

## Client

The drawer form keeps the folder, file and name inputs. It drops
`enctype="multipart/form-data"` and the single-request submit.

`uploadFileWithProgress` in `web/static/js/app.js` becomes a four-stage flow:

1. Read `file.size`, `file.type`, the form's folder and name, and the current
   folder id from `#drive-entries`.
2. `POST /htmx/uploads` and receive part size and count.
3. Slice the file with `file.slice(start, end)` and send parts sequentially
   with XHR, so the existing per-part progress event can drive the bar.
4. `POST /htmx/uploads/:id/complete`, compare the returned folder id, and either
   swap the response HTML into `#drive-entries` or show the toast.

Progress is computed from whole bytes completed plus the current part's
`event.loaded`, divided by total size. Retrying a part does not double-count
completed bytes.

- Parts are sent one at a time. The server enforces one account limit anyway,
  and serial parts make progress and retries simple.
- A part is retried up to three times on network errors or transient 5xx
  responses, with the same part number. A 524 is not retried: it means the part
  budget is wrong for the configured rate, and the same part will fail again.
- Complete is not retried automatically. If the response is lost, refreshing
  shows whether the file landed.
- The submit button is disabled while an upload runs.
- An error is shown in the existing `#upload-error` box.
- The drawer close label and `pagehide` send an abort beacon when an upload is
  still active. Closing the drawer therefore frees the quota immediately; a
  killed browser falls back to the idle expiry.
- The current folder id is kept from init. `#drive-entries` carries the folder
  id on screen as a data attribute (rendered from `response.Drive.FolderID`).
  When completion returns, the client swaps the fragment only if the two ids
  match. Otherwise it leaves the current view alone and shows a small daisyUI
  toast, built by JS from classes already in the compiled CSS.

The browser never loads the whole file into memory; each slice is a `Blob`
backed by the file.

## Config

| Variable | Default | Meaning |
| --- | --- | --- |
| `UPLOAD_MAX_BYTES` | `10737418240` | Whole-file ceiling; 10 GiB |
| `UPLOAD_PART_BYTES` | `52428800` | Preferred part size; 50 MiB |
| `UPLOAD_SESSION_IDLE_TTL` | `1h` | Idle time after the last accepted part before a session is swept |
| `UPLOAD_TIMEOUT` | `5m` | Existing server read timeout; unchanged |

`UPLOAD_PART_BYTES` is a ceiling. The effective part size can shrink to 5 MiB
for a slow account. Values below 5 MiB are rejected at startup or clamped. Very
low positive rates are allowed by the admin form but cannot upload large files
within the proxy timeout; the UI or README should say so.

Add all new variables to `.env.example`, `docker-compose.yml` and the config
tests. Update the `UPLOAD_MAX_BYTES` default in both places.

`config.MultipartOverheadBytes` is no longer used by uploads. Remove it if
nothing else needs it.

## Code Placement

- `internal/entity/upload.go` — `UploadSession`, part math and validation.
  New sentinels in `internal/entity/errors.go`: upload not found, upload part
  mismatch, upload incomplete, upload expired, quota busy. Names follow the
  existing `Err*` style.
- `internal/usecase/file/file.go` — `StartUpload`, `UploadPart`, `CompleteUpload`,
  `AbortUpload` and `SweepUploads` on the file use case. Reuse `storeRow`,
  `checkName`, `storageKey`, `finalizeError` and the existing quota helpers.
- `internal/usecase/contracts.go` — add the five methods to `File`, or split an
  `Upload` interface if the file interface becomes too wide. The controller
  depends on the interface, not the concrete type.
- `internal/repo/contracts.go` — multipart additions to `ObjectStorage`; new
  `UploadSessionRepo` and `TransferLimiter` interfaces.
- `internal/repo/upload/` — Redis session, part hash, live-upload set, sliding
  expiry index and quota lock.
- `internal/repo/transfer/` — Redis GCRA pacer used by the use case.
- `pkg/multipart/` — the neutral `Part{Number, ETag}` type shared by the repo
  interface and the MinIO implementation.
- `pkg/minio/` — multipart methods and the `multipart.Part` conversion.
- `internal/controller/web/upload.go` — start, part, complete and abort
  handlers. Delete the multipart parsing helpers.
- `internal/controller/web/request/` and `response/` — start DTO and JSON
  response.
- `internal/controller/web/router.go` — register the four routes and adjust
  body limits.
- `internal/app/app.go` — wire the new repositories and limiter; start and stop
  the sweeper.
- `pkg/httpserver` — BodyLimit value.
- `web/templates/drive.templ` — form attributes and the `data-folder-id`
  attribute on `#drive-entries`; regenerate with `make templ-generate`.
- `web/static/js/app.js` — chunked uploader, folder-match check, abort beacons
  and the toast helper.
- `web/static/css/app.css` — only if classes change; regenerate with
  `make css-build`.

The old `FileUseCase.Upload`, `parseUpload` and their tests are removed or
rewritten. No second upload path remains.

## Error Handling

- Sentinel errors are mapped to status codes in the controller, as today.
- Unknown or another user's upload responds `404`, never `403`, so upload IDs
  do not leak existence.
- Wrong part size or number responds `422`; an oversized body responds `413`.
- Quota exceeded responds `413`, matching the current upload behavior.
- A name conflict responds `409`.
- Redis or MinIO failures are logged wrapped and return a generic message.
- A failed completion releases the reservation and aborts the multipart upload.
- An abort for an already-finished session is `204`; an abort for another user's
  session is `404`.
- The sweeper logs cleanup failures and leaves the session for the next run.

## Testing

- Entity: part size and count for exact multiples, a remainder, zero bytes, the
  5 MiB floor and the 50 MiB ceiling.
- Use case: init checks quota against `used + reserved`; reserved size is
  released on complete, failure, abort and sweep; a second upload is rejected
  when the first has reserved the last headroom; completion rechecks under the
  user and folder locks; an accepted part slides the idle expiry and a rejected
  part does not.
- Parts: wrong number, wrong length, last-part remainder, retry of the same
  part, out-of-order arrival, missing part at completion.
- Pacer: bytes are delayed at the configured rate; two concurrent consumers
  share one bucket; zero rate skips Redis; Redis error fails closed.
- Handlers: auth required; owner check; body limit; init JSON; complete returns
  the entries fragment and its folder header; abort is idempotent; error
  mapping.
- Client behavior: swap only when the folder id matches; toast otherwise;
  abort beacon on drawer close; 524 is not retried.
- Sweeper: expired session aborts the upload and releases quota; a completed
  session is not touched; running twice is safe.
- Existing upload tests are deleted with the old parser.

Redis repository tests can use a small fake or an in-process Redis test double.
The project currently has no Redis test dependency; adding one is acceptable if
it does not add runtime code.

## Rollout

1. Ship the server and client together; there is no mixed-version requirement.
2. Apply no database migration.
3. Keep `MINIO_PUBLIC_ENDPOINT` as is.
4. Update `.env.example`, `docker-compose.yml` and docs.
5. `make format`, `make linter-golangci`, `make templ-generate`, `make css-build`
   if needed, and the full test suite.

## Approaches Considered

- **Chosen:** app-proxied chunks into a MinIO multipart upload, Redis session and
  pacer, quota reserved in Redis.
- **Rejected:** local temp-file assembly. Simple, but 10 GiB of server disk per
  in-flight upload and a full extra copy.
- **Rejected:** browser-to-MinIO presigned parts. Fastest, but requires an
  unproxied public storage host and moves throttling out of the app.
- **Rejected:** R2 or another storage backend. Solves the proxy cap but changes
  the storage deployment and still needs client changes.
- **Rejected:** strict serial single-request uploads under 100 MB. Does not meet
  the 10 GiB target.
- **Deferred:** resumable sessions across reloads. Explicitly not wanted.
- **Deferred:** global pacing for downloads.

## Left To The Builder

- Exact Redis key names, Lua scripts and lock retry timings.
- Whether the sweeper runs as an `errgroup` goroutine in `app.Run` or a small
  dedicated package.
- Pacer batch size and the exact GCRA burst allowance.
- Whether `File` gains the upload methods or a sibling `Upload` use case is
  introduced; either is acceptable if `storeRow` stays the only quota/name
  writer.
- The retry count and error copy in the drawer.
- Test double choice for Redis.
