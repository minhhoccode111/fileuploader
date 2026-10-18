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
