# ADR-0009 — SSE for realtime Console updates

- **Status:** Accepted
- **Date:** 2026-10-07
- **Issue:** #71, #118

## Context

The Console needs server → client deployment progress; no client → server realtime messaging is required in V0.1.

## Decision

Use Server-Sent Events: `GET /api/v1/deployments/{id}/events/stream`. Clients reconcile by fetching a snapshot on (re)connect.

## Consequences

- Plain HTTP, proxy-friendly, simple reconnect.
- A bidirectional protocol may be introduced later via a new ADR.

## Alternatives considered

WebSocket (unneeded bidirectionality), polling (latency and load).
