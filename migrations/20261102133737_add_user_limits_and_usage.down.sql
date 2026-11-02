ALTER TABLE users
    DROP COLUMN IF EXISTS storage_quota_bytes,
    DROP COLUMN IF EXISTS storage_used_bytes,
    DROP COLUMN IF EXISTS upload_rate_bytes_per_sec,
    DROP COLUMN IF EXISTS download_rate_bytes_per_sec;