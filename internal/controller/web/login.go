package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/request"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/pkg/validatorx"
)

const (
	_authCookieName = "access_token"
	_homePath       = "/"

	// _hxRequestHeader is set by htmx on every request it makes, and
	// _hxRequestTrue is the value the header carries.
	_hxRequestHeader = "HX-Request"
	_hxRequestTrue   = "true"

	// _hxRedirectHeader tells htmx to navigate the whole browser to a path,
	// unlike HX-Location, which swaps a fragment into the current page.
	_hxRedirectHeader = "HX-Redirect"
)

// loginSubmit authenticates the user. On failure it returns an error fragment
// that htmx swaps into #login-error; on success it signs the user in.
func (c *Controller) loginSubmit(ctx *fiber.Ctx) error {
	var req request.Login

	if err := ctx.BodyParser(&req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, []string{"Enter a valid email and password."})
	}

	req.Sanitize()

	if err := c.v.Struct(req); err != nil {
		return c.renderErrors(ctx, http.StatusUnprocessableEntity, validatorx.ExtractErrors(err))
	}

	token, _, err := c.u.Login(ctx.UserContext(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, entity.ErrInvalidCredentials) {
			return c.renderErrors(ctx, http.StatusUnauthorized, []string{"Invalid email or password."})
		}

		c.l.Error(fmt.Errorf("web - loginSubmit - uc.Login: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	return c.renderAuthenticated(ctx, token)
}

// renderAuthenticated stores the token in an HTTP-only cookie and sends the
// browser to the home page with a full navigation. The page is built with the
// fresh session, so the sidebar shows the links of a signed-in account; htmx
// receives HX-Redirect, a plain form post a 302.
func (c *Controller) renderAuthenticated(ctx *fiber.Ctx, token string) error {
	c.setAuthCookie(ctx, token)

	if ctx.Get(_hxRequestHeader) == _hxRequestTrue {
		ctx.Set(_hxRedirectHeader, _homePath)

		return nil
	}

	return ctx.Redirect(_homePath, fiber.StatusFound)
}

func (c *Controller) setAuthCookie(ctx *fiber.Ctx, token string) {
	ctx.Cookie(&fiber.Cookie{
		Name:     _authCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(c.cfg.Session.TTL.Seconds()),
		HTTPOnly: true,
		Secure:   ctx.Protocol() == "https",
		SameSite: fiber.CookieSameSiteLaxMode,
	})
}
