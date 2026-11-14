package middleware

import (
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// BodyLimit bounds request bodies when the server streams them. A known
// Content-Length is checked against the limit of the path before the handler
// runs. The upload prefix carries parts of one file, so it uses the larger
// upload limit and accepts an unknown length, which the part handler checks
// itself. Every other path refuses an unknown length with 411 instead of
// reading an unbounded body.
func BodyLimit(general, upload int64, uploadPrefix string) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		limit := general
		allowUnknown := false

		if strings.HasPrefix(ctx.Path(), uploadPrefix) {
			limit = upload
			allowUnknown = true
		}

		length := int64(ctx.Request().Header.ContentLength())

		if length > limit {
			return ctx.Status(http.StatusRequestEntityTooLarge).SendString("Request Entity Too Large")
		}

		if length < 0 && !allowUnknown {
			return ctx.Status(http.StatusLengthRequired).SendString("Length Required")
		}

		return ctx.Next()
	}
}
