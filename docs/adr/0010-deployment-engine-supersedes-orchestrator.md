# ADR-0010 — Deployment Engine supersedes the generic orchestrator state machine

- **Status:** Accepted
- **Date:** 2026-10-07
- **Issue:** #40, #16

## Context

An early generic Orchestrator state machine (`services/orchestrator`, M3.x framework issues) modeled jobs/tasks between experts. Axiom's product is deployments, whose lifecycle is defined by the API contract (§12).

## Decision

The Deployment Engine inside `services/engine` is the canonical orchestration boundary and sole owner of deployment state (`PENDING … LIVE/FAILED`). The generic orchestrator remains a development/governance framework and must not own deployment state.

## Consequences

- One deployment state machine, enforced in `services/engine/internal/deployment`.
- Glossary marks "Orchestrator state machine" as deprecated for deployments.

## Supersedes

The orchestrator state machine design archived in #40.
