package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/middleware"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/request"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
	"github.com/minhhoccode111/fileuploader/web/templates"
)

// folderNew fills the drawer with the form that creates a folder inside the
// folder on the screen.
func (c *Controller) folderNew(ctx *fiber.Ctx) error {
	return c.renderPage(ctx, templates.FolderCreateContent(ctx.Query("folder")))
}

// folderCreate makes the folder the drawer asked for and refreshes the table
// of the folder it belongs to. The drawer closes itself when the entry-saved
// event fires.
func (c *Controller) folderCreate(ctx *fiber.Ctx) error {
	var req request.FolderCreate

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"Enter the name of the folder."},
		)
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	_, err := c.folders.Create(ctx.UserContext(), userID, req.ParentID, req.Name)
	if err != nil {
		return c.driveError(ctx, err, "folderCreate - uc.folders.Create")
	}

	ctx.Set(_hxTriggerHeader, _entrySavedEvent)

	return c.renderEntries(ctx, userID, req.ParentID)
}

// folderEdit fills the drawer with the form that renames the folder on the
// screen. The root folder has no rename form, because its name is the anchor
// every path is built from.
func (c *Controller) folderEdit(ctx *fiber.Ctx) error {
	folderID := ctx.Params("folder")
	if !c.validID(folderID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{"That folder is not here any more."})
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	folder, err := c.folders.Get(ctx.UserContext(), userID, folderID)
	if err != nil {
		return c.driveError(ctx, err, "folderEdit - uc.folders.Get")
	}

	if folder.IsRoot() {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"The root folder cannot be renamed."})
	}

	return c.renderPage(ctx, templates.FolderEditContent(folder.ID, folder.Name))
}

// folderRename saves the name the drawer submitted and reloads the folder that
// holds it, so the row comes back under the new name. A direct rename of the
// folder on the screen lands on its parent, which is where the row lives.
func (c *Controller) folderRename(ctx *fiber.Ctx) error {
	folderID := ctx.Params("folder")
	if !c.validID(folderID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{"That folder is not here any more."})
	}

	var req request.FolderRename

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"Enter a name for the folder."})
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	folder, err := c.folders.Rename(ctx.UserContext(), userID, folderID, req.Name)
	if err != nil {
		return c.driveError(ctx, err, "folderRename - uc.folders.Rename")
	}

	parent, err := c.folders.Get(ctx.UserContext(), userID, *folder.ParentID)
	if err != nil {
		if !errors.Is(err, entity.ErrFolderNotFound) {
			c.l.Error(fmt.Errorf("web - folderRename - uc.folders.Get: %w", err))
		}

		return middleware.Redirect(ctx, _homePath)
	}

	path := _homePath
	if !parent.IsRoot() {
		path = _foldersPath + parent.ID
	}

	return middleware.Redirect(ctx, path)
}

// folderDelete removes a folder with everything inside it and sends the
// visitor to the folder that held it. The root folder is refused by the use
// case, so a hand-made request cannot wipe the drive.
func (c *Controller) folderDelete(ctx *fiber.Ctx) error {
	folderID := ctx.Params("folder")
	if !c.validID(folderID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{"That folder is not here any more."})
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	parent, err := c.folders.Delete(ctx.UserContext(), userID, folderID)
	if err != nil && !errors.Is(err, entity.ErrStorageCleanup) {
		return c.driveError(ctx, err, "folderDelete - uc.folders.Delete")
	}

	if err != nil {
		c.l.Error(fmt.Errorf("web - folderDelete - uc.folders.Delete: %w", err))
	}

	path := _homePath
	if !parent.IsRoot() {
		path = _foldersPath + parent.ID
	}

	return middleware.Redirect(ctx, path)
}
