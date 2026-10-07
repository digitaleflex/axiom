# ADR-0008 — Agent ↔ Engine communication model

- **Status:** Proposed
- **Date:** 2026-10-07
- **Issue:** #75, #88

## Context

Runtime Agents run on user servers, often behind NAT/firewalls. The Engine must remain authoritative and operations must be replay-resistant.

The domain contract is fixed in [`docs/architecture/agent-protocol.md`](../architecture/agent-protocol.md) with machine types in `services/agent/internal/protocol/`.

## Decision (proposed)

- The Agent initiates an outbound, authenticated connection to the Engine (no inbound port on the server).
- Messages follow a transport-independent protocol (`docs/architecture/agent-protocol.md`, #75): registration, heartbeat, capability report, operation dispatch/ack/result, error envelope, protocol version, correlation ID, idempotency key, timestamps.
- Only explicit operation types are accepted; unknown types are rejected; no shell payload.
- Transport choice (long-poll HTTPS vs WebSocket vs gRPC stream) is decided in #75.

## Consequences

- Works behind NAT; Engine never needs server credentials.
- Requires Agent reconnect/backoff and Engine-side liveness (#78).

## Open questions

Transport selection; mTLS vs token-based agent credentials (#77).
