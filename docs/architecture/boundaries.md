# Platform / Domain Boundaries

> Issue #28. Product-level split: [`../product/scope.md`](../product/scope.md). Components: [`overview.md`](overview.md). Decisions: [ADR-0001](../adr/0001-platform-domain-separation.md).

## 1. Domain Logic vs Platform Services

| Platform services (Axiom owns) | Domain logic (deployed application owns) |
|---|---|
| GitHub connection, repository discovery, snapshot | business features, data model, domain rules |
| analysis, profile, presets, planning | how the app behaves for its users |
| build, runtime, routing, TLS, health gating | application endpoints and responses |
| deployment state, events, logs collection | log content and semantics |
| secret storage & injection | secret meaning and values |

Rule: no Axiom package may import, parse or branch on the business semantics of a deployed application. Analysis reads files only to detect **how** to build and run.

## 2. Product Requirements vs Technical Architecture

| Product requirements | Technical architecture |
|---|---|
| `docs/product/**` — what the user can do, scope, non-goals | `docs/architecture/**`, `docs/adr/**` — how it is built |
| `docs/design/**` — UX and screen contracts | `docs/architecture/api-contract.md` — data and states |
| owned by product/design issues | owned by architecture issues and ADRs |

Rule: design consumes API contracts and may request changes through issues; it never redefines states, endpoints or capabilities. Architecture may not add user-visible behavior that the product scope excludes.

## 3. Infrastructure vs Application Runtime

| Infrastructure (platform) | Application runtime (domain project) |
|---|---|
| server, Docker daemon, Traefik, Runtime Agent | container(s) running the application image |
| network entrypoints, certificates | application port, health endpoint |
| resource limits, isolation, restart policy | process behavior inside limits |

Rule: infrastructure components configure **around** the application (routing, limits, health probes); they never modify application code or data.

## 4. Experts / Agents vs Orchestration (Deployment Engine)

| Deployment Engine (orchestration boundary) | Experts (bounded specialists) | Runtime Agent (bounded execution) |
|---|---|---|
| owns deployment state machine and transitions | produce one artifact type each (analysis, profile, plan, …) | executes Engine-authorized operation types only |
| sequences stages, validates artifacts at quality gates | cannot change state or call other experts directly | rejects unknown operation types; no shell |
| authorizes operations, enforces policy and human approval | no infrastructure access beyond declared tools | reports results; never decides next step |
| persists artifacts, events, idempotency | deterministic code first; provider-agnostic | holds no business or orchestration logic |

The generic Orchestrator (`services/orchestrator`) is superseded for deployments by the Deployment Engine ([ADR-0010](../adr/0010-deployment-engine-supersedes-orchestrator.md)).

## 5. Shared vs Project-Specific Packages

| Location | Kind | Rule |
|---|---|---|
| `schemas/**` | shared contracts | versioned; consumed by all components; no component-specific logic |
| `docs/architecture/api-contract.md` | shared contract | single API source of truth |
| `services/engine/internal/**` | Engine-private | not importable by other modules (`internal`) |
| `services/agent/internal/**` | Agent-private | communicates with Engine only via protocol (#75) |
| `services/orchestrator/internal/**` | framework-private | does not own deployment state |
| `apps/cloud/**` | Console | talks only to Engine `/api/v1` |
| user repository + `axiom.yaml` | project-specific | input only; never stored as platform code |

Each Go module is independent (`go.mod` per service); cross-service sharing happens through contracts, not imports.

## 6. Enforceable Rules

| # | Rule | Enforcement mechanism |
|---|---|---|
| B1 | A domain project can be replaced without redesigning Axiom core | all project input flows through repository snapshot, `axiom.yaml`, configuration values; replacement test in reference qualification (#107) |
| B2 | Infrastructure components cannot own business rules | Runtime Agent operation set is closed (#80); adapters accept only typed operations |
| B3 | Experts cannot silently change security/infrastructure constraints | expert contracts declare forbidden responsibilities; Engine quality gates reject artifacts violating policy; security-relevant changes need human approval (`docs/architecture/contracts.md`) |
| B4 | Clients never reach infrastructure directly | Console has no credentials for Docker/Traefik/DB/Agent; network policy (#88) |
| B5 | Engine is the only writer of deployment state | state transitions validated in `services/engine/internal/deployment` (invalid → conflict) |
| B6 | Repository code is never executed during analysis | analyzer reads files only; build runs in isolated workspace (#98) |
