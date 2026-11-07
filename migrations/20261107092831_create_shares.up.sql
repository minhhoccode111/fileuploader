CREATE TABLE IF NOT EXISTS shares (
  "id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "folder_id" uuid NOT NULL,
  "expires_at" timestamp NULL,
  "created_at" timestamp NOT NULL DEFAULT now(),
  "updated_at" timestamp NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "shares_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "shares_folder_id_fkey" FOREIGN KEY ("folder_id") REFERENCES "folders" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS shares_user_id_idx ON shares (user_id);
CREATE INDEX IF NOT EXISTS shares_folder_id_idx ON shares (folder_id);
CREATE UNIQUE INDEX IF NOT EXISTS shares_user_folder_key ON shares (user_id, folder_id);