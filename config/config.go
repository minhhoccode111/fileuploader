package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

// UploadOverheadBytes is the room the multipart envelope, its headers and
// the name field need on top of the bytes of one part.
const UploadOverheadBytes int64 = 1 << 20

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
		Limits  limits
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
		// MaxBytes is the largest file an account may upload. It bounds the
		// whole file now that it arrives in parts, not just one request.
		MaxBytes int64 `env:"UPLOAD_MAX_BYTES" envDefault:"10737418240"`
		// PartBytes is the size of one part of a multipart upload. The last
		// part may be shorter; the store needs every other part at least as
		// large as its own minimum.
		PartBytes int64 `env:"UPLOAD_PART_BYTES" envDefault:"52428800"`
		// SessionIdleTTL is how long an upload session survives without a new
		// part. Every accepted part pushes the expiry back by this much.
		SessionIdleTTL time.Duration `env:"UPLOAD_SESSION_IDLE_TTL" envDefault:"1h"`
		// Timeout is how long the server waits for a request body, raised from
		// the default so a large upload over a slow line can finish.
		Timeout time.Duration `env:"UPLOAD_TIMEOUT" envDefault:"5m"`
	}

	// Limits -.
	limits struct {
		// StorageQuotaBytes is the default storage quota of an account. A
		// per-account zero inherits it; a zero default means unlimited.
		StorageQuotaBytes int64 `env:"STORAGE_DEFAULT_QUOTA_BYTES" envDefault:"0"`
		// UploadRateBytesPerSec is the default upload speed limit. A
		// per-account zero inherits it; a zero default means unlimited.
		UploadRateBytesPerSec int64 `env:"UPLOAD_DEFAULT_RATE_BYTES_PER_SEC" envDefault:"0"`
		// DownloadRateBytesPerSec is the default download speed limit. A
		// per-account zero inherits it; a zero default means unlimited.
		DownloadRateBytesPerSec int64 `env:"DOWNLOAD_DEFAULT_RATE_BYTES_PER_SEC" envDefault:"0"`
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
