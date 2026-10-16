package middleware

import (
	"fmt"

	"github.com/evrone/go-clean-template/pkg/jwt"
	"github.com/gofiber/fiber/v2"
)

// Cookie, target and redirect names shared by the page guards. The cookie name
// mirrors the one the web controller uses when it sets and clears the session,
// and the target matches the <main> element htmx swaps on client-side navigation.
const (
	_authCookieName = "access_token"
	_homePath       = "/"
	_loginPath      = "/login"
	_userIDLocal    = "userID"

	_pageContent   = "#page-content"
	_swapInnerHTML = "innerHTML"

	_hxRequestHeader  = "HX-Request"
	_hxLocationHeader = "HX-Location"
	_hxRequestTrue    = "true"
)

// RequireAuth lets authenticated visitors through and sends anonymous ones to
// the login page. The resolved user ID is parked in ctx.Locals("userID").
func RequireAuth(jwtManager *jwt.Manager) func(*fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		userID, ok := authenticatedUser(ctx, jwtManager)
		if !ok {
			return redirectTo(ctx, _loginPath)
		}

		ctx.Locals(_userIDLocal, userID)

		return ctx.Next()
	}
}

// RequireGuest lets anonymous visitors through and sends authenticated ones to
// the home page.
func RequireGuest(jwtManager *jwt.Manager) func(*fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		if _, ok := authenticatedUser(ctx, jwtManager); ok {
			return redirectTo(ctx, _homePath)
		}

		return ctx.Next()
	}
}

// authenticatedUser resolves the session cookie to a user ID. A missing or
// invalid cookie is not an error: the visitor is simply anonymous.
func authenticatedUser(ctx *fiber.Ctx, jwtManager *jwt.Manager) (string, bool) {
	token := ctx.Cookies(_authCookieName)
	if token == "" {
		return "", false
	}

	userID, err := jwtManager.ParseToken(token)
	if err != nil {
		return "", false
	}

	return userID, true
}

// redirectTo sends the visitor to a path. htmx requests receive HX-Location so
// htmx swaps the destination fragment into #page-content and pushes the URL,
// matching the app's normal client-side navigation. Every other request gets a
// 302, which the browser follows as a full page load.
func redirectTo(ctx *fiber.Ctx, path string) error {
	if ctx.Get(_hxRequestHeader) == _hxRequestTrue {
		ctx.Set(_hxLocationHeader, partialLocation(path))

		return nil
	}

	return ctx.Redirect(path, fiber.StatusFound)
}

// partialLocation builds the HX-Location value for an in-page redirect: load
// path, swap the fragment into #page-content, and let htmx push the path.
func partialLocation(path string) string {
	return fmt.Sprintf(`{"path":%q,"target":%q,"swap":%q}`, path, _pageContent, _swapInnerHTML)
}
