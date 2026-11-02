# Per-User Storage Quotas and Transfer Speed Limits

## Intent

Give each account a storage quota and upload/download speed limits. Admins set global defaults in configuration and can override them per account from the admin area. Uploads and proxied downloads are throttled in the Go app, and storage usage is tracked so an account cannot exceed its quota.

## Goals

- Per-user storage quota, enforced on upload and folder/file duplicate.
- Per-user upload and download speed limits.
- Global defaults from configuration, with per-user overrides.
- Storage usage counter kept in sync as files and folders are created or deleted.
- Downloads proxied through the app so speed limits apply.
- HTTP range requests supported for resumed/partial downloads.
- Admin UI to view and change per-user limits.

## Non-Goals

- Cross-session/global per-user rate limiting in Redis. Per-request throttling is enough for v1.
- Background reconciliation or quota repair jobs, beyond a documented manual recalculation path.
- Client-side progress UI implementation details; the server only needs to stream with known length and status codes.

## Constraints

- Downloads are currently presigned MinIO URLs. Proxying them changes the download path and removes browser-direct MinIO access.
- Uploads currently use multipart form parsing, which buffers/consumes the request body before the use case runs.
- Storage usage must stay consistent with file rows. All usage changes happen inside the existing unit of work.
- Admins are also subject to limits, but can raise their own.

## Data Model

Add columns to `users`:

- `storage_quota_bytes bigint not null default 0` — `0` means use the global default.
- `storage_used_bytes bigint not null default 0`
- `upload_rate_bytes_per_sec int not null default 0` — `0` means use the global default.
- `download_rate_bytes_per_sec int not null default 0` — `0` means use the global default.

Global defaults live in config:

- `STORAGE_DEFAULT_QUOTA_BYTES`
- `UPLOAD_DEFAULT_RATE_BYTES_PER_SEC`
- `DOWNLOAD_DEFAULT_RATE_BYTES_PER_SEC`

A value of `0` in config means unlimited for that limit. Per-user `0` means inherit the global default.

Effective limits are resolved in the use case or a small domain helper:

- effective quota = user value if non-zero, else global default
- effective upload rate = user value if non-zero, else global default
- effective download rate = user value if non-zero, else global default

`User` and `UserListItem` carry the raw and effective values needed by controllers and admin views. Repository methods update `storage_used_bytes` under a locked user row.

## Storage Usage Accounting

`storage_used_bytes` counts bytes of files currently stored for that user.

Rules:

- Upload adds `file.Size`.
- File duplicate adds `source.Size`.
- Folder duplicate adds the sum of all copied file sizes.
- File delete subtracts `file.Size`.
- Folder delete subtracts the sum of all files in the subtree.
- Move and rename do not change usage.

Implementation:

- `FileUseCase.Upload` checks `used + size <= quota` inside the transaction after locking the user and folder. It stores the file, then increments usage.
- `FileUseCase.Delete` decrements usage inside the transaction before returning.
- `FileUseCase.Duplicate` checks and increments usage for the copied file.
- `FolderUseCase.Duplicate` checks and increments usage for all copied files.
- `FolderUseCase.Delete` reads file sizes in the subtree before deleting rows, then decrements usage.

Lock ordering remains `LockUser` first, then folder locks, matching existing behavior.

## Upload Speed Limit

v1 throttles the upload stream per request.

Because `ctx.BodyParser` already consumes the multipart body, the controller must stop using Fiber's parser for the file bytes. Instead, parse the multipart form manually with a throttled reader:

- Resolve user and effective upload rate.
- Wrap the request body with a rate limiter.
- Read the multipart form from the wrapped reader.
- Stream the file into `storage.PutObject` through the same throttled reader.
- Enforce both the global `UPLOAD_MAX_BYTES` and the user quota.

If the effective rate is unlimited, no throttling is applied.

## Download Proxy and Range Requests

Replace the presigned-URL redirect with a streaming handler:

- Route: `GET /files/:file/download`
- Resolve user and file.
- Resolve effective download rate.
- Ask storage for an object stream with optional offset and length.
- If `Range` is present and valid, return `206 Partial Content` with `Content-Range`, `Accept-Ranges: bytes`, and the requested byte range.
- If no range is present, return `200 OK` with full `Content-Length`.
- Set `Content-Disposition` with the file name and `Content-Type`.
- Stream through a throttled writer when a download rate is configured.
- Support `HEAD` semantics if Fiber route registration allows; otherwise keep GET only for v1.

Storage interface change:

- Add `GetObject(ctx, bucket, key, offset, length) (io.ReadCloser, ObjectInfo, error)` or equivalent.
- MinIO implementation uses `GetObject` with `GetObjectOptions` offset/length.

## Error Handling

Add sentinel errors as needed:

- `ErrStorageQuotaExceeded`
- `ErrTransferRateLimited` only if a distinct status is desired; otherwise return `429 Too Many Requests` directly.

Controllers map:

- quota exceeded -> `413 Request Entity Too Large` or `409 Conflict`; choose one and use consistently.
- invalid range -> `416 Range Not Satisfiable`
- storage cleanup failure -> existing `ErrStorageCleanup` handling.

## Admin UI

Use the existing admin user edit flow. Add fields for:

- storage quota bytes
- upload rate bytes/sec
- download rate bytes/sec
- read-only storage used bytes

Global defaults remain in `.env` / config and docker-compose. Per-user `0` means inherit.

## Migration

- Add the four columns to `users`.
- Backfill `storage_used_bytes` by summing `files.size` grouped by `user_id`.
- Add a manual recalculation helper or admin action for repair if drift is suspected.

## Testing

- Unit tests for effective limit resolution.
- Upload rejects when `used + size > quota`.
- Delete frees quota.
- Duplicate file/folder updates quota.
- Folder delete frees all contained file bytes.
- Speed limiter wraps bytes correctly at the configured rate.
- Range parsing returns correct status and headers.
- Controller download streams full file and partial range.

## Approaches Considered

- **Chosen:** in-app per-request throttling plus DB quota counter.
- **Deferred:** Redis global per-user rate limits.
- **Rejected for v1:** external-only throttling, because per-user dynamic limits become hard to manage and test.