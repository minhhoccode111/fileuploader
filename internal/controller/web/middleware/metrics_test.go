package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// fakeObserver records the one request the middleware reported.
type fakeObserver struct {
	started int
	method  string
	route   string
	status  int
	bytes   int64
}

func (f *fakeObserver) RequestStarted() {
	f.started++
}

func (f *fakeObserver) RequestFinished(method, route string, status int, _ float64, bytes int64) {
	f.method = method
	f.route = route
	f.status = status
	f.bytes = bytes
}

// metricsGet sends one GET through an app that carries the metrics middleware.
func metricsGet(t *testing.T, observer *fakeObserver, path string, register func(*fiber.App)) {
	t.Helper()

	app := fiber.New()
	app.Use(Metrics(observer))

	register(app)

	resp, err := app.Test(httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, http.NoBody))
	if err != nil {
		t.Fatalf("GET %s error = %v, want nil", path, err)
	}

	err = resp.Body.Close()
	if err != nil {
		t.Fatalf("GET %s close error = %v, want nil", path, err)
	}
}

func TestMetricsUsesTheRouteTemplate(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{}

	metricsGet(t, observer, "/folders/abc", func(app *fiber.App) {
		app.Get("/folders/:folder", func(ctx *fiber.Ctx) error {
			return ctx.SendStatus(http.StatusOK)
		})
	})

	if observer.started != 1 {
		t.Errorf("RequestStarted calls = %d, want 1", observer.started)
	}

	if observer.route != "/folders/:folder" {
		t.Errorf("route = %q, want /folders/:folder", observer.route)
	}

	if observer.status != http.StatusOK {
		t.Errorf("status = %d, want %d", observer.status, http.StatusOK)
	}
}

func TestMetricsMarksUnmatchedAnd404(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{}

	metricsGet(t, observer, "/nope", func(app *fiber.App) {
		app.Get("/", func(ctx *fiber.Ctx) error { return ctx.SendStatus(http.StatusOK) })
	})

	if observer.route != "unmatched" {
		t.Errorf("route = %q, want unmatched", observer.route)
	}

	if observer.status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", observer.status, http.StatusNotFound)
	}
}

func TestMetricsMapsAPlainErrorTo500(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{}

	metricsGet(t, observer, "/", func(app *fiber.App) {
		app.Get("/", func(_ *fiber.Ctx) error { return io.EOF })
	})

	if observer.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", observer.status, http.StatusInternalServerError)
	}
}

func TestMetricsPassesStreamSizeThrough(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{}

	metricsGet(t, observer, "/zip", func(app *fiber.App) {
		app.Get("/zip", func(ctx *fiber.Ctx) error {
			ctx.Response().SetBodyStream(io.NopCloser(strings.NewReader("zip")), -1)

			return nil
		})
	})

	if observer.bytes != -1 {
		t.Errorf("bytes = %d, want -1 for a chunked stream", observer.bytes)
	}
}
