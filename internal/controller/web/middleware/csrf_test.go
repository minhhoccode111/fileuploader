package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// csrfApp registers a safe route and an unsafe one behind the CSRF middleware,
// so a test can send the same request with and without the token.
func csrfApp() *fiber.App {
	app := fiber.New()
	app.Use(CSRF())

	handler := func(ctx *fiber.Ctx) error {
		return ctx.SendString("ok")
	}

	app.Get("/", handler)
	app.Post("/", handler)
	app.Put("/", handler)

	return app
}

// csrfCookieToken returns the token of the cookie the middleware set on the
// response, or asks for the GET that issues one.
func csrfCookieToken(t *testing.T, app *fiber.App) string {
	t.Helper()

	resp, err := app.Test(httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody))
	if err != nil {
		t.Fatalf("GET / error = %v, want nil", err)
	}

	defer resp.Body.Close()

	for _, cookie := range resp.Cookies() {
		if cookie.Name == _csrfCookieName {
			return cookie.Value
		}
	}

	t.Fatalf("GET / did not set the %s cookie", _csrfCookieName)

	return ""
}

func TestCSRFSetsTheCookieOnASafeRequest(t *testing.T) {
	t.Parallel()

	resp, err := csrfApp().Test(httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody))
	if err != nil {
		t.Fatalf("GET / error = %v, want nil", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Name == _csrfCookieName && cookie.Value != "" {
			return
		}
	}

	t.Errorf("GET / did not set a non-empty %s cookie", _csrfCookieName)
}

func TestCSRFPassesAnUnsafeRequestWithTheToken(t *testing.T) {
	t.Parallel()

	app := csrfApp()
	token := csrfCookieToken(t, app)

	for _, method := range []string{http.MethodPost, http.MethodPut} {
		req := httptest.NewRequestWithContext(context.Background(), method, "/", http.NoBody)
		req.Header.Set(_csrfHeaderName, token)
		req.AddCookie(&http.Cookie{
			Name:     _csrfCookieName,
			Value:    token,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s / error = %v, want nil", method, err)
		}

		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s / status = %d, want %d", method, resp.StatusCode, http.StatusOK)
		}
	}
}

func TestCSRFRejectsAnUnsafeRequestWithoutTheHeader(t *testing.T) {
	t.Parallel()

	app := csrfApp()
	token := csrfCookieToken(t, app)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", http.NoBody)
	req.AddCookie(&http.Cookie{
		Name:     _csrfCookieName,
		Value:    token,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("POST / error = %v, want nil", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST / status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestCSRFRejectsAnUnsafeRequestWithAMismatchedHeader(t *testing.T) {
	t.Parallel()

	app := csrfApp()
	token := csrfCookieToken(t, app)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", http.NoBody)
	req.Header.Set(_csrfHeaderName, token+"x")
	req.AddCookie(&http.Cookie{
		Name:     _csrfCookieName,
		Value:    token,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("POST / error = %v, want nil", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST / status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// csrfUnsafeRequest builds a form post that carries the token in both halves
// and names the origin the same-origin check sees. An empty origin leaves the
// header off, which is what a non-browser client sends.
func csrfUnsafeRequest(host, origin, token string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", http.NoBody)
	req.Host = host
	req.Header.Set(_csrfHeaderName, token)

	if origin != "" {
		req.Header.Set(fiber.HeaderOrigin, origin)
	}

	req.AddCookie(&http.Cookie{
		Name:     _csrfCookieName,
		Value:    token,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	return req
}

func TestCSRFRejectsAForeignOriginEvenWithTheToken(t *testing.T) {
	t.Parallel()

	app := csrfApp()
	token := csrfCookieToken(t, app)

	resp, err := app.Test(csrfUnsafeRequest("app.example.com", "https://evil.example", token))
	if err != nil {
		t.Fatalf("POST / error = %v, want nil", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST / status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestCSRFPassesASameOriginRequest(t *testing.T) {
	t.Parallel()

	app := csrfApp()
	token := csrfCookieToken(t, app)

	resp, err := app.Test(csrfUnsafeRequest(
		"app.example.com:8090", "http://app.example.com:8090", token,
	))
	if err != nil {
		t.Fatalf("POST / error = %v, want nil", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST / status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestCSRFRejectsAnotherPortOfTheSameHost(t *testing.T) {
	t.Parallel()

	app := csrfApp()
	token := csrfCookieToken(t, app)

	resp, err := app.Test(csrfUnsafeRequest(
		"app.example.com:8090", "http://app.example.com:9000", token,
	))
	if err != nil {
		t.Fatalf("POST / error = %v, want nil", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST / status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestCSRFRejectsAnUnsafeRequestWithoutTheCookie(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", http.NoBody)
	req.Header.Set(_csrfHeaderName, "guessed")

	resp, err := csrfApp().Test(req)
	if err != nil {
		t.Fatalf("POST / error = %v, want nil", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST / status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}
