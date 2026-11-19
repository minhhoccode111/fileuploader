package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/request"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
	"github.com/minhhoccode111/fileuploader/web/templates"
)

// _hxTriggerHeader carries the event htmx fires on the client. The account list
// listens for it, so a role change reloads the rows it just altered.
const (
	_hxTriggerHeader   = "HX-Trigger"
	_usersUpdatedEvent = "usersUpdated"

	// _hxPushURLHeader tells htmx to push the named path into the browser
	// history, which is how the filters of the account list reach the
	// address bar, so a view stays bookmarkable.
	_hxPushURLHeader = "HX-Push-Url"

	// _hxCurrentURLHeader is the location the browser sat on when the
	// request left, which tells the handler whether a push would only add
	// a history entry of the path that is already on the screen.
	_hxCurrentURLHeader = "HX-Current-URL"
)

// adminUsersTable renders the account table htmx swaps into the list on the
// page. It is a request of its own, so the page itself never waits for the
// query, and the query string decides which accounts and which window of the
// list come back. Every success also pushes the page path of the filters
// into the address bar, so the view can be bookmarked.
func (c *Controller) adminUsersTable(ctx *fiber.Ctx) error {
	req, messages := parseUserListQuery(ctx, c.v)
	if messages != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, messages)
	}

	filter := req.Filter()

	list, err := c.u.ListUsers(ctx.UserContext(), &filter)
	if err != nil {
		c.l.Error(fmt.Errorf("web - adminUsersTable - uc.ListUsers: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	rows := make([]response.UserRow, 0, len(list.Items))
	for i := range list.Items {
		rows = append(rows, response.NewUserRow(&list.Items[i]))
	}

	c.pushUserListURL(ctx, &req)

	return c.renderPage(
		ctx,
		templates.AdminUsersFragment(rows, response.NewUserPager(list, &filter), userFilters(&req)),
	)
}

// parseUserListQuery decodes and validates the query string of the account
// table. A malformed filter is a caller error, not a server fault, so it
// comes back as the messages the error fragment prints.
func parseUserListQuery(
	ctx *fiber.Ctx,
	v *validator.Validate,
) (req request.UserListQuery, messages []string) {
	if err := ctx.QueryParser(&req); err != nil {
		return req, []string{"The filters of the list are malformed. Reset them and try again."}
	}

	req.Sanitize()

	if err := v.Struct(req); err != nil {
		return req, validatorx.ExtractErrors(err)
	}

	return req, nil
}

// decodeUserListQuery decodes and normalizes the query string of the account
// list without validating it. The page restores its filter inputs from it,
// so a bookmarked view comes back as it was left; the validation is the
// table request's job.
func decodeUserListQuery(ctx *fiber.Ctx) request.UserListQuery {
	var req request.UserListQuery

	if err := ctx.QueryParser(&req); err != nil {
		return req
	}

	req.Sanitize()

	return req
}

// userFilters maps the decoded query to the state the search bar and the
// pager render.
func userFilters(req *request.UserListQuery) response.UserFilters {
	return response.UserFilters{
		Query:  req.Query,
		Role:   req.Role,
		Date:   req.Date,
		Sort:   string(req.SortField()),
		Dir:    string(req.SortDirection()),
		Limit:  strconv.Itoa(req.PageSize()),
		Page:   req.Page,
		Active: req.HasFilters(),
	}
}

// pushUserListURL points the address bar at the page path that holds the
// current filters, so the visitor can bookmark the view and come back to it.
// A browser that already sits on that path — after a load, a reload or a
// push of its own — gets nothing, which keeps the history free of entries
// that would only repeat the present one.
func (c *Controller) pushUserListURL(ctx *fiber.Ctx, req *request.UserListQuery) {
	values := req.BookmarkQuery()

	path := _adminUsersPath
	if len(values) > 0 {
		path += "?" + values.Encode()
	}

	current, err := url.Parse(ctx.Get(_hxCurrentURLHeader))
	if err == nil && current.Path == _adminUsersPath && current.Query().Encode() == values.Encode() {
		return
	}

	ctx.Set(_hxPushURLHeader, path)
}

// adminUsersEdit loads the edit form of one account into the drawer the row
// click opened. The account travels by its identifier in the query string.
func (c *Controller) adminUsersEdit(ctx *fiber.Ctx) error {
	var req request.UserEditQuery

	if err := ctx.QueryParser(&req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"That account is unknown."})
	}

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	user, err := c.u.GetUser(ctx.UserContext(), req.ID)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return c.renderErrors(ctx, http.StatusNotFound, []string{"That account is not registered."})
		}

		c.l.Error(fmt.Errorf("web - adminUsersEdit - uc.GetUser: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	return c.renderPage(ctx, templates.AdminUsersEditContent(response.NewUserRowFromUser(&user)))
}

// adminUsersEditSubmit saves the account the drawer edits. The form carries
// every editable field; an empty password keeps the one the account holds.
// A role or a password change ends the live sessions of the account, so the
// account lists itself under new values in the same response.
func (c *Controller) adminUsersEditSubmit(ctx *fiber.Ctx) error {
	var req request.UserUpdate

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"Enter the name, the email and the role of the account."},
		)
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	storageQuota, uploadRate, downloadRate, err := req.Limits()
	if err != nil {
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"A size or rate must use an optional K, M or G suffix."},
		)
	}

	if storageQuota < 0 || uploadRate < 0 || downloadRate < 0 {
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"A size or rate must not be negative."},
		)
	}

	user, sessionsEnded, err := c.u.UpdateUser(
		ctx.UserContext(),
		req.ID,
		req.Username,
		req.Email,
		entity.UserRole(req.Role),
		req.Password,
	)
	if err != nil {
		return c.userUpdateError(ctx, err)
	}

	updatedUser, err := c.applyUserLimits(ctx, &user, &req)
	if err != nil {
		return err
	}

	user = updatedUser

	ctx.Set(_hxTriggerHeader, _usersUpdatedEvent)

	return c.renderPage(
		ctx,
		templates.AdminUsersEditDone(response.NewUserRowFromUser(&user), sessionsEnded),
	)
}

