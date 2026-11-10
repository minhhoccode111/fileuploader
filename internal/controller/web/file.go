package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/middleware"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/request"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
	"github.com/minhhoccode111/fileuploader/web/templates"
	"golang.org/x/time/rate"
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
//
//nolint:funlen // upload flow combines parsing, throttling and rendering
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

	rateLimit, err := c.uploadRate(ctx, userID)
	if err != nil {
		return c.driveError(ctx, err, "fileUploadSubmit - c.u.GetUser")
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

	body := newThrottledReader(ctx.UserContext(), upload, rateLimit)

	_, err = c.files.Upload(
		ctx.UserContext(),
		userID,
		req.FolderID,
		req.Name,
		header.Header.Get(fiber.HeaderContentType),
		header.Size,
		body,
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
		c.l.Error(fmt.Errorf("web - fileDelete - uc.files.Delete: %w", err))
	}

	ctx.Set(_hxTriggerHeader, _entrySavedEvent)

	return c.renderEntries(ctx, userID, file.FolderID)
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

	rateLimit, err := c.downloadRate(ctx, userID)
	if err != nil {
		return c.driveError(ctx, err, "fileDownload - c.u.GetUser")
	}

	reader, file, err := c.files.OpenDownload(ctx.UserContext(), userID, fileID)
	if err != nil {
		if !errors.Is(err, entity.ErrFileNotFound) {
			c.l.Error(fmt.Errorf("web - fileDownload - uc.files.OpenDownload: %w", err))
		}

		return middleware.Redirect(ctx, _homePath)
	}

	if rateLimit > 0 {
		reader = newThrottledReader(ctx.UserContext(), reader, rateLimit)
	}

	ctx.Set(fiber.HeaderContentType, file.ContentType)
	ctx.Set(fiber.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%q", file.Name))

	if file.Size > 0 {
		ctx.Set(fiber.HeaderContentLength, fmt.Sprintf("%d", file.Size))
	}

	defer func() {
		closeErr := reader.Close()
		if closeErr != nil {
			c.l.Error(fmt.Errorf("web - fileDownload - reader.Close: %w", closeErr))
		}
	}()

	return ctx.SendStream(reader)
}

// downloadRate resolves the effective download rate for the signed-in account.
func (c *Controller) downloadRate(ctx *fiber.Ctx, userID string) (int64, error) {
	user, err := c.u.GetUser(ctx.UserContext(), userID)
	if err != nil {
		return 0, err
	}

	return user.EffectiveDownloadRate(0), nil
}

// uploadRate resolves the effective upload rate for the signed-in account.
func (c *Controller) uploadRate(ctx *fiber.Ctx, userID string) (int64, error) {
	user, err := c.u.GetUser(ctx.UserContext(), userID)
	if err != nil {
		return 0, err
	}

	return user.EffectiveUploadRate(0), nil
}

// newThrottledReader wraps upload with a rate limiter when rateLimit is
// positive. A non-positive limit leaves the stream untouched.
func newThrottledReader(ctx context.Context, upload io.Reader, rateLimit int64) io.ReadCloser {
	if rateLimit <= 0 {
		if closer, ok := upload.(io.ReadCloser); ok {
			return closer
		}

		return io.NopCloser(upload)
	}

	reader := &throttledReader{
		r:       upload,
		limiter: rate.NewLimiter(rate.Limit(rateLimit), int(rateLimit)),
		ctx:     ctx,
	}

	if closer, ok := upload.(io.Closer); ok {
		reader.closer = closer
	}

	return reader
}

// throttledReader wraps an upload stream with a per-request rate limiter. Each
// completed read is accounted for before it is returned, so the consumer only
// sees the bytes once the limiter has admitted them.
type throttledReader struct {
	r       io.Reader
	limiter *rate.Limiter
	ctx     context.Context
	closer  io.Closer
}

// Close closes the wrapped reader when it can be closed.
func (t *throttledReader) Close() error {
	if t.closer != nil {
		return t.closer.Close()
	}

	return nil
}

// Read pulls bytes from the wrapped reader and waits for the limiter to admit
// them before returning.
func (t *throttledReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 && t.limiter != nil {
		waitCtx := t.ctx
		if waitCtx == nil {
			waitCtx = context.Background()
		}

		if waitErr := t.limiter.WaitN(waitCtx, n); waitErr != nil {
			return n, waitErr
		}
	}

	return n, err
}
