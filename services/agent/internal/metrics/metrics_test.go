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
	c := mustCounter(t, r, "axiom_agent_docker_errors_total", "Docker errors.", "code")
	c.Inc(map[string]string{"code": "no_such_container"})
	c.Inc(map[string]string{"code": "no_such_container"})
	c.Add(map[string]string{"code": "permission_denied"}, 5)

	g := mustGauge(t, r, "axiom_agent_uptime_seconds", "Uptime.")
	g.Set(nil, 10)
	g.Add(nil, 2.5)
	g.Set(nil, 3) // Set replaces.

	out := r.Text()
	if !strings.Contains(out, `axiom_agent_docker_errors_total{code="no_such_container"} 2`) {
		t.Errorf("counter Inc not accumulated:\n%s", out)
	}
	if !strings.Contains(out, `axiom_agent_docker_errors_total{code="permission_denied"} 5`) {
		t.Errorf("counter Add not applied:\n%s", out)
	}
	if !strings.Contains(out, "axiom_agent_uptime_seconds 3") {
		t.Errorf("gauge Set did not replace value:\n%s", out)
	}
}

func TestHistogramCumulativeBuckets(t *testing.T) {
	r := NewRegistry()
	h := mustHistogram(t, r, "axiom_agent_operation_duration_seconds", "Duration.", []float64{0.1, 0.5, 1}, "type")
	h.Observe(map[string]string{"type": "deploy"}, 0.05) // <=0.1
	h.Observe(map[string]string{"type": "deploy"}, 0.3)  // <=0.5
	h.Observe(map[string]string{"type": "deploy"}, 2)    // +Inf

	out := r.Text()
	for _, want := range []string{
		`axiom_agent_operation_duration_seconds_bucket{type="deploy",le="0.1"} 1`,
		`axiom_agent_operation_duration_seconds_bucket{type="deploy",le="0.5"} 2`,
		`axiom_agent_operation_duration_seconds_bucket{type="deploy",le="1"} 2`,
		`axiom_agent_operation_duration_seconds_bucket{type="deploy",le="+Inf"} 3`,
		`axiom_agent_operation_duration_seconds_sum{type="deploy"} 2.35`,
		`axiom_agent_operation_duration_seconds_count{type="deploy"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestExpositionGolden(t *testing.T) {
	r := NewRegistry()
	c := mustCounter(t, r, "axiom_agent_operations_total", "Completed Agent operations by type and result.", "type", "result")
	c.Inc(map[string]string{"type": "deploy", "result": "ok"})
	c.Add(map[string]string{"type": "deploy", "result": "failed"}, 2)
	g := mustGauge(t, r, "axiom_agent_uptime_seconds", "Seconds since the Agent process started.")
	g.Set(nil, 12.5)
	h := mustHistogram(t, r, "axiom_agent_operation_duration_seconds", "Agent operation duration in seconds by type.", []float64{0.1, 0.5, 1}, "type")
	h.Observe(map[string]string{"type": "deploy"}, 0.05)
	h.Observe(map[string]string{"type": "deploy"}, 0.3)
	h.Observe(map[string]string{"type": "deploy"}, 2)

	const want = `# HELP axiom_agent_operation_duration_seconds Agent operation duration in seconds by type.
# TYPE axiom_agent_operation_duration_seconds histogram
axiom_agent_operation_duration_seconds_bucket{type="deploy",le="0.1"} 1
axiom_agent_operation_duration_seconds_bucket{type="deploy",le="0.5"} 2
axiom_agent_operation_duration_seconds_bucket{type="deploy",le="1"} 2
axiom_agent_operation_duration_seconds_bucket{type="deploy",le="+Inf"} 3
axiom_agent_operation_duration_seconds_sum{type="deploy"} 2.35
axiom_agent_operation_duration_seconds_count{type="deploy"} 3
# HELP axiom_agent_operations_total Completed Agent operations by type and result.
# TYPE axiom_agent_operations_total counter
axiom_agent_operations_total{type="deploy",result="failed"} 2
axiom_agent_operations_total{type="deploy",result="ok"} 1
# HELP axiom_agent_uptime_seconds Seconds since the Agent process started.
# TYPE axiom_agent_uptime_seconds gauge
axiom_agent_uptime_seconds 12.5
`
	if got := r.Text(); got != want {
		t.Errorf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestExpositionDeterministic(t *testing.T) {
	build := func() *Registry {
		r := NewRegistry()
		c := mustCounter(t, r, "axiom_agent_operations_total", "ops", "type", "result")
		// Insertion order deliberately not sorted.
		c.Inc(map[string]string{"type": "restart", "result": "ok"})
		c.Inc(map[string]string{"type": "deploy", "result": "ok"})
		c.Inc(map[string]string{"type": "deploy", "result": "failed"})
		g := mustGauge(t, r, "axiom_agent_runtimes", "runtimes", "state")
		g.Set(map[string]string{"state": "running"}, 1)
		g.Set(map[string]string{"state": "stopped"}, 0)
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
		{"unknown key", []string{"type", "zone"}},
		{"deployment id", []string{"deploymentId"}},
		{"deployment id snake", []string{"deployment_id"}},
		{"request id", []string{"requestId"}},
		{"empty key", []string{""}},
		{"duplicate key", []string{"type", "type"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := r.Counter("axiom_agent_operations_total", "ops", tc.keys...); err == nil {
				t.Fatalf("expected registration error for keys %v", tc.keys)
			}
		})
	}
}

func TestCardinalityGuardTruncatesLongValues(t *testing.T) {
	r := NewRegistry()
	g := mustGauge(t, r, "axiom_agent_resources", "resources", "resource")
	long := strings.Repeat("x", 200)
	g.Set(map[string]string{"resource": long}, 1)

	out := r.Text()
	// Extract the label value.
	const prefix = `axiom_agent_resources{resource="`
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
	if _, err := r.Counter("axiom_agent_operations_total", "ops", "type"); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if _, err := r.Gauge("axiom_agent_operations_total", "ops"); err == nil {
		t.Fatal("expected duplicate name across kinds to be rejected")
	}
}

func TestHistogramBucketValidation(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Histogram("axiom_agent_operation_duration_seconds", "d", []float64{0.5, 0.1}, "type"); err == nil {
		t.Fatal("expected unsorted buckets to be rejected")
	}
	if _, err := r.Histogram("axiom_agent_operation_duration_seconds", "d", []float64{0, 1}, "type"); err == nil {
		t.Fatal("expected non-positive bucket to be rejected")
	}
}

func TestNoSecretsInExposition(t *testing.T) {
	r := NewRegistry()
	c := mustCounter(t, r, "axiom_agent_operations_total", "ops", "type", "result")
	c.Inc(map[string]string{"type": "deploy", "result": "ok"})
	h := mustHistogram(t, r, "axiom_agent_operation_duration_seconds", "d", nil, "type")
	h.Observe(map[string]string{"type": "deploy"}, 0.42)
	g := mustGauge(t, r, "axiom_agent_uptime_seconds", "u")
	g.Set(nil, 1)

	out := r.Text()
	for _, forbidden := range []string{"password", "token", "secret", "authorization", "privatekey", "bearer"} {
		if strings.Contains(strings.ToLower(out), forbidden) {
			t.Errorf("exposition contains forbidden token %q:\n%s", forbidden, out)
		}
	}
	// Every data line's value must be numeric (values are floats, never strings).
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

func TestHandler(t *testing.T) {
	r := NewRegistry()
	g := mustGauge(t, r, "axiom_agent_uptime_seconds", "u")
	g.Set(nil, 7)

	rec := httptest.NewRecorder()
	Handler(r)(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != ContentType {
		t.Errorf("content type = %q, want %q", ct, ContentType)
	}
	if !strings.Contains(rec.Body.String(), "axiom_agent_uptime_seconds 7") {
		t.Errorf("body:\n%s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	Handler(r)(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", rec.Code)
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	c := mustCounter(t, r, MetricOperationsTotal, "ops", "type", "result")
	h := mustHistogram(t, r, MetricOperationDurationSeconds, "d", nil, "type")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Inc(map[string]string{"type": "deploy", "result": "ok"})
				h.Observe(map[string]string{"type": "deploy"}, 0.01)
				_ = r.Text()
			}
		}()
	}
	wg.Wait()
	if got := r.Text(); !strings.Contains(got, `axiom_agent_operations_total{type="deploy",result="ok"} 800`) {
		t.Errorf("expected 800 increments:\n%s", got)
	}
}

func TestAgentRegistryRegistersAllStableNames(t *testing.T) {
	r, err := NewAgentRegistry()
	if err != nil {
		t.Fatalf("NewAgentRegistry: %v", err)
	}
	out := r.Text()
	for _, name := range []string{
		MetricUptimeSeconds,
		MetricHeartbeatAgeSeconds,
		MetricOperationsTotal,
		MetricOperationDurationSeconds,
		MetricRuntimes,
		MetricResources,
		MetricDockerErrorsTotal,
	} {
		if !strings.Contains(out, "# HELP "+name+" ") {
			t.Errorf("registry missing %s:\n%s", name, out)
		}
	}
}
