# Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every request an ID, a latency and a real status in the log, expose Prometheus metrics, and serve pprof on a private listener.

**Architecture:** A `RequestID` middleware runs first and parks the ID in Fiber locals; the access logger and the handler error logs read it. A `Metrics` middleware records route-template counters and a histogram after the handler runs. `pkg/metrics` owns an explicit Prometheus registry, and a second `net/http` server serves `/metrics` and `/debug/pprof/` on loopback.

**Tech Stack:** Go 1.27, Fiber v2, zerolog, `prometheus/client_golang`, `net/http/pprof`.

**Spec:** `docs/superpowers/specs/2026-11-24-observability-design.md`

## Global Constraints

- Module path `github.com/minhhoccode111/fileuploader`; Go version from `go.mod`.
- No package-level variables. Collectors, registries and bucket slices are built in `New` and live on a struct (`gochecknoglobals`).
- No bare numeric literals in arguments, operations or returns; declare a named constant (`mnd`).
- Blank line before every `return` and around blocks (`nlreturn`, `wsl_v5`); unexported package constants start with `_`.
- `funlen` caps a function at 65 lines / 40 statements; `dupl` triggers at 100 duplicated lines.
- Middleware order is fixed: `RequestID -> Logger -> Metrics -> Recovery -> Latency -> CSRF -> BodyLimit -> routes`.
- A status recorded by a middleware comes from the error (`*fiber.Error.Code`, else `500`), never from a bare default.
- Metrics labels are route templates. Never a raw path, user ID, file ID or share token.
- The observe listener is skipped when prefork is on or `OBSERVE_ENABLED=false`.
- Every task ends with `make format`, the named test command, `make linter-golangci`, and a Conventional Commit.
- No OTel, no tracing, no dashboards, no alert rules.

## Review Focus

- A request that matches no route must log and count `404` with `route="unmatched"`, never `500` or `route="/"`.
- A chunked download (`-1` byte length) must not panic the metrics counter; it records zero bytes.
- Prefork on must leave the observe listener off, log why, and keep the app serving pages.
- A hostile `X-Request-ID` (too short, 65 chars, a space, a semicolon) must be replaced, and the response header must stay clean.
- A busy `OBSERVE_ADDR` must not stop the app; the error is logged and the pages still serve.

---

### Task 1: Request-ID support in the logger

**Files:**
- Modify: `pkg/logger/logger.go`
- Test: `pkg/logger/logger_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `Interface.WithRequestID(id string) Interface`; `newWithWriter(w io.Writer, level string) *Logger` (unexported, for tests).

- [ ] **Step 1: Write the failing test**

```go
package logger

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestWithRequestIDTagsEveryLine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	l := newWithWriter(&buf, "info")
	l.WithRequestID("abc123").Info("hello")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("Unmarshal() error = %v, want nil", err)
	}

	if line["request_id"] != "abc123" {
		t.Errorf("request_id = %v, want abc123", line["request_id"])
	}

	if line["message"] != "hello" {
		t.Errorf("message = %v, want hello", line["message"])
	}
}

