# Fiber to Gin Migration

## Goal

Replace all Fiber HTTP framework references in the fileuploader project with Gin (using the eform project's migration as reference).

## Scope

Full project: all Go source files, config, go.mod, and env files containing Fiber references.

## Files to Modify

| File | Change Summary |
|------|---------------|
| `go.mod` | Replace `gofiber/fiber/v2`, `gofiber/swagger`, `ansrivas/fiberprometheus/v2` with `gin-gonic/gin`, `swaggo/gin-swagger`, `swaggo/files`, `zsais/go-gin-prometheus`. Remove transitive Fiber deps. |
| `config/config.go` | Remove `UsePreforkMode` from http struct (Fiber-specific). |
| `.env.example` | Remove `HTTP_USE_PREFORK_MODE`. |
| `pkg/httpserver/server.go` | `App *fiber.App` → `Engine *gin.Engine` + `server *http.Server`. `fiber.New()` → `gin.New()`. `Listen()` → `http.Server.ListenAndServe()`. `ShutdownWithTimeout()` → `server.Shutdown()`. |
| `pkg/httpserver/options.go` | Drop `Prefork()` function. |
| `internal/app/app.go` | `httpServer.App` → `httpServer.Engine` in `restapi.NewRouter` call. |
| `internal/controller/restapi/router.go` | Signals type changes from `*fiber.App` to `*gin.Engine`, swap fiberprometheus→ginprometheus, swagger→gin-swagger. |
| `internal/controller/restapi/middleware/auth.go` | `func(*fiber.Ctx) error` → `gin.HandlerFunc`. Use `c.GetHeader()` / `c.Set()` / `c.AbortWithStatusJSON()`. |
| `internal/controller/restapi/middleware/logger.go` | `gin.HandlerFunc`. Use `c.ClientIP()`, `c.Request.Method`, `c.Request.URL.Path`, `c.Writer.Status()`, `c.Writer.Size()`. |
| `internal/controller/restapi/middleware/recovery.go` | Drop Fiber's recover middleware. Use Gin-native `recover()` + `c.AbortWithStatusJSON()`. |
| `internal/controller/restapi/middleware/auth_test.go` | Replace Fiber test app with Gin + `httptest.Server`. |
| `internal/controller/restapi/v1/router.go` | `fiber.Router` → `*gin.RouterGroup`. Match eform's `NewRoutes` signature pattern. |
| `internal/controller/restapi/v1/error.go` | No return value, use `c.AbortWithStatusJSON()`. |
| `internal/controller/restapi/v1/user.go` | All handler signatures: `*fiber.Ctx` → `*gin.Context`. `BodyParser` → `ShouldBindJSON`. `Locals` → `Get`/`Set`. `UserContext` → `c.Request.Context()`. No return value. |
| `internal/controller/restapi/v1/task.go` | Same pattern as user.go. |
| `internal/controller/restapi/v1/translation.go` | Same pattern as user.go. |

## Key API Mapping

| Fiber | Gin |
|-------|-----|
| `ctx.BodyParser(&v)` | `c.ShouldBindJSON(&v)` / `c.ShouldBindQuery(&v)` |
| `ctx.Params("id")` | `c.Param("id")` |
| `ctx.Query("key")` | `c.Query("key")` |
| `ctx.Locals("key")` | `c.Get("key")` for read, `c.Set("key", val)` for write |
| `ctx.UserContext()` | `c.Request.Context()` |
| `ctx.Status(n).JSON(v)` | `c.JSON(n, v)` |
| `ctx.SendStatus(n)` | `c.Status(n)` |
| `func(ctx *fiber.Ctx) error` | `func(c *gin.Context)` — returns void |
| `fiber.Router` | `*gin.RouterGroup` |
| `app.Listen(addr)` | `http.Server{Handler: engine}.ListenAndServe()` |
| `app.Test(req)` | `httptest.NewServer(engine)` + `http.Client.Do(req)` |

## What Does NOT Change

- `internal/app/app.go` — structure stays; only the field name changes
- Integration tests (`integration-test/`) — use plain `net/http`
- All non-HTTP transport layers (gRPC, RabbitMQ RPC, NATS RPC)
- Business logic (usecases, repos, entities)
- Config parsing library (`caarlos0/env`)
- Logging, JWT, Postgres packages
- Build, Docker, Makefile targets

## Verification

1. `go mod tidy && go mod verify` — dependencies resolve
2. `go build ./cmd/app` — compiles cleanly
3. `go test ./internal/... ./pkg/...` — unit tests pass (including migrated auth test)
4. `golangci-lint run` — linter passes
