package web

import (
	"github.com/a-h/templ"
	"github.com/evrone/go-clean-template/web/templates"
	"github.com/gofiber/fiber/v2"
)

// _cookieDeleteMaxAge is a negative Max-Age, which tells the browser to drop the
// cookie immediately.
const _cookieDeleteMaxAge = -1

// logoutSubmit ends the session and sends the user to the login screen, the only
// page an anonymous visitor may reach. It only runs when the user confirms on the
// /logout screen, which is why it is a POST and not a link.
func (c *Controller) logoutSubmit(ctx *fiber.Ctx) error {
	c.clearAuthCookie(ctx)

	// The POST URL is not a page, so tell htmx which URL the browser should show.
	ctx.Set(_hxPushURL, _loginPath)

	// The session is gone, so the sidebar must drop the logout item and show the
	// guest links again: swap it out of band alongside the login fragment.
	return c.render(
		ctx,
		templates.Login(false),
		templ.Join(templates.LoginContent(), templates.SidebarNavUpdate(false)),
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
