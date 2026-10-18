# File Uploader — The Odin Project

Go clean-architecture service. One domain (`user`) exposed over REST (Fiber) from a shared
use-case layer. Module path: `github.com/minhhoccode111/fileuploader`. The Go version is declared in
`go.mod`.

## Commands — drive everything through the Makefile

The `Makefile` loads `.env` (falling back to `.env.example`) and exports it into every target, and
several targets carry flags that matter. Running the underlying tool directly means running it with
different settings than CI. Use the target, not the tool.

| Task                                       | Use this                                | Not this                                                                                 |
| ------------------------------------------ | --------------------------------------- | ---------------------------------------------------------------------------------------- |
| Install tool binaries (`migrate`, linters) | `make bin-deps`                         | `go install ...`                                                                         |
| Lint                                       | `make linter-golangci`                  | `golangci-lint run`                                                                      |
| Format                                     | `make format`                           | `gofmt` — the target runs `go fix`, `gofumpt`, and `gci` with the repo's import grouping |
| Tidy / verify modules                      | `make deps`                             | `go mod tidy`                                                                            |
| Vulnerability scan                         | `make deps-audit`                       | `govulncheck ./...`                                                                      |
| Start Postgres                             | `make compose-up`                       | `docker compose up`                                                                      |
| Start the whole stack including the app    | `make compose-up-all`                   | `docker compose up`                                                                      |
| Tear down                                  | `make compose-down`                     | `docker compose down`                                                                    |
| Run the app locally                        | `make run`                              | `go run ./cmd/app` — the target builds with `-tags migrate`                              |
| Run the dev stack (hot reload)             | `make dev`                              | running air + tailwind by hand                                                           |
| Regenerate templ                           | `make templ-generate`                   | `templ generate`                                                                         |
| Build CSS                                  | `make css-build`                        | invoking the tailwind binary by hand                                                     |
| Download frontend assets                   | `make assets`                           | `curl`-ing htmx/alpine/daisyUI by hand                                                   |
| Create a migration                         | `make migrate-create <name>`            | `migrate create ...`                                                                     |
| Apply the next / all migrations            | `make migrate-up`, `migrate-up-all`     | `migrate -path ... up 1` / `up`                                                          |
| Revert the last / all migrations           | `make migrate-down`, `migrate-down-all` | `migrate -path ... down 1` / `down -all`                                                 |
| Redo the last migration                    | `make migrate-redo`                     | `migrate ... down 1 && ... up 1`                                                         |
| Inspect migrations                         | `make migrate-status`, `migrate-list`   | `migrate ... version` / `ls migrations`                                                  |
| Full check before pushing                  | `make pre-commit`                       | running the steps by hand                                                                |

`make help` lists every target.

Never claim a change is done without `make format` and `make linter-golangci` passing.

## Dependency rule

```
cmd/app → internal/app → internal/controller/*  ─┐
                       → internal/repo/*        ─┤→ internal/usecase (interfaces)
                                                 └→ internal/entity
```

- `internal/entity` — domain types and sentinel errors. Imports nothing from this module.
- `internal/usecase/contracts.go` — the interfaces controllers call. `internal/usecase/<domain>/`
  implements them.
- `internal/repo/contracts.go` — the interfaces use cases call. `internal/repo/persistent/<domain>/`
  (Postgres) implement them.
- `internal/controller/restapi/v1/` — the REST transport, with its own `request/` and `response/`
  DTOs.
- `pkg/` — transport-agnostic infrastructure (servers, logger, jwt, postgres). Must not
  import `internal/`.

Inner layers never import outer ones. Wiring happens exactly once, in `internal/app/app.go`
(`initUseCases`, `initServers`).

## Where code goes

Decide by asking what the code knows about:

