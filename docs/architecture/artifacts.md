# Deployment Artifacts & Handoff Protocol v1

> Issue #32. Schema: [`schemas/artifact.schema.json`](../../schemas/artifact.schema.json). Example: [`schemas/artifact.yaml`](../../schemas/artifact.yaml).

## 1. Purpose

Every Deployment Engine stage consumes and produces explicit, versioned, validated artifacts so that each result is traceable to its repository commit, deployment and execution, and invalid results cannot flow downstream.

## 2. Envelope

| Field | Meaning |
|---|---|
| `id` | opaque artifact ID (`art_…`) |
| `type` | artifact type (§3) |
| `schemaVersion` | payload schema version |
| `revision` | resubmission counter |
| `producer` / `consumers` | expert IDs (`schemas/experts`), `engine` or `user` |
| `state` | `draft`, `valid`, `rejected`, `superseded` |
| `rejection` | `gate` + `reason` (required when rejected) |
| `supersededBy` | replacing artifact (required when superseded) |
| `fingerprint` | `sha256:` over canonical payload + input fingerprints (determinism) |
| `trace` | `applicationId`, `repositoryId`, `ref`, `commit`, `deploymentId`, `planId`, `executionId`, `requestId` |
| `dependsOn` | input artifact IDs |
| `payload` | type-specific content |

## 3. Types & Stage Matrix

| Stage | Artifact | Producer | Consumers | API exposure |
|---|---|---|---|---|
| Snapshot | RepositorySnapshot | engine (GitHub integration) | repository-analyzer | — |
| Manifest | ProjectManifest | engine (parsed `axiom.yaml`) | analyzer, stack-detector, security | via analysis |
| Analysis | RepositoryAnalysis | repository-analyzer | stack-detector | `GET /applications/{id}/analysis/{analysisId}` |
| Profile | ApplicationProfile | stack-detector | deployment-planner | `GET /applications/{id}/profile` |
| Server | ServerProfile | infrastructure-expert | deployment-planner | `GET /servers/{id}` |
| Configuration | DeploymentConfiguration | user (Console) | deployment-planner | plan request |
| Plan | DeploymentPlan | deployment-planner | security-expert, build-expert, runtime-expert | `GET /deployment-plans/{id}` |
| Security | SecurityReport | security-expert | engine (gate) | — |
| Build | BuildArtifact (image reference + digest) | build-expert | runtime-expert | deployment detail |
| Health | HealthResult | runtime-expert | engine (LIVE gate) | `GET /deployments/{id}/health` |
| Result | DeploymentResult | runtime-expert | engine, Console | `GET /deployments/{id}` |

## 4. Validation

1. Envelope validated against `artifact.schema.json`; payload against its `$defs` type.
2. Producer quality gates from its expert contract.
3. Engine cross-checks: `trace.commit` equals the commit of every `dependsOn` artifact; consumers listed in the producer contract.
4. Result: `valid`, or `rejected` with the failing gate.

## 5. Rules

| # | Rule |
|---|---|
| R1 | Only `valid` artifacts satisfy a downstream input requirement. `draft`, `rejected` and `superseded` never do. |
| R2 | Artifacts are immutable once `valid`; changes create a new artifact (new revision or new inputs) and mark the old one `superseded`. |
| R3 | A DeploymentPlan is single-use: one deployment per plan. |
| R4 | LIVE requires a `valid` HealthResult with `status: HEALTHY` for the deployment. |
| R5 | Identical inputs produce identical fingerprints (plans: #97). |
| R6 | Payloads never contain secret values — only names/references. |
| R7 | Every artifact traces to `applicationId`, `repositoryId` and an exact `commit`. |

## 6. Rejection & Revision

```text
draft → (gates) → valid
draft → (gate fails) → rejected {gate, reason}
rejected → producer resubmits → new artifact, revision+1, draft
valid → newer inputs → superseded {supersededBy}
```

`on_failure: escalate` gates route to the escalation target in the producer contract (user, human approval, security expert).

## 7. Persistence

V0.1 stores artifacts in PostgreSQL ([ADR-0003](../adr/0003-postgresql-persistence.md)) keyed by `id`, indexed by `trace.applicationId`, `trace.deploymentId` and `fingerprint` (#116).
