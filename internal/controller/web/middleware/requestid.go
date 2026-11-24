package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

const (
	// _requestIDHeader carries the identifier in both directions.
	_requestIDHeader = "X-Request-ID"

	// _requestIDLocal is where the identifier is parked for the request.
	_requestIDLocal = "request_id"

	// _requestIDMinLen and _requestIDMaxLen bound an accepted inbound value.
	_requestIDMinLen = 8
	_requestIDMaxLen = 64
)

// RequestID gives every request an identifier, keeps one the caller sent when
// it is safe, and writes it back on the response. The access logger and the
// handler error logs read the same value, so one request can be followed from
// the proxy through every line it wrote.
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

// RequestIDFrom returns the identifier of the request, or an empty string when
// the middleware did not run.
func RequestIDFrom(ctx *fiber.Ctx) string {
	id, ok := ctx.Locals(_requestIDLocal).(string)
	if !ok {
		return ""
	}

	return id
}

// RequestLogger tags a logger with the request identifier. Without an
// identifier it returns the logger unchanged, so a caller outside the
// middleware stack still logs.
func RequestLogger(l logger.Interface, ctx *fiber.Ctx) logger.Interface {
	id := RequestIDFrom(ctx)
	if id == "" {
		return l
	}

	return l.WithRequestID(id)
}

// validRequestID accepts what a proxy sends and what this package generates,
// and nothing that could break a log line or a header. The character set is
// deliberately small: a request identifier only has to be unique, not rich.
func validRequestID(value string) bool {
	if len(value) < _requestIDMinLen || len(value) > _requestIDMaxLen {
		return false
	}

	for index := 0; index < len(value); index++ {
		char := value[index]
		switch {
		case char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9',
			char == '.', char == '_', char == '-':
		default:
			return false
		}
	}

	return true
}
