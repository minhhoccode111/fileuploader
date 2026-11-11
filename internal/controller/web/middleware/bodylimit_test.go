package middleware

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func bodyLimitApp(limit int64, streamPath string, called *bool) *fiber.App {
	app := fiber.New()
	app.Use(BodyLimit(limit, streamPath))

	handler := func(ctx *fiber.Ctx) error {
		*called = true

		body := ctx.Request().BodyStream()
		if body == nil {
			body = bytes.NewReader(ctx.Request().Body())
		}

		if _, err := io.Copy(io.Discard, body); err != nil {
			return err
		}

		return ctx.SendString("ok")
	}

	app.Post("/", handler)
	app.Post("/other", handler)

	return app
}

func TestBodyLimitRejectsAKnownOversizedBody(t *testing.T) {
	t.Parallel()

	called := false
	app := bodyLimitApp(4, "/", &called)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader([]byte("hello")))

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusRequestEntityTooLarge)
	}

	if called {
		t.Error("handler ran for a body over the limit")
	}
}

func TestBodyLimitAllowsAKnownBodyUnderTheLimit(t *testing.T) {
	t.Parallel()

	called := false
	app := bodyLimitApp(8, "/", &called)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader([]byte("hello")))

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}

	if !called {
		t.Error("handler did not run for a body under the limit")
	}
}

// A chunked body is only allowed on the path whose handler bounds the stream
// itself. Every other path answers 411 instead of reading an unbounded body.
func TestBodyLimitRejectsChunkedBodiesOnOtherPaths(t *testing.T) {
	t.Parallel()

	called := false
	app := bodyLimitApp(8, "/upload", &called)

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v, want nil", err)
	}

	go func() {
		if listenErr := app.Listener(ln); listenErr != nil {
			t.Errorf("Listener() error = %v, want nil", listenErr)
		}
	}()

	defer func() {
		if shutdownErr := app.Shutdown(); shutdownErr != nil {
			t.Errorf("Shutdown() error = %v, want nil", shutdownErr)
		}
	}()

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"http://"+ln.Addr().String()+"/other",
		io.NopCloser(strings.NewReader("hello")),
	)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v, want nil", err)
	}

	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusLengthRequired {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusLengthRequired)
	}

	if called {
		t.Error("handler ran for a chunked body on another path")
	}
}
