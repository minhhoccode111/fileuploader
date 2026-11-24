package postgres

// Logger is the narrow logging surface the package needs. It is defined here
// so the package stays free of the application layers and accepts any logger
// that can report an informational message and an error.
type Logger interface {
	Info(message string, args ...any)
	Error(message any, args ...any)
}
