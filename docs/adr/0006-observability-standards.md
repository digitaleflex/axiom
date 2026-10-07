# ADR-0006 — Observability standards

- **Status:** Accepted
- **Date:** 2026-10-07
- **Issue:** #101, #103

## Context

Failed deployments must be diagnosable without SSH; components must correlate events across Console, Engine and Agent.

## Decision

- Structured JSON logs via `log/slog` in all Go services.
- Every request carries a correlation ID (`X-Request-ID`, generated if absent) propagated to Agent operations and included in logs, events and API errors (`requestId`).
- Operational metrics exposed in a Prometheus-compatible, low-cardinality format, without secrets.
- Secrets are redacted before logging or emitting events.
- Distributed tracing (OpenTelemetry) is deferred; IDs are compatible with later adoption.

## Consequences

- Uniform log parsing and correlation from day one.
- No tracing backend required for V0.1.

## Alternatives considered

Full OpenTelemetry in V0.1 — deferred (operational overhead for a single-server reference).
