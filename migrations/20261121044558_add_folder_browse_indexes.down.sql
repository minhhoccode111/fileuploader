-- pg_trgm stays installed: it may predate this migration and other objects may
-- use it, so a rollback only removes the indexes this migration created.
DROP INDEX IF EXISTS folders_user_name_idx;
DROP INDEX IF EXISTS files_user_name_idx;
DROP INDEX IF EXISTS folders_name_trgm_idx;
DROP INDEX IF EXISTS files_name_trgm_idx;
