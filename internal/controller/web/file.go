package web

import (
	"errors"
	"fmt"
	"net/http"

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
	return c.renderPage(ctx, templates.FileUploadContent(ctx.Query("folder")))
}

// fileUploadSubmit stores the one file the drawer form carried. The name is
// the one in the form, not the one the browser sent with the file, so the
// visitor can name it before it is stored. The body limit of the server and
// the use case both guard the size.
func (c *Controller) fileUploadSubmit(ctx *fiber.Ctx) error {
	var req request.FileUpload

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"Choose a file and give it a name."},
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

	header, err := ctx.FormFile("file")
	if err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"Choose a file to upload."})
	}

	if header.Size > c.cfg.Upload.MaxBytes {
		return c.renderErrors(
			ctx,
			http.StatusRequestEntityTooLarge,
			[]string{"That file is larger than this server accepts."},
		)
	}

	upload, err := header.Open()
	if err != nil {
		c.l.Error(fmt.Errorf("web - fileUploadSubmit - header.Open: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	defer func() {
		closeErr := upload.Close()
		if closeErr != nil {
			c.l.Error(fmt.Errorf("web - fileUploadSubmit - upload.Close: %w", closeErr))
		}
	}()

	_, err = c.files.Upload(
		ctx.UserContext(),
		userID,
		req.FolderID,
		req.Name,
		header.Header.Get(fiber.HeaderContentType),
		header.Size,
		upload,
	)
	if err != nil {
		return c.uploadError(ctx, err)
	}

	ctx.Set(_hxTriggerHeader, _entrySavedEvent)

	return c.renderEntries(ctx, userID, req.FolderID)
}

// uploadError maps a failed upload to the response the drawer shows. A delete
// that could not remove the object is logged, because the row is gone and the
// visitor cannot act on the leftover.
func (c *Controller) uploadError(ctx *fiber.Ctx, err error) error {
	if errors.Is(err, entity.ErrStorageCleanup) {
		c.l.Error(fmt.Errorf("web - fileUploadSubmit - uc.files.Upload cleanup: %w", err))
	}

	return c.driveError(ctx, err, "fileUploadSubmit - uc.files.Upload")
}

// fileDrawer fills the drawer with the actions of one file: the download
// link, the rename form and the delete form.
func (c *Controller) fileDrawer(ctx *fiber.Ctx) error {
	fileID := ctx.Params("file")
	if !c.validID(fileID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{"That file is not here any more."})
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

// fileRename changes the name of one file and refreshes the table it sits in.
func (c *Controller) fileRename(ctx *fiber.Ctx) error {
	fileID := ctx.Params("file")
	if !c.validID(fileID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{"That file is not here any more."})
	}

	var req request.FileRename

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"Enter a name for the file."})
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	file, err := c.files.Rename(ctx.UserContext(), userID, fileID, req.Name)
	if err != nil {
		return c.driveError(ctx, err, "fileRename - uc.files.Rename")
	}

	ctx.Set(_hxTriggerHeader, _entrySavedEvent)

	return c.renderEntries(ctx, userID, file.FolderID)
}

// fileDelete removes one file and the object behind it, then refreshes the
// table it sat in.
func (c *Controller) fileDelete(ctx *fiber.Ctx) error {
	fileID := ctx.Params("file")
	if !c.validID(fileID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{"That file is not here any more."})
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
		c.l.Error(fmt.Errorf("web - fileDelete - uc.files.Delete: %w", err))
	}

	ctx.Set(_hxTriggerHeader, _entrySavedEvent)

	return c.renderEntries(ctx, userID, file.FolderID)
}

// fileDownload sends the browser to a time-limited link to the object. The
// request is a plain navigation, so the redirect leaves the app entirely.
func (c *Controller) fileDownload(ctx *fiber.Ctx) error {
	fileID := ctx.Params("file")
	if !c.validID(fileID) {
		return middleware.Redirect(ctx, _homePath)
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	link, err := c.files.Download(ctx.UserContext(), userID, fileID)
	if err != nil {
		if !errors.Is(err, entity.ErrFileNotFound) {
			c.l.Error(fmt.Errorf("web - fileDownload - uc.files.Download: %w", err))
		}

		return middleware.Redirect(ctx, _homePath)
	}

	return ctx.Redirect(link, fiber.StatusFound)
}
