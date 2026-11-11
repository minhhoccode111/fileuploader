package httpserver_test

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/pkg/httpserver"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
)

const (
	_slowDownloadSize  = 512 * 1024
	_slowDownloadChunk = 4096
	_slowDownloadDelay = 40 * time.Millisecond
)

// slowReader produces the body of a download slower than the old five-second
// write timeout.
type slowReader struct {
	left int
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.left <= 0 {
		return 0, io.EOF
	}

	if len(p) > _slowDownloadChunk {
		p = p[:_slowDownloadChunk]
	}

	if len(p) > s.left {
		p = p[:s.left]
	}

	time.Sleep(_slowDownloadDelay)

	s.left -= len(p)

	return len(p), nil
}

// A streamed download may take longer than five seconds. The server must not
// cut it with the old default write deadline.
func TestStreamedDownloadIsNotCutByAWriteTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("slow streaming test")
	}

	t.Parallel()

	srv := httpserver.New(logger.New("error"))
	srv.App.Get("/slow", func(ctx *fiber.Ctx) error {
		return ctx.SendStream(&slowReader{left: _slowDownloadSize})
	})

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v, want nil", err)
	}

	go func() {
		if listenErr := srv.App.Listener(ln); listenErr != nil {
			t.Errorf("Listener() error = %v, want nil", listenErr)
		}
	}()

	defer func() {
		if shutdownErr := srv.App.Shutdown(); shutdownErr != nil {
			t.Errorf("Shutdown() error = %v, want nil", shutdownErr)
		}
	}()

	client := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"http://"+ln.Addr().String()+"/slow",
		http.NoBody,
	)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v, want nil", err)
	}

	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v, want nil", err)
	}

	defer res.Body.Close()

	n, err := io.Copy(io.Discard, res.Body)
	if err != nil {
		t.Fatalf("Copy() error = %v, want nil", err)
	}

	if n != _slowDownloadSize {
		t.Errorf("read %d bytes, want %d", n, _slowDownloadSize)
	}
}
