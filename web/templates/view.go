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
	// DemoLogin carries the try-out account the login page offers to a
	// visitor. It is nil when the environment did not name one, so the
	// page renders exactly as it did before.
	DemoLogin *DemoLogin
}

// DemoLogin carries the try-out account the login page offers to a visitor.
// It is nil when the environment did not name one.
type DemoLogin struct {
	Email    string
	Username string
	Password string
}

// State returns the Alpine scope the login form reads the demo credentials
// from. A nil demo yields an empty scope, so the attribute renders
// unconditionally and the page behaves as it did before.
func (d *DemoLogin) State() map[string]string {
	if d == nil {
		return map[string]string{}
	}

	return map[string]string{
		"demoEmail":    d.Email,
		"demoPassword": d.Password,
	}
}
