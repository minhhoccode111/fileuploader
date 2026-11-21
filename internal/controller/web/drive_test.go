package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

// A drive action that would push the account past its quota is a client
// problem, not a server fault: the handler must answer 413 and say why.
func TestDriveErrorMapsStorageQuotaExceeded(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	c := &Controller{l: logger.New("error")}

	app.Get("/", func(ctx *fiber.Ctx) error {
		return c.driveError(ctx, entity.ErrStorageQuotaExceeded, "test")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}

	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", res.StatusCode, http.StatusRequestEntityTooLarge)
	}

	if len(body) == 0 {
		t.Error("body is empty, want the quota message")
	}
}

// A partial listing must never restart at the first page: an absent or
// tampered cursor is a client error, and the answer carries no rows.
func TestFolderEntriesRejectsAMalformedCursor(t *testing.T) {
	t.Parallel()

	const entries = "/htmx/folders/550e8400-e29b-41d4-a716-446655440000/entries"

	cases := []struct {
		name string
		path string
	}{
		{name: "a missing cursor", path: entries},
		{name: "a tampered cursor", path: entries + "?after=not-a-cursor"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			status, body := folderEntriesResponse(t, tc.path)

			if status != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", status, http.StatusBadRequest)
			}

			if strings.Contains(body, "<tr") {
				t.Errorf("body = %q, want no rows", body)
			}
		})
	}
}

// folderEntriesResponse sends one request to the partial route and returns the
// status and the body of the answer.
func folderEntriesResponse(t *testing.T, path string) (status int, body string) {
	t.Helper()

	app := fiber.New()
	c := &Controller{l: logger.New("error"), v: validator.New()}

	app.Get("/htmx/folders/:folder/entries", func(ctx *fiber.Ctx) error {
		ctx.Locals(_sessionLocal, entity.Session{UserID: "550e8400-e29b-41d4-a716-446655440001"})

		return c.folderEntries(ctx)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v, want nil", err)
	}

	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}

	return res.StatusCode, string(raw)
}
