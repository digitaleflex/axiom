# ADR-0004 — Docker and Docker Compose before Kubernetes

- **Status:** Accepted
- **Date:** 2026-10-07
- **Issue:** #83, #121–#123

## Context

V0.1 targets one user-owned server and must stay operable without cluster knowledge.

## Decision

The V0.1 application runtime is a Docker container (single-service presets) or Docker Compose services (multi-service preset) on one server, operated by the Runtime Agent's Docker adapter. Kubernetes is out of scope.

## Consequences

- Simple operational model, matches reference VPS.
- No horizontal scaling across servers in V0.1.
- UI and contracts avoid Kubernetes vocabulary.

## Alternatives considered

Kubernetes/k3s (premature complexity), bare processes/systemd (weak isolation and packaging).
