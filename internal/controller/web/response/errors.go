// Package response holds the payloads the web controllers render as fragments.
package response

// Errors carries every user-facing error message produced by one request so the
// client can render them together instead of one at a time.
type Errors struct {
	Messages []string
}
