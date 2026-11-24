package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

// countingReader records whether the logger touched a stream.
type countingReader struct {
	reads int
}

func (r *countingReader) Read([]byte) (int, error) {
	r.reads++

	return 0, io.EOF
}

func doGet(t *testing.T, app *fiber.App, path string) {
	t.Helper()

	resp, err := app.Test(httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, http.NoBody))
	if err != nil {
		t.Fatalf("GET %s error = %v, want nil", path, err)
	}

	err = resp.Body.Close()
	if err != nil {
		t.Fatalf("GET %s close error = %v, want nil", path, err)
	}
}

func TestLoggerRecordsStatusAndLatency(t *testing.T) {
	t.Parallel()

	capture := &captureLogger{}

	app := fiber.New()
	app.Use(Logger(capture))
	app.Get("/", func(ctx *fiber.Ctx) error { return ctx.SendStatus(http.StatusOK) })

	doGet(t, app, "/")

	if len(capture.lines) != 1 {
		t.Fatalf("log lines = %d, want 1", len(capture.lines))
	}

	if line := capture.lines[0]; !strings.Contains(line, " 200 ") || !strings.HasSuffix(line, "ms") {
		t.Errorf("line = %q, want a 200 status and a latency", line)
	}
}

func TestLoggerRecordsTheFiberErrorStatus(t *testing.T) {
	t.Parallel()

	capture := &captureLogger{}

	app := fiber.New()
	app.Use(Logger(capture))
	app.Get("/", func(_ *fiber.Ctx) error { return fiber.ErrNotFound })

	doGet(t, app, "/")

	if len(capture.lines) != 1 {
		t.Fatalf("log lines = %d, want 1", len(capture.lines))
	}

	if line := capture.lines[0]; !strings.Contains(line, " 404 ") {
		t.Errorf("line = %q, want the 404 status", line)
	}
}

func TestLoggerUsesTheRequestID(t *testing.T) {
	t.Parallel()

	capture := &captureLogger{}

	app := fiber.New()
	app.Use(RequestID())
	app.Use(Logger(capture))
	app.Get("/", func(ctx *fiber.Ctx) error { return ctx.SendStatus(http.StatusOK) })

	resp, err := app.Test(httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/", http.NoBody,
	))
	if err != nil {
		t.Fatalf("GET / error = %v, want nil", err)
	}

	defer resp.Body.Close()

	if len(capture.ids) != 1 {
		t.Fatalf("WithRequestID calls = %d, want 1", len(capture.ids))
	}

	if got := resp.Header.Get(_requestIDHeader); capture.ids[0] != got {
		t.Errorf("logged id = %q, want %q", capture.ids[0], got)
	}
}

func TestLoggerSkipsHealthAndStatic(t *testing.T) {
	t.Parallel()

	capture := &captureLogger{}

	app := fiber.New()
	app.Use(Logger(capture))
	app.Get("/healthz", func(ctx *fiber.Ctx) error { return ctx.SendStatus(http.StatusOK) })

	doGet(t, app, "/healthz")
	doGet(t, app, "/static/app.css")

	if len(capture.lines) != 0 {
		t.Errorf("log lines = %v, want none", capture.lines)
	}
}

// Logging a streamed response must not read the stream: Body() would buffer the
// whole download and delay every header until the last byte.
func TestBuildRequestMessageDoesNotReadAStream(t *testing.T) {
	t.Parallel()

	app := fiber.New()

	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	defer app.ReleaseCtx(ctx)

	ctx.Request().Header.SetMethod(http.MethodGet)
	ctx.Request().SetRequestURI("/stream")

	stream := &countingReader{}
	ctx.Response().SetBodyStream(stream, -1)

	got := buildRequestMessage(ctx, http.StatusOK, 10*time.Millisecond)

	if stream.reads != 0 {
		t.Errorf("stream reads = %d, want 0", stream.reads)
	}

	if !strings.Contains(got, " -1 ") {
		t.Errorf("message = %q, want the unknown size -1", got)
	}
}

// A stream with a declared size reports that size without reading it.
func TestBuildRequestMessageReportsAStreamLength(t *testing.T) {
	t.Parallel()

	app := fiber.New()

	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	defer app.ReleaseCtx(ctx)

	ctx.Request().Header.SetMethod(http.MethodGet)
	ctx.Request().SetRequestURI("/file")

	stream := &countingReader{}
	ctx.Response().SetBodyStream(stream, 12)

	got := buildRequestMessage(ctx, http.StatusOK, 10*time.Millisecond)

	if stream.reads != 0 {
		t.Errorf("stream reads = %d, want 0", stream.reads)
	}

	if !strings.Contains(got, " 12 ") {
		t.Errorf("message = %q, want the declared size 12", got)
	}
}

// A buffered response still reports its body length.
func TestBuildRequestMessageReportsABufferedBody(t *testing.T) {
	t.Parallel()

	app := fiber.New()

	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	defer app.ReleaseCtx(ctx)

	ctx.Request().Header.SetMethod(http.MethodGet)
	ctx.Request().SetRequestURI("/page")
	ctx.Response().SetBodyString("hello")

	got := buildRequestMessage(ctx, http.StatusOK, 10*time.Millisecond)

	if !strings.Contains(got, " 5 ") {
		t.Errorf("message = %q, want the body length 5", got)
	}
}
