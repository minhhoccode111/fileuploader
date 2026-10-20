package middleware

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/internal/entity"
)

// Cookie and redirect names shared by the page guards. The cookie name
// mirrors the one the web controller uses when it sets and clears the session.
const (
	_authCookieName = "access_token"
	_homePath       = "/"
	_loginPath      = "/login"
	_userIDLocal    = "userID"
	_sessionLocal   = "session"
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
			return Redirect(ctx, _loginPath)
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
			return Redirect(ctx, _loginPath)
		}

		if session.Role != entity.UserRoleAdmin {
			return Redirect(ctx, _homePath)
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
			return Redirect(ctx, _homePath)
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
