package middleware

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// RequestObserver records the outcome of one request. It is the narrow view of
// pkg/metrics the transport needs, so a test can count without a registry.
type RequestObserver interface {
	RequestStarted()
	RequestFinished(method, route string, status int, seconds float64, bytes int64)
}

// Metrics records every request the chain sees, including the ones a guard or
// the CSRF check refuses. It sits outside Recovery, so a panic has already
// become a 500 before the status is read.
func Metrics(m RequestObserver) func(ctx *fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		if m == nil {
			return ctx.Next()
		}

		start := time.Now()

		m.RequestStarted()

		err := ctx.Next()
		latency := time.Since(start)

		// ctx.Method() is a string over fasthttp's request buffer, and that
		// buffer is reused by the next request. A metric label outlives the
		// request, so it needs its own copy or it mutates after the fact.
		m.RequestFinished(
			strings.Clone(ctx.Method()),
			requestRoute(ctx, err),
			responseStatus(ctx, err),
			latency.Seconds(),
			int64(responseBodySize(ctx)),
		)

		return err
	}
}
