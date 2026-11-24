package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

// gather returns the exposition of the metrics object, indexed by family name.
func gather(t *testing.T, m *Metrics) map[string]*dto.MetricFamily {
	t.Helper()

	families, err := m.registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v, want nil", err)
	}

	index := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		index[family.GetName()] = family
	}

	return index
}

// metricWith returns the child of a family that carries exactly the labels.
func metricWith(
	t *testing.T,
	families map[string]*dto.MetricFamily,
	name string,
	labels map[string]string,
) *dto.Metric {
	t.Helper()

	family, ok := families[name]
	if !ok {
		t.Fatalf("family %q missing from the exposition", name)
	}

	for _, metric := range family.GetMetric() {
		if hasLabels(metric, labels) {
			return metric
		}
	}

	t.Fatalf("family %q has no child with labels %v", name, labels)

	return nil
}

func hasLabels(metric *dto.Metric, labels map[string]string) bool {
	if len(metric.GetLabel()) != len(labels) {
		return false
	}

	for _, pair := range metric.GetLabel() {
		if labels[pair.GetName()] != pair.GetValue() {
			return false
		}
	}

	return true
}

func counterValue(metric *dto.Metric) float64 {
	return metric.GetCounter().GetValue()
}

func gaugeValue(metric *dto.Metric) float64 {
	return metric.GetGauge().GetValue()
}

func TestRequestMetricsRecordOneRequest(t *testing.T) {
	t.Parallel()

	m := New(Options{Version: "test", PoolStats: nil})

	m.RequestStarted()
	m.RequestFinished("GET", "/folders/:folder", 200, 0.012, 42)

	families := gather(t, m)

	requests := counterValue(metricWith(t, families, "http_requests_total", map[string]string{
		"method": "GET", "route": "/folders/:folder", "status": "200",
	}))
	if requests != 1 {
		t.Errorf("http_requests_total = %v, want 1", requests)
	}

	bytes := counterValue(metricWith(t, families, "http_response_bytes_total", map[string]string{
		"method": "GET", "route": "/folders/:folder",
	}))
	if bytes != 42 {
		t.Errorf("http_response_bytes_total = %v, want 42", bytes)
	}

	duration := metricWith(t, families, "http_request_duration_seconds", map[string]string{
		"method": "GET", "route": "/folders/:folder",
	})
	if count := duration.GetHistogram().GetSampleCount(); count != 1 {
		t.Errorf("duration samples = %d, want 1", count)
	}

	inFlight := gaugeValue(metricWith(t, families, "http_requests_in_flight", map[string]string{}))
	if inFlight != 0 {
		t.Errorf("http_requests_in_flight = %v, want 0", inFlight)
	}
}

func TestNegativeBytesCountAsZero(t *testing.T) {
	t.Parallel()

	m := New(Options{Version: "test", PoolStats: nil})

	m.RequestStarted()
	m.RequestFinished("GET", "/folders/:folder/download", 200, 0.5, -1)

	bytes := counterValue(metricWith(t, gather(t, m), "http_response_bytes_total", map[string]string{
		"method": "GET", "route": "/folders/:folder/download",
	}))
	if bytes != 0 {
		t.Errorf("http_response_bytes_total = %v, want 0", bytes)
	}
}

func TestPoolGaugesReadTheClosure(t *testing.T) {
	t.Parallel()

	m := New(Options{
		Version:   "test",
		PoolStats: func() PoolStats { return PoolStats{Total: 3, Idle: 2, Acquired: 1, Max: 10} },
	})

	families := gather(t, m)

	cases := map[string]float64{"total": 3, "idle": 2, "acquired": 1, "max": 10}
	for state, want := range cases {
		got := gaugeValue(metricWith(t, families, "db_pool_conns", map[string]string{"state": state}))
		if got != want {
			t.Errorf("db_pool_conns{state=%q} = %v, want %v", state, got, want)
		}
	}
}

func TestHandlerServesTheExposition(t *testing.T) {
	t.Parallel()

	m := New(Options{Version: "1.2.3", PoolStats: nil})

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/metrics", http.NoBody,
	))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "app_build_info") || !strings.Contains(body, `version="1.2.3"`) {
		t.Errorf("body = %q, want the build info with its version", body)
	}
}
