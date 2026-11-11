// Package httpserver implements HTTP server.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"golang.org/x/sync/errgroup"
)

const (
	_defaultAddr = ":80"
	// _defaultReadTimeout bounds how long the server waits for a request body.
	_defaultReadTimeout = 5 * time.Second
	// _defaultWriteTimeout is zero so a streamed download can run as long as
	// the client keeps reading. A fixed deadline cuts large downloads.
	_defaultWriteTimeout    = 0
	_defaultShutdownTimeout = 3 * time.Second
)

// Server -.
type Server struct {
	ctx context.Context
	eg  *errgroup.Group

	App    *fiber.App
	notify chan error

	address         string
	prefork         bool
	bodyLimit       int
	readTimeout     time.Duration
	writeTimeout    time.Duration
	shutdownTimeout time.Duration

	logger logger.Interface
}

// New -.
func New(l logger.Interface, opts ...Option) *Server {
	group, ctx := errgroup.WithContext(context.Background())
	group.SetLimit(1) // Run only one goroutine

	s := &Server{
		ctx:             ctx,
		eg:              group,
		App:             nil,
		notify:          make(chan error, 1),
		address:         _defaultAddr,
		readTimeout:     _defaultReadTimeout,
		writeTimeout:    _defaultWriteTimeout,
		shutdownTimeout: _defaultShutdownTimeout,
		logger:          l,
	}

	// Custom options
	for _, opt := range opts {
		opt(s)
	}

	app := fiber.New(fiber.Config{
		Prefork:                      s.prefork,
		BodyLimit:                    s.bodyLimit,
		ReadTimeout:                  s.readTimeout,
		WriteTimeout:                 s.writeTimeout,
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true,
		JSONDecoder:                  json.Unmarshal,
		JSONEncoder:                  json.Marshal,
	})

	s.App = app

	return s
}

// Start -.
func (s *Server) Start() {
	s.eg.Go(func() error {
		err := s.App.Listen(s.address)
		if err != nil {
			s.notify <- err

			close(s.notify)

			return err
		}

		return nil
	})

	s.logger.Info("http server - Server - Started")
}

// Notify -.
func (s *Server) Notify() <-chan error {
	return s.notify
}

// Shutdown -.
func (s *Server) Shutdown() error {
	var shutdownErrors []error

	err := s.App.ShutdownWithTimeout(s.shutdownTimeout)
	if err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error(fmt.Errorf("http server - Server - Shutdown - s.App.ShutdownWithTimeout: %w", err))

		shutdownErrors = append(shutdownErrors, err)
	}

	// Wait for all goroutines to finish and get any error
	err = s.eg.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error(fmt.Errorf("http server - Server - Shutdown - s.eg.Wait: %w", err))

		shutdownErrors = append(shutdownErrors, err)
	}

	s.logger.Info("http server - Server - Shutdown")

	return errors.Join(shutdownErrors...)
}
