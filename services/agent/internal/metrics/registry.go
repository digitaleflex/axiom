package metrics

// Stable Agent metric names (issue #87). These strings are part of the
// operational contract and must not change without a deprecation cycle.
const (
	// MetricUptimeSeconds is the seconds since the Agent process started.
	MetricUptimeSeconds = "axiom_agent_uptime_seconds"
	// MetricHeartbeatAgeSeconds is the seconds since the last successful
	// heartbeat to the Engine.
	MetricHeartbeatAgeSeconds = "axiom_agent_heartbeat_age_seconds"
	// MetricOperationsTotal counts completed operations by type and result.
	MetricOperationsTotal = "axiom_agent_operations_total"
	// MetricOperationDurationSeconds observes operation duration by type.
	MetricOperationDurationSeconds = "axiom_agent_operation_duration_seconds"
	// MetricRuntimes reports the number of managed runtimes by state.
	MetricRuntimes = "axiom_agent_runtimes"
	// MetricResources reports a resource snapshot by resource name.
	MetricResources = "axiom_agent_resources"
	// MetricDockerErrorsTotal counts Docker errors by stable code.
	MetricDockerErrorsTotal = "axiom_agent_docker_errors_total"
)

// Bounded label values for the resource snapshot.
const (
	ResourceCPU      = "cpu"
	ResourceMemoryMB = "memory_mb"
	ResourceDiskFree = "disk_free_mb"
)

// NewAgentRegistry registers every stable Agent metric with its fixed label
// schema and default histogram buckets. It returns an error only if a metric
// is misdeclared (which would be a programming error, caught by tests).
func NewAgentRegistry() (*Registry, error) {
	r := NewRegistry()
	if _, err := r.Gauge(MetricUptimeSeconds, "Seconds since the Agent process started."); err != nil {
		return nil, err
	}
	if _, err := r.Gauge(MetricHeartbeatAgeSeconds, "Seconds since the last successful heartbeat to the Engine."); err != nil {
		return nil, err
	}
	if _, err := r.Counter(MetricOperationsTotal, "Completed Agent operations by type and result.", "type", "result"); err != nil {
		return nil, err
	}
	if _, err := r.Histogram(MetricOperationDurationSeconds, "Agent operation duration in seconds by type.", DefaultDurationBuckets, "type"); err != nil {
		return nil, err
	}
	if _, err := r.Gauge(MetricRuntimes, "Managed runtimes by state.", "state"); err != nil {
		return nil, err
	}
	if _, err := r.Gauge(MetricResources, "Resource snapshot by resource (cpu, memory_mb, disk_free_mb).", "resource"); err != nil {
		return nil, err
	}
	if _, err := r.Counter(MetricDockerErrorsTotal, "Docker errors by stable code.", "code"); err != nil {
		return nil, err
	}
	return r, nil
}
