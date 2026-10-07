# Security Expert — Reference Specification

> Issue #39. Contract: [`schemas/experts/security-expert.yaml`](../../../../schemas/experts/security-expert.yaml) (conforms to `expert-contract.schema.json` v1). Implementation: `services/engine (policy, planned`.

## Purpose

Evaluate plans and operations against security policy and produce a security report.

## Inputs

| Artifact | Required |
|---|---|
| DeploymentPlan | yes |
| ProjectManifest | no |

## Outputs

| Artifact | Schema |
|---|---|
| SecurityReport | `schemas/artifact.schema.json#SecurityReport` |

## Capabilities

`policy.evaluate`, `secrets.audit`

## Tools

`policy.evaluate` — see [tool catalog](../../deployment/tools.md).

## Context

Scopes: `plan`, `manifest`, `configuration.names`, `policy`, `events`. TTL 600s.

## Runtime limits

Max 30s, 0 retries, network `none`.

## Authority boundaries — forbidden

- approve its own policy exceptions
- read secret values
- modify plans

## Dependencies

[deployment-planner](deployment-planner.md)

## Behavior

1. Evaluate the plan and manifest against policy: no secret values, allowed strategies, allowed ports, domain ownership, image source.
2. Produce SecurityReport with decision allow/deny and findings.
3. Exceptions are escalated to human approval; the expert never approves them.

## Quality gates

| Gate | Rule | On failure |
|---|---|---|
| `no-secret-leak` | plan and manifest contain no secret values | reject |
| `policy-pass` | policy decision is allow or deny with reason | escalate |

## Escalation

| When | To |
|---|---|
| policy exception requested | human-approval |

## Error mapping

Deny → `POLICY_DENIED`.

## Adapter requirements

Implements the Engine `Expert` interface (`docs/agents/deployment/README.md`). Must be deterministic for identical inputs, must not access tools outside the list above, and returns a `draft` artifact; validation is the Engine's responsibility.
