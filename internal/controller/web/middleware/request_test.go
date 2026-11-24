package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// requestFacts is what the test middleware saw after the handler ran.
type requestFacts struct {
	status int
	route  string
}

// factsOf runs one request through a stack that records the status and route
// the real middlewares would record.
func factsOf(t *testing.T, method, path string, register func(*fiber.App)) requestFacts {
	t.Helper()

	facts := requestFacts{}

	app := fiber.New()
	app.Use(func(ctx *fiber.Ctx) error {
		err := ctx.Next()

		facts.status = responseStatus(ctx, err)
		facts.route = requestRoute(ctx, err)

		return err
	})

	register(app)

	resp, err := app.Test(httptest.NewRequestWithContext(context.Background(), method, path, http.NoBody))
	if err != nil {
		t.Fatalf("%s %s error = %v, want nil", method, path, err)
	}

	defer resp.Body.Close()

	return facts
}

func TestResponseStatusUsesTheFiberCode(t *testing.T) {
	t.Parallel()

	facts := factsOf(t, http.MethodGet, "/", func(app *fiber.App) {
		app.Get("/", func(_ *fiber.Ctx) error {
			return fiber.NewError(fiber.StatusNotFound, "gone")
		})
	})

	if facts.status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", facts.status, http.StatusNotFound)
	}
}

func TestResponseStatusDefaultsTo500(t *testing.T) {
	t.Parallel()

	facts := factsOf(t, http.MethodGet, "/", func(app *fiber.App) {
		app.Get("/", func(_ *fiber.Ctx) error { return io.EOF })
	})

	if facts.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", facts.status, http.StatusInternalServerError)
	}
}

func TestResponseStatusUsesTheWrittenStatus(t *testing.T) {
	t.Parallel()

	facts := factsOf(t, http.MethodGet, "/", func(app *fiber.App) {
		app.Get("/", func(ctx *fiber.Ctx) error { return ctx.SendStatus(http.StatusNoContent) })
	})

	if facts.status != http.StatusNoContent {
		t.Errorf("status = %d, want %d", facts.status, http.StatusNoContent)
	}
}

func TestRequestRouteReturnsTheTemplate(t *testing.T) {
	t.Parallel()

	facts := factsOf(t, http.MethodGet, "/folders/abc", func(app *fiber.App) {
		app.Get("/folders/:folder", func(ctx *fiber.Ctx) error {
			return ctx.SendStatus(http.StatusOK)
		})
	})

	if facts.route != "/folders/:folder" {
		t.Errorf("route = %q, want /folders/:folder", facts.route)
	}
}

func TestRequestRouteMarksUnmatched(t *testing.T) {
	t.Parallel()

	facts := factsOf(t, http.MethodGet, "/nope", func(_ *fiber.App) {})

	if facts.route != "unmatched" {
		t.Errorf("route = %q, want unmatched", facts.route)
	}

	if facts.status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", facts.status, http.StatusNotFound)
	}
}

func TestRequestRouteMarksMethodNotAllowed(t *testing.T) {
	t.Parallel()

	facts := factsOf(t, http.MethodPost, "/only-get", func(app *fiber.App) {
		app.Get("/only-get", func(ctx *fiber.Ctx) error { return ctx.SendStatus(http.StatusOK) })
	})

	if facts.route != "unmatched" {
		t.Errorf("route = %q, want unmatched", facts.route)
	}

	if facts.status != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", facts.status, http.StatusMethodNotAllowed)
	}
}
