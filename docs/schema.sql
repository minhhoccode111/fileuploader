-- pg_trgm is a deployment requirement: the trigram indexes below need it.
-- Atlas does not dump extensions, so this line keeps the dump replayable.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Add new schema named "public"
CREATE SCHEMA IF NOT EXISTS "public";
-- Set comment to schema: "public"
COMMENT ON SCHEMA "public" IS 'standard public schema';
-- Create "schema_migrations" table
CREATE TABLE "public"."schema_migrations" (
  "version" bigint NOT NULL,
  "dirty" boolean NOT NULL,
  PRIMARY KEY ("version")
);
-- Create "users" table
CREATE TABLE "public"."users" (
  "id" uuid NOT NULL,
  "username" character varying(255) NOT NULL,
  "email" character varying(255) NOT NULL,
  "password_hash" character varying(255) NOT NULL,
  "created_at" timestamp NOT NULL DEFAULT now(),
  "updated_at" timestamp NOT NULL DEFAULT now(),
  "role" character varying(16) NOT NULL DEFAULT 'user',
  "storage_quota_bytes" bigint NOT NULL DEFAULT 0,
  "storage_used_bytes" bigint NOT NULL DEFAULT 0,
  "upload_rate_bytes_per_sec" bigint NOT NULL DEFAULT 0,
  "download_rate_bytes_per_sec" bigint NOT NULL DEFAULT 0,
  "status" character varying(16) NOT NULL DEFAULT 'active',
  PRIMARY KEY ("id"),
  CONSTRAINT "users_email_key" UNIQUE ("email"),
  CONSTRAINT "users_username_key" UNIQUE ("username"),
  CONSTRAINT "users_role_check" CHECK ((role)::text = ANY ((ARRAY['admin'::character varying, 'user'::character varying])::text[])),
  CONSTRAINT "users_status_check" CHECK ((status)::text = ANY ((ARRAY['pending'::character varying, 'active'::character varying, 'disabled'::character varying])::text[]))
);
-- Create "folders" table
CREATE TABLE "public"."folders" (
  "id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "parent_id" uuid NULL,
  "name" character varying(255) NOT NULL,
  "created_at" timestamp NOT NULL DEFAULT now(),
  "updated_at" timestamp NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "folders_parent_id_fkey" FOREIGN KEY ("parent_id") REFERENCES "public"."folders" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "folders_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "folders_name_trgm_idx" to table: "folders"
CREATE INDEX "folders_name_trgm_idx" ON "public"."folders" USING GIN ((lower((name)::text)) public.gin_trgm_ops);
-- Create index "folders_parent_idx" to table: "folders"
CREATE INDEX "folders_parent_idx" ON "public"."folders" ("parent_id");
-- Create index "folders_root_key" to table: "folders"
CREATE UNIQUE INDEX "folders_root_key" ON "public"."folders" ("user_id") WHERE (parent_id IS NULL);
-- Create index "folders_sibling_name_key" to table: "folders"
CREATE UNIQUE INDEX "folders_sibling_name_key" ON "public"."folders" ("user_id", "parent_id", (lower((name)::text)));
-- Create index "folders_user_name_idx" to table: "folders"
CREATE INDEX "folders_user_name_idx" ON "public"."folders" ("user_id", (lower((name)::text)), "id");
-- Create "files" table
CREATE TABLE "public"."files" (
  "id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "folder_id" uuid NOT NULL,
  "name" character varying(255) NOT NULL,
  "content_type" character varying(255) NOT NULL,
  "storage_key" character varying(512) NOT NULL,
  "size" bigint NOT NULL,
  "created_at" timestamp NOT NULL DEFAULT now(),
  "updated_at" timestamp NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "files_storage_key_key" UNIQUE ("storage_key"),
  CONSTRAINT "files_folder_id_fkey" FOREIGN KEY ("folder_id") REFERENCES "public"."folders" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "files_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "files_folder_idx" to table: "files"
CREATE INDEX "files_folder_idx" ON "public"."files" ("folder_id");
-- Create index "files_folder_name_key" to table: "files"
CREATE UNIQUE INDEX "files_folder_name_key" ON "public"."files" ("folder_id", (lower((name)::text)));
-- Create index "files_name_trgm_idx" to table: "files"
CREATE INDEX "files_name_trgm_idx" ON "public"."files" USING GIN ((lower((name)::text)) public.gin_trgm_ops);
-- Create index "files_user_name_idx" to table: "files"
CREATE INDEX "files_user_name_idx" ON "public"."files" ("user_id", (lower((name)::text)), "id");
-- Create "shares" table
CREATE TABLE "public"."shares" (
  "id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "folder_id" uuid NOT NULL,
  "expires_at" timestamp NULL,
  "created_at" timestamp NOT NULL DEFAULT now(),
  "updated_at" timestamp NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "shares_folder_id_fkey" FOREIGN KEY ("folder_id") REFERENCES "public"."folders" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "shares_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "public"."users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "shares_folder_id_idx" to table: "shares"
CREATE INDEX "shares_folder_id_idx" ON "public"."shares" ("folder_id");
-- Create index "shares_user_folder_key" to table: "shares"
CREATE UNIQUE INDEX "shares_user_folder_key" ON "public"."shares" ("user_id", "folder_id");
-- Create index "shares_user_id_idx" to table: "shares"
CREATE INDEX "shares_user_id_idx" ON "public"."shares" ("user_id");
