package config

import "testing"

// setRequiredEnv fills the environment the config parser requires.
func setRequiredEnv(t *testing.T) {
	t.Helper()

	//nolint:gosec // test fixture, not a real credential
	required := map[string]string{
		"APP_NAME":         "file-uploader",
		"APP_VERSION":      "1.0.0",
		"HTTP_PORT":        "8090",
		"LOG_LEVEL":        "debug",
		"PG_POOL_MAX":      "10",
		"PG_URL":           "postgres://user:password@localhost:5433/fileuploader",
		"REDIS_ADDR":       "localhost:6379",
		"MINIO_ENDPOINT":   "localhost:9000",
		"MINIO_ACCESS_KEY": "minioadmin",
		"MINIO_SECRET_KEY": "P@55w0rd",
	}

	for name, value := range required {
		t.Setenv(name, value)
	}
}

//nolint:paralleltest // t.Setenv cannot run in a parallel test
func TestLimitsDefaultToUnlimited(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error = %v, want nil", err)
	}

	if cfg.Limits.StorageQuotaBytes != 0 {
		t.Errorf("storage quota = %d, want 0", cfg.Limits.StorageQuotaBytes)
	}

	if cfg.Limits.UploadRateBytesPerSec != 0 {
		t.Errorf("upload rate = %d, want 0", cfg.Limits.UploadRateBytesPerSec)
	}

	if cfg.Limits.DownloadRateBytesPerSec != 0 {
		t.Errorf("download rate = %d, want 0", cfg.Limits.DownloadRateBytesPerSec)
	}
}

func TestLimitsReadFromEnvironment(t *testing.T) {
	setRequiredEnv(t)

	t.Setenv("STORAGE_DEFAULT_QUOTA_BYTES", "10000000000")
	t.Setenv("UPLOAD_DEFAULT_RATE_BYTES_PER_SEC", "1048576")
	t.Setenv("DOWNLOAD_DEFAULT_RATE_BYTES_PER_SEC", "2097152")

	cfg, err := NewConfig()
	if err != nil {
		t.Fatalf("NewConfig() error = %v, want nil", err)
	}

	if cfg.Limits.StorageQuotaBytes != 10000000000 {
		t.Errorf("storage quota = %d, want 10000000000", cfg.Limits.StorageQuotaBytes)
	}

	if cfg.Limits.UploadRateBytesPerSec != 1048576 {
		t.Errorf("upload rate = %d, want 1048576", cfg.Limits.UploadRateBytesPerSec)
	}

	if cfg.Limits.DownloadRateBytesPerSec != 2097152 {
		t.Errorf("download rate = %d, want 2097152", cfg.Limits.DownloadRateBytesPerSec)
	}
}
