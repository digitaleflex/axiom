// Package logging is the Engine's structured logging convention (issue #101,
// ADR-0006): correlation field helpers, level mapping, secret-redacting
// handlers and operation timing helpers.
//
// It defines the stable JSON keys operators and the Console rely on
// (requestId, correlationId, deploymentId, service, component, operation,
// durationMs, result) and the rules for choosing a severity. The full
// convention lives in docs/architecture/logging.md.
//
// This package adds no new runtime behavior: existing call sites keep logging
// through their own *slog.Logger. It exists so those sites converge on one set
// of field names and one redaction path instead of each re-implementing them.
package logging
