package web

import (
	"context"
	"errors"
	"fmt"
	"math"
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

// fileNew fills the drawer with the form that uploads one file into the folder
// on the screen.
func (c *Controller) fileNew(ctx *fiber.Ctx) error {
	ctx.Set("Cache-Control", "no-store")

	return c.renderPage(ctx, templates.FileUploadContent(ctx.Query("folder")))
}

// fileDrawer fills the drawer with the actions of one file: the download
// link, the rename form and the delete form.
func (c *Controller) fileDrawer(ctx *fiber.Ctx) error {
	fileID := ctx.Params("file")
	if !c.validID(fileID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{_fileNotFoundMessage})
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	file, err := c.files.Get(ctx.UserContext(), userID, fileID)
	if err != nil {
		return c.driveError(ctx, err, "fileDrawer - uc.files.Get")
	}

	return c.renderPage(ctx, templates.FileDrawerContent(response.NewFileInfo(&file)))
}

// _fileActionReq is any request body that can be sanitized before validation.
type _fileActionReq interface {
	Sanitize()
}

// fileAction runs the shared boilerplate of file actions that take a body and
// refresh the file's folder: decode, validate, resolve the user, run the
// action, then re-render the folder table. The action closure captures the
// concrete request type so it can read its fields.
func (c *Controller) fileAction(
	ctx *fiber.Ctx,
	req _fileActionReq,
	badRequest string,
	run func(context.Context, string, string) (entity.File, error),
	action string,
) error {
	fileID := ctx.Params("file")
	if !c.validID(fileID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{_fileNotFoundMessage})
	}

	if err := ctx.BodyParser(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{badRequest})
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	file, err := run(ctx.UserContext(), userID, fileID)
	if err != nil {
		return c.driveError(ctx, err, action)
	}

	ctx.Set(_hxTriggerHeader, _entrySavedEvent)

	return c.renderEntries(ctx, userID, file.FolderID)
}

// fileRename changes the name of one file and refreshes the table it sits in.
func (c *Controller) fileRename(ctx *fiber.Ctx) error {
	var req request.FileRename

	return c.fileAction(
		ctx,
		&req,
		"Enter a name for the file.",
		func(ctx context.Context, userID, fileID string) (entity.File, error) {
			return c.files.Rename(ctx, userID, fileID, req.Name)
		},
		"fileRename - uc.files.Rename",
	)
}

// fileActionWithGet runs the shared boilerplate of file actions that take a
// body, read the source file first (to know which folder table to refresh
// after the action), then run the action. The action closure captures the
// concrete request type so it can read its fields.
func (c *Controller) fileActionWithGet(
	ctx *fiber.Ctx,
	req _fileActionReq,
	badRequest string,
	run func(context.Context, string, string) error,
	action string,
) error {
	fileID := ctx.Params("file")
	if !c.validID(fileID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{_fileNotFoundMessage})
	}

	if err := ctx.BodyParser(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{badRequest})
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	file, err := c.files.Get(ctx.UserContext(), userID, fileID)
	if err != nil {
		return c.driveError(ctx, err, "fileActionWithGet - uc.files.Get")
	}

	currentFolderID := file.FolderID

	if err := run(ctx.UserContext(), userID, fileID); err != nil {
		return c.driveError(ctx, err, action)
	}

	ctx.Set(_hxTriggerHeader, _entrySavedEvent)

	return c.renderEntries(ctx, userID, currentFolderID)
}

// fileMove moves one file to another folder and refreshes the table of the
// folder the file came from, so the visitor stays on the page they were on.
func (c *Controller) fileMove(ctx *fiber.Ctx) error {
	var req request.FileMove

	return c.fileActionWithGet(
		ctx,
		&req,
		"Choose a folder.",
		func(ctx context.Context, userID, fileID string) error {
			_, err := c.files.Move(ctx, userID, fileID, req.FolderID)

			return err
		},
		"fileMove - uc.files.Move",
	)
}

