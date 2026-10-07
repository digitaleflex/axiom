package metrics

// Stable Engine metric names (issue #103). These strings are part of the
// operational contract and must not change without a deprecation cycle.
const (
	// MetricHTTPRequestsTotal counts API requests by method and status.
	MetricHTTPRequestsTotal = "axiom_engine_http_requests_total"
	// MetricHTTPRequestDurationSeconds observes API request duration by method.
	MetricHTTPRequestDurationSeconds = "axiom_engine_http_request_duration_seconds"
	// MetricDeployments reports the number of deployments by state.
	MetricDeployments = "axiom_engine_deployments"
	// MetricOperationDurationSeconds observes execution step duration by step.
	MetricOperationDurationSeconds = "axiom_engine_operation_duration_seconds"
	// MetricOperationFailuresTotal counts operation failures by stable code.
	MetricOperationFailuresTotal = "axiom_engine_operation_failures_total"
	// MetricHeartbeatAgeSeconds reports the age of the last heartbeat by server.
	MetricHeartbeatAgeSeconds = "axiom_engine_heartbeat_age_seconds"
	// MetricRuntimes reports the number of runtimes by state.
	MetricRuntimes = "axiom_engine_runtimes"
	// MetricResources reports a resource snapshot by resource name.
	MetricResources = "axiom_engine_resources"
)

// Bounded label values for the resource snapshot.
const (
	ResourceCPU      = "cpu"
	ResourceMemoryMB = "memory_mb"
	ResourceDiskFree = "disk_free_mb"
)

// NewEngineRegistry registers every stable Engine metric with its fixed label
// schema and default histogram buckets. It returns an error only if a metric
// is misdeclared (a programming error, caught by tests).
func NewEngineRegistry() (*Registry, error) {
	r := NewRegistry()
	if _, err := r.Counter(MetricHTTPRequestsTotal, "API requests by method and status.", "method", "status"); err != nil {
		return nil, err
	}
	if _, err := r.Histogram(MetricHTTPRequestDurationSeconds, "API request duration in seconds by method.", DefaultDurationBuckets, "method"); err != nil {
		return nil, err
	}
	if _, err := r.Gauge(MetricDeployments, "Deployments by state.", "state"); err != nil {
		return nil, err
	}
	if _, err := r.Histogram(MetricOperationDurationSeconds, "Execution step duration in seconds by step.", DefaultDurationBuckets, "step"); err != nil {
		return nil, err
	}
	if _, err := r.Counter(MetricOperationFailuresTotal, "Operation failures by stable code.", "code"); err != nil {
		return nil, err
	}
	if _, err := r.Gauge(MetricHeartbeatAgeSeconds, "Age of the last agent heartbeat in seconds, by server.", "server"); err != nil {
		return nil, err
	}
	if _, err := r.Gauge(MetricRuntimes, "Runtimes by state.", "state"); err != nil {
		return nil, err
	}
	if _, err := r.Gauge(MetricResources, "Resource snapshot by resource (cpu, memory_mb, disk_free_mb).", "resource"); err != nil {
		return nil, err
	}
	return r, nil
}
