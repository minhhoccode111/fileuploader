package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

// latencyApp wires the fake latency in front of a page that answers at once.
func latencyApp(minDelay, maxDelay time.Duration) *fiber.App {
	app := fiber.New()

	app.Use(Latency(minDelay, maxDelay))
	app.Get("/", func(ctx *fiber.Ctx) error {
		return ctx.SendString("ok")
	})

	return app
}

// requestTime sends a request to the page and reports how long it took.
func requestTime(t *testing.T, app *fiber.App) time.Duration {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

	start := time.Now()

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}

	return time.Since(start)
}

func TestLatencyHoldsTheRequestBack(t *testing.T) {
	t.Parallel()

	minDelay := 50 * time.Millisecond
	maxDelay := 100 * time.Millisecond

	elapsed := requestTime(t, latencyApp(minDelay, maxDelay))

	if elapsed < minDelay {
		t.Errorf("elapsed = %s, want at least %s", elapsed, minDelay)
	}
}

func TestRandomDelayIsZeroWhenTheRangeIsOff(t *testing.T) {
	t.Parallel()

	for _, maxDelay := range []time.Duration{0, -time.Second} {
		if got := randomDelay(time.Second, maxDelay); got != 0 {
			t.Errorf("randomDelay(max=%s) = %s, want 0", maxDelay, got)
		}
	}
}

func TestRandomDelayLandsInsideTheRange(t *testing.T) {
	t.Parallel()

	minDelay := 100 * time.Millisecond
	maxDelay := 200 * time.Millisecond

	for range 100 {
		delay := randomDelay(minDelay, maxDelay)

		if delay < minDelay || delay > maxDelay {
			t.Fatalf("delay = %s, want it inside [%s, %s]", delay, minDelay, maxDelay)
		}
	}
}
