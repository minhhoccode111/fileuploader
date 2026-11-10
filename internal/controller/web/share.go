package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/middleware"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/request"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
	"github.com/minhhoccode111/fileuploader/web/templates"
)

// shareIndex lists the active shares of the signed-in account.
func (c *Controller) shareIndex(ctx *fiber.Ctx) error {
	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	shares, err := c.shares.List(ctx.UserContext(), userID)
	if err != nil {
		c.l.Error(fmt.Errorf("web - shareIndex - uc.shares.List: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	rows := make([]response.ShareRow, 0, len(shares))
	for i := range shares {
		name := shares[i].FolderID

		folder, err := c.folders.Get(ctx.UserContext(), userID, shares[i].FolderID)
		if err == nil {
			name = folder.Name
		} else if !errors.Is(err, entity.ErrFolderNotFound) {
			c.l.Error(fmt.Errorf("web - shareIndex - uc.folders.Get: %w", err))
		}

		rows = append(rows, response.NewShareRow(&shares[i], name))
	}

	return c.render(ctx, templates.ShareIndex(c.view(ctx), rows), templates.ShareIndexContent(rows), templates.ShareTitle)
}

// shareForm fills the drawer with the form that creates a share for a folder.
func (c *Controller) shareForm(ctx *fiber.Ctx) error {
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
		return c.driveError(ctx, err, "shareForm - uc.folders.Get")
	}

	return c.renderPage(ctx, templates.ShareCreateContent(folder.ID, folder.Name))
}

// shareCreate makes a share for the folder the drawer names and sends the
// visitor to the share index, where the new link shows up.
func (c *Controller) shareCreate(ctx *fiber.Ctx) error {
	var req request.ShareCreate

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"Choose how long the link stays valid."})
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	expiresAt, err := parseShareExpiry(req.ExpiresAt, req.Unlimited)
	if err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"Enter a valid date and time."})
	}

	if _, err := c.shares.Create(ctx.UserContext(), userID, req.FolderID, expiresAt); err != nil {
		return c.driveError(ctx, err, "shareCreate - uc.shares.Create")
	}

	return middleware.Redirect(ctx, "/share")
}