func TestWithRequestIDLeavesTheParentAlone(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	l := newWithWriter(&buf, "info")
	l.WithRequestID("abc123")
	l.Info("plain")

	if bytes.Contains(buf.Bytes(), []byte("request_id")) {
		t.Error("parent logger gained a request_id field")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./pkg/logger -run 'TestWithRequestID' -v`
Expected: FAIL with `undefined: newWithWriter` and `l.WithRequestID undefined`.

- [ ] **Step 3: Implement it**

In `pkg/logger/logger.go`:

- Add `WithRequestID(id string) Interface` to `Interface`, with a doc comment saying it returns a child logger that tags every line.
- Rename the body of `New` into `newWithWriter(w io.Writer, level string) *Logger`; `New` calls it with `os.Stdout`.
- Replace `os.Stdout` in the zerolog chain with `w`.
- Add:

```go
// WithRequestID returns a child logger that tags every line with the request
// it belongs to. The parent keeps logging without the field.
func (l *Logger) WithRequestID(id string) Interface {
	child := l.logger.With().Str(_requestIDField, id).Logger()

	return &Logger{logger: &child}
}
```

with `_requestIDField = "request_id"` as a package constant. Keep `var _ Interface = (*Logger)(nil)`.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./pkg/logger -run 'TestWithRequestID' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Format, lint, commit**

```bash
make format
make linter-golangci
git add pkg/logger
git commit -m "feat(logger): tag log lines with a request id"
```

---

### Task 2: Request-ID middleware and shared status/route helpers

**Files:**
- Create: `internal/controller/web/middleware/requestid.go`
- Create: `internal/controller/web/middleware/request.go`
- Test: `internal/controller/web/middleware/requestid_test.go`, `internal/controller/web/middleware/request_test.go` (create)

**Interfaces:**
- Consumes: `logger.Interface.WithRequestID` from Task 1.
- Produces:
  - `func RequestID() fiber.Handler`
  - `func RequestIDFrom(ctx *fiber.Ctx) string`
  - `func RequestLogger(l logger.Interface, ctx *fiber.Ctx) logger.Interface`
  - `func responseStatus(ctx *fiber.Ctx, err error) int` (unexported, same package)
  - `func requestRoute(ctx *fiber.Ctx) string` (unexported, same package)

- [ ] **Step 1: Write the failing tests**

`requestid_test.go`:

```go
func TestRequestIDGeneratesOneWhenAbsent(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Use(RequestID())
	app.Get("/", func(ctx *fiber.Ctx) error { return ctx.SendStatus(fiber.StatusOK) })

	resp, err := app.Test(httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody))
	if err != nil {
		t.Fatalf("GET / error = %v, want nil", err)
	}
	defer resp.Body.Close()

	id := resp.Header.Get("X-Request-ID")
	if len(id) < _requestIDMinLen || len(id) > _requestIDMaxLen {
		t.Errorf("X-Request-ID = %q, want %d..%d chars", id, _requestIDMinLen, _requestIDMaxLen)
	}
}

func TestRequestIDKeepsAValidInboundValue(t *testing.T) { /* header "abcDEF123_-x", want same value back */ }

func TestRequestIDReplacesAMalformedValue(t *testing.T) {
	// table: "short", strings.Repeat("a", _requestIDMaxLen+1), "bad value", "bad;value"
	// each request sends the value and expects a different, valid response header
}

func TestRequestLoggerAddsTheIDToTheChild(t *testing.T) {
	// fake logger records the id passed to WithRequestID; RequestLogger returns it
}
```

Use `httptest.NewRequestWithContext` and set `req.Header.Set("X-Request-ID", ...)`.

`request_test.go`:

```go
func TestResponseStatusUsesTheFiberCode(t *testing.T) {
	// app handler returns fiber.NewError(fiber.StatusNotFound, "gone")
	// middleware captures responseStatus(ctx, ctx.Next()) and the test wants 404
}

func TestResponseStatusDefaultsTo500(t *testing.T) {
	// handler returns errors.New("boom"); want 500
}

func TestResponseStatusUsesTheWrittenStatus(t *testing.T) {
	// handler returns ctx.SendStatus(204); want 204
}

func TestRequestRouteReturnsTheTemplate(t *testing.T) {
	// app.Get("/folders/:folder", ...); want "/folders/:folder", not "/folders/abc"
}

func TestRequestRouteMarksUnmatched(t *testing.T) {
	// GET /nope; want "unmatched"
}

func TestRequestRouteMarksMethodNotAllowed(t *testing.T) {
	// app.Get("/only-get"); POST /only-get; want "unmatched"
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/controller/web/middleware -run 'TestRequest|TestResponseStatus' -v`
Expected: FAIL with `undefined: RequestID`.

- [ ] **Step 3: Implement `requestid.go`**

```go
const (
	_requestIDHeader = "X-Request-ID"
	_requestIDLocal  = "request_id"
	_requestIDMinLen = 8
	_requestIDMaxLen = 64
)

func RequestID() fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		id := ctx.Get(_requestIDHeader)
		if !validRequestID(id) {
			id = uuid.NewString()
		}

		ctx.Locals(_requestIDLocal, id)
		ctx.Set(_requestIDHeader, id)

		return ctx.Next()
	}
}

