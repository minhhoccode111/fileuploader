package web

import (
	"bytes"
	"context"
	"testing"
)

// A rate below the size of one read must not make the limiter reject the read:
// the wrapper caps each read to the burst it configured.
func TestThrottledReaderCapsReadsToItsBurst(t *testing.T) {
	t.Parallel()

	reader := newThrottledReader(context.Background(), bytes.NewReader(make([]byte, 64*1024)), 1024)
	defer reader.Close()

	buf := make([]byte, 64*1024)

	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v, want nil", err)
	}

	if n > 1024 {
		t.Errorf("Read() n = %d, want at most the burst of 1024", n)
	}
}
