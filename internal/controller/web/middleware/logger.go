package middleware

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

const (
	// _healthPath is the liveness probe. Logging it would drown the useful lines.
	_healthPath = "/healthz"

	// _staticPrefix marks the asset routes, which are logged only by metrics.
	_staticPrefix = "/static/"
)

// Logger writes one line per request with the status the client sees, the
// response size and the time the request took. The line carries the request
// identifier through the child logger, so it matches the handler's error lines.
func Logger(l logger.Interface) func(ctx *fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		if skipRequestLog(ctx.Path()) {
			return ctx.Next()
		}

		start := time.Now()
		err := ctx.Next()
		latency := time.Since(start)

		RequestLogger(l, ctx).Info("%s", buildRequestMessage(ctx, responseStatus(ctx, err), latency))

		return err
	}
}

func buildRequestMessage(ctx *fiber.Ctx, status int, latency time.Duration) string {
	var result strings.Builder

	result.WriteString(ctx.IP())
	result.WriteString(" - ")
	result.WriteString(ctx.Method())
	result.WriteString(" ")
	result.WriteString(ctx.OriginalURL())
	result.WriteString(" - ")
	result.WriteString(strconv.Itoa(status))
	result.WriteString(" ")
	result.WriteString(strconv.Itoa(responseBodySize(ctx)))
	result.WriteString(" ")
	fmt.Fprintf(&result, "%.1fms", float64(latency)/float64(time.Millisecond))

	return result.String()
}

// responseBodySize reports the buffered body length, or the declared length of
// a stream. It must not call Body() on a stream: that reads the whole response
// into memory before the server can write the headers. A chunked stream has no
// declared length and reports -1.
func responseBodySize(ctx *fiber.Ctx) int {
	if ctx.Response().IsBodyStream() {
		return ctx.Response().Header.ContentLength()
	}

	return len(ctx.Response().Body())
}

// skipRequestLog reports whether a path is counted but not logged.
func skipRequestLog(path string) bool {
	return path == _healthPath || strings.HasPrefix(path, _staticPrefix)
}
