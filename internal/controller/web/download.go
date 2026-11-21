package web

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

const (
	// _nosniffHeader keeps a browser from guessing a body into another type
	// than the one the handler declared.
	_nosniffHeader = "X-Content-Type-Options"

	// _downloadFallbackName is the name a client that ignores filename* saves
	// the file under when the name itself is not ASCII.
	_downloadFallbackName = "download"
)

// setDownloadHeaders marks a response as an attachment and keeps the browser
// from sniffing the body into another type.
func setDownloadHeaders(ctx *fiber.Ctx, contentType, name string) {
	ctx.Set(fiber.HeaderContentType, contentType)
	ctx.Set(fiber.HeaderContentDisposition, downloadDisposition(name))
	ctx.Set(_nosniffHeader, "nosniff")
}

// downloadDisposition builds the attachment header with the name twice: a
// quoted ASCII fallback every client can read, and the exact UTF-8 name in
// filename* for the clients that follow RFC 5987.
func downloadDisposition(name string) string {
	return `attachment; filename="` + asciiFallback(name) +
		`"; filename*=UTF-8''` + rfc5987Escape(name)
}

// asciiFallback replaces every byte a quoted header value cannot carry with an
// underscore, so the fallback can never end the quoted string early. A name
// with no ASCII at all falls back to a plain word, because a run of
// underscores tells the visitor nothing.
func asciiFallback(name string) string {
	var builder strings.Builder

	written := false

	for _, r := range name {
		if r >= 0x20 && r < 0x7F && r != '"' && r != '\\' {
			builder.WriteRune(r)

			written = true

			continue
		}

		builder.WriteByte('_')
	}

	if !written {
		return _downloadFallbackName
	}

	fallback := strings.TrimSpace(builder.String())
	if fallback == "" {
		return _downloadFallbackName
	}

	return fallback
}

// rfc5987Escape percent-encodes every byte that is not an RFC 5987 attr-char.
// The name is UTF-8, so a multi-byte rune becomes one escape per byte.
func rfc5987Escape(name string) string {
	const hex = "0123456789ABCDEF"

	var builder strings.Builder
	builder.Grow(len(name))

	for i := 0; i < len(name); i++ {
		char := name[i]
		if isAttrChar(char) {
			builder.WriteByte(char)

			continue
		}

		builder.WriteByte('%')
		builder.WriteByte(hex[char>>4])
		builder.WriteByte(hex[char&0xF])
	}

	return builder.String()
}

// isAttrChar reports whether c may survive unescaped in an RFC 5987 value.
func isAttrChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}

	switch c {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}

	return false
}
