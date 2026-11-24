# Observability: Request IDs, Metrics, and Pprof

## Intent

Make the running service explain itself without a tracing stack. Today a request log
line shows the method, path, status and size, but nothing ties a later error log to the
request that caused it, and there is no way to see rates, latency or process state
without reading the container's stdout.

The work is three tiers, cheapest first: a request ID and latency on every log line,
Prometheus metrics, and `net/http/pprof` for on-demand profiles. Tracing (OTel) stays
out: one service, no cross-service waterfall to unwind.

## Goals

- Every response carries an `X-Request-ID` header and a matching log line with status,
  bytes and latency.
- An error logged by a handler carries the same request ID as the access line.
- `/metrics` exposes request count, latency, bytes, in-flight requests, build info and
  Postgres pool gauges in Prometheus format.
- CPU, heap, goroutine and trace profiles are reachable while the app runs.
- Neither metrics nor pprof is reachable through the public listener.
- No new configuration is required to start the app locally.

## Non-Goals

- OTel tracing, spans, or propagation into Postgres, Redis or MinIO. Revisit when there
  is a second service.
- Log shipping (Loki, ELK), alert rules, Grafana dashboards, SLOs.
- Business metrics (uploads per day, bytes stored per account). Cardinality and privacy
  need their own conversation.
- Per-user, per-file or per-IP labels. Every label must come from a bounded set.
- Profiling the whole process continuously. pprof is on-demand only.

## Anti-Goals

- An observability component that can stop the app. A busy or unreachable observe port
  is logged and the app keeps serving.
- Request handling that gets slower by a measurable amount. Middleware adds two clock
  reads and a few map-free counter updates.
- A public, unauthenticated pprof endpoint. Heap profiles can contain secrets.

## Constraints

- The web transport is Fiber over fasthttp. `net/http/pprof` needs a `net/http` server,
  so it cannot share the Fiber listener without an adapter.
- `pkg/` must not import `internal/`. Wiring lives in `internal/app/app.go`.
- `gochecknoglobals` is enabled, so no package-level collectors, registries or slices.
  Every Prometheus object lives on a struct built by `New`.
- Prometheus must use an explicit registry, not the default one, so tests do not share
  state and so the registry contains only what this app registers.
- `mnd` rejects bare numeric literals, so histogram buckets and limits become named
  constants.

## Decisions

These are my calls, made without asking. Each is reversible.

1. **One internal listener serves `/metrics` and `/debug/pprof`.** Default
   `OBSERVE_ADDR=127.0.0.1:9090`, gated by `OBSERVE_ENABLED` (default `true`). The gate
   is a separate boolean because `caarlos0/env` applies `envDefault` when a variable is
   set but empty, so `OBSERVE_ADDR=""` would silently mean "the default address", not
   "off". Prometheus cannot present a session cookie, so a guarded route cannot serve
   it. Once a separate listener exists, pprof belongs there too: the binding is a
   stronger control than an admin session, it is not subject to CSRF, fake latency or
   the body limit, and it keeps unauthenticated diagnostics off the public router.
2. **The app generates request IDs; an inbound one is honored only when it is safe.**
   `X-Request-ID` is accepted when it matches `[A-Za-z0-9._-]{8,64}`; anything else is
   replaced by a fresh UUID. nginx sets the header from `$request_id`, so the proxy log
   and the app log name the same request. Trusting an arbitrary header would let a
   client write the log.
3. **`logger.Interface` gains `WithRequestID(id) Interface`.** The repository has one
   implementation, so the cost is one method plus a small refactor to let tests build a
   logger around a `bytes.Buffer`. It keeps request IDs as a structured zerolog field
   instead of string concatenation at 35 call sites.
4. **Metrics use route templates, never raw paths.** `ctx.Route().Path` gives
   `/folders/:folder`; an unmatched path gets `route="unmatched"`. After `Next()`
   returns, `ctx.Route()` is the last route that matched, and for a 404 that is the
   last `app.Use` entry (`Method == "USE"`, `Path == "/"`). The middleware checks that
   method, not the path, to decide `unmatched`, so every 404 does not collapse into
   `route="/"`.
