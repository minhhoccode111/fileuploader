package middleware

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

// The headers that carry an in-page redirect to htmx, and the target the
// fragment is swapped into.
const (
	_hxRequestHeader  = "HX-Request"
	_hxLocationHeader = "HX-Location"
	_hxRequestTrue    = "true"

	_pageContent   = "#page-content"
	_swapInnerHTML = "innerHTML"
)

// Redirect sends the visitor to a path. htmx requests receive HX-Location so
// htmx swaps the destination fragment into #page-content and pushes the URL,
// matching the app's normal client-side navigation. Every other request gets a
// 302, which the browser follows as a full page load.
func Redirect(ctx *fiber.Ctx, path string) error {
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
