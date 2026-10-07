// Package observability groups the Engine's cross-cutting operational
// concerns: structured logging, correlation identifiers and secret redaction
// (ADR-0006, issue #101).
//
// The logging subpackage carries the shared convention every Engine log line
// follows; see docs/architecture/logging.md for the field and severity rules.
package observability
