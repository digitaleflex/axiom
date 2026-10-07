package metrics

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func mustCounter(t *testing.T, r *Registry, name, help string, keys ...string) *Counter {
	t.Helper()
	c, err := r.Counter(name, help, keys...)
	if err != nil {
		t.Fatalf("Counter(%q): %v", name, err)
	}
	return c
}

func mustGauge(t *testing.T, r *Registry, name, help string, keys ...string) *Gauge {
	t.Helper()
	g, err := r.Gauge(name, help, keys...)
	if err != nil {
		t.Fatalf("Gauge(%q): %v", name, err)
	}
	return g
}

func mustHistogram(t *testing.T, r *Registry, name, help string, buckets []float64, keys ...string) *Histogram {
	t.Helper()
	h, err := r.Histogram(name, help, buckets, keys...)
	if err != nil {
		t.Fatalf("Histogram(%q): %v", name, err)
	}
	return h
}

func TestCounterAndGaugeSemantics(t *testing.T) {
	r := NewRegistry()
	c := mustCounter(t, r, MetricOperationFailuresTotal, "failures", "code")
	c.Inc(map[string]string{"code": "BUILD_FAILED"})
	c.Inc(map[string]string{"code": "BUILD_FAILED"})
	c.Add(map[string]string{"code": "POLICY_DENIED"}, 3)

	g := mustGauge(t, r, MetricDeployments, "deployments", "state")
	g.Set(map[string]string{"state": "RUNNING"}, 4)
	g.Add(map[string]string{"state": "RUNNING"}, 1)
	g.Set(map[string]string{"state": "RUNNING"}, 5)

	out := r.Text()
	if !strings.Contains(out, `axiom_engine_operation_failures_total{code="BUILD_FAILED"} 2`) {
		t.Errorf("counter Inc not accumulated:\n%s", out)
	}
	if !strings.Contains(out, `axiom_engine_operation_failures_total{code="POLICY_DENIED"} 3`) {
		t.Errorf("counter Add not applied:\n%s", out)
	}
	if !strings.Contains(out, `axiom_engine_deployments{state="RUNNING"} 5`) {
		t.Errorf("gauge Set did not replace:\n%s", out)
	}
}

