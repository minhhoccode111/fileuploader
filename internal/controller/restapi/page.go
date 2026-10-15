package restapi

import (
	"bytes"
	"net/http"

	"github.com/evrone/go-clean-template/pkg/logger"
	"github.com/evrone/go-clean-template/web/templates"
	"github.com/gofiber/fiber/v2"
)

// index renders the home page.
func index(l logger.Interface) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		buf := new(bytes.Buffer)

		if err := templates.Index().Render(ctx.UserContext(), buf); err != nil {
			l.Error(err, "restapi - index")

			return ctx.SendStatus(http.StatusInternalServerError)
		}

		ctx.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)

		return ctx.Send(buf.Bytes())
	}
}
