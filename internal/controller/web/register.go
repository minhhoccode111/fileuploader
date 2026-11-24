package web

import (
	"fmt"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/request"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
)

// registerSubmit creates the account and then signs the new user in. On success
// it swaps the home fragment into <main>, pushes "/", and stores the session ID
// in an HTTP-only cookie. On failure it returns an error fragment for
// #register-error.
func (c *Controller) registerSubmit(ctx *fiber.Ctx) error {
	var req request.Register

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"Enter a username, email and password."},
		)
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	if _, err := c.u.Register(
		ctx.UserContext(),
		req.Username,
		req.Email,
		req.Password,
	); err != nil {
		return c.registerError(ctx, err)
	}

	// A registration is a request for access: the account stays pending until
	// an administrator approves it, so signing the visitor in here would skip
	// that gate.
	return c.renderErrors(
		ctx,
		http.StatusAccepted,
		[]string{"Your account request has been submitted and is awaiting admin approval."},
	)
}

// registerError maps a registration failure to a status and a message the caller
// may see. Only the entity sentinels are translated; everything else is a server
// fault and is logged instead of leaked.
func (c *Controller) registerError(ctx *fiber.Ctx, err error) error {
	handled, renderErr := c.renderMappedError(ctx, err, []errorCase{
		{entity.ErrEmailAlreadyExists, http.StatusConflict, "That email is already registered."},
		{entity.ErrUsernameAlreadyExists, http.StatusConflict, "That username is already taken."},
		{entity.ErrUserAlreadyExists, http.StatusConflict, "That email or username is already taken."},
		{entity.ErrPasswordTooLong, http.StatusUnprocessableEntity, "Password is too long."},
	})
	if handled {
		return renderErr
	}

	c.l.Error(fmt.Errorf("web - registerSubmit - uc.Register: %w", err))

	return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
}