| The code knows about…                                         | It belongs in                                   | Shape                                              |
| ------------------------------------------------------------- | ----------------------------------------------- | -------------------------------------------------- |
| nothing but the domain                                        | `internal/entity/<name>.go`                     | struct + methods (`User.IsValid`, …)               |
| a domain rule that spans repositories                         | `internal/usecase/<domain>/<domain>.go`         | method on `UseCase`                                |
| SQL, a table, a driver error code                             | `internal/repo/persistent/<domain>/<domain>.go` | method on `Repo`                                   |
| an outbound HTTP API                                          | `internal/repo/webapi/<name>.go`                | method on the webapi struct (add the package back) |
| an HTTP status or a message envelope                          | `internal/controller/restapi/v1/<domain>.go`    | handler                                            |
| a browser page or fragment                                    | `web/templates/<name>.templ`                    | templ component                                    |
| a server, pool, client or middleware with no domain knowledge | `pkg/<name>/`                                   | reusable package                                   |

Layout rules:

- One file per domain per layer, named after the domain (`user.go`).
- Package name matches the directory name.
- Constructors are always `New`, return the _interface_ from `contracts.go`, and take their
  dependencies as arguments. No package-level state — `gochecknoglobals` and `gochecknoinits` are on.
- Unexported package constants are prefixed with an underscore (`_defaultEntityCap`). `mnd` rejects
  bare numeric literals, so add a constant rather than inlining one.

## Frontend

`web/` holds everything the browser gets. It is plain static output — no Node at runtime.

- `web/templates/*.templ` — pages, compiled to Go by templ. `*_templ.go` is generated and
  **committed**; run `make templ-generate` after every `.templ` edit or the build serves stale HTML.
- `web/app.css` — the Tailwind input. `@plugin "./plugins/daisyui.js"` pulls in daisyUI.
- `web/static/css/app.css` — the Tailwind output. **Committed**, so the Docker image needs no Node.
  Never hand-edit it: change classes or `web/app.css`, then `make css-build`.
- `web/plugins/daisyui.js` and `web/static/js/{htmx,alpine}.min.js` — pinned vendor files,
  fetched by `make assets`. All versions live in the `Makefile` (and `tool` in `go.mod`); bump them
  there, never in the file.

The app serves `web/static` at `/static` (`app.Static`) and renders `web/templates` at `/`.

`make dev` runs air (rebuilds Go and templ on save) together with the Tailwind watcher. It needs
Postgres up first: `make compose-up`. air only watches `.go` and `.templ`; CSS changes are handled
by the Tailwind watcher, so a class edit does not restart the server.

Alpine shorthands (`@click`, `:class`) pass through templ unchanged; use `x-on:` / `x-bind:` only if
you prefer the long form. To feed Go data into `x-data`, use a templ expression:
`<div x-data={ templ.JSONString(data) }>`. See `.opencode/skills/alpinejs/SKILL.md`.

## The handler contract

Every REST controller method does the same six things in the same order. Copy the shape from a
neighbouring handler.

1. **Get the caller.** `ctx.Locals("userID").(string)`. A failure here is an auth error, not a 500.
2. **Decode into a DTO** from `restapi/v1/request/`. Never decode into an `entity` type, and never
   pass a `request.*` type into a use case.
3. **Validate the DTO** with the injected `*validator.Validate` (`r.v.Struct(req)`). Structural
   validation (required, min, max, oneof) lives here. Domain validation — anything that needs to
   know the rules, like a status transition — lives on the entity or the use case and must not be
   duplicated in the controller.
4. **Call the use case** with the request context: `ctx.UserContext()`. `context.Context` is the
   first parameter of every method that crosses a layer.
5. **Map the error.** `errors.Is` against the `entity.Err*` sentinels → an HTTP status. Log the
   wrapped error, return a generic message to the caller.
6. **Encode the response.** Return an `entity` type directly only when its JSON shape is already
   the API shape — `entity.User` is safe this way because `PasswordHash` is tagged `json:"-"`.
   Anything with a wrapper, a projection or a different field set gets a type in
   `restapi/v1/response/` (`response.Token`, `response.Error`).

## Conventions that will bite you

