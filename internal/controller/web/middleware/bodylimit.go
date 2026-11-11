package middleware

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
)

// BodyLimit bounds request bodies when the server streams them. A known
// Content-Length is checked before the handler runs. A chunked body is refused
// unless the request targets a handler that enforces its own stream limit.
func BodyLimit(limit int64, streamPath string) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		length := int64(ctx.Request().Header.ContentLength())

		if length > limit {
			return ctx.Status(http.StatusRequestEntityTooLarge).SendString("Request Entity Too Large")
		}

		if length == -1 && ctx.Path() != streamPath {
			return ctx.Status(http.StatusLengthRequired).SendString("Length Required")
		}

		return ctx.Next()
	}
}
