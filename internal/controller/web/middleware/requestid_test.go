package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// captureLogger records what a middleware asked it to log.
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

func requestIDApp() *fiber.App {
	app := fiber.New()
	app.Use(RequestID())
	app.Get("/", func(ctx *fiber.Ctx) error { return ctx.SendStatus(fiber.StatusOK) })

	return app
}

func TestRequestIDGeneratesOneWhenAbsent(t *testing.T) {
	t.Parallel()

	app := requestIDApp()

	resp, err := app.Test(httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/", http.NoBody,
	))
	if err != nil {
		t.Fatalf("GET / error = %v, want nil", err)
	}

	defer resp.Body.Close()

	id := resp.Header.Get(_requestIDHeader)
	if len(id) < _requestIDMinLen || len(id) > _requestIDMaxLen {
		t.Errorf("X-Request-ID = %q, want %d..%d chars", id, _requestIDMinLen, _requestIDMaxLen)
	}
}

func TestRequestIDKeepsAValidInboundValue(t *testing.T) {
	t.Parallel()

	app := requestIDApp()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
	req.Header.Set(_requestIDHeader, "abcDEF123_-x")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET / error = %v, want nil", err)
	}

	defer resp.Body.Close()

	if got := resp.Header.Get(_requestIDHeader); got != "abcDEF123_-x" {
		t.Errorf("X-Request-ID = %q, want abcDEF123_-x", got)
	}
}

func TestRequestIDReplacesAMalformedValue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
	}{
		{name: "short", value: "short"},
		{name: "too long", value: strings.Repeat("a", _requestIDMaxLen+1)},
		{name: "space", value: "bad value"},
		{name: "semicolon", value: "bad;value"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := requestIDApp()

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody)
			req.Header.Set(_requestIDHeader, testCase.value)

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("GET / error = %v, want nil", err)
			}

			defer resp.Body.Close()

			got := resp.Header.Get(_requestIDHeader)
			if got == testCase.value {
				t.Errorf("X-Request-ID = %q, want a replacement", got)
			}

			if !validRequestID(got) {
				t.Errorf("X-Request-ID = %q, want a valid replacement", got)
			}
		})
	}
}

func TestRequestLoggerAddsTheIDToTheChild(t *testing.T) {
	t.Parallel()

	capture := &captureLogger{}

	app := fiber.New()
	app.Use(RequestID())
	app.Get("/", func(ctx *fiber.Ctx) error {
		RequestLogger(capture, ctx).Info("hello")

		return ctx.SendStatus(fiber.StatusOK)
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

	if capture.ids[0] != resp.Header.Get(_requestIDHeader) {
		t.Errorf("logged id = %q, want %q", capture.ids[0], resp.Header.Get(_requestIDHeader))
	}
}