**Error style.** Domain failures are sentinel errors in `internal/entity/errors.go`
(`ErrUserNotFound`, `ErrUserAlreadyExists`, `ErrInvalidCredentials`). Every other error is wrapped
with the layer path: `fmt.Errorf("TaskUseCase - Create - uc.repo.Store: %w", err)`. The `err113`
linter rejects `errors.New`/`fmt.Errorf` for new dynamic errors — add a sentinel to `entity`
instead. Controllers are the only place that translates a sentinel into an HTTP status.

**Config is environment-only.** `config/config.go` parses env vars with `caarlos0/env`. A new
setting needs the struct field, an entry in `.env.example`, and — if the container needs it — the
`environment` block in `docker-compose.yml`. Fields tagged `required` make the app fail at
startup.

**The linter is strict and non-obvious.** `.golangci.yml` enables ~45 linters. The ones that
reject otherwise-fine code: `wsl_v5` and `nlreturn` (blank line required before `return` and
around blocks), `funlen` (65 lines / 40 statements), `gocyclo` (10), `gocognit` (15), `mnd`,
`gochecknoglobals`, `exhaustive` (switches over enum types need every case or a `default`), `dupl`
(threshold 100 — copy-pasting a handler trips it).

## Adding a use case to an existing domain

1. Add the method to `internal/usecase/contracts.go`.
2. Implement it in `internal/usecase/<domain>/<domain>.go`.
3. If it needs new persistence, repeat the same steps in `internal/repo/contracts.go` and
   `internal/repo/persistent/<domain>/`.
4. Expose it on REST: handler in `internal/controller/restapi/v1/<domain>.go` + route in that
   package's `router.go`.

## Adding a whole domain

Work outward from the entity. Every step has a counterpart in the existing `user` domain — read it
before writing.

1. **Entity** — `internal/entity/<domain>.go` with the type and its invariants; new sentinel errors
   go in `internal/entity/errors.go`.
2. **Migration** — `make migrate-create create_<table>`, then fill both the `.up.sql` and the
   `.down.sql`. A missing down-migration is a review failure.
3. **Repo contract** — add the interface to `internal/repo/contracts.go`.
4. **Repo implementation** — `internal/repo/persistent/<domain>/<domain>.go` (`package <domain>`,
   Squirrel queries, driver errors mapped to sentinels) and a `New` returning the interface.
5. **Use-case contract** — add the interface to `internal/usecase/contracts.go`.
6. **Use-case implementation** — `internal/usecase/<domain>/<domain>.go`, same `New` pattern.
7. **Wiring** — add a field to `useCases` in `internal/app/app.go`, construct repo and use case in
   `initUseCases`, and thread the use case through `initServers` into the router.
8. **REST** — DTOs in `restapi/v1/request/` and `response/`, handlers in
   `restapi/v1/<domain>.go`, registration in that transport's `v1/router.go`, and the use case
   added to the `V1` controller struct in `restapi/v1/controller.go`.

## API versioning

Routes are grouped under a version package (`restapi/v1/`). A v2 means a new sibling package with
the same layout, registered alongside v1 in the transport's top-level `router.go` with an
`app.Group("/v2")`. v1 stays untouched.

## Database

`migrations/` holds golang-migrate pairs (`<timestamp>_<name>.up.sql` / `.down.sql`). Create with
`make migrate-create <name>`, apply with `make migrate-up`. The app applies them itself at startup
only when built with the `migrate` build tag (`internal/app/migrate.go`) — that is what `make run`
and the Dockerfile do. Queries are built with Squirrel via the embedded `*postgres.Postgres`;
there is no ORM and no raw string concatenation.

`make migrate-up` / `make migrate-down` run exactly one step and print `no change` when there is
nothing to do. golang-migrate's raw `up 1` / `down 1` demand _exactly_ one migration and fail with a
misleading `error: file does not exist` when none is left, so the Makefile wraps them — do not call
the subcommand bare. Never run a bare `migrate down`: with no argument it prompts to revert _all_
migrations.

Object storage is S3-compatible, run as the `minio` service (`docker.io/pgsty/silo`, a maintained
MinIO fork — the upstream image is no longer published) on `:9000` (S3) and `:9001` (console).