5. **Health and static requests are counted but not logged.** Probes every few seconds
   and one line per asset would drown the useful lines.
6. **Pool gauges read `pgxpool.Stat()` at scrape time** through `prometheus.NewGaugeFunc`;
   the app does not maintain a parallel copy of pool state.
7. **`/healthz` keeps its fake latency.** It is a development aid and the internal
   listener bypasses Fiber, so changing it is out of scope.
8. **Prefork and the observe listener are mutually exclusive, and prefork loses.**
   Fiber's prefork master does not serve; it re-execs the binary per worker and every
   copy runs `Run`, so every process would race for `OBSERVE_ADDR`, and the winner
   could be the idle master. A per-worker port cannot be derived because Fiber exposes
   no child index. The app starts the listener only when prefork is off; with prefork
   on it logs one warning and continues. The shipped `.env.example` and compose file
   switch prefork to `false`: the stack already sets `GOMAXPROCS=1`, so prefork buys
   one extra process and no extra parallelism.
9. **Status comes from the error, not from a bare `500`.** A Fiber 404 or 405 arrives
   as `*fiber.Error` with a `Code`; the client gets that code, so the log and the metric
   must record it too. Only an error that carries no status becomes `500`.
10. **Unknown body sizes count as zero bytes.** `responseBodySize` returns `-1` for a
    chunked stream, and `prometheus.Counter.Add(-1)` panics. `RequestFinished` clamps a
    negative count to zero, so the ZIP download route cannot crash the process. The
    access log keeps `-1` to mean "unknown".
11. **Containers on `app_network` can reach `app:9090`, and that is accepted.** They are
    the app's own stack (db, redis, minio, nginx) and already share the deployment's
    secrets. The boundary that matters is the public one: the port is published only on
    host loopback and never routed through nginx. A future in-network Prometheus joins
    the same network on purpose.

## Tier 1 — Request ID and Latency

### Middleware order

```text
RequestID -> Logger -> Metrics -> Recovery -> Latency -> CSRF -> BodyLimit -> routes
```

- `RequestID` runs first so both loggers can read the ID.
- `Metrics` sits outside `Recovery`, so a panic is caught before the counter records the
  status. Place it inside `Logger` so the access line still logs either way.
- A handler that returns an error gets its final status in both the log line and the
  metric: `*fiber.Error` contributes its `Code` (404, 405, …), anything else is `500`.
  Fiber's error handler runs after the middleware chain returns, so the middleware must
  read the error itself instead of `ctx.Response().StatusCode()`.
- `Recovery` stays inside `Logger`, so a panic still produces one access line and one
  panic line with the same ID.

### Request ID middleware

`internal/controller/web/middleware/requestid.go`:

- Constructor `RequestID() fiber.Handler` compiles the validation expression once.
- Reads `X-Request-ID`. A valid value is kept; otherwise `uuid.NewString()` is used.
- Stores the ID under `Locals("request_id")` and writes it to the response header before
  the handler runs, so even a failed request carries it.
- Exposes `RequestIDFrom(ctx)` for other middleware and the controller.

The locals key is a constant shared with `middleware.Logger`, `middleware.Recovery` and
`Controller.requestLogger`.

### Access log line

`middleware.Logger` changes from a fixed string to:

```text
ip - METHOD path - status bytes latency
```

for example:

```text
10.0.0.7 - GET /folders/6f1c - 200 18342 12.4ms
```

The request ID is a zerolog field added by `WithRequestID`, not part of the message. A
test that reads the JSON asserts `request_id` is present.

Log lines for `/healthz` and anything under `/static/` are skipped. Metrics still count
them.

### Handler error logs

`Controller` gains:

```go
func (c *Controller) requestLogger(ctx *fiber.Ctx) logger.Interface
```

