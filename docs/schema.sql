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
  PRIMARY KEY ("id"),
  CONSTRAINT "users_email_key" UNIQUE ("email"),
  CONSTRAINT "users_username_key" UNIQUE ("username"),
  CONSTRAINT "users_role_check" CHECK ((role)::text = ANY ((ARRAY['admin'::character varying, 'user'::character varying])::text[]))
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
-- Create index "folders_parent_idx" to table: "folders"
CREATE INDEX "folders_parent_idx" ON "public"."folders" ("parent_id");
-- Create index "folders_root_key" to table: "folders"
CREATE UNIQUE INDEX "folders_root_key" ON "public"."folders" ("user_id") WHERE (parent_id IS NULL);
-- Create index "folders_sibling_name_key" to table: "folders"
CREATE UNIQUE INDEX "folders_sibling_name_key" ON "public"."folders" ("user_id", "parent_id", (lower((name)::text)));
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
