package minio

import "time"

// Option -.
type Option func(*MinIO)

// ConnAttempts -.
func ConnAttempts(attempts int) Option {
	return func(m *MinIO) {
		m.connAttempts = attempts
	}
}

// ConnTimeout -.
func ConnTimeout(timeout time.Duration) Option {
	return func(m *MinIO) {
		m.connTimeout = timeout
	}
}

// UseSSL -.
func UseSSL(secure bool) Option {
	return func(m *MinIO) {
		m.secure = secure
	}
}

// PublicEndpoint sets the host the browser reaches the bucket at. The default
// is the endpoint the app writes to, which is right when both run on the same
// host. A deployment where the app talks to a container name passes the host
// the visitor can resolve instead.
func PublicEndpoint(endpoint string) Option {
	return func(m *MinIO) {
		if endpoint != "" {
			m.publicEndpoint = endpoint
		}
	}
}
