package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/middleware"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/request"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
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
		return c.renderErrors(ctx, http.StatusNotFound, []string{_folderNotFoundMessage})
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
		return c.renderErrors(ctx, http.StatusNotFound, []string{_folderNotFoundMessage})
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

// folderDestinationForm runs the shared boilerplate of folder destination
// forms: resolve the user, read the source folder, reject the root, list all
// folders, then render the template with the folder's current parent
// pre-selected.
func (c *Controller) folderDestinationForm(
	ctx *fiber.Ctx,
	rootMessage string,
	render func(folderID, currentFolderID string, folders []response.FolderOption) templ.Component,
) error {
	folderID := ctx.Params("folder")
	if !c.validID(folderID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{_folderNotFoundMessage})
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	folder, err := c.folders.Get(ctx.UserContext(), userID, folderID)
	if err != nil {
		return c.driveError(ctx, err, "folderDestinationForm - uc.folders.Get")
	}

	if folder.IsRoot() {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{rootMessage})
	}

	all, err := c.folders.ListAll(ctx.UserContext(), userID)
	if err != nil {
		return c.driveError(ctx, err, "folderDestinationForm - uc.folders.ListAll")
	}

	valid := movableFolders(all, folderID)

	currentFolderID := ""
	if folder.ParentID != nil {
		currentFolderID = *folder.ParentID
	}

	return c.renderPage(ctx, render(folderID, currentFolderID, response.FolderOptions(valid)))
}

// folderMoveForm fills the drawer with the form that moves one folder to
// another. The destination folder is picked from a select of the account's
// folders, indented by depth so the tree shape shows. The folder's current
// parent is pre-selected. The folder and its subtree are left out of the
// list, because moving into them would close a cycle.
func (c *Controller) folderMoveForm(ctx *fiber.Ctx) error {
	folderID := ctx.Params("folder")
	if !c.validID(folderID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{_folderNotFoundMessage})
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	folder, err := c.folders.Get(ctx.UserContext(), userID, folderID)
	if err != nil {
		return c.driveError(ctx, err, "folderMoveForm - uc.folders.Get")
	}

	if folder.IsRoot() {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"The root folder cannot be moved."})
	}

	all, err := c.folders.ListAll(ctx.UserContext(), userID)
	if err != nil {
		return c.driveError(ctx, err, "folderMoveForm - uc.folders.ListAll")
	}

	valid := movableFolders(all, folderID)

	currentFolderID := ""
	if folder.ParentID != nil {
		currentFolderID = *folder.ParentID
	}

	return c.renderPage(ctx, templates.FolderMoveContent(folderID, currentFolderID, response.FolderOptions(valid)))
}

// folderMove moves one folder to another and refreshes the table of the
// folder it used to sit in, so the visitor stays on the page they were on.
func (c *Controller) folderMove(ctx *fiber.Ctx) error {
	folderID := ctx.Params("folder")
	if !c.validID(folderID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{_folderNotFoundMessage})
	}

	var req request.FolderMove

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"Choose a folder."})
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	folder, err := c.folders.Get(ctx.UserContext(), userID, folderID)
	if err != nil {
		return c.driveError(ctx, err, "folderMove - uc.folders.Get")
	}

	currentFolderID := ""
	if folder.ParentID != nil {
		currentFolderID = *folder.ParentID
	}

	_, err = c.folders.Move(ctx.UserContext(), userID, folderID, req.FolderID)
	if err != nil {
		return c.driveError(ctx, err, "folderMove - uc.folders.Move")
	}

	ctx.Set(_hxTriggerHeader, _entrySavedEvent)

	return c.renderEntries(ctx, userID, currentFolderID)
}

// _folderActionReq is any request body that can be sanitized before validation.
type _folderActionReq interface {
	Sanitize()
}

