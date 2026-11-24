package middleware

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v2"
)

// _routeRejectedLocal marks a request that a middleware refused before the
// router could match a route. Without it such a request would be labeled with
// the middleware entry's "/" path, which is also the home page.
const _routeRejectedLocal = "route_rejected"

// markRouteRejected records that no route handler ran for this request.
func markRouteRejected(ctx *fiber.Ctx) {
	ctx.Locals(_routeRejectedLocal, true)
}

// responseStatus reports the status the client will see. Fiber runs its error
// handler after the middleware chain returns, so a handler error has not
// reached the response yet; the error carries the code instead.
func responseStatus(ctx *fiber.Ctx, err error) int {
	if err != nil {
		if fiberErr, ok := errors.AsType[*fiber.Error](err); ok {
			return fiberErr.Code
		}

		return http.StatusInternalServerError
	}

	return ctx.Response().StatusCode()
}

// requestRoute reports the template of the matched route, never the path that
// was requested: "/folders/:folder", not "/folders/6f1c". A path with no route
// is "unmatched", which keeps the label set bounded.
//
// Fiber copies a middleware entry into every method stack and overwrites the
// copy's Method, so Route() cannot tell a middleware entry from a handler.
// The routing error can: only the router produces a 404 or 405 here, because
// every handler answers those with a rendered page and a nil error. A
// middleware that refused before routing marks the request instead.
func requestRoute(ctx *fiber.Ctx, err error) string {
	if rejected, ok := ctx.Locals(_routeRejectedLocal).(bool); ok && rejected {
		return "unmatched"
	}

	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) &&
		(fiberErr.Code == fiber.StatusNotFound || fiberErr.Code == fiber.StatusMethodNotAllowed) {
		return "unmatched"
	}

	return ctx.Route().Path
}
