package middleware

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// Cookie, target and redirect names shared by the page guards. The cookie name
// mirrors the one the web controller uses when it sets and clears the session,
// and the target matches the <main> element htmx swaps on client-side navigation.
const (
	_authCookieName = "access_token"
	_homePath       = "/"
	_loginPath      = "/login"
	_userIDLocal    = "userID"
	_sessionLocal   = "session"

	_pageContent   = "#page-content"
	_swapInnerHTML = "innerHTML"

	_hxRequestHeader  = "HX-Request"
	_hxLocationHeader = "HX-Location"
	_hxRequestTrue    = "true"
)

// sessionAuthenticator resolves a session identifier to the session behind it.
// The web controller's use case implements it.
type sessionAuthenticator interface {
	Authenticate(ctx context.Context, sessionID string) (entity.Session, error)
}

// RequireAuth lets authenticated visitors through and sends anonymous ones to
// the login page. The resolved session is parked in ctx.Locals("session") and
// the user ID it holds in ctx.Locals("userID").
func RequireAuth(auth sessionAuthenticator) func(*fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		session, ok := authenticatedSession(ctx, auth)
		if !ok {
			return redirectTo(ctx, _loginPath)
		}

		parkSession(ctx, session)

		return ctx.Next()
	}
}

// RequireAdmin lets administrators through and sends everyone else away: an
// anonymous visitor to the login page, a signed-in visitor without the admin
// role to the home page. The admin link is hidden from non-admins, so a direct
// request must not hand them the page either.
func RequireAdmin(auth sessionAuthenticator) func(*fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		session, ok := authenticatedSession(ctx, auth)
		if !ok {
			return redirectTo(ctx, _loginPath)
		}

		if session.Role != entity.UserRoleAdmin {
			return redirectTo(ctx, _homePath)
		}

		parkSession(ctx, session)

		return ctx.Next()
	}
}

// RequireGuest lets anonymous visitors through and sends authenticated ones to
// the home page.
func RequireGuest(auth sessionAuthenticator) func(*fiber.Ctx) error {
	return func(ctx *fiber.Ctx) error {
		if _, ok := authenticatedSession(ctx, auth); ok {
			return redirectTo(ctx, _homePath)
		}

		return ctx.Next()
	}
}

// parkSession stores the resolved session where the handlers can find it.
func parkSession(ctx *fiber.Ctx, session entity.Session) {
	ctx.Locals(_sessionLocal, session)
	ctx.Locals(_userIDLocal, session.UserID)
}

// authenticatedSession resolves the session cookie to the session behind it. A
// missing cookie, an unknown session or a store error all mean anonymous: the
// guard redirects rather than failing the request.
func authenticatedSession(ctx *fiber.Ctx, auth sessionAuthenticator) (entity.Session, bool) {
	sessionID := ctx.Cookies(_authCookieName)
	if sessionID == "" {
		return entity.Session{}, false
	}

	session, err := auth.Authenticate(ctx.UserContext(), sessionID)
	if err != nil {
		return entity.Session{}, false
	}

	return session, true
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
