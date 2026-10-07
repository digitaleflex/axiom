# ADR-0001 — Platform / domain separation

- **Status:** Accepted
- **Date:** 2026-10-07
- **Issue:** #28, #25

## Context

Axiom deploys arbitrary applications. Coupling platform code to any deployed application's business would make Axiom non-reusable and unsafe.

## Decision

Axiom (Engine, Runtime Agent, Console, contracts) owns build, runtime, routing, health, deployment state, secrets handling and security constraints. Deployed applications ("domain projects") own all business logic and influence deployment only through declared inputs: repository content, optional `axiom.yaml`, configuration values and user choices.

## Consequences

- Any supported repository can be swapped without platform changes (replacement test).
- Analysis must stay semantic-free (file-based detection only).
- Security constraints cannot be overridden by project input.

## Alternatives considered

Project-specific deployment logic in the Engine — rejected: not reusable, unbounded.
