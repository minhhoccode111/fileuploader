CREATE TABLE IF NOT EXISTS folders (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    parent_id UUID REFERENCES folders (id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS folders_sibling_name_key
    ON folders (user_id, parent_id, lower(name));

CREATE UNIQUE INDEX IF NOT EXISTS folders_root_key
    ON folders (user_id)
    WHERE parent_id IS NULL;

CREATE INDEX IF NOT EXISTS folders_parent_idx ON folders (parent_id);

CREATE TABLE IF NOT EXISTS files (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    folder_id UUID NOT NULL REFERENCES folders (id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    content_type VARCHAR(255) NOT NULL,
    storage_key VARCHAR(512) NOT NULL UNIQUE,
    size BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    updated_at TIMESTAMP NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS files_folder_name_key
    ON files (folder_id, lower(name));

CREATE INDEX IF NOT EXISTS files_folder_idx ON files (folder_id);

-- Accounts that registered before the drive existed get their root now, so
-- every account can list, create and upload from its first visit.
INSERT INTO folders (id, user_id, name)
SELECT gen_random_uuid(), users.id, '/'
FROM users
WHERE NOT EXISTS (
    SELECT 1 FROM folders WHERE folders.user_id = users.id AND folders.parent_id IS NULL
);
