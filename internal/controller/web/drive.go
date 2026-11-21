package web

import (
	"errors"
	"fmt"
	"net/http"
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

// folderPage renders one folder of the signed-in account. A malformed
// identifier cannot reach a UUID column, so it looks like a folder that is
// gone and sends the visitor home.
func (c *Controller) folderPage(ctx *fiber.Ctx) error {
	folderID := ctx.Params("folder")
	if !c.validID(folderID) {
		return middleware.Redirect(ctx, _homePath)
	}

	return c.drive(ctx, folderID)
}

// drive renders a folder page: the full page on a browser navigation, the
// content fragment when htmx swaps it into the shell. A folder that is gone —
// deleted in another tab, or a stale bookmark — sends the visitor home.
func (c *Controller) drive(ctx *fiber.Ctx, folderID string) error {
	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	page, err := c.drivePage(ctx, userID, folderID)
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

// drivePage loads one folder of the account as the page the templates render.
func (c *Controller) drivePage(ctx *fiber.Ctx, userID, folderID string) (response.Drive, error) {
	view, err := c.folders.List(ctx.UserContext(), userID, folderID, "", nil)
	if err != nil {
		return response.Drive{}, err
	}

	return response.NewDrive(&view), nil
}

// renderEntries answers a successful drive action with the table of the
// folder it changed, so htmx swaps the rows in place.
func (c *Controller) renderEntries(
	ctx *fiber.Ctx,
	userID, folderID string,
) error {
	page, err := c.drivePage(ctx, userID, folderID)
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
