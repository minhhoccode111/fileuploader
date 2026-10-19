package middleware

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// errSessionGone is the failure the fake reports for an unknown session.
var errSessionGone = errors.New("session gone")

// fakeAuthenticator resolves a session identifier to the session behind it.
type fakeAuthenticator struct {
	session entity.Session
	err     error
}

// Authenticate -.
func (f *fakeAuthenticator) Authenticate(_ context.Context, _ string) (entity.Session, error) {
	if f.err != nil {
		return entity.Session{}, f.err
	}

	return f.session, nil
}

// result holds what a test needs to know about a response.
type result struct {
	status  int
	headers http.Header
	body    string
}

// adminApp wires the admin guard in front of a handler that reports what the
// guard parked in the request.
func adminApp(auth sessionAuthenticator) *fiber.App {
	app := fiber.New()

	app.Get("/admin", RequireAdmin(auth), func(ctx *fiber.Ctx) error {
		return ctx.SendString("admin page")
	})

	return app
}

// call sends a request to the guarded page, with a session cookie unless the
// test wants an anonymous visitor.
func call(t *testing.T, app *fiber.App, cookie bool, headers map[string]string) result {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin", http.NoBody)

	if cookie {
		req.AddCookie(&http.Cookie{
			Name:     _authCookieName,
			Value:    "session-id",
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}

	for name, value := range headers {
		req.Header.Set(name, value)
	}

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}

	return result{status: res.StatusCode, headers: res.Header, body: string(body)}
}

func TestRequireAdminLetsAnAdminThrough(t *testing.T) {
	t.Parallel()

	auth := &fakeAuthenticator{session: entity.Session{UserID: "1", Role: entity.UserRoleAdmin}}
	res := call(t, adminApp(auth), true, nil)

	if res.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.status, http.StatusOK)
	}
}

func TestRequireAdminParksTheSession(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	auth := &fakeAuthenticator{session: entity.Session{UserID: "1", Role: entity.UserRoleAdmin}}

	app.Get("/admin", RequireAdmin(auth), func(ctx *fiber.Ctx) error {
		session, ok := ctx.Locals(_sessionLocal).(entity.Session)
		if !ok {
			return ctx.SendString("no session")
		}

		return ctx.SendString(string(session.Role))
	})

	res := call(t, app, true, nil)

	if res.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.status, http.StatusOK)
	}

	if res.body != string(entity.UserRoleAdmin) {
		t.Errorf("body = %q, want %q", res.body, entity.UserRoleAdmin)
	}
}

func TestRequireAdminSendsVisitorsWithoutTheRoleAway(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		auth     sessionAuthenticator
		cookie   bool
		wantCode int
		wantPath string
	}{
		{
			name:     "anonymous visitor",
			auth:     &fakeAuthenticator{err: errSessionGone},
			wantCode: http.StatusFound,
			wantPath: _loginPath,
		},
		{
			name:     "signed in without the role",
			auth:     &fakeAuthenticator{session: entity.Session{UserID: "1", Role: entity.UserRoleUser}},
			cookie:   true,
			wantCode: http.StatusFound,
			wantPath: _homePath,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := call(t, adminApp(tc.auth), tc.cookie, nil)

			if res.status != tc.wantCode {
				t.Fatalf("status = %d, want %d", res.status, tc.wantCode)
			}

			if location := res.headers.Get("Location"); location != tc.wantPath {
				t.Errorf("Location = %q, want %q", location, tc.wantPath)
			}
		})
	}
}

func TestRequireAdminRedirectsAnHtmxRequestInPlace(t *testing.T) {
	t.Parallel()

	auth := &fakeAuthenticator{session: entity.Session{UserID: "1", Role: entity.UserRoleUser}}
	res := call(t, adminApp(auth), true, map[string]string{_hxRequestHeader: _hxRequestTrue})

	if res.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.status, http.StatusOK)
	}

	location := res.headers.Get(_hxLocationHeader)
	if location == "" {
		t.Fatal("the guard did not set the location header")
	}

	if !strings.Contains(location, _homePath) {
		t.Errorf("location = %q, want it to point at %q", location, _homePath)
	}
}
