-- Roles are plain text, not an enum: an enum cannot take a new value inside a
-- transaction, and a CHECK constraint is trivial to widen later.
ALTER TABLE users
    ADD COLUMN role VARCHAR(16) NOT NULL DEFAULT 'user',
    ADD CONSTRAINT users_role_check CHECK (role IN ('admin', 'user'));
