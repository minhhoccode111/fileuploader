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
	}

	// Session -.
	session struct {
		TTL time.Duration `env:"SESSION_TTL" envDefault:"24h"`
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
