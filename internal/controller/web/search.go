package web

import (
	"fmt"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/middleware"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/web/templates"
)

const (
	// _searchScopeFolder is the scope value that asks for the folder named by
	// from instead of the whole drive.
	_searchScopeFolder = "folder"
)

// searchPage renders the whole-drive search of the signed-in account. A search
// that started from a folder carries that folder in from, which only builds the
// links of the page: the identifier is validated first, and the use case still
// scopes the results to the account. The scope control goes back to the folder
// with the same term, so the search state is left behind.
func (c *Controller) searchPage(ctx *fiber.Ctx) error {
	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	from := c.searchFrom(ctx)
	if ctx.Query("scope") == _searchScopeFolder && from != "" {
		return middleware.Redirect(ctx, withSearch(_foldersPath+from, entity.CleanSearch(ctx.Query("q"))))
	}

	page, err := c.searchResults(ctx, userID, ctx.Query("q"), from, nil)
	if err != nil {
		c.l.Error(fmt.Errorf("web - searchPage - uc.folders.Search: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	return c.render(
		ctx,
		templates.Search(c.view(ctx), templates.SearchTitle, page),
		templates.SearchContent(page),
		templates.SearchTitle,
	)
}

// searchEntries answers the load-more sentinel of the search page: the next
// batch after the cursor, as rows alone so htmx replaces the sentinel with
// them. A missing or tampered cursor is a client error and never a restart at
// the first page.
func (c *Controller) searchEntries(ctx *fiber.Ctx) error {
	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	after, err := entity.DecodeEntryCursor(ctx.Query("after"))
	if err != nil || after == nil {
		return ctx.SendStatus(http.StatusBadRequest)
	}

	page, err := c.searchResults(ctx, userID, ctx.Query("q"), c.searchFrom(ctx), after)
	if err != nil {
		c.l.Error(fmt.Errorf("web - searchEntries - uc.folders.Search: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	return c.renderPage(ctx, templates.SearchEntryRows(page))
}

// searchResults loads one page of the whole-drive search as the page the
// templates render.
func (c *Controller) searchResults(
	ctx *fiber.Ctx,
	userID, search, from string,
	after *entity.EntryCursor,
) (response.Drive, error) {
	view, err := c.folders.Search(ctx.UserContext(), userID, search, from, after)
	if err != nil {
		return response.Drive{}, fmt.Errorf("uc.folders.Search: %w", err)
	}

	return response.NewSearch(&view), nil
}

// searchFrom reads the folder a search came from. It is validated like any
// identifier from the address bar, and a malformed value is dropped rather than
// followed, because it only builds links and scope redirects.
func (c *Controller) searchFrom(ctx *fiber.Ctx) string {
	from := ctx.Query("from")
	if !c.validID(from) {
		return ""
	}

	return from
}
