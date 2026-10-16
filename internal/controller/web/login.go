package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/a-h/templ"
	"github.com/evrone/go-clean-template/internal/controller/web/request"
	"github.com/evrone/go-clean-template/internal/entity"
	"github.com/evrone/go-clean-template/pkg/validatorx"
	"github.com/evrone/go-clean-template/web/templates"
	"github.com/gofiber/fiber/v2"
)

const (
	_authCookieName = "access_token"
	_hxRetarget     = "HX-Retarget"
	_hxReswap       = "HX-Reswap"
	_hxPushURL      = "HX-Push-Url"
	_pageContent    = "#page-content"
	_swapInnerHTML  = "innerHTML"
	_homePath       = "/"
	_loginPath      = "/login"
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

	token, err := c.u.Login(ctx.UserContext(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, entity.ErrInvalidCredentials) {
			return c.renderErrors(ctx, http.StatusUnauthorized, []string{"Invalid email or password."})
		}

		c.l.Error(fmt.Errorf("web - loginSubmit - uc.Login: %w", err))

		return c.renderErrors(ctx, http.StatusInternalServerError, []string{_genericErrorMessage})
	}

	return c.renderAuthenticated(ctx, token)
}

// renderAuthenticated stores the token in an HTTP-only cookie and swaps the home
// fragment into <main>, pushing "/" so the address bar follows the new page. It
// also updates the sidebar out of band, since the nav changed with the auth state.
func (c *Controller) renderAuthenticated(ctx *fiber.Ctx, token string) error {
	c.setAuthCookie(ctx, token)

	ctx.Set(_hxRetarget, _pageContent)
	ctx.Set(_hxReswap, _swapInnerHTML)
	ctx.Set(_hxPushURL, _homePath)

	return c.renderPage(ctx, templ.Join(
		templates.HomeContent(),
		templates.SidebarNavUpdate(true),
	))
}

func (c *Controller) setAuthCookie(ctx *fiber.Ctx, token string) {
	ctx.Cookie(&fiber.Cookie{
		Name:     _authCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(c.cfg.JWT.TokenExpiry.Seconds()),
		HTTPOnly: true,
		Secure:   ctx.Protocol() == "https",
		SameSite: fiber.CookieSameSiteLaxMode,
	})
}
