package middleware

import (
	"io"
	"net/http"
	"strings"
	"testing"

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

	got := buildRequestMessage(ctx)

	if stream.reads != 0 {
		t.Errorf("stream reads = %d, want 0", stream.reads)
	}

	if !strings.HasSuffix(got, " -1") {
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

	got := buildRequestMessage(ctx)

	if stream.reads != 0 {
		t.Errorf("stream reads = %d, want 0", stream.reads)
	}

	if !strings.HasSuffix(got, " 12") {
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

	got := buildRequestMessage(ctx)

	if !strings.HasSuffix(got, " 5") {
		t.Errorf("message = %q, want the body length 5", got)
	}
}
