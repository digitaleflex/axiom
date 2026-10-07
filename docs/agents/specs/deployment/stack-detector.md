# Stack Detector — Reference Specification

> Issue #39. Contract: [`schemas/experts/stack-detector.yaml`](../../../../schemas/experts/stack-detector.yaml) (conforms to `expert-contract.schema.json` v1). Implementation: `services/engine/internal/profile`.

## Purpose

Turn analysis findings and manifest into a canonical Application Profile resolved to a V0.1 preset.

## Inputs

| Artifact | Required |
|---|---|
| RepositoryAnalysis | yes |
| ProjectManifest | no |

## Outputs

| Artifact | Schema |
|---|---|
| ApplicationProfile | `schemas/artifact.schema.json#ApplicationProfile` |

## Capabilities

`profile.build`, `preset.resolve`

## Tools

`preset.resolve` — see [tool catalog](../../deployment/tools.md).

## Context

Scopes: `analysis`, `manifest`. TTL 900s.

## Runtime limits

Max 30s, 1 retries, network `none`.

## Authority boundaries — forbidden

- present defaults as detected values
- mark unsupported stacks deployable
- infer unsafe commands

## Dependencies

[repository-analyzer](repository-analyzer.md)

## Behavior

1. Consume a valid RepositoryAnalysis and optional manifest.
2. Apply precedence Override › Manifest › Detected › Default per field, recording provenance.
3. Resolve to a V0.1 preset (#96): nextjs, vite, node, go, dockerfile, compose.
4. Mark blocking facts (ambiguous, low confidence, not detected required) for user resolution.
5. Unsupported stack → profile with `unsupported` reason; never deployable.

## Quality gates

| Gate | Rule | On failure |
|---|---|---|
| `provenance` | every profile value carries provenance (override, manifest, detected, default) | reject |
| `preset-known` | profile resolves to a known V0.1 preset or is marked unsupported with reason | reject |

## Escalation

| When | To |
|---|---|
| ambiguous or low-confidence required fact | user |

## Error mapping

Unsupported → profile flagged, planner refuses; ambiguous → escalate to user.

## Adapter requirements

Implements the Engine `Expert` interface (`docs/agents/deployment/README.md`). Must be deterministic for identical inputs, must not access tools outside the list above, and returns a `draft` artifact; validation is the Engine's responsibility.
