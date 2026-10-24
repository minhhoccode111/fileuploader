package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type (
	// Config -.
	Config struct {
		App     app
		HTTP    http
		Log     log
		PG      pg
		Redis   redis
		MinIO   minio
		Session session
		Upload  upload
	}

	// App -.
	app struct {
		Name    string `env:"APP_NAME,required"`
		Version string `env:"APP_VERSION,required"`
	}

	// HTTP -.
	http struct {
		Port           string `env:"HTTP_PORT,required"`
		UsePreforkMode bool   `env:"HTTP_USE_PREFORK_MODE" envDefault:"false"`
		// FakeLatencyMin and FakeLatencyMax bound the delay every request
		// waits for. A non-positive max turns the fake latency off.
		FakeLatencyMin time.Duration `env:"HTTP_FAKE_LATENCY_MIN" envDefault:"100ms"`
		FakeLatencyMax time.Duration `env:"HTTP_FAKE_LATENCY_MAX" envDefault:"200ms"`
	}

	// Log -.
	log struct {
		Level string `env:"LOG_LEVEL,required"`
	}

	// PG -.
	pg struct {
		PoolMax int    `env:"PG_POOL_MAX,required"`
		URL     string `env:"PG_URL,required"`
	}

	// Redis -.
	redis struct {
		Addr string `env:"REDIS_ADDR,required"`
	}

	// MinIO -.
	minio struct {
		Endpoint  string `env:"MINIO_ENDPOINT,required"`
		AccessKey string `env:"MINIO_ACCESS_KEY,required"`
		SecretKey string `env:"MINIO_SECRET_KEY,required"`
		UseSSL    bool   `env:"MINIO_USE_SSL" envDefault:"false"`
		Bucket    string `env:"MINIO_BUCKET" envDefault:"files"`
		// PublicEndpoint is the host the browser reaches the bucket at. It
		// defaults to Endpoint, which is right when both run on one host.
		PublicEndpoint string `env:"MINIO_PUBLIC_ENDPOINT"`
	}

	// Session -.
	session struct {
		TTL time.Duration `env:"SESSION_TTL" envDefault:"24h"`
	}

	// Upload -.
	upload struct {
		// MaxBytes is the largest body the server accepts. It bounds one
		// multipart upload, so a single request cannot exceed the configured
		// size.
		MaxBytes int64 `env:"UPLOAD_MAX_BYTES" envDefault:"104857600"`
		// Timeout is how long the server waits for a request body, raised from
		// the default so a large upload over a slow line can finish.
		Timeout time.Duration `env:"UPLOAD_TIMEOUT" envDefault:"5m"`
	}
)

// NewConfig returns app config.
func NewConfig() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}

	return cfg, nil
}
