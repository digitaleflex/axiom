# Architecture Decision Records

> Issue #33. ADRs document significant technical and architectural decisions and prevent undocumented drift.

## Process

1. Copy [`0000-template.md`](0000-template.md) to `NNNN-short-title.md` (next free number).
2. Open a PR with status **Proposed**, linked to an issue.
3. On review: **Accepted** or **Rejected** (rejected ADRs are kept).
4. A later ADR may **Supersede** an accepted one; the old ADR's status becomes `Superseded by ADR-NNNN` and stays in the repository.
5. **Deprecated**: decision no longer relevant, without replacement.

A change is "significant" when it alters a component boundary, a runtime technology, a persistence model, a security boundary, a contract, or a dependency direction.

## Required sections

Context · Decision · Consequences · Alternatives considered (when relevant) · Status · Date · Supersedes / Superseded by.

## Index

| ADR | Title | Status |
|---|---|---|
| [0001](0001-platform-domain-separation.md) | Platform / domain separation | Accepted |
| [0002](0002-go-for-engine-and-agent.md) | Go for Engine and Runtime Agent | Accepted |
| [0003](0003-postgresql-persistence.md) | PostgreSQL as primary persistence | Accepted |
| [0004](0004-docker-and-compose-runtime.md) | Docker and Docker Compose before Kubernetes | Accepted |
| [0005](0005-traefik-reverse-proxy.md) | Traefik as initial reverse proxy | Accepted |
| [0006](0006-observability-standards.md) | Observability standards | Accepted |
| [0007](0007-explicit-expert-contracts.md) | Explicit contracts between experts and the Engine | Accepted |
| [0008](0008-agent-engine-communication.md) | Agent ↔ Engine communication model | Proposed |
| [0009](0009-sse-for-realtime.md) | SSE for realtime Console updates | Accepted |
| [0010](0010-deployment-engine-supersedes-orchestrator.md) | Deployment Engine supersedes generic orchestrator state machine | Accepted |

Note: the initial plan named "OpenTelemetry-based observability" for ADR-0006. The accepted ADR-0006 records structured logs + Prometheus-compatible metrics for V0.1, with OpenTelemetry tracing deferred (see ADR text).
