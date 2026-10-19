package templates

// View carries the per-request state the layout and the navigation render. The
// handlers build it once and hand it to every component, so a page never has
// to work out who the visitor is.
type View struct {
	// Authenticated reports whether a signed-in visitor is behind the request.
	Authenticated bool
	// Admin reports whether that visitor holds the admin role. Only an admin
	// sees the admin link and the admin page.
	Admin bool
}
