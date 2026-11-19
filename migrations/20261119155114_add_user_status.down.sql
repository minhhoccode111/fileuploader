ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_status_check,
    DROP COLUMN IF EXISTS status;