func RequestIDFrom(ctx *fiber.Ctx) string {
	id, _ := ctx.Locals(_requestIDLocal).(string)

	return id
}

func RequestLogger(l logger.Interface, ctx *fiber.Ctx) logger.Interface {
	id := RequestIDFrom(ctx)
	if id == "" {
		return l
	}

	return l.WithRequestID(id)
}

func validRequestID(value string) bool {
	// length bounds, then only [A-Za-z0-9._-]
}
```

`validRequestID` loops over bytes with a `switch` and a `default: return false`; do not compile a regex into a package variable.

- [ ] **Step 4: Implement `request.go`**

```go
const _methodUse = "USE" // fiber's internal method for app.Use

func responseStatus(ctx *fiber.Ctx, err error) int {
	if err != nil {
		var fiberErr *fiber.Error
		if errors.As(err, &fiberErr) {
			return fiberErr.Code
		}

		return fiber.StatusInternalServerError
	}

	return ctx.Response().StatusCode()
}

func requestRoute(ctx *fiber.Ctx) string {
	route := ctx.Route()
	if route.Method == _methodUse {
		return "unmatched"
	}

	return route.Path
}
```

Add a comment on `_methodUse` explaining that after `Next()` a 404 leaves the last `app.Use` route in `ctx.Route()`.

- [ ] **Step 5: Run them to verify they pass**

Run: `go test ./internal/controller/web/middleware -run 'TestRequest|TestResponseStatus' -v`
Expected: PASS.

- [ ] **Step 6: Format, lint, commit**

```bash
make format
make linter-golangci
git add internal/controller/web/middleware
git commit -m "feat(web): add request id middleware"
```

---

### Task 3: Access log with latency and the real status

**Files:**
- Modify: `internal/controller/web/middleware/logger.go`
- Modify: `internal/controller/web/middleware/recovery.go`
- Modify: `internal/controller/web/router.go`
- Modify: `internal/controller/web/middleware/logger_test.go`

**Interfaces:**
- Consumes: `RequestID`, `RequestIDFrom`, `RequestLogger`, `responseStatus` from Task 2.
- Produces: `buildRequestMessage(ctx *fiber.Ctx, status int, latency time.Duration) string`.

- [ ] **Step 1: Write the failing tests**

Extend `logger_test.go` with a capturing logger:

```go
type captureLogger struct {
	lines []string
	ids   []string
}

func (c *captureLogger) Debug(message any, args ...any) { c.lines = append(c.lines, fmt.Sprint(message)) }
func (c *captureLogger) Info(message string, args ...any) { c.lines = append(c.lines, fmt.Sprintf(message, args...)) }
func (c *captureLogger) Warn(message string, args ...any) { c.lines = append(c.lines, fmt.Sprintf(message, args...)) }
func (c *captureLogger) Error(message any, args ...any) { c.lines = append(c.lines, fmt.Sprint(message)) }
func (c *captureLogger) Fatal(message any, args ...any) { c.lines = append(c.lines, fmt.Sprint(message)) }
func (c *captureLogger) WithRequestID(id string) logger.Interface {
	c.ids = append(c.ids, id)

	return c
}

func TestLoggerRecordsStatusAndLatency(t *testing.T) { /* one GET /, assert line has "200" and a "ms" suffix */ }

func TestLoggerRecordsTheFiberErrorStatus(t *testing.T) { /* handler returns fiber.ErrNotFound; assert "404" */ }

func TestLoggerUsesTheRequestID(t *testing.T) { /* app.Use(RequestID()); assert capture.ids[0] == response X-Request-ID */ }

func TestLoggerSkipsHealthAndStatic(t *testing.T) { /* GET /healthz and /static/app.css; assert no lines */ }
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/controller/web/middleware -run 'TestLogger' -v`
Expected: FAIL: lines lack the latency, 404 test sees 200.

- [ ] **Step 3: Rewrite `logger.go`**

```go
const (
	_healthPath   = "/healthz"
	_staticPrefix = "/static/"
)

