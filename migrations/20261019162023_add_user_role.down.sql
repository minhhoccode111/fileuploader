-- The CHECK constraint depends on the column, so it goes first.
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_role_check;

ALTER TABLE users
    DROP COLUMN IF EXISTS role;
