package web

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/a-h/templ"
	"github.com/evrone/go-clean-template/internal/controller/web/response"
	"github.com/evrone/go-clean-template/web/templates"
	"github.com/gofiber/fiber/v2"
)

// _hxRequestTypeHeader carries htmx's swap kind: "partial" for a targeted swap, "full" for body swaps.
const (
	_hxRequestTypeHeader = "HX-Request-Type"
	_hxRequestPartial    = "partial"

	// _genericErrorMessage is shown when the server fails for a reason the caller
	// must not see. The real error is logged instead.
	_genericErrorMessage = "Something went wrong. Please try again."
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
	return c.render(ctx, templates.Home(), templates.HomeContent())
}

func (c *Controller) login(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Login(), templates.LoginContent())
}

func (c *Controller) register(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Register(), templates.RegisterContent())
}

// logout renders the confirmation screen. It is a GET because it changes nothing:
// the session is ended only by the POST to /htmx/logout that the screen triggers.
func (c *Controller) logout(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Logout(), templates.LogoutContent())
}