// applyUserLimits persists the account's storage and transfer limits when they
// changed, and reports any failure as a rendered error.
func (c *Controller) applyUserLimits(
	ctx *fiber.Ctx,
	user *entity.User,
	req *request.UserUpdate,
) (entity.User, error) {
	storageQuota, uploadRate, downloadRate, err := req.Limits()
	if err != nil {
		return *user, c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"A size or rate must use an optional K, M or G suffix."},
		)
	}

	if storageQuota == user.StorageQuotaBytes &&
		uploadRate == user.UploadRateBytesPerSec &&
		downloadRate == user.DownloadRateBytesPerSec {
		return *user, nil
	}

	updated, err := c.u.SetUserLimits(
		ctx.UserContext(),
		req.ID,
		storageQuota,
		uploadRate,
		downloadRate,
	)
	if err != nil {
		return *user, c.userUpdateError(ctx, err)
	}

	return updated, nil
}

// userUpdateError maps a failed account update to a status and a message the
// admin may see. Only the entity sentinels are translated; everything else is
// a server fault and is logged instead of leaked.
func (c *Controller) userUpdateError(ctx *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, entity.ErrUserNotFound):
		return c.renderErrors(ctx, http.StatusNotFound, []string{"That account is not registered."})

	case errors.Is(err, entity.ErrEmailAlreadyExists):
		return c.renderErrors(
			ctx,
			http.StatusConflict,
			[]string{"That email is already registered."},
		)

	case errors.Is(err, entity.ErrUsernameAlreadyExists):
		return c.renderErrors(ctx, http.StatusConflict, []string{"That username is already taken."})

	case errors.Is(err, entity.ErrUserAlreadyExists):
		return c.renderErrors(
			ctx,
			http.StatusConflict,
			[]string{"That email or username is already taken."},
		)

	case errors.Is(err, entity.ErrInvalidRole):
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"That role is not known."})

	case errors.Is(err, entity.ErrPasswordTooLong):
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"Password is too long."})

	case errors.Is(err, entity.ErrInvalidLimits):
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"Limits must not be negative."})

	default:
		c.l.Error(fmt.Errorf("web - userUpdateError - uc.UpdateUser: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}
}

// roleSubmit changes the role of an account. The role travels with the
// session, so the account is signed out everywhere: it picks the new role up
// on its next sign-in, and nobody keeps a role that was taken away.
func (c *Controller) roleSubmit(ctx *fiber.Ctx) error {
	var req request.Role

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"Enter the email of the account and the role it gets."},
		)
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	err := c.u.SetRole(ctx.UserContext(), req.Email, entity.UserRole(req.Role))
	if err != nil {
		return c.roleError(ctx, err)
	}

	ctx.Set(_hxTriggerHeader, _usersUpdatedEvent)

	return c.renderPage(ctx, templates.RoleUpdated(req.Email, req.Role))
}

// roleError maps a failed role change to a status and a message the caller may
// see. Only the entity sentinels are translated; everything else is a server
// fault and is logged instead of leaked.
func (c *Controller) roleError(ctx *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, entity.ErrUserNotFound):
		return c.renderErrors(ctx, http.StatusNotFound, []string{"That email is not registered."})

	case errors.Is(err, entity.ErrInvalidRole):
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"That role is not known."},
		)

	default:
		c.l.Error(fmt.Errorf("web - roleSubmit - uc.SetRole: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}
}