func TestHistogramCumulativeBuckets(t *testing.T) {
	r := NewRegistry()
	h := mustHistogram(t, r, MetricOperationDurationSeconds, "d", []float64{0.1, 0.5, 1}, "step")
	h.Observe(map[string]string{"step": "BUILD"}, 0.05)
	h.Observe(map[string]string{"step": "BUILD"}, 0.3)
	h.Observe(map[string]string{"step": "BUILD"}, 2)

	out := r.Text()
	for _, want := range []string{
		`axiom_engine_operation_duration_seconds_bucket{step="BUILD",le="0.1"} 1`,
		`axiom_engine_operation_duration_seconds_bucket{step="BUILD",le="0.5"} 2`,
		`axiom_engine_operation_duration_seconds_bucket{step="BUILD",le="1"} 2`,
		`axiom_engine_operation_duration_seconds_bucket{step="BUILD",le="+Inf"} 3`,
		`axiom_engine_operation_duration_seconds_sum{step="BUILD"} 2.35`,
		`axiom_engine_operation_duration_seconds_count{step="BUILD"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestExpositionGolden(t *testing.T) {
	r := NewRegistry()
	c := mustCounter(t, r, MetricHTTPRequestsTotal, "API requests by method and status.", "method", "status")
	c.Inc(map[string]string{"method": "GET", "status": "200"})
	c.Add(map[string]string{"method": "POST", "status": "500"}, 2)
	h := mustHistogram(t, r, MetricOperationDurationSeconds, "Execution step duration in seconds by step.", []float64{1, 5}, "step")
	h.Observe(map[string]string{"step": "BUILD"}, 0.4)
	h.Observe(map[string]string{"step": "BUILD"}, 7)
	g := mustGauge(t, r, MetricHeartbeatAgeSeconds, "Age of the last agent heartbeat in seconds, by server.", "server")
	g.Set(map[string]string{"server": "srv_1"}, 3)

	const want = `# HELP axiom_engine_heartbeat_age_seconds Age of the last agent heartbeat in seconds, by server.
# TYPE axiom_engine_heartbeat_age_seconds gauge
axiom_engine_heartbeat_age_seconds{server="srv_1"} 3
# HELP axiom_engine_http_requests_total API requests by method and status.
# TYPE axiom_engine_http_requests_total counter
axiom_engine_http_requests_total{method="GET",status="200"} 1
axiom_engine_http_requests_total{method="POST",status="500"} 2
# HELP axiom_engine_operation_duration_seconds Execution step duration in seconds by step.
# TYPE axiom_engine_operation_duration_seconds histogram
axiom_engine_operation_duration_seconds_bucket{step="BUILD",le="1"} 1
axiom_engine_operation_duration_seconds_bucket{step="BUILD",le="5"} 1
axiom_engine_operation_duration_seconds_bucket{step="BUILD",le="+Inf"} 2
axiom_engine_operation_duration_seconds_sum{step="BUILD"} 7.4
axiom_engine_operation_duration_seconds_count{step="BUILD"} 2
`
	if got := r.Text(); got != want {
		t.Errorf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestEmptyRegistryIsValid(t *testing.T) {
	r := NewRegistry()
	if got := r.Text(); got != "" {
		t.Errorf("empty registry Text() = %q, want empty", got)
	}
	rec := httptest.NewRecorder()
	Handler(r)(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("empty handler status = %d", rec.Code)
	}
	if body := rec.Body.String(); body != "" {
		t.Errorf("empty handler body = %q, want empty", body)
	}
}

func TestHandlerContentType(t *testing.T) {
	r := NewRegistry()
	g := mustGauge(t, r, MetricHeartbeatAgeSeconds, "hb", "server")
	g.Set(map[string]string{"server": "srv_1"}, 3)

	rec := httptest.NewRecorder()
	Handler(r)(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("content type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `axiom_engine_heartbeat_age_seconds{server="srv_1"} 3`) {
		t.Errorf("body:\n%s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	Handler(r)(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rec.Code)
	}
}

func TestExpositionDeterministic(t *testing.T) {
	build := func() *Registry {
		r := NewRegistry()
		c := mustCounter(t, r, MetricHTTPRequestsTotal, "reqs", "method", "status")
		c.Inc(map[string]string{"method": "PUT", "status": "204"})
		c.Inc(map[string]string{"method": "GET", "status": "200"})
		c.Inc(map[string]string{"method": "GET", "status": "500"})
		g := mustGauge(t, r, MetricRuntimes, "runtimes", "state")
		g.Set(map[string]string{"state": "READY"}, 1)
		g.Set(map[string]string{"state": "DEGRADED"}, 0)
		return r
	}
	first := build().Text()
	for i := 0; i < 20; i++ {
		if got := build().Text(); got != first {
			t.Fatalf("non-deterministic exposition at iteration %d\n%s", i, got)
		}
	}
}

func TestCardinalityGuardRejectsUnknownLabelKeys(t *testing.T) {
	r := NewRegistry()
	cases := []struct {
		name string
		keys []string
	}{
		{"unknown key", []string{"method", "zone"}},
		{"deployment id", []string{"deploymentId"}},
		{"request id", []string{"requestId"}},
		{"correlation id", []string{"correlation_id"}},
		{"path", []string{"path"}},
		{"empty key", []string{""}},
		{"duplicate key", []string{"method", "method"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := r.Counter(MetricHTTPRequestsTotal, "reqs", tc.keys...); err == nil {
				t.Fatalf("expected registration error for keys %v", tc.keys)
			}
		})
	}
}

func TestCardinalityGuardTruncatesLongValues(t *testing.T) {
	r := NewRegistry()
	g := mustGauge(t, r, MetricResources, "resources", "resource")
	long := strings.Repeat("y", 300)
	g.Set(map[string]string{"resource": long}, 1)

	out := r.Text()
	const prefix = `axiom_engine_resources{resource="`
	i := strings.Index(out, prefix)
	if i < 0 {
		t.Fatalf("resource sample missing:\n%s", out)
	}
	rest := out[i+len(prefix):]
	end := strings.IndexByte(rest, '"')
	value := rest[:end]
	if n := len([]rune(value)); n != MaxLabelValueLen {
		t.Fatalf("truncated value has %d runes, want %d", n, MaxLabelValueLen)
	}
	if !strings.HasSuffix(value, truncatedMarker) {
		t.Fatalf("truncated value %q lacks marker", value)
	}
}

func TestDuplicateRegistrationRejected(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Counter(MetricHTTPRequestsTotal, "reqs", "method"); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if _, err := r.Gauge(MetricHTTPRequestsTotal, "reqs"); err == nil {
		t.Fatal("expected duplicate name across kinds to be rejected")
	}
}

func TestHistogramBucketValidation(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Histogram(MetricOperationDurationSeconds, "d", []float64{0.5, 0.1}, "step"); err == nil {
		t.Fatal("expected unsorted buckets to be rejected")
	}
	if _, err := r.Histogram(MetricOperationDurationSeconds, "d", []float64{0, 1}, "step"); err == nil {
		t.Fatal("expected non-positive bucket to be rejected")
	}
}

func TestNoSecretsInExposition(t *testing.T) {
	r := NewRegistry()
	c := mustCounter(t, r, MetricHTTPRequestsTotal, "reqs", "method", "status")
	c.Inc(map[string]string{"method": "GET", "status": "200"})
	h := mustHistogram(t, r, MetricHTTPRequestDurationSeconds, "d", nil, "method")
	h.Observe(map[string]string{"method": "GET"}, 0.42)
	g := mustGauge(t, r, MetricHeartbeatAgeSeconds, "hb", "server")
	g.Set(map[string]string{"server": "srv_1"}, 1)

	out := r.Text()
	for _, forbidden := range []string{"password", "token", "secret", "authorization", "privatekey", "bearer"} {
		if strings.Contains(strings.ToLower(out), forbidden) {
			t.Errorf("exposition contains forbidden token %q:\n%s", forbidden, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.LastIndexByte(line, ' ')
		if idx < 0 {
			t.Fatalf("malformed exposition line %q", line)
		}
		if _, err := strconv.ParseFloat(line[idx+1:], 64); err != nil {
			t.Errorf("non-numeric value in line %q: %v", line, err)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	c := mustCounter(t, r, MetricHTTPRequestsTotal, "reqs", "method", "status")
	h := mustHistogram(t, r, MetricHTTPRequestDurationSeconds, "d", nil, "method")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Inc(map[string]string{"method": "GET", "status": "200"})
				h.Observe(map[string]string{"method": "GET"}, 0.01)
				_ = r.Text()
			}
		}()
	}
	wg.Wait()
	if got := r.Text(); !strings.Contains(got, `axiom_engine_http_requests_total{method="GET",status="200"} 800`) {
		t.Errorf("expected 800 increments:\n%s", got)
	}
}

func TestEngineRegistryRegistersAllStableNames(t *testing.T) {
	r, err := NewEngineRegistry()
	if err != nil {
		t.Fatalf("NewEngineRegistry: %v", err)
	}
	out := r.Text()
	for _, name := range []string{
		MetricHTTPRequestsTotal,
		MetricHTTPRequestDurationSeconds,
		MetricDeployments,
		MetricOperationDurationSeconds,
		MetricOperationFailuresTotal,
		MetricHeartbeatAgeSeconds,
		MetricRuntimes,
		MetricResources,
	} {
		if !strings.Contains(out, "# HELP "+name+" ") {
			t.Errorf("registry missing %s:\n%s", name, out)
		}
	}
}
