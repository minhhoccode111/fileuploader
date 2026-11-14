package middleware

import (
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// BodyLimit bounds request bodies when the server streams them. A known
// Content-Length is checked against the limit of the path before the handler
// runs. The upload prefix carries parts of one file, so it uses the larger
// upload limit and accepts a chunked body, which the part handler checks
// itself. Every other path refuses a chunked body with 411 instead of reading
// an unbounded body; a request with no body at all passes through, so a plain
// page load is not mistaken for one.
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

		if length < 0 && !allowUnknown && announceUnknownLength(ctx) {
			return ctx.Status(http.StatusLengthRequired).SendString("Length Required")
		}

		return ctx.Next()
	}
}

// announceUnknownLength reports whether the request announces a body whose
// length the server cannot see, which is a chunked body. A request with neither
// Content-Length nor Transfer-Encoding carries no body at all — it is how a
// browser fetches a page — so it passes through.
func announceUnknownLength(ctx *fiber.Ctx) bool {
	return len(ctx.Request().Header.Peek(fiber.HeaderTransferEncoding)) > 0
}
