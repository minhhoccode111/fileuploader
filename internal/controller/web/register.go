package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/evrone/go-clean-template/internal/controller/web/request"
	"github.com/evrone/go-clean-template/internal/entity"
	"github.com/evrone/go-clean-template/pkg/validatorx"
	"github.com/gofiber/fiber/v2"
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

	token, err := c.u.Login(ctx.UserContext(), req.Email, req.Password)
	if err != nil {
		c.l.Error(fmt.Errorf("web - registerSubmit - uc.Login: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	return c.renderAuthenticated(ctx, token)
}

// registerError maps a registration failure to a status and a message the caller
// may see. Only the entity sentinels are translated; everything else is a server
// fault and is logged instead of leaked.
func (c *Controller) registerError(ctx *fiber.Ctx, err error) error {
	switch {
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

	case errors.Is(err, entity.ErrPasswordTooLong):
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"Password is too long."},
		)

	default:
		c.l.Error(fmt.Errorf("web - registerSubmit - uc.Register: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}
}
