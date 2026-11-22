package httpserver

import (
	"testing"
	"time"

	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

func TestNewUsesNoWriteTimeoutByDefault(t *testing.T) {
	t.Parallel()

	srv := New(logger.New("error"))
	if srv.writeTimeout != 0 {
		t.Errorf("writeTimeout = %s, want 0", srv.writeTimeout)
	}
}

func TestWriteTimeoutOptionIsApplied(t *testing.T) {
	t.Parallel()

	srv := New(logger.New("error"), WriteTimeout(50*time.Millisecond))
	if srv.writeTimeout != 50*time.Millisecond {
		t.Errorf("writeTimeout = %s, want 50ms", srv.writeTimeout)
	}
}
