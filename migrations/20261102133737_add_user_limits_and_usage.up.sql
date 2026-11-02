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
