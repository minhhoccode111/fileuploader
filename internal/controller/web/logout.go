package web

import (
	"errors"
	"fmt"

	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/web/templates"
)

const (
	// _cookieDeleteMaxAge is a negative Max-Age, which tells the browser to drop
	// the cookie immediately.
	_cookieDeleteMaxAge = -1

	_loginPath = "/login"
)

// logoutSubmit ends the session and sends the user to the login screen, the only
// page an anonymous visitor may reach. It only runs when the user confirms on the
// /logout screen, which is why it is a POST and not a link.
func (c *Controller) logoutSubmit(ctx *fiber.Ctx) error {
	if err := c.u.Logout(ctx.UserContext(), ctx.Cookies(_authCookieName)); err != nil &&
		!errors.Is(err, entity.ErrSessionNotFound) {
		c.requestLogger(ctx).Error(fmt.Errorf("web - logoutSubmit - uc.Logout: %w", err))
	}

	c.clearAuthCookie(ctx)

	// The POST URL is not a page, so tell htmx which URL the browser should show.
	ctx.Set(_hxPushURLHeader, _loginPath)

	// The session is gone, so the sidebar must drop the logout item and show the
	// guest links again: swap it out of band alongside the login fragment.
	return c.render(
		ctx,
		templates.Login(templates.View{}),
		templ.Join(templates.LoginContent(), templates.SidebarNavUpdate(templates.View{})),
		templates.LoginTitle,
	)
}

// clearAuthCookie expires the session cookie. It repeats the attributes used when
// the cookie was set so the browser matches and removes the same cookie.
func (c *Controller) clearAuthCookie(ctx *fiber.Ctx) {
	ctx.Cookie(&fiber.Cookie{
		Name:     _authCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   _cookieDeleteMaxAge,
		HTTPOnly: true,
		Secure:   ctx.Protocol() == "https",
		SameSite: fiber.CookieSameSiteLaxMode,
	})
}
