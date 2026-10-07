# Repository Analyzer — Reference Specification

> Issue #39. Contract: [`schemas/experts/repository-analyzer.yaml`](../../../../schemas/experts/repository-analyzer.yaml) (conforms to `expert-contract.schema.json` v1). Implementation: `services/engine/internal/analyzer`.

## Purpose

Inspect a read-only repository snapshot and record findings with evidence and confidence.

## Inputs

| Artifact | Required |
|---|---|
| RepositorySnapshot | yes |
| ProjectManifest | no |

## Outputs

| Artifact | Schema |
|---|---|
| RepositoryAnalysis | `schemas/artifact.schema.json#RepositoryAnalysis` |

## Capabilities

`analysis.findings`, `analysis.evidence`

## Tools

`repository.read` — see [tool catalog](../../deployment/tools.md).

## Context

Scopes: `repository.snapshot`, `repository.metadata`, `manifest`. TTL 900s.

## Runtime limits

Max 120s, 1 retries, network `none`.

## Authority boundaries — forbidden

- execute repository code or scripts
- choose deployment strategy or server
- modify repository content
- read secret values

## Dependencies

none

## Behavior

1. Receive a pinned snapshot (commit SHA) — never a moving ref.
2. Parse `axiom.yaml` if present (validated first; invalid manifest → finding with `MANIFEST_*` code, stop).
3. Run detectors over files: language, framework, package manager, lockfiles, Dockerfile, Compose, scripts, ports.
4. Record one finding per kind with state (`detected`, `ambiguous`, `not_detected`, `unsupported`, `not_applicable`), confidence and evidence records (source, path, lines, effect, rule).
5. Never execute scripts, install dependencies or follow symlinks outside the snapshot.

## Quality gates

| Gate | Rule | On failure |
|---|---|---|
| `evidence-required` | every finding has at least one evidence record | reject |
| `confidence-bounded` | confidence is within [0,1] | reject |
| `conflicts-explicit` | conflicting evidence produces an ambiguous finding, never a silent choice | revise |

## Escalation

| When | To |
|---|---|
| snapshot unreadable or ref missing | engine |

## Error mapping

Snapshot unreadable / ref missing → stage fails `INVALID_REQUEST`/`NOT_FOUND`; oversized repository → `VALIDATION_FAILED` with limit.

## Adapter requirements

Implements the Engine `Expert` interface (`docs/agents/deployment/README.md`). Must be deterministic for identical inputs, must not access tools outside the list above, and returns a `draft` artifact; validation is the Engine's responsibility.
