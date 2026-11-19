ALTER TABLE users
    ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'active',
    ADD CONSTRAINT users_status_check CHECK (status IN ('pending', 'active', 'disabled'));
