package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/middleware"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/web/templates"
)

const (
	// _foldersPath is the page route of a folder. The home route renders the
	// root, so only its children use it.
	_foldersPath = "/folders/"

	// _searchPath is the whole-drive search page the scope control leaves for.
	// The folder page answers a scope change with a redirect rather than a
	// second rendering path, so plain HTML and htmx both land on the same page.
	_searchPath = "/search"

	// _scopeAll is the scope value that asks for the whole drive instead of the
	// folder on the screen.
	_scopeAll = "all"

	// _idValidation is the rule every identifier from a path has to pass
	// before it reaches a query, so a malformed path is a caller error.
	_idValidation = "required,uuid4"

	// _entrySavedEvent tells the drawer that an action succeeded and the
	// panel can close. The handlers set it as the HX-Trigger header.
	_entrySavedEvent = "entrySaved"
)

// home renders the root folder of the signed-in account.
func (c *Controller) home(ctx *fiber.Ctx) error {
	return c.drive(ctx, "")
}

// folderPage renders one folder of the signed-in account.
func (c *Controller) folderPage(ctx *fiber.Ctx) error {
	return c.drive(ctx, ctx.Params("folder"))
}

// drive renders a folder page: the full page on a browser navigation, the
// content fragment when htmx swaps it into the shell. A folder that is gone —
// deleted in another tab, or a stale bookmark — sends the visitor home.
//
// The scope control is answered before anything is read: a request for the
// whole drive leaves for the search page, and the folder identifier only
// becomes the way back. A malformed identifier cannot reach a UUID column, so
// it looks like a folder that is gone and sends the visitor home.
func (c *Controller) drive(ctx *fiber.Ctx, folderID string) error {
	if folderID != "" && !c.validID(folderID) {
		return middleware.Redirect(ctx, _homePath)
	}

	if path := wholeDrivePath(ctx, folderID); path != "" {
		return middleware.Redirect(ctx, path)
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	page, err := c.drivePage(ctx, userID, folderID, ctx.Query("q"))
	if err != nil {
		if errors.Is(err, entity.ErrFolderNotFound) {
			return middleware.Redirect(ctx, _homePath)
		}

		c.l.Error(fmt.Errorf("web - drive - uc.folders.List: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	title := templates.HomeTitle
	if !page.IsRoot {
		title = page.FolderName
	}

	return c.render(ctx, templates.Drive(c.view(ctx), title, page), templates.DriveContent(page), title)
}

// drivePage loads one folder of the account, narrowed by the search on the
// page, as the page the templates render.
func (c *Controller) drivePage(
	ctx *fiber.Ctx,
	userID, folderID, search string,
) (response.Drive, error) {
	view, err := c.folders.List(ctx.UserContext(), userID, folderID, search, nil)
	if err != nil {
		return response.Drive{}, err
	}

	return response.NewDrive(&view), nil
}

// wholeDrivePath builds the path the scope control of a folder page asks for.
// The search page carries the term and the folder it came from, so its scope
// control can bring the visitor back. Home names no folder, so its search has
// no way back. An empty path means the request stays on the folder page.
func wholeDrivePath(ctx *fiber.Ctx, folderID string) string {
	if ctx.Query("scope") != _scopeAll {
		return ""
	}

	query := url.Values{}

	if search := entity.CleanSearch(ctx.Query("q")); search != "" {
		query.Set("q", search)
	}

	if folderID != "" {
		query.Set("from", folderID)
	}

	if len(query) == 0 {
		return _searchPath
	}

	return _searchPath + "?" + query.Encode()
}

// entrySearch reads the local search of the page an action was submitted from:
// the query string when the action carried it (upload completion is a plain
// fetch), the URL htmx reports otherwise. Cleaning runs here too, so a hand-made
// header cannot widen the term the listing runs with.
func (c *Controller) entrySearch(ctx *fiber.Ctx) string {
	raw := ctx.Query("q")
	if raw == "" {
		raw = searchFromURL(ctx.Get(_hxCurrentURLHeader))
	}

	return entity.CleanSearch(raw)
}

// searchFromURL reads the local search out of the URL of the page on screen. A
// value that is not a URL means no search, so a missing or odd header never
// fails a refresh.
func searchFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	return parsed.Query().Get("q")
}

// withSearch puts the active local search on a folder path, so a redirect lands
// on the same filtered view the visitor left. An empty search leaves the path
// alone.
func withSearch(path, search string) string {
	if search == "" {
		return path
	}

	query := url.Values{}
	query.Set("q", search)

	return path + "?" + query.Encode()
}

// folderEntries answers the load-more sentinel of a folder listing: the next
// batch after the cursor, as rows alone so htmx replaces the sentinel with
// them. A missing or tampered cursor is a client error and never a restart at
// the first page.
func (c *Controller) folderEntries(ctx *fiber.Ctx) error {
	folderID := ctx.Params("folder")
	if !c.validID(folderID) {
		return ctx.SendStatus(http.StatusBadRequest)
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	after, err := entity.DecodeEntryCursor(ctx.Query("after"))
	if err != nil || after == nil {
		return ctx.SendStatus(http.StatusBadRequest)
	}

	view, err := c.folders.List(ctx.UserContext(), userID, folderID, ctx.Query("q"), after)
	if err != nil {
		if errors.Is(err, entity.ErrFolderNotFound) {
			return middleware.Redirect(ctx, _homePath)
		}

		c.l.Error(fmt.Errorf("web - folderEntries - uc.folders.List: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	return c.renderPage(ctx, templates.EntryRows(response.NewDrive(&view)))
}

// renderEntries answers a successful drive action with the table of the
// folder it changed, under the search the page was showing, so htmx swaps in
// rows that match the input the visitor still sees.
func (c *Controller) renderEntries(
	ctx *fiber.Ctx,
	userID, folderID string,
) error {
	page, err := c.drivePage(ctx, userID, folderID, c.entrySearch(ctx))
	if err != nil {
		if errors.Is(err, entity.ErrFolderNotFound) {
			return middleware.Redirect(ctx, _homePath)
		}

		c.l.Error(fmt.Errorf("web - renderEntries - uc.folders.List: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	return c.renderPage(ctx, templates.DriveEntries(page))
}

// driveError maps a failed drive action to a status and a message the caller
// may see. Only the entity sentinels are translated; everything else is a
// server fault and is logged instead of leaked.
//
//nolint:cyclop // one branch per sentinel the drive can return
func (c *Controller) driveError(ctx *fiber.Ctx, err error, action string) error {
	if errors.Is(err, entity.ErrFolderTooDeep) {
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"That would nest folders deeper than this server allows."},
		)
	}

	switch {
	case errors.Is(err, entity.ErrFolderNotFound):
		return c.renderErrors(ctx, http.StatusNotFound, []string{_folderNotFoundMessage})

	case errors.Is(err, entity.ErrFileNotFound):
		return c.renderErrors(ctx, http.StatusNotFound, []string{_fileNotFoundMessage})

	case errors.Is(err, entity.ErrEntryAlreadyExists):
		return c.renderErrors(
			ctx,
			http.StatusConflict,
			[]string{"A folder or file with that name is already here."},
		)

	case errors.Is(err, entity.ErrInvalidName):
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"Enter a name without slashes or reserved characters, up to 255 characters."},
		)

	case errors.Is(err, entity.ErrFileTooLarge):
		return c.renderErrors(
			ctx,
			http.StatusRequestEntityTooLarge,
			[]string{"That file is larger than this server accepts."},
		)

	case errors.Is(err, entity.ErrStorageQuotaExceeded):
		return c.renderErrors(
			ctx,
			http.StatusRequestEntityTooLarge,
			[]string{"That would exceed the storage quota of this account."},
		)

	case errors.Is(err, entity.ErrRootFolder):
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"The root folder cannot be changed."},
		)

	case errors.Is(err, entity.ErrFolderCycle):
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{folderCycleMessage(action)},
		)

	default:
		c.l.Error(fmt.Errorf("web - %s: %w", action, err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}
}

// folderCycleMessage words the cycle error for the action that triggered it.
func folderCycleMessage(action string) string {
	if strings.Contains(action, "Move") {
		return "That folder cannot be moved there."
	}

	return "A folder cannot be copied into itself or one of its folders."
}

// userID returns the account behind the request. The auth guard parks its
// session before the handler runs, so a missing value means the request did
// not go through the guard.
func (c *Controller) userID(ctx *fiber.Ctx) (string, bool) {
	session, ok := ctx.Locals(_sessionLocal).(entity.Session)
	if !ok || session.UserID == "" {
		return "", false
	}

	return session.UserID, true
}

// validID reports whether a path identifier can reach a UUID column.
func (c *Controller) validID(id string) bool {
	return c.v.Var(id, _idValidation) == nil
}
