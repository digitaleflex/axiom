# Infrastructure Expert — Reference Specification

> Issue #39. Contract: [`schemas/experts/infrastructure-expert.yaml`](../../../../schemas/experts/infrastructure-expert.yaml) (conforms to `expert-contract.schema.json` v1). Implementation: `services/engine/internal/server`.

## Purpose

Assess server eligibility and configure routing, domains and TLS through the Runtime Agent.

## Inputs

| Artifact | Required |
|---|---|
| ServerProfile | yes |
| DeploymentPlan | no |

## Outputs

| Artifact | Schema |
|---|---|
| ServerProfile | `schemas/artifact.schema.json#ServerProfile` |

## Capabilities

`server.eligibility`, `network.configure`

## Tools

`server.read`, `agent.dispatch` — see [tool catalog](../../deployment/tools.md).

## Context

Scopes: `server.profile`, `plan`, `runtime.state`. Sensitive (references only): `credentials.agent`. TTL 600s.

## Runtime limits

Max 120s, 2 retries, network `agent`.

## Authority boundaries — forbidden

- expose host shell or privileged host operations
- manage resources not created by Axiom
- weaken TLS or network isolation

## Dependencies

none

## Behavior

1. Evaluate eligibility of each server for a profile/plan, listing all failing requirements.
2. For NETWORK: dispatch typed routing/TLS operations (hostname, target port) to the Agent.
3. Report degraded/offline state with reasons and last-seen.

## Quality gates

| Gate | Rule | On failure |
|---|---|---|
| `eligibility-reasons` | every non-eligible server lists all failing requirements | reject |

## Escalation

| When | To |
|---|---|
| server offline or degraded | user |

## Error mapping

Server offline → `DEPLOYMENT_NOT_ELIGIBLE`; routing failure → `RUNTIME_FAILED` (until a network error class exists).

## Adapter requirements

Implements the Engine `Expert` interface (`docs/agents/deployment/README.md`). Must be deterministic for identical inputs, must not access tools outside the list above, and returns a `draft` artifact; validation is the Engine's responsibility.
