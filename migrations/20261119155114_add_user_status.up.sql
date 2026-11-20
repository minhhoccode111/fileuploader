ALTER TABLE users
    ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'pending',
    ADD CONSTRAINT users_status_check CHECK (status IN ('pending', 'active', 'disabled'));

-- Accounts that existed before this migration keep their access; only future
-- inserts fall back to pending.
UPDATE users
SET status = 'active'
WHERE status = 'pending';
