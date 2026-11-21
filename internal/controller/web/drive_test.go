package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
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

			status, body := folderEntriesResponse(t, nil, tc.path)

			if status != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", status, http.StatusBadRequest)
			}

			if strings.Contains(body, "<tr") {
				t.Errorf("body = %q, want no rows", body)
			}
		})
	}
}

// The load-more link carries the search, so the partial must run the same
// filter: unfiltered rows appended under a filtered table would be wrong.
func TestFolderEntriesKeepsTheSearch(t *testing.T) {
	t.Parallel()

	const folderID = "550e8400-e29b-41d4-a716-446655440000"

	cursor := entity.EntryCursor{
		Kind: entity.EntryKindFile,
		Name: "notes.txt",
		ID:   "d9b1f4a6-4f5e-4b4a-9a39-2d8c7b6a5e44",
	}
	next := entity.EntryCursor{
		Kind: entity.EntryKindFile,
		Name: "zeta.txt",
		ID:   "0f6a1c2e-3b4d-4e5f-8a9b-0c1d2e3f4a5b",
	}

	folders := &driveFolder{view: entity.FolderView{
		Folder: entity.Folder{ID: folderID, Name: "/"},
		Files: []entity.FileItem{{
			ID:          "b1e2d3c4-5a6f-4b7c-8d9e-0f1a2b3c4d5e",
			Name:        "note.txt",
			ContentType: "text/plain",
			Size:        5,
		}},
		Search: "note",
		Next:   &next,
	}}

	path := "/htmx/folders/" + folderID + "/entries?q=note&after=" + cursor.Encode()

	status, body := folderEntriesResponse(t, folders, path)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if folders.search != "note" {
		t.Errorf("search = %q, want %q", folders.search, "note")
	}

	if folders.folderID != folderID {
		t.Errorf("folderID = %q, want %q", folders.folderID, folderID)
	}

	if folders.after == nil || *folders.after != cursor {
		t.Errorf("after = %v, want %v", folders.after, cursor)
	}

	for _, part := range []string{"note.txt", "Load more"} {
		if !strings.Contains(body, part) {
			t.Errorf("body = %q, want it to contain %q", body, part)
		}
	}
}

// driveFolder records the listing call of the partial route and answers with
// the canned view.
type driveFolder struct {
	usecase.Folder

	view     entity.FolderView
	folderID string
	search   string
	after    *entity.EntryCursor
}

func (f *driveFolder) List(
	_ context.Context,
	_, folderID, search string,
	after *entity.EntryCursor,
) (entity.FolderView, error) {
	f.folderID = folderID
	f.search = search
	f.after = after

	return f.view, nil
}

// folderEntriesResponse sends one request to the partial route and returns the
// status and the body of the answer.
func folderEntriesResponse(t *testing.T, folders usecase.Folder, path string) (status int, body string) {
	t.Helper()

	app := fiber.New()
	c := &Controller{l: logger.New("error"), v: validator.New(), folders: folders}

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