// fileDuplicate copies one file to another folder and refreshes the table of
// the folder the file came from, so the visitor stays on the page they were on.
func (c *Controller) fileDuplicate(ctx *fiber.Ctx) error {
	var req request.FileDuplicate

	return c.fileActionWithGet(
		ctx,
		&req,
		"Choose a destination folder.",
		func(ctx context.Context, userID, fileID string) error {
			_, err := c.files.Duplicate(ctx, userID, fileID, req.FolderID)

			return err
		},
		"fileDuplicate - uc.files.Duplicate",
	)
}

// fileDestinationForm runs the shared boilerplate of file destination forms:
// resolve the user, read the source file, list all folders, then render the
// template with the file's current folder pre-selected.
func (c *Controller) fileDestinationForm(
	ctx *fiber.Ctx,
	render func(fileID, currentFolderID string, folders []response.FolderOption) templ.Component,
) error {
	fileID := ctx.Params("file")
	if !c.validID(fileID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{_fileNotFoundMessage})
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	file, err := c.files.Get(ctx.UserContext(), userID, fileID)
	if err != nil {
		return c.driveError(ctx, err, "fileDestinationForm - uc.files.Get")
	}

	folders, err := c.folders.ListAll(ctx.UserContext(), userID)
	if err != nil {
		return c.driveError(ctx, err, "fileDestinationForm - uc.folders.ListAll")
	}

	return c.renderPage(ctx, render(fileID, file.FolderID, response.FolderOptions(folders)))
}

// fileMoveForm fills the drawer with the form that moves one file to another
// folder. The destination folder is picked from a select of the account's
// folders, with the file's current folder pre-selected.
func (c *Controller) fileMoveForm(ctx *fiber.Ctx) error {
	return c.fileDestinationForm(ctx, templates.FileMoveContent)
}

// fileDuplicateForm fills the drawer with the form that copies one file to
// another folder. The destination folder is picked from a select of the
// account's folders, with the file's current folder pre-selected.
func (c *Controller) fileDuplicateForm(ctx *fiber.Ctx) error {
	return c.fileDestinationForm(ctx, templates.FileDuplicateContent)
}

// fileDelete removes one file and the object behind it, then refreshes the
// table it sat in.
func (c *Controller) fileDelete(ctx *fiber.Ctx) error {
	fileID := ctx.Params("file")
	if !c.validID(fileID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{_fileNotFoundMessage})
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	file, err := c.files.Delete(ctx.UserContext(), userID, fileID)
	if err != nil && !errors.Is(err, entity.ErrStorageCleanup) {
		return c.driveError(ctx, err, "fileDelete - uc.files.Delete")
	}

	if err != nil {
		c.requestLogger(ctx).Error(fmt.Errorf("web - fileDelete - uc.files.Delete: %w", err))
	}

	ctx.Set(_hxTriggerHeader, _entrySavedEvent)

	return c.renderEntries(ctx, userID, file.FolderID)
}

// streamLength returns the length Fiber takes for a streamed body. It reports
// false when the size is not a positive length or does not fit an int on this
// build, so the caller streams without a length instead of sending a truncated
// or negative Content-Length.
func streamLength(size int64) (int, bool) {
	if size <= 0 || size > int64(math.MaxInt) {
		return 0, false
	}

	return int(size), true
}

// fileDownload streams the bytes of one file through the app, so the per-user
// download speed limit applies. The browser saves it under the file's name.
func (c *Controller) fileDownload(ctx *fiber.Ctx) error {
	fileID := ctx.Params("file")
	if !c.validID(fileID) {
		return middleware.Redirect(ctx, _homePath)
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	reader, file, err := c.files.OpenDownload(ctx.UserContext(), userID, fileID)
	if err != nil {
		if !errors.Is(err, entity.ErrFileNotFound) {
			c.requestLogger(ctx).Error(fmt.Errorf("web - fileDownload - uc.files.OpenDownload: %w", err))
		}

		return middleware.Redirect(ctx, _homePath)
	}

	setDownloadHeaders(ctx, file.ContentType, file.Name)

	// The size keeps the response determinate, so the browser can show real
	// download progress while the body still streams. A size an int cannot
	// carry falls back to a chunked body rather than a truncated header.
	if length, ok := streamLength(file.Size); ok {
		return ctx.SendStream(reader, length)
	}

	// The server closes the stream after it writes the response. Closing it
	// here would close the object before the body is read.
	return ctx.SendStream(reader)
}
