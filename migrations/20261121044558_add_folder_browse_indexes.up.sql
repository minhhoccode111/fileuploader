CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Each index build holds an ACCESS EXCLUSIVE lock on its table, so a
-- conflicting transaction would queue every read and write behind it. Fail
-- fast instead; the migration can be retried once the lock is free.
SET lock_timeout = '5s';

CREATE INDEX IF NOT EXISTS folders_user_name_idx
    ON folders (user_id, lower(name), id);
CREATE INDEX IF NOT EXISTS files_user_name_idx
    ON files (user_id, lower(name), id);
CREATE INDEX IF NOT EXISTS folders_name_trgm_idx
    ON folders USING gin (lower(name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS files_name_trgm_idx
    ON files USING gin (lower(name) gin_trgm_ops);
