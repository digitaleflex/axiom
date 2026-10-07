# Axiom Core Contracts

> Issues #30 (expert contract), #31 (manifest), #32 (artifacts). Decision: [ADR-0007](../adr/0007-explicit-expert-contracts.md).

## 1. Purpose & Principles

Contracts are the boundary that lets the Engine, experts, Runtime Agent and Console evolve independently.

1. Explicit inputs and outputs.
2. Versioned contracts; breaking changes need a new version and migration path.
3. Traceable artifacts (repository, commit, deployment, execution).
4. Least authority between components.
5. Deterministic validation before execution; invalid inputs fail with actionable diagnostics.
6. Human approval for sensitive or irreversible operations.
7. No implicit transfer of business authority to technical components.
8. Secrets are references or managed values, never embedded in declarative contracts.

## 2. Contract Inventory

| Contract | Schema | Spec | Status |
|---|---|---|---|
| Deployment expert contract | [`schemas/expert-contract.schema.json`](../../schemas/expert-contract.schema.json) | §3 | v1, normative |
| Reference expert contracts | [`schemas/experts/*.yaml`](../../schemas/experts/) | §3.3 | v1, normative |
| Project manifest (`axiom.yaml`) | [`schemas/axiom.schema.json`](../../schemas/axiom.schema.json) | [`axiom-yaml.md`](axiom-yaml.md) | v1, normative |
| Deployment artifact envelope | [`schemas/artifact.schema.json`](../../schemas/artifact.schema.json) | [`artifacts.md`](artifacts.md) | v1, normative |
| REST API & SSE | — | [`api-contract.md`](api-contract.md) | v1 |
| Agent ↔ Engine protocol | — | `agent-protocol.md` (#75) | planned |
| Legacy framework agent contract | `schemas/agent-contract.schema.json`, `schemas/agent.yaml`, `schemas/task.yaml` | §6 | **legacy** — development/governance framework only, not for deployment experts |

Validation: `python3 tests/contracts/validate_schemas.py` (schemas + valid/invalid examples).

## 3. Deployment Expert Contract

### 3.1 Bounded experts

Experts are **bounded specialists**, each producing one kind of artifact. They are not a generic software-factory pipeline: they cannot write application code, cannot call each other, and cannot change deployment state. The Deployment Engine invokes them through an internal interface independent of any model provider (V0.1 experts are deterministic code).

### 3.2 Fields

| Field | Meaning |
|---|---|
| `expert` | identity: id, name, purpose, implementation owner path |
| `capabilities` | what the expert can produce/decide |
| `inputs` | artifact types consumed; only `valid` artifacts |
| `outputs` | artifact types produced + schema reference |
| `tools` | closed list of permitted tools (e.g. `repository.read`, `agent.dispatch`) |
| `forbidden_responsibilities` | explicit authority limits |
| `dependencies` | upstream experts whose artifacts are required |
| `quality_gates` | validation rules with failure action `reject` / `revise` / `escalate` |
| `escalation` | conditions routed to `engine`, `user`, `human-approval` or `security-expert` |
| `implementation` | `code` or `model-assisted`, determinism, `provider_coupling: none` |

### 3.3 Reference experts

| Expert | Produces | Consumes | Key forbidden responsibility | Owner |
|---|---|---|---|---|
| Repository Analyzer | RepositoryAnalysis | RepositorySnapshot, ProjectManifest | execute repository code | `internal/analyzer` |
| Stack Detector | ApplicationProfile | RepositoryAnalysis, ProjectManifest | present defaults as detected | `internal/profile` |
| Deployment Planner | DeploymentPlan | ApplicationProfile, ServerProfile, DeploymentConfiguration | execute steps; embed secrets | `internal/planner` |
| Build Expert | BuildArtifact | DeploymentPlan | deploy; leak secrets into images | `internal/build` |
| Runtime Expert | DeploymentResult, HealthResult | DeploymentPlan, BuildArtifact | shell commands; LIVE without health | `internal/executor` |
| Infrastructure Expert | ServerProfile | ServerProfile, DeploymentPlan | privileged host operations | `internal/server` |
| Security Expert | SecurityReport | DeploymentPlan, ProjectManifest | approve own exceptions | planned (#129) |

Dependency chain: Analyzer → Stack Detector → Planner → Build → Runtime; Infrastructure feeds Planner; Security evaluates plans before execution.

## 4. Authority Boundaries

| Actor | Owns | Never owns |
|---|---|---|
| Deployment Engine | state machine, sequencing, artifact validation, authorization, policy, approvals | business semantics |
| Experts | their artifact within declared tools | state transitions, other experts' artifacts, security exceptions |
| Runtime Agent | bounded execution of typed operations on one server | orchestration, business logic, shell |
| User / Domain project | repository, `axiom.yaml`, configuration values, approvals | platform constraints |

## 5. Handoff Lifecycle

```text
draft ──validate──► valid ──consumed by downstream stage
  │                  │
  └──► rejected      └──► superseded (newer revision/inputs)
          │
          └── resubmitted as new revision (draft)
```

Rejected artifacts identify the failed quality gate; downstream stages accept only `valid` artifacts (artifacts.md §5).

## 6. Legacy Framework Contracts

`schemas/agent-contract.schema.json`, `schemas/agent.yaml` and `schemas/task.yaml` describe the generic development/governance framework (roles such as architect, backend). They remain for `services/orchestrator` and the coding-agent workflow, but are superseded for deployment by §3 ([ADR-0010](../adr/0010-deployment-engine-supersedes-orchestrator.md)).

## 7. Code Alignment Notes

| Contract | Go code | Discrepancy |
|---|---|---|
| ApplicationProfile payload | `internal/profile.Profile` | aligned — golden profiles are validated against the schema by `tests/contracts/validate_schemas.py` (#95) |
| RepositoryAnalysis finding | `internal/analyzer/evidence.Finding` | aligned: states, candidates, structured evidence (#94) |
| DeploymentPlan payload | `internal/planner.Plan` | Go steps are `{Name, Order}` structs; no environment or fingerprint yet (#97) |
| Health result | API §16 | aligned |
