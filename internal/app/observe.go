package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"github.com/minhhoccode111/fileuploader/pkg/metrics"
)

const (
	// _observeShutdownTimeout bounds the drain of the private listener.
	_observeShutdownTimeout = 5 * time.Second

	// _observeReadHeaderTimeout keeps a slow client from holding a diagnostics
	// connection open forever.
	_observeReadHeaderTimeout = 5 * time.Second
)

// observeServer is the private listener that serves metrics and pprof. It is
// separate from the Fiber server because net/http/pprof speaks net/http, and
// because the diagnostics must not pass the public middleware chain.
type observeServer struct {
	srv *http.Server
	ln  net.Listener
}

// observeAddress returns the address the listener should bind, or an empty
// address and the reason it is off. Prefork spawns one serving process per
// worker, and every copy would race for the same port; the diagnostics need one
// process, so prefork turns them off.
func observeAddress(cfg *config.Config) (addr, reason string) {
	if !cfg.Observability.Enabled {
		return "", "OBSERVE_ENABLED is false"
	}

	if cfg.HTTP.UsePreforkMode {
		return "", "prefork runs one process per worker and the listener needs one"
	}

	if cfg.Observability.Addr == "" {
		return "", "OBSERVE_ADDR is empty"
	}

	return cfg.Observability.Addr, ""
}

// observeMux builds the diagnostic routes on a mux owned by this package, so
// the process never exposes whatever net/http registers globally.
func observeMux(m *metrics.Metrics) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	return mux
}

// startObserveServer binds the diagnostics port and serves it in the
// background. A bind failure is logged and reported as nil: observability must
// never stop the app from starting.
func startObserveServer(addr string, m *metrics.Metrics, l logger.Interface) *observeServer {
	if addr == "" {
		return nil
	}

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
	if err != nil {
		l.Error(fmt.Errorf("app - startObserveServer - net.Listen: %w", err))

		return nil
	}

	server := &observeServer{
		srv: &http.Server{
			Handler:           observeMux(m),
			ReadHeaderTimeout: _observeReadHeaderTimeout,
		},
		ln: ln,
	}

	go func() {
		err := server.srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			l.Error(fmt.Errorf("app - startObserveServer - srv.Serve: %w", err))
		}
	}()

	l.Info("observe server - listening on %s", ln.Addr().String())

	return server
}

// Addr returns the address the listener bound, which is how a test reaches a
// server started on port zero.
func (s *observeServer) Addr() string {
	return s.ln.Addr().String()
}

// Shutdown drains the diagnostics listener.
func (s *observeServer) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, _observeShutdownTimeout)
	defer cancel()

	err := s.srv.Shutdown(ctx)
	if err != nil {
		return fmt.Errorf("app - observeServer.Shutdown - srv.Shutdown: %w", err)
	}

	return nil
}
