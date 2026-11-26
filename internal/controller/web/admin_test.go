package web

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/request"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
	"github.com/minhhoccode111/fileuploader/web/templates"
)

// adminApp wires the admin routes in front of the handlers under test.
func adminApp() *fiber.App {
	app := fiber.New()
	c := &Controller{cfg: &config.Config{}}

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

// The page carries the drawer the account editor opens from, even before any
// account reached it.
func TestAdminUsersPageHoldsTheEditDrawer(t *testing.T) {
	t.Parallel()

	status, _, body := adminResponse(t, adminApp(), _adminUsersPath, nil)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	want := []string{
		`id="user-edit-drawer"`,
		`id="user-edit-panel"`,
	}

	for _, part := range want {
		if !strings.Contains(body, part) {
			t.Errorf("body = %q, want it to contain %q", body, part)
		}
	}
}

// The edit form the drawer fills carries the fields of the account and points
// its save at the edit endpoint, its errors at the error line.
func TestAdminUsersEditFormCarriesTheFields(t *testing.T) {
	t.Parallel()

	row := response.UserRow{ID: "550e8400-e29b-41d4-a716-446655440000", Username: "johndoe", Email: "john@example.com", IsAdmin: true}

	page := renderComponent(t, templates.AdminUsersEditContent(row))

	want := []string{
		`id="user-edit-form"`,
		`hx-post="/htmx/admin/users/edit"`,
		`hx-status:4xx="target:#user-edit-error"`,
		`value="550e8400-e29b-41d4-a716-446655440000"`,
		`value="johndoe"`,
		`value="john@example.com"`,
		`<option value="admin" selected>admin</option>`,
		`placeholder="Leave empty to keep the current one"`,
	}

	for _, part := range want {
		if !strings.Contains(page, part) {
			t.Errorf("body = %q, want it to contain %q", page, part)
		}
	}

	saved := renderComponent(t, templates.AdminUsersEditDone(row, true))

	if !strings.Contains(saved, "Every live session of the account was ended") {
		t.Errorf("body = %q, want the note about the ended sessions", saved)
	}
}

// A column title links to the table ordered by that column, and the next
// click on the column the table is ordered by turns the order around. The
// link keeps the filters, so the view stays bookmarkable.
func TestAdminUsersSortHeader(t *testing.T) {
	t.Parallel()

	filters := response.UserFilters{
		Query: "john",
		Role:  "admin",
		Limit: "10",
		Sort:  "username",
		Dir:   "asc",
	}

	active := renderComponent(t, templates.AdminUsersSortHeader(filters, "username", "Username"))

	want := []string{
		`aria-sort="ascending"`,
		`href="/admin/users?`,
		`sort=username`,
		`dir=desc`,
		`q=john`,
		`role=admin`,
		`hx-get="/htmx/admin/users"`,
		`hx-include="#user-filters, #user-limit"`,
		`hx-target="#admin-users"`,
		`>Username`,
	}

	for _, part := range want {
		if !strings.Contains(active, part) {
			t.Errorf("header = %q, want it to contain %q", active, part)
		}
	}

	other := renderComponent(t, templates.AdminUsersSortHeader(filters, "email", "Email"))

	if !strings.Contains(other, `aria-sort="none"`) {
		t.Errorf("header = %q, want an unsorted column", other)
	}

	if !strings.Contains(other, "sort=email") || !strings.Contains(other, "dir=asc") {
		t.Errorf("header = %q, want the first click to sort the column ascending", other)
	}
}

func TestAdminUsersTableShowsTheStorage(t *testing.T) {
	t.Parallel()

	rows := []response.UserRow{{
		ID:                "1",
		Username:          "johndoe",
		StorageUsedBytes:  1536,
		StorageQuotaBytes: 1048576,
	}}

	page := renderComponent(t, templates.AdminUsersTable(rows, response.UserPager{Page: 1, Pages: 1}, response.UserFilters{Limit: "10"}))

	want := []string{
		"Storage used",
		"1.5 KB / 1.0 MB",
		"sort=storage_used_bytes",
	}

	for _, part := range want {
		if !strings.Contains(page, part) {
			t.Errorf("table = %q, want it to contain %q", page, part)
		}
	}
}

func TestUserFiltersCarryTheSort(t *testing.T) {
	t.Parallel()

	req := request.UserListQuery{Sort: "username", Dir: "asc"}
	filters := userFilters(&req)

	if filters.Sort != "username" || filters.Dir != "asc" {
		t.Errorf("sort = %q %q, want %q %q", filters.Sort, filters.Dir, "username", "asc")
	}

	empty := request.UserListQuery{}
	fallback := userFilters(&empty)

	if fallback.Sort != "created_at" || fallback.Dir != "desc" {
		t.Errorf("sort = %q %q, want the default order", fallback.Sort, fallback.Dir)
	}
}

// renderComponent renders one component into its HTML text.
func renderComponent(t *testing.T, component templ.Component) string {
	t.Helper()

	buf := new(bytes.Buffer)

	err := component.Render(context.Background(), buf)
	if err != nil {
		t.Fatalf("component.Render() error = %v, want nil", err)
	}

	return buf.String()
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
		{name: "the users page", path: _adminUsersPath, want: "Users"},
		{name: "the users page with a trailing slash", path: _adminUsersPath + "/", want: "Users"},
		{name: "the settings page", path: _adminSettingsPath, want: "Hello from Admin Settings"},
		{
			name:        "the fragment",
			path:        _adminUsersPath,
			want:        "Users",
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
