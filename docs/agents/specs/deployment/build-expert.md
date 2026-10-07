# Build Expert — Reference Specification

> Issue #39. Contract: [`schemas/experts/build-expert.yaml`](../../../../schemas/experts/build-expert.yaml) (conforms to `expert-contract.schema.json` v1). Implementation: `services/engine/internal/build`.

## Purpose

Build an application image from an exact commit in an isolated workspace.

## Inputs

| Artifact | Required |
|---|---|
| DeploymentPlan | yes |

## Outputs

| Artifact | Schema |
|---|---|
| BuildArtifact | `schemas/artifact.schema.json#BuildArtifact` |

## Capabilities

`build.image`, `build.metadata`

## Tools

`build.workspace`, `image.build`, `secret.reference` — see [tool catalog](../../deployment/tools.md).

## Context

Scopes: `plan`, `repository.snapshot`, `configuration.names`. Sensitive (references only): `secret.references`, `github.token`. TTL 3600s.

## Runtime limits

Max 1800s, 1 retries, network `registry`.

## Authority boundaries — forbidden

- deploy or start runtimes
- write secrets into image layers or logs
- build from an unresolved ref

## Dependencies

[deployment-planner](deployment-planner.md)

## Behavior

1. Create an ephemeral workspace; checkout the plan commit with a short-lived token.
2. Generate build context from preset (Dockerfile template or repository Dockerfile).
3. Build image with resource/time limits; inject build-time secrets by reference only.
4. Push/load image; record image reference + digest + duration.
5. Destroy workspace.

## Quality gates

| Gate | Rule | On failure |
|---|---|---|
| `commit-pinned` | build source is the plan commit SHA | reject |
| `image-referenced` | output includes image reference and digest | reject |

## Escalation

| When | To |
|---|---|
| build command fails | engine |

## Error mapping

Build command failure → `BUILD_FAILED` with exit code; timeout → `BUILD_FAILED` (timeout detail).

## Adapter requirements

Implements the Engine `Expert` interface (`docs/agents/deployment/README.md`). Must be deterministic for identical inputs, must not access tools outside the list above, and returns a `draft` artifact; validation is the Engine's responsibility.
