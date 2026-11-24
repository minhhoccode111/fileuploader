package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/middleware"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// captureLogger records what a handler asked it to log.
type captureLogger struct {
	lines []string
	ids   []string
}

func (c *captureLogger) Debug(message any, _ ...any) {
	c.lines = append(c.lines, fmt.Sprint(message))
}

func (c *captureLogger) Info(message string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(message, args...))
}

func (c *captureLogger) Warn(message string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(message, args...))
}

func (c *captureLogger) Error(message any, _ ...any) {
	c.lines = append(c.lines, fmt.Sprint(message))
}

func (c *captureLogger) Fatal(message any, _ ...any) {
	c.lines = append(c.lines, fmt.Sprint(message))
}

func (c *captureLogger) WithRequestID(id string) logger.Interface {
	c.ids = append(c.ids, id)

	return c
}

func TestRequestLoggerTagsHandlerErrors(t *testing.T) {
	t.Parallel()

	capture := &captureLogger{}
	c := &Controller{l: capture}

	app := fiber.New()
	app.Use(middleware.RequestID())
	app.Get("/", func(ctx *fiber.Ctx) error {
		c.requestLogger(ctx).Error(io.EOF)

		return ctx.SendStatus(http.StatusOK)
	})

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

	if got := resp.Header.Get("X-Request-ID"); capture.ids[0] != got {
		t.Errorf("logged id = %q, want %q", capture.ids[0], got)
	}
}