// shareUpdateTime changes the expiry of a share owned by the account.
func (c *Controller) shareUpdateTime(ctx *fiber.Ctx) error {
	shareID := ctx.Params("share")
	if !c.validID(shareID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{"That share is no longer here."})
	}

	var req request.ShareUpdateExpiry

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"Choose how long the link stays valid."})
	}

	req.Sanitize()

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	expiresAt, err := parseShareExpiry(req.ExpiresAt, req.Unlimited)
	if err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"Enter a valid date and time."})
	}

	if _, err := c.shares.UpdateExpiry(ctx.UserContext(), userID, shareID, expiresAt); err != nil {
		if errors.Is(err, entity.ErrShareNotFound) {
			return c.renderErrors(ctx, http.StatusNotFound, []string{"That share is no longer here."})
		}

		c.l.Error(fmt.Errorf("web - shareUpdateTime - uc.shares.UpdateExpiry: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	return middleware.Redirect(ctx, "/share")
}

// shareDelete removes a share owned by the account.
func (c *Controller) shareDelete(ctx *fiber.Ctx) error {
	shareID := ctx.Params("share")
	if !c.validID(shareID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{"That share is no longer here."})
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	if err := c.shares.Delete(ctx.UserContext(), userID, shareID); err != nil {
		if errors.Is(err, entity.ErrShareNotFound) {
			return c.renderErrors(ctx, http.StatusNotFound, []string{"That share is no longer here."})
		}

		c.l.Error(fmt.Errorf("web - shareDelete - uc.shares.Delete: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	return middleware.Redirect(ctx, "/share")
}

// sharedRoot opens the root of a shared folder. An owner who opens her own
// link is sent to the same folder in her drive instead.
func (c *Controller) sharedRoot(ctx *fiber.Ctx) error {
	return c.sharedFolder(ctx, ctx.Params("share"), "")
}

// sharedFolderRoute is the fiber handler for a nested shared folder.
func (c *Controller) sharedFolderRoute(ctx *fiber.Ctx) error {
	return c.sharedFolder(ctx, ctx.Params("share"), ctx.Params("folder"))
}

// sharedFolder opens one folder inside a shared tree.
func (c *Controller) sharedFolder(ctx *fiber.Ctx, shareID, folderID string) error {
	if !c.validID(shareID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{"This shared folder is no longer available."})
	}

	if folderID != "" && !c.validID(folderID) {
		return middleware.Redirect(ctx, "/share/"+shareID)
	}

	// An owner who reaches her own link through the public route lands in her
	// drive instead, so she never browses her own folder as a stranger.
	if redirected, err := c.redirectOwnerToDrive(ctx, shareID); redirected {
		return err
	}

	share, folder, err := c.resolveSharedFolder(ctx, shareID, folderID)
	if err != nil {
		return c.sharedError(ctx, shareID, err)
	}

	view, err := c.folders.List(ctx.UserContext(), share.UserID, folder.ID)
	if err != nil {
		c.l.Error(fmt.Errorf("web - sharedFolder - uc.folders.List: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	page := response.NewSharedDrive(share.ID, &view)

	return c.render(ctx, templates.SharedDrive(c.view(ctx), page), templates.SharedDriveContent(page), templates.SharedTitle)
}

// sharedFileDownload sends a visitor of a shared folder to a presigned link
// for one file inside that folder's tree.
func (c *Controller) sharedFileDownload(ctx *fiber.Ctx) error {
	shareID := ctx.Params("share")
	fileID := ctx.Params("file")

	if !c.validID(shareID) || !c.validID(fileID) {
		return c.renderErrors(ctx, http.StatusNotFound, []string{"This shared folder is no longer available."})
	}

	share, file, err := c.shares.ResolveFile(ctx.UserContext(), shareID, fileID)
	if err != nil {
		return c.sharedError(ctx, shareID, err)
	}

	reader, opened, err := c.files.OpenDownload(ctx.UserContext(), share.UserID, file.ID)
	if err != nil {
		c.l.Error(fmt.Errorf("web - sharedFileDownload - uc.files.OpenDownload: %w", err))

		return c.renderErrors(ctx, http.StatusNotFound, []string{"That file is not here any more."})
	}

	rateLimit := int64(0)
	if user, userErr := c.u.GetUser(ctx.UserContext(), share.UserID); userErr == nil {
		rateLimit = user.EffectiveDownloadRate(0)
	}

	if rateLimit > 0 {
		reader = newThrottledReader(ctx.UserContext(), reader, rateLimit)
	}

	ctx.Set(fiber.HeaderContentType, opened.ContentType)
	ctx.Set(fiber.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%q", opened.Name))

	if opened.Size > 0 {
		ctx.Set(fiber.HeaderContentLength, fmt.Sprintf("%d", opened.Size))
	}

	defer func() {
		closeErr := reader.Close()
		if closeErr != nil {
			c.l.Error(fmt.Errorf("web - sharedFileDownload - reader.Close: %w", closeErr))
		}
	}()

	return ctx.SendStream(reader)
}

// resolveSharedFolder resolves a share and a folder inside its tree, mapping
// the sentinel errors to HTTP responses.
func (c *Controller) resolveSharedFolder(
	ctx *fiber.Ctx,
	shareID, folderID string,
) (entity.Share, entity.Folder, error) {
	if folderID == "" {
		share, err := c.shares.Get(ctx.UserContext(), shareID)
		if err != nil {
			return entity.Share{}, entity.Folder{}, err
		}

		folderID = share.FolderID
	}

	share, folder, err := c.shares.ResolveFolder(ctx.UserContext(), shareID, folderID)
	if err != nil {
		return entity.Share{}, entity.Folder{}, err
	}

	return share, folder, nil
}

// sharedError maps a failed shared-folder action to a status the visitor may
// see.
func (c *Controller) sharedError(ctx *fiber.Ctx, shareID string, err error) error {
	switch {
	case errors.Is(err, entity.ErrShareNotFound), errors.Is(err, entity.ErrShareExpired):
		return c.renderErrors(ctx, http.StatusNotFound, []string{"This shared folder is no longer available."})

	case errors.Is(err, entity.ErrFolderNotFound), errors.Is(err, entity.ErrFileNotFound):
		return middleware.Redirect(ctx, "/share/"+shareID)

	default:
		c.l.Error(fmt.Errorf("web - shared: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}
}

// redirectOwnerToDrive sends an owner who opens her own share link to the same
// folder in her drive. It reports whether it handled the request.
func (c *Controller) redirectOwnerToDrive(ctx *fiber.Ctx, shareID string) (bool, error) {
	session, ok := c.optionalSession(ctx)
	if !ok {
		return false, nil
	}

	share, err := c.shares.Get(ctx.UserContext(), shareID)
	if err != nil || share.UserID != session.UserID {
		return false, nil
	}

	folder, err := c.folders.Get(ctx.UserContext(), share.UserID, share.FolderID)

	path := _homePath
	if err == nil && !folder.IsRoot() {
		path = _foldersPath + folder.ID
	}

	return true, middleware.Redirect(ctx, path)
}

// optionalSession resolves the session cookie without requiring it.
func (c *Controller) optionalSession(ctx *fiber.Ctx) (entity.Session, bool) {
	sessionID := ctx.Cookies(_authCookieName)
	if sessionID == "" {
		return entity.Session{}, false
	}

	session, err := c.u.Authenticate(ctx.UserContext(), sessionID)
	if err != nil {
		return entity.Session{}, false
	}

	return session, true
}

func parseShareExpiry(raw, unlimited string) (*time.Time, error) {
	if unlimited == "true" || strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	// datetime-local omits a zone; treat it as the server's local time, then
	// store it as UTC.
	parsed, err := time.ParseInLocation("2006-01-02T15:04", raw, time.Local)
	if err != nil {
		parsed, err = time.ParseInLocation("2006-01-02T15:04:05", raw, time.Local)
		if err != nil {
			return nil, err
		}
	}

	utc := parsed.UTC()

	return &utc, nil
}
