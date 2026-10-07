# ADR-0003 — PostgreSQL as primary persistence

- **Status:** Accepted
- **Date:** 2026-10-07
- **Issue:** #70, #115

## Context

Deployments require transactional state transitions, idempotency records, event history and relational ownership.

## Decision

PostgreSQL (17 in development) is the single primary datastore of the Engine. Schema changes go through versioned migrations (`services/engine/migrations`) applied at startup. No secondary datastore in V0.1.

## Consequences

- Transactions protect state machine transitions and idempotency.
- Events/log metadata stored in PostgreSQL in V0.1; high-volume runtime logs may need a separate store later (new ADR).
- Integration tests run against a real PostgreSQL when `AXIOM_TEST_DATABASE_URL` is set.

## Alternatives considered

SQLite (no multi-instance), document stores (weaker transactional guarantees).
