package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/internal/usecase"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

const (
	// _searchTestUser is the account behind a search test request.
	_searchTestUser = "user-1"

	// _searchTestFolder is the folder a search test came from. It is a version
	// 4 UUID because the page validates the value before it builds a path.
	_searchTestFolder = "b6a1d2c3-4e5f-4a6b-8c7d-9e0f1a2b3c4d"

	// _searchTestMinRunes is the rune count below which the use case refuses to
	// run a search. The fake mirrors it, so a test sees the prompt the real
	// view produces.
	_searchTestMinRunes = 3
)

// A term too short to run never reaches the listing: the page shows the prompt
// and no result table, and the use case decides that with the cleaned term.
func TestSearchPageRendersThePromptForAShortQuery(t *testing.T) {
	t.Parallel()

	folders := &searchFolder{}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/search?q=ab", http.NoBody)

	status, _, body := driveResponse(t, searchApp(folders), req)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if folders.searchCalls != 1 {
		t.Errorf("Search calls = %d, want 1", folders.searchCalls)
	}

	if folders.search != "ab" {
		t.Errorf("search = %q, want %q", folders.search, "ab")
	}

	if !strings.Contains(body, "Type at least three characters to search your drive.") {
		t.Errorf("body = %q, want the short-query prompt", body)
	}

	if strings.Contains(body, "<table") {
		t.Errorf("body = %q, want no result table for a short query", body)
	}
}

// The scope control of the search page goes back to the folder the search came
// from, with the same term and through the same redirect the folder page uses.
func TestSearchScopeFolderRedirectsBack(t *testing.T) {
	t.Parallel()

	want := "/folders/" + _searchTestFolder + "?q=note"
	path := "/search?q=note&from=" + _searchTestFolder + "&scope=folder"

	t.Run("a browser navigation", func(t *testing.T) {
		t.Parallel()

		folders := &searchFolder{}

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)

		status, header, _ := driveResponse(t, searchApp(folders), req)

		if status != http.StatusFound {
			t.Errorf("status = %d, want %d", status, http.StatusFound)
		}

		if got := header.Get(fiber.HeaderLocation); got != want {
			t.Errorf("Location = %q, want %q", got, want)
		}

		if folders.searchCalls != 0 {
			t.Errorf("Search calls = %d, want 0", folders.searchCalls)
		}
	})

	t.Run("an htmx request", func(t *testing.T) {
		t.Parallel()

		folders := &searchFolder{}

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody)
		req.Header.Set(_hxRequestHeader, "true")

		_, header, _ := driveResponse(t, searchApp(folders), req)

		if got := header.Get("HX-Location"); !strings.Contains(got, want) {
			t.Errorf("HX-Location = %q, want it to contain %q", got, want)
		}

		if folders.searchCalls != 0 {
			t.Errorf("Search calls = %d, want 0", folders.searchCalls)
		}
	})
}

// A scope change to a folder cannot be followed without a valid identifier: the
// page stays on search, and the value only ever builds links, never a query.
func TestSearchScopeFolderKeepsAMalformedFromOnSearch(t *testing.T) {
	t.Parallel()

	folders := &searchFolder{}

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/search?q=note&from=folder-1&scope=folder",
		http.NoBody,
	)

	status, header, _ := driveResponse(t, searchApp(folders), req)

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}

	if got := header.Get(fiber.HeaderLocation); got != "" {
		t.Errorf("Location = %q, want no redirect", got)
	}

	if folders.from != "" {
		t.Errorf("from = %q, want the invalid value dropped", folders.from)
	}
}

// A partial result must never restart at the first page: an absent or tampered
// cursor is a client error, and the answer carries no rows.
func TestSearchPartialRejectsABadCursor(t *testing.T) {
	t.Parallel()

	const search = "/htmx/search?q=note"

	cases := []struct {
		name string
		path string
	}{
		{name: "a missing cursor", path: search},
		{name: "a tampered cursor", path: search + "&after=not-a-cursor"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, http.NoBody)

			status, _, body := driveResponse(t, searchApp(&searchFolder{}), req)

			if status != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", status, http.StatusBadRequest)
			}

			if strings.Contains(body, "<tr") {
				t.Errorf("body = %q, want no rows", body)
			}
		})
	}
}

// The page runs the whole-drive search of the signed-in account, under the term
// and the source folder the address bar carries.
func TestSearchPassesTheOwnerAndFromToTheUseCase(t *testing.T) {
	t.Parallel()

	folders := &searchFolder{}

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/search?q=note&from="+_searchTestFolder,
		http.NoBody,
	)

	status, _, body := driveResponse(t, searchApp(folders), req)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if folders.userID != _searchTestUser {
		t.Errorf("userID = %q, want %q", folders.userID, _searchTestUser)
	}

	if folders.search != "note" {
		t.Errorf("search = %q, want %q", folders.search, "note")
	}

	if folders.from != _searchTestFolder {
		t.Errorf("from = %q, want %q", folders.from, _searchTestFolder)
	}

	if !strings.Contains(body, `value="note"`) {
		t.Errorf("body = %q, want the form to carry the term", body)
	}

	if !strings.Contains(body, `name="from" value="`+_searchTestFolder+`"`) {
		t.Errorf("body = %q, want the form to carry the source folder", body)
	}
}

// searchFolder records the drive-wide search a page or partial route ran and
// answers with a canned view.
type searchFolder struct {
	usecase.Folder

	view        entity.SearchView
	userID      string
	search      string
	from        string
	after       *entity.EntryCursor
	searchCalls int
}

func (f *searchFolder) Search(
	_ context.Context,
	userID, search, from string,
	after *entity.EntryCursor,
) (entity.SearchView, error) {
	f.searchCalls++
	f.userID = userID
	f.search = search
	f.from = from
	f.after = after

	view := f.view
	view.Search = search
	view.From = from
	view.TooShort = utf8.RuneCountInString(search) < _searchTestMinRunes

	return view, nil
}

// searchApp wires the search page and partial routes behind a middleware that
// parks the session the auth guard would park.
func searchApp(folders usecase.Folder) *fiber.App {
	c := &Controller{l: logger.New("error"), v: validator.New(), folders: folders}

	app := fiber.New()

	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Locals(_sessionLocal, entity.Session{UserID: _searchTestUser})

		return ctx.Next()
	})

	app.Get("/search", c.searchPage)
	app.Get("/htmx/search", c.searchEntries)

	return app
}
