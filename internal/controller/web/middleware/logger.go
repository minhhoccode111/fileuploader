package middleware

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

func buildRequestMessage(ctx *fiber.Ctx) string {
	var result strings.Builder

	result.WriteString(ctx.IP())
	result.WriteString(" - ")
	result.WriteString(ctx.Method())
	result.WriteString(" ")
	result.WriteString(ctx.OriginalURL())
	result.WriteString(" - ")
	result.WriteString(strconv.Itoa(ctx.Response().StatusCode()))
	result.WriteString(" ")
	result.WriteString(strconv.Itoa(responseBodySize(ctx)))

	return result.String()
}

// responseBodySize reports the buffered body length, or the declared length of
// a stream. It must not call Body() on a stream: that reads the whole response
// into memory before the server can write the headers. A chunked stream has no
// declared length and reports -1.
func responseBodySize(ctx *fiber.Ctx) int {
	if ctx.Response().IsBodyStream() {
		return ctx.Response().Header.ContentLength()
	}

	return len(ctx.Response().Body())
}

func Logger(l logger.Interface) func(c *fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		err := ctx.Next()

		l.Info("%s", buildRequestMessage(ctx))

		return err
	}
}
