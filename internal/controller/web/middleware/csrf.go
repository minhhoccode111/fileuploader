package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v2"
)

const (
	// _csrfCookieName holds the double-submit token. The browser reads it with
	// script, so it is not an HTTP-only cookie.
	_csrfCookieName = "csrf_token"

	// _csrfHeaderName carries the token back on every unsafe request.
	_csrfHeaderName = "X-CSRF-Token"

	// _csrfTokenBytes is the size of the random token. 32 bytes (256 bits)
	// makes guessing a live token infeasible.
	_csrfTokenBytes = 32
)

// CSRF protects state-changing requests with the double-submit cookie pattern:
// every visitor gets a random cookie, and a POST, PUT, PATCH or DELETE must
// send the same value in the X-CSRF-Token header. A cross-site page can make
// the browser send the cookie but cannot read it to fill the header. The
// comparison is constant-time so a response cannot leak the token byte by byte.
// An unsafe request whose Origin header names another host is refused even with
// a matching token: a related subdomain can plant a cookie, but it cannot make
// the browser claim the request came from this origin.
//
// Safe methods are not checked, so a page load always issues the cookie first.
func CSRF() fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		token := ctx.Cookies(_csrfCookieName)
		if token == "" {
			generated, err := newCSRFToken()
			if err != nil {
				markRouteRejected(ctx)

				return fiber.ErrInternalServerError
			}

			token = generated

			ctx.Cookie(&fiber.Cookie{
				Name:     _csrfCookieName,
				Value:    token,
				Path:     "/",
				HTTPOnly: false,
				Secure:   ctx.Protocol() == "https",
				SameSite: fiber.CookieSameSiteLaxMode,
			})
		}

		if isSafeMethod(ctx.Method()) {
			return ctx.Next()
		}

		presented := ctx.Get(_csrfHeaderName)

		if !sameOrigin(ctx) || subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
			markRouteRejected(ctx)

			return ctx.Status(fiber.StatusForbidden).
				SendString("CSRF token missing or invalid. Reload the page and retry.")
		}

		return ctx.Next()
	}
}

// sameOrigin reports whether an unsafe request names this host in its Origin
// header. A browser sends the header on every unsafe request, so a foreign value
// means another site started it. An absent header falls back to the token
// check, which a non-browser client has to pass anyway.
func sameOrigin(ctx *fiber.Ctx) bool {
	origin := ctx.Get(fiber.HeaderOrigin)
	if origin == "" {
		return true
	}

	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}

	return strings.EqualFold(parsed.Host, ctx.Hostname())
}

// isSafeMethod reports whether a method only reads state. TRACE and CONNECT are
// not routed by the app, so they are left out.
func isSafeMethod(method string) bool {
	return method == fiber.MethodGet ||
		method == fiber.MethodHead ||
		method == fiber.MethodOptions
}

// newCSRFToken returns a fresh opaque token.
func newCSRFToken() (string, error) {
	token := make([]byte, _csrfTokenBytes)

	_, err := io.ReadFull(rand.Reader, token)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(token), nil
}
