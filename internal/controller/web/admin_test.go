package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// adminApp wires the admin routes in front of the handlers under test.
func adminApp() *fiber.App {
	app := fiber.New()
	c := &Controller{}

	app.Get(_adminPath, c.admin)
	app.Get(_adminUsersPath, c.adminUsers)
	app.Get(_adminSettingsPath, c.adminSettings)

	return app
}

// adminResponse sends a request to the app and returns the status, the headers
// and the body of the response.
func adminResponse(t *testing.T, app *fiber.App, path string, headers map[string]string) (status int, head http.Header, body string) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)

	for name, value := range headers {
		req.Header.Set(name, value)
	}

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}

	return res.StatusCode, res.Header, string(raw)
}

func TestAdminRedirectsToTheUsersPage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
	}{
		{name: "the admin area", path: _adminPath},
		{name: "the admin area with a trailing slash", path: _adminPath + "/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			status, headers, _ := adminResponse(t, adminApp(), tc.path, nil)

			if status != http.StatusFound {
				t.Fatalf("status = %d, want %d", status, http.StatusFound)
			}

			if location := headers.Get("Location"); location != _adminUsersPath {
				t.Errorf("Location = %q, want %q", location, _adminUsersPath)
			}
		})
	}
}

func TestAdminRedirectsAnHtmxRequestInPlace(t *testing.T) {
	t.Parallel()

	status, headers, _ := adminResponse(t, adminApp(), _adminPath, map[string]string{"HX-Request": "true"})

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	location := headers.Get("HX-Location")

	if !strings.Contains(location, _adminUsersPath) {
		t.Errorf("HX-Location = %q, want it to point at %q", location, _adminUsersPath)
	}

	if !strings.Contains(location, "#page-content") {
		t.Errorf("HX-Location = %q, want it to swap into the page content", location)
	}
}

func TestAdminPagesRenderTheGreeting(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		path        string
		want        string
		headers     map[string]string
		notWantBody string
	}{
		{name: "the users page", path: _adminUsersPath, want: "Hello from Admin"},
		{name: "the users page with a trailing slash", path: _adminUsersPath + "/", want: "Hello from Admin"},
		{name: "the settings page", path: _adminSettingsPath, want: "Hello from Admin Settings"},
		{
			name:        "the fragment",
			path:        _adminUsersPath,
			want:        "Hello from Admin",
			headers:     map[string]string{"HX-Request-Type": "partial"},
			notWantBody: "<!DOCTYPE html>",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			status, _, body := adminResponse(t, adminApp(), tc.path, tc.headers)

			if status != http.StatusOK {
				t.Fatalf("status = %d, want %d", status, http.StatusOK)
			}

			if !strings.Contains(body, tc.want) {
				t.Errorf("body = %q, want it to contain %q", body, tc.want)
			}

			if tc.notWantBody != "" && strings.Contains(body, tc.notWantBody) {
				t.Errorf("body = %q, want it to not contain %q", body, tc.notWantBody)
			}
		})
	}
}
