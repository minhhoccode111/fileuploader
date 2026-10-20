package middleware

import (
	"math/rand/v2"
	"time"

	"github.com/gofiber/fiber/v2"
)

// Latency delays every request by a random duration between minDelay and
// maxDelay, so the browser can be exercised against a slow network. A
// non-positive maxDelay turns the delay off.
func Latency(minDelay, maxDelay time.Duration) func(ctx *fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		time.Sleep(randomDelay(minDelay, maxDelay))

		return ctx.Next()
	}
}

// randomDelay picks a duration inside [minDelay, maxDelay]. It returns 0 when
// the delay is off and maxDelay when the range holds a single value.
func randomDelay(minDelay, maxDelay time.Duration) time.Duration {
	if maxDelay <= 0 {
		return 0
	}

	if minDelay >= maxDelay {
		return maxDelay
	}

	span := int64(maxDelay - minDelay)

	//nolint:gosec // fake latency needs no cryptographic randomness.
	jitter := time.Duration(rand.Int64N(span + 1))

	return minDelay + jitter
}
