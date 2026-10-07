# Deployment Planner — Reference Specification

> Issue #39. Contract: [`schemas/experts/deployment-planner.yaml`](../../../../schemas/experts/deployment-planner.yaml) (conforms to `expert-contract.schema.json` v1). Implementation: `services/engine/internal/planner`.

## Purpose

Produce a deterministic, reviewable Deployment Plan from profile, server and configuration.

## Inputs

| Artifact | Required |
|---|---|
| ApplicationProfile | yes |
| ServerProfile | yes |
| DeploymentConfiguration | yes |

## Outputs

| Artifact | Schema |
|---|---|
| DeploymentPlan | `schemas/artifact.schema.json#DeploymentPlan` |

## Capabilities

`plan.generate`, `plan.fingerprint`

## Tools

`profile.read`, `server.read`, `plan.validate` — see [tool catalog](../../deployment/tools.md).

## Context

Scopes: `profile`, `server.profile`, `configuration.names`, `policy`. TTL 900s.

## Runtime limits

Max 30s, 0 retries, network `none`.

## Authority boundaries — forbidden

- execute any step
- select a server that is not eligible
- embed secret values in the plan

## Dependencies

[stack-detector](stack-detector.md)

## Behavior

1. Consume valid ApplicationProfile, ServerProfile and DeploymentConfiguration.
2. Check server eligibility requirements (capabilities, resources, architecture).
3. Emit canonical steps in order: BUILD, CREATE_RUNTIME, NETWORK, START, VERIFY (subset only when a step is not applicable, e.g. no domain).
4. Fill build/runtime/network/health/rollback sections from preset + profile.
5. Compute fingerprint = sha256(canonical JSON of inputs + plan).

## Quality gates

| Gate | Rule | On failure |
|---|---|---|
| `deterministic` | identical inputs produce an identical fingerprint | reject |
| `steps-canonical` | steps are a subset of BUILD, CREATE_RUNTIME, NETWORK, START, VERIFY in canonical order without duplicates | reject |
| `capabilities-satisfied` | server capabilities satisfy every step | reject |

## Escalation

| When | To |
|---|---|
| no eligible server | user |

## Error mapping

Not eligible → `DEPLOYMENT_NOT_ELIGIBLE`; invalid port/domain → `VALIDATION_FAILED`.

## Adapter requirements

Implements the Engine `Expert` interface (`docs/agents/deployment/README.md`). Must be deterministic for identical inputs, must not access tools outside the list above, and returns a `draft` artifact; validation is the Engine's responsibility.
