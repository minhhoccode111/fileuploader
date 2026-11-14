package web

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/middleware"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/request"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
	"github.com/minhhoccode111/fileuploader/internal/entity"
)

const (
	// _uploadFolderHeader carries the folder a completed upload landed in, so
	// the page that started the upload can reload the right table.
	_uploadFolderHeader = "X-Upload-Folder"

	// _uploadInvalidMessage is shown when the start body or a path parameter
	// cannot name an upload, so the browser can correct the request.
	_uploadInvalidMessage = "That upload request is not valid."

	// _uploadNameMessage is shown when the name is empty or the drive cannot
	// store it.
	_uploadNameMessage = "Enter a file name without slashes, up to 255 characters."

	// _uploadTooLargeMessage is shown when the file or the request body is
	// larger than the server accepts.
	_uploadTooLargeMessage = "That file is larger than this server accepts."

	// _uploadQuotaMessage is shown when the file would push the account past
	// its storage quota.
	_uploadQuotaMessage = "That would exceed the storage quota of this account."

	// _uploadMissingMessage is shown when the upload session is gone, has
	// expired, or belongs to another account.
	_uploadMissingMessage = "That upload is not here any more."

	// _uploadConflictMessage is shown when the folder already holds a file of
	// that name.
	_uploadConflictMessage = "A file with that name is already here."

	// _uploadIncompleteMessage is shown when a part is still missing.
	_uploadIncompleteMessage = "That upload is missing one or more parts. Send them again."

	// _uploadBusyMessage is shown when another start holds the quota lock of
	// the account, so the caller may retry.
	_uploadBusyMessage = "Another upload is starting right now. Please try again."

	// _generalBodyLimitBytes bounds every request that is not a part of a
	// chunked upload. Forms and JSON bodies are small; a part is the only body
	// that is meant to be large.
	_generalBodyLimitBytes = int64(1 << 20)
)

// uploadStart opens one chunked upload and answers with the part plan the
// browser follows to send it. The declared size is reserved against the quota
// before any byte arrives, so two uploads cannot both claim the same headroom.
func (c *Controller) uploadStart(ctx *fiber.Ctx) error {
	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	var req request.UploadStart

	if err := ctx.BodyParser(&req); err != nil {
		return ctx.Status(http.StatusUnprocessableEntity).SendString(_uploadInvalidMessage)
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return ctx.Status(http.StatusUnprocessableEntity).SendString(_uploadInvalidMessage)
	}

	session, err := c.files.StartUpload(
		ctx.UserContext(),
		userID,
		req.FolderID,
		req.Name,
		req.ContentType,
		req.Size,
	)
	if err != nil {
		return c.uploadFailure(ctx, err, "uploadStart - uc.files.StartUpload")
	}

	return ctx.JSON(response.UploadStart{
		UploadID:  session.ID,
		PartSize:  session.PartSize,
		PartCount: session.PartCount,
	})
}

// uploadPart stores one numbered part of an open upload. The body must carry
// the size the part has, so the handler refuses a request without a length and
// one larger than a part before the use case reads a byte.
func (c *Controller) uploadPart(ctx *fiber.Ctx) error {
	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	uploadID := ctx.Params("upload")
	if !c.validID(uploadID) {
		return c.uploadFailure(ctx, entity.ErrInvalidUpload, "uploadPart - c.validID")
	}

	partNumber, err := strconv.Atoi(ctx.Params("part"))
	if err != nil || partNumber < 1 {
		return c.uploadFailure(ctx, entity.ErrInvalidUpload, "uploadPart - strconv.Atoi")
	}

	length := int64(ctx.Request().Header.ContentLength())

	switch {
	case length < 0:
		return ctx.Status(http.StatusLengthRequired).SendString(http.StatusText(http.StatusLengthRequired))

	case length > c.cfg.Upload.PartBytes:
		return ctx.Status(http.StatusRequestEntityTooLarge).
			SendString(http.StatusText(http.StatusRequestEntityTooLarge))
	}

	body := ctx.Request().BodyStream()
	if body == nil {
		body = bytes.NewReader(ctx.Request().Body())
	}

	err = c.files.UploadPart(
		ctx.UserContext(),
		userID,
		uploadID,
		partNumber,
		body,
		length,
	)
	if err != nil {
		return c.uploadFailure(ctx, err, "uploadPart - uc.files.UploadPart")
	}

	return ctx.SendStatus(http.StatusNoContent)
}

