package pace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/minhhoccode111/fileuploader/internal/repo"
)

// fakeLimiter records the reservations a paced read makes.
type fakeLimiter struct {
	calls []fakeCall
	err   error
}

type fakeCall struct {
	userID string
	scope  repo.TransferScope
	bytes  int64
	rate   int64
}

func (f *fakeLimiter) Pace(
	_ context.Context,
	userID string,
	scope repo.TransferScope,
	size, rate int64,
) error {
	f.calls = append(f.calls, fakeCall{userID: userID, scope: scope, bytes: size, rate: rate})

	return f.err
}

// A caller with a small buffer still makes one reservation per batch, and the
// reservations add up to the whole stream.
func TestReaderPacesOneBatchPerReservation(t *testing.T) {
	t.Parallel()

	const (
		rate    = int64(1) << 20
		bodyLen = int64(1) << 20
	)

	limiter := &fakeLimiter{}

	reader := NewReader(
		context.Background(),
		bytes.NewReader(make([]byte, bodyLen)),
		limiter,
		"user",
		repo.TransferDownload,
		rate,
	)

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}

	if int64(len(got)) != bodyLen {
		t.Fatalf("ReadAll() bytes = %d, want %d", len(got), bodyLen)
	}

	batch := int64(BatchSize(rate))

	if len(limiter.calls) != 4 {
		t.Fatalf("Pace() calls = %d, want 4", len(limiter.calls))
	}

	var paced int64

	for _, call := range limiter.calls {
		if call.userID != "user" {
			t.Errorf("Pace() user = %q, want %q", call.userID, "user")
		}

		if call.scope != repo.TransferDownload {
			t.Errorf("Pace() scope = %q, want %q", call.scope, repo.TransferDownload)
		}

		if call.rate != rate {
			t.Errorf("Pace() rate = %d, want %d", call.rate, rate)
		}

		if call.bytes > batch {
			t.Errorf("Pace() bytes = %d, want at most %d", call.bytes, batch)
		}

		paced += call.bytes
	}

	if paced != bodyLen {
		t.Errorf("paced bytes = %d, want %d", paced, bodyLen)
	}
}

// A short final read is handed over before the stream error is reported.
func TestReaderServesTheLastBytesBeforeEOF(t *testing.T) {
	t.Parallel()

	limiter := &fakeLimiter{}

	reader := NewReader(
		context.Background(),
		strings.NewReader("hello"),
		limiter,
		"user",
		repo.TransferUpload,
		1<<20,
	)

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}

	if string(got) != "hello" {
		t.Errorf("ReadAll() = %q, want %q", got, "hello")
	}

	if len(limiter.calls) != 1 {
		t.Errorf("Pace() calls = %d, want 1", len(limiter.calls))
	}
}

// stuckReader reports no progress and no error, which a paced read must not
// pass on as if it had made progress.
type stuckReader struct{}

func (stuckReader) Read([]byte) (int, error) { return 0, nil }

func TestReaderReportsNoProgress(t *testing.T) {
	t.Parallel()

	limiter := &fakeLimiter{}

	reader := NewReader(
		context.Background(),
		stuckReader{},
		limiter,
		"user",
		repo.TransferUpload,
		1<<20,
	)

	n, err := reader.Read(make([]byte, 16))
	if n != 0 || !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("Read() = %d, %v; want 0, %v", n, err, io.ErrNoProgress)
	}

	if len(limiter.calls) != 0 {
		t.Errorf("Pace() calls = %d, want none without bytes", len(limiter.calls))
	}
}

// A failed reservation is reported instead of the bytes it could not admit.
func TestReaderReturnsTheLimiterFailure(t *testing.T) {
	t.Parallel()

	limiter := &fakeLimiter{err: io.ErrUnexpectedEOF}
	reader := NewReader(
		context.Background(),
		strings.NewReader("hello"),
		limiter,
		"user",
		repo.TransferUpload,
		1<<20,
	)

	_, err := reader.Read(make([]byte, 16))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Read() error = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

// The wrapper closes the stream it wraps.
type closeReader struct {
	closed bool
}

func (c *closeReader) Read([]byte) (int, error) { return 0, io.EOF }

func (c *closeReader) Close() error {
	c.closed = true

	return nil
}

func TestReaderClosesTheWrappedStream(t *testing.T) {
	t.Parallel()

	underlying := &closeReader{}
	reader := NewReader(
		context.Background(),
		underlying,
		&fakeLimiter{},
		"user",
		repo.TransferDownload,
		1<<20,
	)

	if err := reader.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}

	if !underlying.closed {
		t.Error("wrapped stream was not closed")
	}
}

func TestNewReaderLeavesAnUnlimitedStreamAlone(t *testing.T) {
	t.Parallel()

	limiter := &fakeLimiter{}

	reader := NewReader(
		context.Background(),
		strings.NewReader("hello"),
		limiter,
		"user",
		repo.TransferDownload,
		0,
	)

	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}

	if string(got) != "hello" {
		t.Errorf("ReadAll() = %q, want %q", got, "hello")
	}

	if len(limiter.calls) != 0 {
		t.Errorf("Pace() calls = %d, want none at rate 0", len(limiter.calls))
	}
}

func TestBatchSizeClamps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rate int64
		want int
	}{
		{name: "floor", rate: 1, want: 64 << 10},
		{name: "quarter second", rate: 1 << 20, want: 256 << 10},
		{name: "ceiling", rate: 8 << 20, want: 1 << 20},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := BatchSize(tc.rate); got != tc.want {
				t.Errorf("BatchSize(%d) = %d, want %d", tc.rate, got, tc.want)
			}
		})
	}
}
