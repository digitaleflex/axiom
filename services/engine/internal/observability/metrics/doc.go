// Package metrics is the Engine's in-process metrics registry (issue #103,
// ADR-0006). It is stdlib-only by design, mirroring the Agent's registry
// (services/agent/internal/metrics) so both sides share one exposition
// contract without a shared module or a third-party client library.
//
// The registry provides counters, gauges and fixed-bucket histograms carrying
// a bounded label set declared at registration time, a cardinality guard, and
// deterministic Prometheus text exposition. The Engine's GET /metrics route
// (internal/httpserver) serves Registry through Handler.
//
// Deployment and request identifiers must never be label values: they are
// unbounded and would make series cardinality grow with every request. Use the
// bounded dimensions declared here (method/status/state/step/code/server/
// resource); see docs/operations/metrics.md.
package metrics