// uploadComplete joins the parts of an open upload into the file it names. The
// table of the folder it landed in is the response, so htmx swaps the new row
// in place.
func (c *Controller) uploadComplete(ctx *fiber.Ctx) error {
	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	uploadID := ctx.Params("upload")
	if !c.validID(uploadID) {
		return c.uploadFailure(ctx, entity.ErrInvalidUpload, "uploadComplete - c.validID")
	}

	file, err := c.files.CompleteUpload(ctx.UserContext(), userID, uploadID)
	if err != nil {
		return c.uploadFailure(ctx, err, "uploadComplete - uc.files.CompleteUpload")
	}

	ctx.Set(_uploadFolderHeader, file.FolderID)
	ctx.Set(_hxTriggerHeader, _entrySavedEvent)

	return c.renderEntries(ctx, userID, file.FolderID)
}

// uploadAbort ends one open upload without storing the file. A session that is
// already gone is not an error, so a retried abort still answers 204.
func (c *Controller) uploadAbort(ctx *fiber.Ctx) error {
	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	uploadID := ctx.Params("upload")
	if !c.validID(uploadID) {
		return c.uploadFailure(ctx, entity.ErrInvalidUpload, "uploadAbort - c.validID")
	}

	if err := c.files.AbortUpload(ctx.UserContext(), userID, uploadID); err != nil {
		return c.uploadFailure(ctx, err, "uploadAbort - uc.files.AbortUpload")
	}

	return ctx.SendStatus(http.StatusNoContent)
}

// uploadFailure maps a failed upload call to a plain-text response. The browser
// shows the text in its upload error box, so the body is a sentence rather than
// an HTML fragment. Only the entity sentinels are translated; everything else is
// a server fault and is logged instead of leaked.
//
//nolint:cyclop,gocyclo // one branch per sentinel the upload can return
func (c *Controller) uploadFailure(ctx *fiber.Ctx, err error, action string) error {
	switch {
	case errors.Is(err, entity.ErrInvalidName):
		return ctx.Status(http.StatusUnprocessableEntity).SendString(_uploadNameMessage)

	case errors.Is(err, entity.ErrInvalidUpload):
		return ctx.Status(http.StatusUnprocessableEntity).SendString(_uploadInvalidMessage)

	case errors.Is(err, entity.ErrUploadPartMismatch):
		return ctx.Status(http.StatusUnprocessableEntity).SendString(_uploadInvalidMessage)

	case errors.Is(err, entity.ErrFileTooLarge):
		return ctx.Status(http.StatusRequestEntityTooLarge).SendString(_uploadTooLargeMessage)

	case errors.Is(err, entity.ErrStorageQuotaExceeded):
		return ctx.Status(http.StatusRequestEntityTooLarge).SendString(_uploadQuotaMessage)

	case errors.Is(err, entity.ErrFolderNotFound):
		return ctx.Status(http.StatusNotFound).SendString(_folderNotFoundMessage)

	case errors.Is(err, entity.ErrUploadNotFound):
		return ctx.Status(http.StatusNotFound).SendString(_uploadMissingMessage)

	case errors.Is(err, entity.ErrEntryAlreadyExists):
		return ctx.Status(http.StatusConflict).SendString(_uploadConflictMessage)

	case errors.Is(err, entity.ErrUploadIncomplete):
		return ctx.Status(http.StatusConflict).SendString(_uploadIncompleteMessage)

	case errors.Is(err, entity.ErrUploadQuotaBusy):
		return ctx.Status(http.StatusServiceUnavailable).SendString(_uploadBusyMessage)

	default:
		c.l.Error(fmt.Errorf("web - %s: %w", action, err))

		return ctx.Status(http.StatusInternalServerError).SendString(_genericErrorMessage)
	}
}