It returns `c.l.WithRequestID(id)` when the ID is present and `c.l` otherwise. Every
`c.l.Error(...)`, `c.l.Warn(...)`, `c.l.Info(...)` and `c.l.Debug(...)` inside a handler
moves to `c.requestLogger(ctx).X(...)`. `middleware.Recovery` does the same through the
helper, so a panic line and its access line share an ID.

Use cases and repositories do not log; they wrap errors. The controller logs the final
error, which is where the ID is needed.

## Tier 2 — Prometheus Metrics

### Package

`pkg/metrics/metrics.go`, no domain knowledge and no globals:

```go
type Metrics struct { /* registry, collectors */ }

type Options struct {
    Version   string
    PoolStats func() PoolStats // nil disables the pool gauges
}

type PoolStats struct {
    Total, Idle, Acquired, Max int32
}

func New(opts Options) *Metrics
func (m *Metrics) Handler() http.Handler
func (m *Metrics) RequestStarted()
func (m *Metrics) RequestFinished(method, route string, status int, seconds float64, bytes int64)
```

`New` builds `prometheus.NewRegistry()` and registers every collector. Tests call
`Handler()` and parse the exposition, or use `prometheus/testutil`.

### Series

| Name | Type | Labels |
| --- | --- | --- |
| `http_requests_total` | counter | `method`, `route`, `status` |
| `http_request_duration_seconds` | histogram | `method`, `route` |
| `http_response_bytes_total` | counter | `method`, `route` |
| `http_requests_in_flight` | gauge | none |
| `app_build_info` | gauge, value 1 | `version` |
| `db_pool_conns` | gauge | `state` = `total`, `idle`, `acquired`, `max` |

Duration buckets start at 5 ms and end at 300 s, with extra entries around the 100–200 ms
fake latency and the multi-minute upload window. Every boundary is a named constant.

No `path`, no user ID, no file ID, no share token as a label.

### Middleware

`internal/controller/web/middleware/metrics.go` takes a small interface:

```go
type requestObserver interface {
    RequestStarted()
    RequestFinished(method, route string, status int, seconds float64, bytes int64)
}
```

It records after `Next()`, resolves the route template, maps a route whose method is
`USE` to `unmatched`, and derives the status through the shared helper described above.
`responseBodySize` is reused so a streamed download reports its declared length; a
negative value is clamped to zero before it reaches the counter.

### Pool gauges

`pkg/postgres` gains:

```go
type PoolStat struct { Total, Idle, Acquired, Max int32 }

func (p *Postgres) Stat() PoolStat
```

`internal/app` passes a small closure to `metrics.New` that adapts `postgres.PoolStat`
to `metrics.PoolStats`. The `pgxpool` type never crosses either package boundary.

## Tier 3 — Pprof

`internal/app/observe.go` starts a `net/http.Server` on `OBSERVE_ADDR`:

- `/metrics` -> `metrics.Handler()`.
- `/debug/pprof/`, `/cmdline`, `/profile`, `/symbol`, `/trace` -> the `net/http/pprof`
  handlers, registered on a mux built in this package.
- No `DefaultServeMux` and no side-effect import, so the process has one visible set of
  diagnostic routes.

Lifecycle:

- Skipped entirely when `OBSERVE_ENABLED=false` or when `HTTP_USE_PREFORK_MODE=true`;
  each case logs one line saying why.
- Started after the Fiber server, with `net.Listen` done first so a busy port is an
  error at startup rather than a silent no-op.
- A listen failure is logged at error level and the app continues. Observability must
  not be a new way to fail a deploy.
- `Shutdown` runs after the Fiber shutdown, with its own 5 s timeout.

Security:

- Default binding is loopback. The doc, `.env.example` and compose file all say the
  port must never be published on `0.0.0.0`.
- In Docker, the app listens on `0.0.0.0:9090` inside the container and compose
  publishes it as `127.0.0.1:9090:9090`. Every container on `app_network` can still
  reach `app:9090`; that is accepted and documented, because those are the app's own
  services and a future Prometheus has to reach it the same way.

## Configuration and Deployment

