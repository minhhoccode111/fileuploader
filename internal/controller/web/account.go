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

// accountPage renders the account page of the signed-in visitor. The guard has
// already resolved the session, so the handler only loads the account behind
// it and hands the page the values it prints.
func (c *Controller) accountPage(ctx *fiber.Ctx) error {
	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	user, err := c.u.GetUser(ctx.UserContext(), userID)
	if err != nil {
		if errors.Is(err, entity.ErrUserNotFound) {
			return middleware.Redirect(ctx, _loginPath)
		}

		c.requestLogger(ctx).Error(fmt.Errorf("web - accountPage - uc.GetUser: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	account := response.NewUserRowFromUser(&user)

	return c.render(
		ctx,
		templates.Account(c.view(ctx), account),
		templates.AccountContent(account),
		templates.AccountTitle,
	)
}

// accountProfileSubmit saves the name and the email of the signed-in visitor.
// A validation failure comes back as the messages of the error fragment, so
// the form keeps what the visitor typed.
func (c *Controller) accountProfileSubmit(ctx *fiber.Ctx) error {
	var req request.ProfileUpdate

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"Enter the username and the email of your account."},
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

	user, err := c.u.UpdateProfile(ctx.UserContext(), userID, req.Username, req.Email)
	if err != nil {
		return c.accountError(ctx, err)
	}

	return c.renderPage(
		ctx,
		templates.AccountProfilePanel(response.NewUserRowFromUser(&user), "Your profile was saved."),
	)
}

// accountPasswordSubmit replaces the password of the signed-in visitor after
// checking the current one. The form must name the new password twice, and the
// session that asked stays signed in while every other session ends.
func (c *Controller) accountPasswordSubmit(ctx *fiber.Ctx) error {
	var req request.PasswordUpdate

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"Enter your current password and the new one twice."},
		)
	}

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	if !req.Matches() {
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"The new passwords do not match."},
		)
	}

	userID, ok := c.userID(ctx)
	if !ok {
		return middleware.Redirect(ctx, _loginPath)
	}

	err := c.u.ChangePassword(
		ctx.UserContext(),
		userID,
		req.CurrentPassword,
		req.NewPassword,
		ctx.Cookies(_authCookieName),
	)
	if err != nil {
		return c.accountError(ctx, err)
	}

	return c.renderPage(
		ctx,
		templates.AccountPasswordPanel(
			"Your password was changed. Every other session of the account was signed out.",
		),
	)
}

// accountError maps a failed account change to a status and a message the
// visitor may see. Only the entity sentinels are translated; everything else
// is a server fault and is logged instead of leaked.
func (c *Controller) accountError(ctx *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, entity.ErrUserNotFound):
		return middleware.Redirect(ctx, _loginPath)

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

	case errors.Is(err, entity.ErrInvalidCredentials):
		return c.renderErrors(
			ctx,
			http.StatusUnprocessableEntity,
			[]string{"Your current password is incorrect."},
		)

	case errors.Is(err, entity.ErrPasswordTooLong):
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"Password is too long."})

	default:
		c.requestLogger(ctx).Error(fmt.Errorf("web - accountError - uc.UpdateProfile: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}
}
