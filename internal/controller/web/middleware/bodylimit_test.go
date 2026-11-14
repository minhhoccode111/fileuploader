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

const (
	// _testGeneralLimit is the limit every other route carries in these tests.
	_testGeneralLimit = int64(1 << 20)

	// _testUploadLimit is the limit of one part under the upload prefix, small
	// enough to cross with one short body.
	_testUploadLimit = int64(8)
)

// bodyLimitApp registers the three shapes of route the body limit has to tell
// apart: a general page form, another general route, and one path under the
// upload prefix.
func bodyLimitApp(called *bool) *fiber.App {
	app := fiber.New()
	app.Use(BodyLimit(_testGeneralLimit, _testUploadLimit, "/htmx/uploads/"))

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
	app.Put("/htmx/uploads/:upload/parts/:part", handler)

	return app
}

// postBody sends one request with a known length through app.Test and reports
// the status and whether the handler ran.
func postBody(t *testing.T, method, path string, payload []byte) (int, bool) {
	t.Helper()

	called := false
	app := bodyLimitApp(&called)

	req := httptest.NewRequestWithContext(t.Context(), method, path, bytes.NewReader(payload))

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	if _, err := io.Copy(io.Discard, res.Body); err != nil {
		t.Fatalf("io.Copy() error = %v, want nil", err)
	}

	return res.StatusCode, called
}

func TestBodyLimitAcceptsAGeneralBodyUpToTheLimit(t *testing.T) {
	t.Parallel()

	status, called := postBody(t, http.MethodPost, "/", make([]byte, _testGeneralLimit))

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}

	if !called {
		t.Error("handler did not run for a body at the limit")
	}
}

func TestBodyLimitAcceptsAPartUnderTheUploadLimit(t *testing.T) {
	t.Parallel()

	status, called := postBody(
		t,
		http.MethodPut,
		"/htmx/uploads/upload/parts/1",
		make([]byte, _testUploadLimit),
	)

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}

	if !called {
		t.Error("handler did not run for a part under the upload limit")
	}
}

func TestBodyLimitRejectsAnOversizedPart(t *testing.T) {
	t.Parallel()

	status, called := postBody(
		t,
		http.MethodPut,
		"/htmx/uploads/upload/parts/1",
		make([]byte, _testUploadLimit+1),
	)

	if status != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", status, http.StatusRequestEntityTooLarge)
	}

	if called {
		t.Error("handler ran for a part over the upload limit")
	}
}

// A chunked body carries no length to check, so only the path whose handler
// bounds the stream itself may accept it. Every other path answers 411.
func TestBodyLimitRejectsChunkedBodiesOutsideTheUploadPrefix(t *testing.T) {
	t.Parallel()

	status, called := chunkedRequest(t, http.MethodPost, "/other")

	if status != http.StatusLengthRequired {
		t.Errorf("status = %d, want %d", status, http.StatusLengthRequired)
	}

	if called {
		t.Error("handler ran for a chunked body on another path")
	}
}

// The upload prefix accepts a chunked body, because the part handler rejects a
// request without a length itself.
func TestBodyLimitAcceptsAChunkedBodyUnderTheUploadPrefix(t *testing.T) {
	t.Parallel()

	status, called := chunkedRequest(t, http.MethodPut, "/htmx/uploads/upload/parts/1")

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}

	if !called {
		t.Error("handler did not run for a chunked part under the upload prefix")
	}
}

// chunkedRequest sends one chunked body to path over a real connection, which
// is how a client that cannot announce a length reaches the server.
func chunkedRequest(t *testing.T, method, path string) (int, bool) {
	t.Helper()

	called := false
	app := bodyLimitApp(&called)

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v, want nil", err)
	}

	go func() {
		if listenErr := app.Listener(ln); listenErr != nil {
			t.Errorf("Listener() error = %v, want nil", listenErr)
		}
	}()

	t.Cleanup(func() {
		if shutdownErr := app.Shutdown(); shutdownErr != nil {
			t.Errorf("Shutdown() error = %v, want nil", shutdownErr)
		}
	})

	req, err := http.NewRequestWithContext(
		t.Context(),
		method,
		"http://"+ln.Addr().String()+path,
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

	if _, err := io.Copy(io.Discard, res.Body); err != nil {
		t.Fatalf("io.Copy() error = %v, want nil", err)
	}

	return res.StatusCode, called
}
