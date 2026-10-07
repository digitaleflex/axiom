# Deployment Expert Context Boundaries

> Issue #36. Declared per expert in `context` of `schemas/experts/<id>.yaml`.

## 1. Context scopes

| Scope | Content | Contains secrets? |
|---|---|---|
| `repository.snapshot` | files of the pinned commit (read-only) | no (repository content may contain committed secrets: treated as untrusted, never logged) |
| `repository.metadata` | repository, ref, commit, default branch | no |
| `manifest` | parsed, validated `axiom.yaml` | no (values forbidden by schema) |
| `analysis` | valid RepositoryAnalysis | no |
| `profile` | valid ApplicationProfile | no |
| `configuration.names` | configuration variable names, required/secret flags | no |
| `server.profile` | capabilities, resources, liveness | no |
| `plan` | valid DeploymentPlan | no |
| `build.metadata` | image reference, digest | no |
| `runtime.state` | runtime status from Agent | no |
| `health` | health results | no |
| `events` | deployment events | no (redacted) |
| `policy` | applicable security policy | no |

## 2. Sensitive context

| Sensitive item | Access rule |
|---|---|
| `secret.references` | references only; values injected by the execution boundary (build workspace / Agent), never visible to the expert |
| `github.token` | short-lived, repository-scoped; only Build Expert checkout and Engine GitHub integration |
| `credentials.agent` | never passed to experts as values; Engine signs dispatched operations |

## 3. Matrix

| Expert | Scopes | Sensitive |
|---|---|---|
| Repository Analyzer | snapshot, metadata, manifest | — |
| Stack Detector | analysis, manifest | — |
| Deployment Planner | profile, server.profile, configuration.names, policy | — |
| Build Expert | plan, snapshot, configuration.names | secret.references, github.token |
| Runtime Expert | plan, build.metadata, runtime.state, health, server.profile | secret.references, credentials.agent |
| Infrastructure Expert | server.profile, plan, runtime.state | credentials.agent |
| Security Expert | plan, manifest, configuration.names, policy, events | — |

## 4. Versioning & expiration

- Context is assembled from artifact IDs (immutable) plus a snapshot timestamp.
- `ttl_seconds` bounds how long an assembled context is valid; expired context must be rebuilt (e.g. stale server liveness).

## 5. Reconstructibility

Each invocation records: expert ID, contract version, config version, input artifact IDs, context scopes, context timestamp, request/correlation ID, output artifact ID and gate results. This record (without secret values) allows replaying or auditing any decision (#128).
