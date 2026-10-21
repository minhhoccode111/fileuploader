package web

import (
	"errors"
	"fmt"
	"net/http"

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
)

// adminUsersTable renders the account table htmx swaps into the list on the
// page. It is a request of its own, so the page itself never waits for the
// query.
func (c *Controller) adminUsersTable(ctx *fiber.Ctx) error {
	list, err := c.u.ListUsers(ctx.UserContext())
	if err != nil {
		c.l.Error(fmt.Errorf("web - adminUsersTable - uc.ListUsers: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	rows := make([]response.UserRow, 0, len(list))
	for i := range list {
		rows = append(rows, response.NewUserRow(&list[i]))
	}

	return c.renderPage(ctx, templates.AdminUsersTable(rows))
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
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"That role is not known."})

	default:
		c.l.Error(fmt.Errorf("web - roleSubmit - uc.SetRole: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}
}
