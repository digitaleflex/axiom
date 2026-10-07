# Runtime Expert — Reference Specification

> Issue #39. Contract: [`schemas/experts/runtime-expert.yaml`](../../../../schemas/experts/runtime-expert.yaml) (conforms to `expert-contract.schema.json` v1). Implementation: `services/engine/internal/executor`.

## Purpose

Translate plan runtime steps into bounded Runtime Agent operations and verify health.

## Inputs

| Artifact | Required |
|---|---|
| DeploymentPlan | yes |
| BuildArtifact | yes |

## Outputs

| Artifact | Schema |
|---|---|
| DeploymentResult | `schemas/artifact.schema.json#DeploymentResult` |
| HealthResult | `schemas/artifact.schema.json#HealthResult` |

## Capabilities

`runtime.create`, `runtime.start`, `runtime.verify`

## Tools

`agent.dispatch`, `secret.reference` — see [tool catalog](../../deployment/tools.md).

## Context

Scopes: `plan`, `build.metadata`, `runtime.state`, `health`, `server.profile`. Sensitive (references only): `secret.references`, `credentials.agent`. TTL 3600s.

## Runtime limits

Max 900s, 2 retries, network `agent`.

## Authority boundaries — forbidden

- send shell commands to the Runtime Agent
- mark a deployment LIVE without a passing health result
- change deployment state directly

## Dependencies

[build-expert](build-expert.md)

## Behavior

1. For CREATE_RUNTIME/START: dispatch typed operations to the Runtime Agent with image digest, port, configuration references.
2. For VERIFY: request health probes until success or retries exhausted.
3. Produce HealthResult and DeploymentResult (LIVE only if HEALTHY).
4. On failure keep previous LIVE runtime serving per plan rollback strategy.

## Quality gates

| Gate | Rule | On failure |
|---|---|---|
| `typed-operations` | every dispatched operation has an explicit operation type | reject |
| `health-gates-live` | LIVE requires HealthResult status HEALTHY | reject |

## Escalation

| When | To |
|---|---|
| health verification fails | engine |

## Error mapping

Start failure → `RUNTIME_FAILED`; probe failure → `HEALTH_CHECK_FAILED`.

## Adapter requirements

Implements the Engine `Expert` interface (`docs/agents/deployment/README.md`). Must be deterministic for identical inputs, must not access tools outside the list above, and returns a `draft` artifact; validation is the Engine's responsibility.
