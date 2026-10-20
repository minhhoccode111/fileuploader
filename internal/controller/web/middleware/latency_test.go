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

func TestLatencyStaysUnderTheCeiling(t *testing.T) {
	t.Parallel()

	minDelay := 10 * time.Millisecond
	maxDelay := 30 * time.Millisecond

	elapsed := requestTime(t, latencyApp(minDelay, maxDelay))

	if elapsed > maxDelay {
		t.Errorf("elapsed = %s, want at most %s", elapsed, maxDelay)
	}
}

func TestLatencyStaysOffWithoutAMaximum(t *testing.T) {
	t.Parallel()

	elapsed := requestTime(t, latencyApp(0, 0))

	if elapsed > 50*time.Millisecond {
		t.Errorf("elapsed = %s, want the request to pass through at once", elapsed)
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
