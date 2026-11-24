// Package metrics owns the Prometheus registry and the collectors this
// service exposes. Every object lives on a Metrics value: no package-level
// state, so tests build their own registry and never see each other's data.
package metrics

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Duration bucket boundaries in seconds. They cover a cached page, the
// hundred-millisecond fake latency of local runs, and a multi-minute upload.
const (
	_bucket5ms    = 0.005
	_bucket10ms   = 0.01
	_bucket25ms   = 0.025
	_bucket50ms   = 0.05
	_bucket100ms  = 0.1
	_bucket250ms  = 0.25
	_bucket500ms  = 0.5
	_bucket1s     = 1
	_bucket2p5s   = 2.5
	_bucket5s     = 5
	_bucket10s    = 10
	_bucket30s    = 30
	_bucket60s    = 60
	_bucket120s   = 120
	_bucket300s   = 300
	_buildInfoVal = 1
)

// Options configures a Metrics value.
type Options struct {
	// Version is the build the process reports.
	Version string
	// PoolStats reads the Postgres pool at scrape time. Nil disables the gauges.
	PoolStats func() PoolStats
}

// PoolStats is a snapshot of the Postgres connection pool.
type PoolStats struct {
	Total    int32
	Idle     int32
	Acquired int32
	Max      int32
}

// Metrics is the registry plus the collectors that write into it.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	bytes    *prometheus.CounterVec
	inFlight prometheus.Gauge
}

// New builds an empty registry and every collector this service exposes.
func New(opts Options) *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Requests handled, by method, route template and status.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Request time in seconds, by method and route template.",
			Buckets: requestBuckets(),
		}, []string{"method", "route"}),
		bytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_response_bytes_total",
			Help: "Response bytes written, by method and route template.",
		}, []string{"method", "route"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Requests currently being served.",
		}),
	}

	buildInfo := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "app_build_info",
		Help:        "Build information; the value is always 1.",
		ConstLabels: prometheus.Labels{"version": opts.Version},
	})
	buildInfo.Set(_buildInfoVal)

	m.registry.MustRegister(m.requests, m.duration, m.bytes, m.inFlight, buildInfo)

	if opts.PoolStats != nil {
		m.registerPoolGauges(opts.PoolStats)
	}

	return m
}

// Handler serves the exposition in Prometheus format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

// RequestStarted counts a request that entered the middleware chain.
func (m *Metrics) RequestStarted() {
	m.inFlight.Inc()
}

// RequestFinished records the outcome of one request. A negative byte count
// means the response was a chunked stream of unknown length; the counter cannot
// go down, so it records zero instead of panicking.
func (m *Metrics) RequestFinished(method, route string, status int, seconds float64, bytes int64) {
	if bytes < 0 {
		bytes = 0
	}

	m.inFlight.Dec()
	m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.duration.WithLabelValues(method, route).Observe(seconds)
	m.bytes.WithLabelValues(method, route).Add(float64(bytes))
}

// requestBuckets returns the histogram boundaries. It is a function, not a
// variable, because the linters keep package-level state out of this codebase.
func requestBuckets() []float64 {
	return []float64{
		_bucket5ms, _bucket10ms, _bucket25ms, _bucket50ms, _bucket100ms,
		_bucket250ms, _bucket500ms, _bucket1s, _bucket2p5s, _bucket5s,
		_bucket10s, _bucket30s, _bucket60s, _bucket120s, _bucket300s,
	}
}

// registerPoolGauges exposes one gauge per pool state. The value is read when
// Prometheus scrapes, so the app never keeps a parallel copy of pool state.
func (m *Metrics) registerPoolGauges(stats func() PoolStats) {
	m.registry.MustRegister(
		poolGauge("total", stats, func(stat PoolStats) int32 { return stat.Total }),
		poolGauge("idle", stats, func(stat PoolStats) int32 { return stat.Idle }),
		poolGauge("acquired", stats, func(stat PoolStats) int32 { return stat.Acquired }),
		poolGauge("max", stats, func(stat PoolStats) int32 { return stat.Max }),
	)
}

func poolGauge(state string, stats func() PoolStats, pick func(PoolStats) int32) prometheus.Collector {
	opts := prometheus.GaugeOpts{
		Name:        "db_pool_conns",
		Help:        "Postgres pool connections, by state.",
		ConstLabels: prometheus.Labels{"state": state},
	}

	return prometheus.NewGaugeFunc(opts, func() float64 {
		return float64(pick(stats()))
	})
}
