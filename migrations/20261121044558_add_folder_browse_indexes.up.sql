CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS folders_user_name_idx
    ON folders (user_id, lower(name), id);
CREATE INDEX IF NOT EXISTS files_user_name_idx
    ON files (user_id, lower(name), id);
CREATE INDEX IF NOT EXISTS folders_name_trgm_idx
    ON folders USING gin (lower(name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS files_name_trgm_idx
    ON files USING gin (lower(name) gin_trgm_ops);
