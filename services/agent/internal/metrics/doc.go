// Package metrics is the Agent's in-process metrics registry (issue #87,
// ADR-0006). It is stdlib-only by design: the Agent ships with no third-party
// dependencies, so the registry, the histogram and the Prometheus text
// exposition formatter are implemented here instead of pulling in a client
// library.
//
// The registry is deliberately small and opinionated:
//
//   - Counters, gauges and fixed-bucket histograms, each carrying a bounded
//     label set declared at registration time.
//   - A cardinality guard: label keys are validated against a fixed allow-list
//     and label values are truncated to MaxLabelValueLen with a marker.
//   - Deterministic Prometheus text exposition (families sorted by name,
//     samples sorted by label set) so snapshots are byte-stable and safe to
//     golden-test.
//
// Deployment identifiers must never be label values: they are unbounded and
// would make the metric series cardinality grow with every deployment. Use a
// bounded dimension (type/state/result/code) instead; see docs/operations/metrics.md.
package metrics