// folderActionWithGet runs the shared boilerplate of folder actions that take a
// body, read the source folder first (to know which folder table to refresh
// after the action), then run the action. The action closure captures the
// concrete request type so it can read its fields.
func (c *Controller) folderActionWithGet(
	ctx *fiber.Ctx,
	req _folderActionReq,
	badRequest string,
	run func(context.Context, string, string) error,
	action string,
) error {
	folderID := ctx.Params("folder")
	if !c.validID(folderID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{_folderNotFoundMessage})
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	if err := ctx.BodyParser(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{badRequest})
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	folder, err := c.folders.Get(ctx.UserContext(), userID, folderID)
	if err != nil {
		return c.driveError(ctx, err, action+" - uc.folders.Get")
	}

	currentFolderID := ""
	if folder.ParentID != nil {
		currentFolderID = *folder.ParentID
	}

	if err := run(ctx.UserContext(), userID, folderID); err != nil {
		return c.driveError(ctx, err, action)
	}

	ctx.Set(_hxTriggerHeader, _entrySavedEvent)

	return c.renderEntries(ctx, userID, currentFolderID)
}

// folderDuplicateForm fills the drawer with the form that copies one folder to
// another folder. The destination folder is picked from a select of the
// account's folders, with the folder's current parent pre-selected. The folder
// and its subtree are left out of the list, because copying into them would
// close a cycle.
func (c *Controller) folderDuplicateForm(ctx *fiber.Ctx) error {
	return c.folderDestinationForm(ctx, "The root folder cannot be duplicated.", templates.FolderDuplicateContent)
}

// folderDuplicate copies one folder and everything under it to another folder
// and refreshes the table of the folder the source sits in, so the visitor
// stays on the page they were on.
func (c *Controller) folderDuplicate(ctx *fiber.Ctx) error {
	var req request.FolderDuplicate

	return c.folderActionWithGet(
		ctx,
		&req,
		"Choose a folder.",
		func(ctx context.Context, userID, folderID string) error {
			_, err := c.folders.Duplicate(ctx, userID, folderID, req.FolderID)

			return err
		},
		"folderDuplicate - uc.folders.Duplicate",
	)
}

// folderTree fills the drawer with an expandable tree of every folder of the
// account. Clicking a folder expands its children; the arrow navigates into it.
func (c *Controller) folderTree(ctx *fiber.Ctx) error {
	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	all, err := c.folders.ListAll(ctx.UserContext(), userID)
	if err != nil {
		return c.driveError(ctx, err, "folderTree - uc.folders.ListAll")
	}

	return c.renderPage(ctx, templates.FolderTreeContent(response.FolderTree(all)))
}

// folderDelete removes a folder with everything inside it and sends the
// visitor to the folder that held it. The root folder is refused by the use
// case, so a hand-made request cannot wipe the drive.
func (c *Controller) folderDelete(ctx *fiber.Ctx) error {
	folderID := ctx.Params("folder")
	if !c.validID(folderID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{_folderNotFoundMessage})
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

// folderDownloadZip streams a zip archive of the folder and everything under
// it, so a whole directory can be downloaded at once. The per-user download
// speed limit applies to the stream.
func (c *Controller) folderDownloadZip(ctx *fiber.Ctx) error {
	folderID := ctx.Params("folder")

	if folderID != "" && !c.validID(folderID) {
		return middleware.Redirect(ctx, _homePath)
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	reader, filename, err := c.folders.DownloadZip(ctx.UserContext(), userID, folderID)
	if err != nil {
		c.l.Error(fmt.Errorf("web - folderDownloadZip - uc.folders.DownloadZip: %w", err))

		return middleware.Redirect(ctx, _homePath)
	}

	setDownloadHeaders(ctx, "application/zip", filename)

	// The server closes the stream after it writes the response. Closing it
	// here would close the pipe before the archive is read.
	return ctx.SendStream(reader)
}

// movableFolders drops the folder being moved and everything under it from the
// list of possible destinations, so a move cannot close a cycle in the tree. It
// also drops destinations that would push the moved subtree past the deepest
// level the drive allows.
func movableFolders(folders []entity.Folder, rootID string) []entity.Folder {
	excluded := map[string]bool{rootID: true}

	changed := true
	for changed {
		changed = false

		for i := range folders {
			f := folders[i]
			if excluded[f.ID] {
				continue
			}

			if f.ParentID != nil && excluded[*f.ParentID] {
				excluded[f.ID] = true
				changed = true
			}
		}
	}

	sourceHeight := entity.FolderSubtreeHeight(folders, rootID)

	out := make([]entity.Folder, 0, len(folders))
	for i := range folders {
		f := folders[i]
		if excluded[f.ID] {
			continue
		}

		if entity.FolderDepth(folders, f.ID)+1+sourceHeight > entity.MaxFolderDepth {
			continue
		}

		out = append(out, f)
	}

	return out
}
