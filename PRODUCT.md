# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Primary user is a learner working through The Odin Project, running the app locally to study and extend it. The app's own accounts are demo accounts: a registered user manages their own files, and an admin (granted out of band in the database) manages accounts. No external customers or organizations are represented.

## Product Purpose

A server-rendered file storage application used as a learning project. It demonstrates a Go clean-architecture service (Fiber REST + templ/htmx SSR) backed by Postgres, Redis sessions, and S3-compatible object storage (MinIO): registration and sign-in, a personal folder/file drive, upload/download/move/duplicate/rename/delete, and an admin area for account management. Success means a runnable local stack and a codebase a learner can read and extend.

## Positioning

The mechanism a neighboring project could not truthfully copy is the combination of a single shared use-case layer serving both a REST API and an htmx-driven server-rendered UI, with the role and session index stored in Redis and object bytes reached only through presigned URLs.

## Operating Context

Runs locally through Docker Compose (Postgres on 5433, Redis, MinIO on 9000/9001, optional nginx) with `make dev` for hot reload. Configuration is environment-only. The browser talks to the app on port 8090; every request waits 100-200ms by default to exercise slow-network behavior. Migrations apply at startup when built with the `migrate` tag.

## Capabilities and Constraints

- Accounts: register (always role `user`), sign in/out, session TTL 24h; first admin is granted with `UPDATE users SET role = 'admin'`. A signed-in account changes its own username, email and password at `/account`; a password change requires the current password, keeps the current session and ends the others.
- Drive: nested folders and files per account; root folder `/` is created with the account and cannot be deleted or renamed; create/rename/delete folders; upload (default max 100 MiB), download, rename, move, duplicate, delete files.
- Admin: filter/search/sort/paginate accounts; edit username, email, role, password; changing a role or password ends all of that account's live sessions.
- Storage: private bucket; browser reaches objects through presigned URLs; storage keys are derived from identifiers, so renames never touch stored objects.
- Roles: `users.role` is constrained to `admin`/`user` by a CHECK constraint; role travels with the Redis session as a snapshot, so a guard reads Redis and never the database.
- Sessions: Redis holds `{user_id, role}` under `session:<id>` plus an index under `user-sessions:<user_id>`.
- Constraints: max upload size and request timeout configurable; fake latency max of 0 turns the delay off; no raw SQL string concatenation (Squirrel).

## Evidence on Hand

Repository source, migrations, and `docs/schema.sql` (when generated). No real testimonials, customers, benchmarks, pricing, or deployment claims exist and none should be invented.

## Product Principles

- Keep domain rules in entities and use cases; keep SQL in repositories and HTTP in controllers.
- Preserve the learning value of the codebase: clarity and small, named layers over cleverness.
- Private storage by default; never expose object bytes or password hashes.
- Keep admin views bookmarkable and self-consistent under htmx swaps.
- Prefer confirmed facts over invented product claims.

## Accessibility & Inclusion

No product-specific accessibility requirement was established beyond semantic HTML, keyboard-operable controls, and screen-reader labels already present in the templates.