func Logger(l logger.Interface) func(ctx *fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		if skipRequestLog(ctx.Path()) {
			return ctx.Next()
		}

		start := time.Now()
		err := ctx.Next()
		latency := time.Since(start)

		RequestLogger(l, ctx).Info(
			"%s", buildRequestMessage(ctx, responseStatus(ctx, err), latency),
		)

		return err
	}
}

func buildRequestMessage(ctx *fiber.Ctx, status int, latency time.Duration) string {
	// ip - METHOD path - status bytes latency, where latency is
	// fmt.Sprintf("%.1fms", float64(latency)/float64(time.Millisecond))
}

func skipRequestLog(path string) bool {
	return path == _healthPath || strings.HasPrefix(path, _staticPrefix)
}
```

Keep `responseBodySize` unchanged.

- [ ] **Step 4: Use the request logger in `recovery.go`**

Replace `l.Error(buildPanicMessage(ctx, err))` with
`RequestLogger(l, ctx).Error(buildPanicMessage(ctx, err))`.

- [ ] **Step 5: Register `RequestID` first in `router.go`**

`app.Use(middleware.RequestID())` goes before `app.Use(middleware.Logger(l))`. Leave the
rest of the order alone until Task 6.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/controller/web/middleware -run 'TestLogger|TestBuildRequestMessage' -v`
Expected: PASS. Update the existing `TestBuildRequestMessage*` tests for the new
signature if they do not compile.

- [ ] **Step 7: Format, lint, commit**

```bash
make format
make linter-golangci
git add internal/controller/web
git commit -m "feat(web): log latency and the real status per request"
```

---

### Task 4: Handler error logs carry the request ID

**Files:**
- Modify: `internal/controller/web/pages.go`
- Modify: every `internal/controller/web/*.go` that calls `c.l.X(` (35 call sites)
- Test: `internal/controller/web/pages_test.go` (create)

**Interfaces:**
- Consumes: `middleware.RequestLogger`, `middleware.RequestID` from Tasks 2–3.
- Produces: `func (c *Controller) requestLogger(ctx *fiber.Ctx) logger.Interface`.

- [ ] **Step 1: Write the failing test**

```go
func TestRequestLoggerTagsHandlerErrors(t *testing.T) {
	t.Parallel()

	capture := &captureLogger{}

	c := &Controller{l: capture}

	app := fiber.New()
	app.Use(middleware.RequestID())
	app.Get("/", func(ctx *fiber.Ctx) error {
		c.requestLogger(ctx).Error(errors.New("boom"))

		return ctx.SendStatus(fiber.StatusOK)
	})

	resp, err := app.Test(httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody))
	if err != nil {
		t.Fatalf("GET / error = %v, want nil", err)
	}
	defer resp.Body.Close()

	if len(capture.ids) != 1 || capture.ids[0] != resp.Header.Get("X-Request-ID") {
		t.Errorf("logged ids = %v, want the response request id", capture.ids)
	}
}
```

Add the same `captureLogger` shape as Task 3 inside `package web`, with `WithRequestID`
recording the ID and returning itself.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/controller/web -run TestRequestLoggerTagsHandlerErrors -v`
Expected: FAIL with `c.requestLogger undefined`.

- [ ] **Step 3: Implement the helper in `pages.go`**

```go
// requestLogger returns the logger tagged with the ID of this request, so an
// error line can be matched to the access line that reported it.
func (c *Controller) requestLogger(ctx *fiber.Ctx) logger.Interface {
	return middleware.RequestLogger(c.l, ctx)
}
```

Import `pkg/logger` and keep the existing `middleware` import.

- [ ] **Step 4: Rewrite the call sites**

Run:

```bash
grep -rn 'c\.l\.\(Error\|Warn\|Info\|Debug\)(' internal/controller/web --include='*.go' | grep -v '_test.go'
```

Replace each `c.l.X(` with `c.requestLogger(ctx).X(`. Every handler names its Fiber
context `ctx`; a build failure names any exception.

- [ ] **Step 5: Run the package tests**

Run: `go test ./internal/controller/web -v`
Expected: PASS, including the new test.

- [ ] **Step 6: Format, lint, commit**

```bash
make format
make linter-golangci
git add internal/controller/web
git commit -m "refactor(web): log handler errors with the request id"
```

---

### Task 5: Prometheus metrics package

**Files:**
- Create: `pkg/metrics/metrics.go`
- Test: `pkg/metrics/metrics_test.go` (create)
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type Options struct { Version string; PoolStats func() PoolStats }`
  - `type PoolStats struct { Total, Idle, Acquired, Max int32 }`
  - `func New(opts Options) *Metrics`
  - `func (m *Metrics) Handler() http.Handler`
  - `func (m *Metrics) RequestStarted()`
  - `func (m *Metrics) RequestFinished(method, route string, status int, seconds float64, bytes int64)`

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/prometheus/client_golang/prometheus/promhttp
make deps
```

- [ ] **Step 2: Write the failing test**

```go
package metrics

func gather(t *testing.T, m *Metrics) map[string]*dto.MetricFamily {
	t.Helper()

	families, err := m.registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v, want nil", err)
	}

	index := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		index[family.GetName()] = family
	}

	return index
}

