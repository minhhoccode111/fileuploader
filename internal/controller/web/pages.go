package web

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/controller/web/response"
	"github.com/minhhoccode111/fileuploader/internal/entity"
	"github.com/minhhoccode111/fileuploader/web/templates"
)

// _hxRequestTypeHeader carries htmx's swap kind: "partial" for a targeted swap, "full" for body swaps.
const (
	_hxRequestTypeHeader = "HX-Request-Type"
	_hxRequestPartial    = "partial"

	// _genericErrorMessage is shown when the server fails for a reason the caller
	// must not see. The real error is logged instead.
	_genericErrorMessage = "Something went wrong. Please try again."

	// _sessionLocal is where the auth guard parks the session it resolved, which
	// is how a handler learns who the visitor is and what role they hold.
	_sessionLocal = "session"
)

// renderErrors renders the error fragment htmx swaps into a form's error target.
func (c *Controller) renderErrors(ctx *fiber.Ctx, status int, messages []string) error {
	ctx.Status(status)

	return c.renderPage(ctx, templates.ErrorList(response.Errors{Messages: messages}))
}

// renderPage renders a templ component as an HTML response.
func (c *Controller) renderPage(ctx *fiber.Ctx, component templ.Component) error {
	buf := new(bytes.Buffer)
	if err := component.Render(ctx.UserContext(), buf); err != nil {
		c.l.Error(fmt.Errorf("web - renderPage - component.Render: %w", err))

		return ctx.SendStatus(http.StatusInternalServerError)
	}

	ctx.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)

	return ctx.Send(buf.Bytes())
}

// render returns the fragment for an htmx partial swap, the full page otherwise.
func (c *Controller) render(ctx *fiber.Ctx, full, fragment templ.Component) error {
	if ctx.Get(_hxRequestTypeHeader) == _hxRequestPartial {
		return c.renderPage(ctx, fragment)
	}

	return c.renderPage(ctx, full)
}

func (c *Controller) home(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Home(c.view(ctx)), templates.HomeContent())
}

func (c *Controller) login(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Login(c.view(ctx)), templates.LoginContent())
}

func (c *Controller) register(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Register(c.view(ctx)), templates.RegisterContent())
}

// admin renders the admin page. The guard has already checked the role, so the
// handler only renders.
func (c *Controller) admin(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Admin(c.view(ctx)), templates.AdminContent())
}

// share renders a placeholder page for the Share section. It is auth-only, so
// the guard has already resolved a user before it runs.
func (c *Controller) share(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Share(c.view(ctx)), templates.ShareContent())
}

// public renders a placeholder page for the Public section. It is auth-only,
// so the guard has already resolved a user before it runs.
func (c *Controller) public(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Public(c.view(ctx)), templates.PublicContent())
}

// logout renders the confirmation screen. It is a GET because it changes nothing:
// the session is ended only by the POST to /htmx/logout that the screen triggers.
func (c *Controller) logout(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Logout(c.view(ctx)), templates.LogoutContent())
}

// view builds the per-request state the templates need. The auth guard parks
// the session it resolved, so a handler never has to look the visitor up: an
// anonymous request gets the empty view.
func (c *Controller) view(ctx *fiber.Ctx) templates.View {
	session, ok := ctx.Locals(_sessionLocal).(entity.Session)
	if !ok {
		return templates.View{}
	}

	return templates.View{
		Authenticated: true,
		Admin:         session.Role == entity.UserRoleAdmin,
	}
}
