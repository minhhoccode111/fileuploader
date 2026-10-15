package web

import (
	"bytes"
	"net/http"

	"github.com/a-h/templ"
	"github.com/evrone/go-clean-template/web/templates"
	"github.com/gofiber/fiber/v2"
)

// _hxRequestTypeHeader carries htmx's swap kind: "partial" for a targeted swap, "full" for body swaps.
const (
	_hxRequestTypeHeader = "HX-Request-Type"
	_hxRequestPartial    = "partial"
)

// renderPage renders a templ component as an HTML response.
func (c *Controller) renderPage(ctx *fiber.Ctx, component templ.Component) error {
	buf := new(bytes.Buffer)
	if err := component.Render(ctx.UserContext(), buf); err != nil {
		c.l.Error(err, "web - page")

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

func (c *Controller) logout(ctx *fiber.Ctx) error {
	return c.render(ctx, templates.Logout(), templates.LogoutContent())
}