func TestRequestMetricsRecordOneRequest(t *testing.T) {
	t.Parallel()

	m := New(Options{Version: "test", PoolStats: nil})

	m.RequestStarted()
	m.RequestFinished("GET", "/folders/:folder", 200, 0.012, 42)

	families := gather(t, m)
	// assert http_requests_total{status="200"} == 1,
	// http_request_duration_seconds count == 1,
	// http_response_bytes_total == 42,
	// http_requests_in_flight == 0
}

func TestNegativeBytesCountAsZero(t *testing.T) {
	t.Parallel()

	m := New(Options{Version: "test", PoolStats: nil})

	m.RequestStarted()
	m.RequestFinished("GET", "/folders/:folder/download", 200, 0.5, -1) // must not panic

	// assert http_response_bytes_total == 0
}

func TestPoolGaugesReadTheClosure(t *testing.T) {
	t.Parallel()

	m := New(Options{
		Version:   "test",
		PoolStats: func() PoolStats { return PoolStats{Total: 3, Idle: 2, Acquired: 1, Max: 10} },
	})

	// assert db_pool_conns{state="total"} == 3, idle 2, acquired 1, max 10
}

func TestHandlerServesTheExposition(t *testing.T) {
	t.Parallel()

	m := New(Options{Version: "1.2.3", PoolStats: nil})

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", http.NoBody))

	// assert 200, body contains `app_build_info` and `version="1.2.3"`
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `go test ./pkg/metrics -v`
Expected: FAIL with `undefined: New`.

- [ ] **Step 4: Implement `metrics.go`**

- `Metrics` holds `registry`, `requests *prometheus.CounterVec`, `duration *prometheus.HistogramVec`, `bytes *prometheus.CounterVec`, `inFlight prometheus.Gauge`.
- `New` creates `prometheus.NewRegistry()`, builds each collector, registers it, sets `app_build_info` to 1 with `ConstLabels: prometheus.Labels{"version": opts.Version}`.
- Histogram buckets: named constants from `_bucket5ms = 0.005` through `_bucket300s = 300`, sliced inside `New` (local literal, no package variable). Include 0.1, 0.25, 1, 2.5, 5, 10, 30, 60, 120 between the ends.
- Unknown byte counts are clamped in `RequestFinished`: increment requests with `method`, `route`, `strconv.Itoa(status)`; observe duration with `method`, `route`; add bytes with `method`, `route` after `if bytes < 0 { bytes = 0 }`; decrement `inFlight`.
- `RequestStarted` increments `inFlight`.
- `Handler` returns `promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})`.
- Pool gauges: when `opts.PoolStats != nil`, register four `prometheus.NewGaugeFunc` collectors named `db_pool_conns` with `ConstLabels{"state": …}` for `total`, `idle`, `acquired`, `max`; each closure calls `opts.PoolStats()` and reads one field.

- [ ] **Step 5: Run it to verify it passes**

Run: `go test ./pkg/metrics -v`
Expected: PASS.

- [ ] **Step 6: Format, lint, commit**

```bash
make format
make linter-golangci
git add go.mod go.sum pkg/metrics
git commit -m "feat(metrics): add prometheus collectors"
```

---

### Task 6: Metrics middleware and router registration

**Files:**
- Create: `internal/controller/web/middleware/metrics.go`
- Test: `internal/controller/web/middleware/metrics_test.go` (create)
- Modify: `internal/controller/web/router.go`
- Modify: `internal/app/app.go`

**Interfaces:**
- Consumes: `responseStatus`, `requestRoute`, `responseBodySize` from Tasks 2–3; `*metrics.Metrics` from Task 5.
- Produces: `type RequestObserver interface { RequestStarted(); RequestFinished(method, route string, status int, seconds float64, bytes int64) }` and `func Metrics(m RequestObserver) fiber.Handler`.

- [ ] **Step 1: Write the failing test**

```go
type fakeObserver struct {
	started int
	method  string
	route   string
	status  int
	bytes   int64
}

func (f *fakeObserver) RequestStarted() { f.started++ }

func (f *fakeObserver) RequestFinished(method, route string, status int, _ float64, bytes int64) {
	f.method, f.route, f.status, f.bytes = method, route, status, bytes
}

func TestMetricsUsesTheRouteTemplate(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{}
	app := fiber.New()
	app.Use(Metrics(observer))
	app.Get("/folders/:folder", func(ctx *fiber.Ctx) error { return ctx.SendStatus(fiber.StatusOK) })

	// GET /folders/abc; want observer.route == "/folders/:folder", status 200, started 1
}

func TestMetricsMarksUnmatchedAnd404(t *testing.T) {
	// app has one route; GET /nope; want route "unmatched", status 404
}

func TestMetricsMapsAPlainErrorTo500(t *testing.T) {
	// handler returns errors.New("boom"); want status 500
}

func TestMetricsPassesStreamSizeThrough(t *testing.T) {
	// handler sets SetBodyStream(io.NopCloser(strings.NewReader("zip")), -1)
	// want observer.bytes == -1; pkg/metrics clamps it in Task 5
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/controller/web/middleware -run TestMetrics -v`
Expected: FAIL with `undefined: Metrics`.

- [ ] **Step 3: Implement `metrics.go`**

```go
type RequestObserver interface {
	RequestStarted()
	RequestFinished(method, route string, status int, seconds float64, bytes int64)
}

func Metrics(m RequestObserver) func(ctx *fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		start := time.Now()

		m.RequestStarted()

		err := ctx.Next()
		latency := time.Since(start)

		m.RequestFinished(
			ctx.Method(),
			requestRoute(ctx),
			responseStatus(ctx, err),
			latency.Seconds(),
			int64(responseBodySize(ctx)),
		)

		return err
	}
}
```

If `m` is a nil interface, return `ctx.Next()` unchanged; the router always passes a
value, but a nil check keeps a test app simple.

- [ ] **Step 4: Register it in `router.go`**

`NewRouter` gains a `m middleware.RequestObserver` parameter. Register
`app.Use(middleware.Metrics(m))` between `Logger` and `Recovery`.

- [ ] **Step 5: Pass a metrics value from `app.go`**

`initServers` creates `metrics.New(metrics.Options{Version: cfg.App.Version, PoolStats: nil})`
and passes it to `web.NewRouter`. Task 7 replaces the nil pool closure.

- [ ] **Step 6: Run the tests and build**

Run: `go test ./internal/controller/web/middleware -run TestMetrics -v && go build ./...`
Expected: PASS and a clean build.

- [ ] **Step 6: Format, lint, commit**

```bash
make format
make linter-golangci
git add internal/controller/web internal/app
git commit -m "feat(web): record request metrics"
```

---

### Task 7: Postgres pool stats and app wiring

**Files:**
- Modify: `pkg/postgres/postgres.go`
- Modify: `pkg/postgres/tx.go` (extend `Pool` with `Stat() *pgxpool.Stat`)
- Modify: `internal/app/app.go`
- Test: none for the adapter; `pkg/metrics` Task 5 tests the gauge closure.

**Interfaces:**
- Consumes: `metrics.New`, `metrics.Options`, `metrics.PoolStats` from Task 5.
- Produces: `type PoolStat struct { Total, Idle, Acquired, Max int32 }` and `func (p *Postgres) Stat() PoolStat`.

- [ ] **Step 1: Add the accessor**

In `pkg/postgres/postgres.go`:

```go
// PoolStat is a snapshot of the connection pool, read at scrape time.
type PoolStat struct {
	Total, Idle, Acquired, Max int32
}

// Stat reports the current pool usage. A closed pool reports zero.
func (p *Postgres) Stat() PoolStat {
	if p.pool == nil {
		return PoolStat{}
	}

	stat := p.pool.Stat()

	return PoolStat{
		Total:    stat.TotalConns(),
		Idle:     stat.IdleConns(),
		Acquired: stat.AcquiredConns(),
		Max:      stat.MaxConns(),
	}
}
```

Add `Stat() *pgxpool.Stat` to the `Pool` interface in `tx.go` and import `pgxpool`.
(`*pgxpool.Pool` already satisfies it.)

- [ ] **Step 2: Add the pool closure in `app.go`**

`initServers` gains `pg *postgres.Postgres` as a parameter, and `Run` passes `pg` when it
calls `initServers`. Replace the `PoolStats: nil` from Task 6 with:

```go
PoolStats: func() metrics.PoolStats {
	stat := pg.Stat()

	return metrics.PoolStats{
		Total: stat.Total, Idle: stat.Idle, Acquired: stat.Acquired, Max: stat.Max,
	}
},
```

- [ ] **Step 3: Verify**

Run: `go build ./... && go test ./internal/app ./pkg/postgres ./pkg/metrics`
Expected: PASS.

- [ ] **Step 4: Format, lint, commit**

```bash
make format
make linter-golangci
git add pkg/postgres internal/app
git commit -m "feat(app): expose postgres pool stats"
```

---

### Task 8: Observe listener, config, and lifecycle

**Files:**
- Modify: `config/config.go`
- Create: `internal/app/observe.go`
- Test: `internal/app/observe_test.go` (create)
- Modify: `internal/app/app.go`

**Interfaces:**
- Consumes: `metrics.Metrics.Handler` from Task 5, `metrics.New` value from Task 7.
- Produces:
  - `config.Observability` field with `Enabled bool` and `Addr string`
  - `func observeAddress(cfg *config.Config) (string, string)` — address and skip reason
  - `func startObserveServer(addr string, m *metrics.Metrics, l logger.Interface) *observeServer`
  - `func (s *observeServer) Shutdown(ctx context.Context) error`
  - `func (s *observeServer) Addr() string`

- [ ] **Step 1: Write the failing tests**

```go
func TestObserveAddressFollowsTheConfig(t *testing.T) {
	// enabled + no prefork -> "127.0.0.1:9090", ""
	// disabled -> "", reason mentions OBSERVE_ENABLED
	// prefork -> "", reason mentions prefork
}

func TestObserveServerServesMetricsAndPprof(t *testing.T) {
	// start on "127.0.0.1:0", read Addr(), GET /metrics -> 200,
	// GET /debug/pprof/ -> 200
}

func TestObserveServerReportsABusyPort(t *testing.T) {
	// start one on 127.0.0.1:0, start a second on its Addr() -> nil
}

func TestObserveServerStopsOnShutdown(t *testing.T) {
	// start, Shutdown, GET Addr() -> connection error
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/app -run TestObserve -v`
Expected: FAIL with `undefined: observeAddress`.

Build the config values by assignment (`cfg := &config.Config{}; cfg.HTTP.UsePreforkMode = true`)
because the nested config types are unexported; a composite literal from another
package does not compile. Shut the test servers down in `t.Cleanup`.

- [ ] **Step 3: Add the config**

```go
// Observability -.
observability struct {
	Enabled bool   `env:"OBSERVE_ENABLED" envDefault:"true"`
	Addr    string `env:"OBSERVE_ADDR" envDefault:"127.0.0.1:9090"`
}
```

Field `Observability observability` on `Config`. A comment must say the listener is for
metrics and pprof and belongs on loopback only.

- [ ] **Step 4: Implement `observe.go`**

- Build a `http.NewServeMux` with `promhttp` from `m.Handler()` at `/metrics` and the
  `net/http/pprof` functions at the five paths; no `DefaultServeMux`, no blank import.
- `startObserveServer` returns nil when `net.Listen` fails, after logging the error;
  otherwise it starts `srv.Serve(ln)` in a goroutine and logs one line with the address.
  Ignore `http.ErrServerClosed`.
- `Shutdown` uses `context.WithTimeout` with a `_observeShutdownTimeout = 5 * time.Second`
  constant.
- `observeAddress` returns `(addr, "")` only when enabled and prefork is off; otherwise
  `("", reason)`.

- [ ] **Step 5: Wire the lifecycle**

- `initServers` calls `observeAddress(cfg)` once and stores the address, the skip reason
  and the `*metrics.Metrics` value on `servers`.
- `startServers(l logger.Interface)` starts HTTP, then starts the observer when the
  address is non-empty; on a skip reason it logs one line instead.
- `shutdownServers` shuts HTTP down, then the observer when non-nil.
- `Run` calls `s.startServers(l)`.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/app -v`
Expected: PASS.

- [ ] **Step 7: Format, lint, commit**

```bash
make format
make linter-golangci
git add config internal/app
git commit -m "feat(app): serve metrics and pprof on a private listener"
```

---

### Task 9: Deployment files and documentation

**Files:**
- Modify: `.env.example`
- Modify: `docker-compose.yml`
- Modify: `nginx/nginx.conf`
- Modify: `AGENTS.md`
- Modify: `README.md` (only if it lists environment variables)

**Interfaces:** none.

- [ ] **Step 1: Update `.env.example`**

- `HTTP_USE_PREFORK_MODE=false`.
- Add, after the HTTP block:

```text
# Metrics and pprof listener. Loopback only: never publish it on 0.0.0.0.
OBSERVE_ENABLED=true
OBSERVE_ADDR=127.0.0.1:9090
```

- [ ] **Step 2: Update `docker-compose.yml`**

- `HTTP_USE_PREFORK_MODE: "false"` in `x-backend-app-environment`.
- `OBSERVE_ENABLED: "true"` and `OBSERVE_ADDR: "0.0.0.0:9090"` next to it.
- App service port: `- "127.0.0.1:9090:9090"` beside `8090:8090`.

- [ ] **Step 3: Update `nginx/nginx.conf`**

Inside `location /`, add `proxy_set_header X-Request-ID $request_id;` next to the other
`proxy_set_header` lines.

- [ ] **Step 4: Update `AGENTS.md`**

Add an Observability section covering: the request ID contract (header, accepted
charset, nginx supplies it), the log fields, the skipped paths, the internal listener and
its binding rule, the metric names, the prefork conflict, and `OBSERVE_ENABLED=false`.

- [ ] **Step 5: Verify the whole repo**

```bash
make format
make linter-golangci
make test
node --check web/static/js/app.js
```

Expected: 0 lint issues, every package `ok`.

- [ ] **Step 6: Manual check**

```bash
make run
curl -sI http://127.0.0.1:8090/login | grep -i x-request-id
curl -s http://127.0.0.1:9090/metrics | head
curl -sI http://127.0.0.1:9090/debug/pprof/ | head -1
```

Expected: a request ID header, an exposition containing `app_build_info`, and `200 OK`
for pprof. Stop the app afterwards.

- [ ] **Step 7: Commit**

```bash
git add .env.example docker-compose.yml nginx/nginx.conf AGENTS.md README.md
git commit -m "chore(deploy): expose metrics and pprof on loopback"
```

---

## Self-Review

- **Spec coverage:** request ID (Tasks 1–4), metrics (Tasks 5–7), pprof and listener
  (Task 8), config/deployment/docs (Tasks 8–9). Prefork gate in Task 8; negative bytes
  in Task 5; 404 status/route in Tasks 2, 3, 6.
- **Type consistency:** `RequestStarted`/`RequestFinished` are used by the middleware
  interface and `pkg/metrics`; `PoolStat` (Postgres) and `PoolStats` (metrics) stay
  distinct and are adapted in Task 7.
- **Review Focus:** each line has a test in the owning task (404 in Tasks 2/3/6, `-1`
  bytes in Task 5, prefork in Task 8, hostile header in Task 2, busy port in Task 8).
