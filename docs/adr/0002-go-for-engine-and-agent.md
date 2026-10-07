# ADR-0002 — Go for Engine and Runtime Agent

- **Status:** Accepted
- **Date:** 2026-10-07
- **Issue:** #69

## Context

The Engine needs a reliable HTTP/SSE server and concurrency; the Agent must be a small, static binary on user servers with Docker integration.

## Decision

Implement the Engine (`services/engine`) and Runtime Agent (`services/agent`) in Go, one module per service, standard library first (`net/http`, `log/slog`), minimal dependencies (e.g. `pgx` for PostgreSQL).

## Consequences

- Single static binaries, simple distribution of the Agent.
- First-class Docker ecosystem libraries available.
- Frontend remains TypeScript (`apps/cloud`); contracts are language-neutral (JSON/YAML schemas).

## Alternatives considered

Node.js/TypeScript backend (weaker fit for a static agent binary); Rust (higher development cost for V0.1).