- `config.Config` gains an `Observability` struct:
  `Enabled bool` (`OBSERVE_ENABLED`, default `true`) and `Addr string`
  (`OBSERVE_ADDR`, default `127.0.0.1:9090`).
- `.env.example` documents both variables and the binding rule.
- `.env.example` and `docker-compose.yml` switch `HTTP_USE_PREFORK_MODE` to `false`, so
  the shipped stack gets the listener. Compose adds `OBSERVE_ENABLED: "true"`,
  `OBSERVE_ADDR: "0.0.0.0:9090"` and a `127.0.0.1:9090:9090` port mapping on the app
  service.
- `nginx/nginx.conf`: `proxy_set_header X-Request-ID $request_id;` so proxy and app
  logs agree. Optionally add `$upstream_http_x_request_id` to a `log_format`.
- `AGENTS.md` gains an Observability section: the listener, the binding rule, the
  metric names, the request ID contract, and the fact that `/healthz` and `/static/`
  are not logged.

## File Map

| Path | Change |
| --- | --- |
| `internal/controller/web/middleware/requestid.go` | new middleware and accessor |
| `internal/controller/web/middleware/logger.go` | latency, ID, skip rules |
| `internal/controller/web/middleware/recovery.go` | log through the ID |
| `internal/controller/web/middleware/metrics.go` | new middleware |
| `internal/controller/web/pages.go` | `requestLogger` helper |
| `internal/controller/web/*.go` | log calls go through the helper |
| `internal/controller/web/router.go` | middleware order |
| `pkg/logger/logger.go` | `WithRequestID`, writable constructor for tests |
| `pkg/metrics/*.go` | registry, collectors, handler |
| `pkg/postgres/postgres.go` | `Stat()` accessor |
| `internal/app/observe.go` | internal listener |
| `internal/app/app.go` | config, metrics wiring, server lifecycle |
| `config/config.go` | `OBSERVE_ENABLED`, `OBSERVE_ADDR` |
| `.env.example`, `docker-compose.yml`, `nginx/nginx.conf`, `AGENTS.md` | docs and deployment |
| `go.mod`, `go.sum` | `prometheus/client_golang` |

## Testing

- Request ID: generated when absent, kept when valid, replaced when malformed, present
  on the response header, reachable through the accessor.
- Logger: line carries ID, latency, status and bytes; a 404 logs `404` and a bare error
  logs `500`; the health and static paths are skipped.
- Controller: an error rendered with `renderPage` logs the request ID.
- Metrics middleware: counters move for a matched route, the route label is the
  template, an unmatched path is `unmatched` (not `/`), a 404 stays `404`, a bare error
  becomes `500`, a stream is not buffered, a `-1` size does not panic.
- `pkg/metrics`: exposition contains every family; the pool gauge reports the states
  the closure returns; the version label is set; a negative byte count adds zero.
- Observe server: disabled when `OBSERVE_ENABLED=false`; skipped when prefork is on
  (the decision helper is tested without starting a process); serves `/metrics` and the
  pprof index on `127.0.0.1:0`; returns an error on a busy port; shuts down cleanly.

## Rollout

- No schema change and no migration.
- One new direct dependency: `github.com/prometheus/client_golang`.
- Prefork changes from `true` to `false` in the shipped environment. The stack already
  pins `GOMAXPROCS=1`, so this removes one process and changes no concurrency.
- Existing deployments keep working: the listener defaults to loopback, so an operator
  who never scrapes anything loses nothing but gains one bound port on the host.
  `OBSERVE_ENABLED=false` turns it off.

## Calls Made Without Asking

- The internal listener replaces the earlier "pprof behind the admin guard" idea. If an
  admin-visible pprof page is wanted instead, that is a separate route and a separate
  design.
- `http_response_bytes_total` is included; it costs one counter per response and answers
  throughput questions without a second scrape source.
- Pool gauges are included. They are the most likely cause of a stalled request in this
  app, where every page hits Postgres.
- Health checks stay counted. They are cheap and prove the probe path is alive.
