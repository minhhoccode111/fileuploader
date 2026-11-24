package app

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/minhhoccode111/fileuploader/config"
	"github.com/minhhoccode111/fileuploader/pkg/logger"
	"github.com/minhhoccode111/fileuploader/pkg/metrics"
)

// silentLogger keeps the expected listen errors out of the test output.
type silentLogger struct{}

func (silentLogger) Debug(_ any, _ ...any)                   {}
func (silentLogger) Info(_ string, _ ...any)                 {}
func (silentLogger) Warn(_ string, _ ...any)                 {}
func (silentLogger) Error(_ any, _ ...any)                   {}
func (silentLogger) Fatal(_ any, _ ...any)                   {}
func (silentLogger) WithRequestID(_ string) logger.Interface { return silentLogger{} }

// observeConfig returns the config an app starts with before a test mutates it.
func observeConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Observability.Enabled = true
	cfg.Observability.Addr = "127.0.0.1:9090"

	return cfg
}

func TestObserveAddressFollowsTheConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		mutate     func(*config.Config)
		wantAddr   string
		wantReason string
	}{
		{
			name:     "enabled",
			mutate:   func(*config.Config) {},
			wantAddr: "127.0.0.1:9090",
		},
		{
			name:       "disabled",
			mutate:     func(cfg *config.Config) { cfg.Observability.Enabled = false },
			wantReason: "OBSERVE_ENABLED",
		},
		{
			name:       "prefork",
			mutate:     func(cfg *config.Config) { cfg.HTTP.UsePreforkMode = true },
			wantReason: "prefork",
		},
		{
			name:       "empty address",
			mutate:     func(cfg *config.Config) { cfg.Observability.Addr = "" },
			wantReason: "OBSERVE_ADDR",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := observeConfig()
			testCase.mutate(cfg)

			addr, reason := observeAddress(cfg)

			if addr != testCase.wantAddr {
				t.Errorf("addr = %q, want %q", addr, testCase.wantAddr)
			}

			if !strings.Contains(reason, testCase.wantReason) {
				t.Errorf("reason = %q, want it to mention %q", reason, testCase.wantReason)
			}
		})
	}
}

func observeDo(t *testing.T, addr, path string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+path, http.NoBody)
	if err != nil {
		t.Fatalf("NewRequest(%s) error = %v, want nil", path, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s error = %v, want nil", path, err)
	}

	return resp
}

func TestObserveServerServesMetricsAndPprof(t *testing.T) {
	t.Parallel()

	m := metrics.New(metrics.Options{Version: "test", PoolStats: nil})
	server := startObserveServer("127.0.0.1:0", m, silentLogger{})

	if server == nil {
		t.Fatal("startObserveServer() = nil, want a server")
	}

	t.Cleanup(func() {
		err := server.Shutdown(context.Background())
		if err != nil {
			t.Errorf("Shutdown() error = %v, want nil", err)
		}
	})

	metricsResp := observeDo(t, server.Addr(), "/metrics")
	defer metricsResp.Body.Close()

	if metricsResp.StatusCode != http.StatusOK {
		t.Errorf("/metrics status = %d, want %d", metricsResp.StatusCode, http.StatusOK)
	}

	pprofResp := observeDo(t, server.Addr(), "/debug/pprof/")
	defer pprofResp.Body.Close()

	if pprofResp.StatusCode != http.StatusOK {
		t.Errorf("/debug/pprof/ status = %d, want %d", pprofResp.StatusCode, http.StatusOK)
	}
}

func TestObserveServerReportsABusyPort(t *testing.T) {
	t.Parallel()

	m := metrics.New(metrics.Options{Version: "test", PoolStats: nil})

	first := startObserveServer("127.0.0.1:0", m, silentLogger{})
	if first == nil {
		t.Fatal("first startObserveServer() = nil, want a server")
	}

	t.Cleanup(func() {
		err := first.Shutdown(context.Background())
		if err != nil {
			t.Errorf("Shutdown() error = %v, want nil", err)
		}
	})

	if second := startObserveServer(first.Addr(), m, silentLogger{}); second != nil {
		t.Errorf("second startObserveServer() = %v, want nil on a busy port", second)

		err := second.Shutdown(context.Background())
		if err != nil {
			t.Errorf("Shutdown() error = %v, want nil", err)
		}
	}
}

func TestObserveServerStopsOnShutdown(t *testing.T) {
	t.Parallel()

	m := metrics.New(metrics.Options{Version: "test", PoolStats: nil})

	server := startObserveServer("127.0.0.1:0", m, silentLogger{})
	if server == nil {
		t.Fatal("startObserveServer() = nil, want a server")
	}

	addr := server.Addr()

	err := server.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown() error = %v, want nil", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/metrics", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequest() error = %v, want nil", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()

		t.Error("GET after Shutdown() succeeded, want a connection error")
	}
}
