package web

import (
	"bytes"
	"net/http"

	"github.com/a-h/templ"
	"github.com/evrone/go-clean-template/web/templates"
	"github.com/gofiber/fiber/v2"
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

func (c *Controller) home(ctx *fiber.Ctx) error {
	return c.renderPage(ctx, templates.Home())
}

func (c *Controller) login(ctx *fiber.Ctx) error {
	return c.renderPage(ctx, templates.Login())
}

func (c *Controller) register(ctx *fiber.Ctx) error {
	return c.renderPage(ctx, templates.Register())
}

func (c *Controller) logout(ctx *fiber.Ctx) error {
	return c.renderPage(ctx, templates.Logout())
}
