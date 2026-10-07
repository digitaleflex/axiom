# Deployment Expert Roles & Responsibility Matrix

> Issue #34. Contracts: `schemas/experts/*.yaml`.

## 1. Core roles — one primary responsibility each

| Expert | Primary responsibility | Artifact | Decides | Never decides |
|---|---|---|---|---|
| Repository Analyzer | detect facts from files with evidence | RepositoryAnalysis | what the repository contains | how to deploy |
| Stack Detector | canonical profile + preset | ApplicationProfile | build/run description | server, execution |
| Deployment Planner | deterministic plan | DeploymentPlan | step sequence and parameters | execution, server eligibility override |
| Build Expert | image from pinned commit | BuildArtifact | build execution within preset | runtime, routing |
| Runtime Expert | runtime lifecycle via Agent + health | DeploymentResult, HealthResult | operation dispatch for plan steps | state transitions, LIVE without health |
| Infrastructure Expert | server eligibility, routing/TLS | ServerProfile | eligibility, network config | host administration |
| Security Expert | policy evaluation | SecurityReport | allow/deny with reasons | its own exceptions |

## 2. RACI per deployment stage

R = responsible, A = accountable, C = consulted. The **Deployment Engine is accountable for every stage**.

| Stage | Analyzer | Stack | Planner | Build | Runtime | Infra | Security | Engine | User |
|---|---|---|---|---|---|---|---|---|---|
| Analyze | R | | | | | | | A | |
| Profile | C | R | | | | | | A | C (overrides) |
| Eligibility | | | C | | | R | | A | |
| Plan | | C | R | | | C | C | A | C (review) |
| Policy | | | | | | | R | A | approval if escalated |
| Build | | | | R | | | | A | |
| Runtime/Network/Verify | | | | | R | R (network) | | A | |
| LIVE transition | | | | | C | | | R/A | |

## 3. Authority limits

- **No expert owns orchestration**: only the Engine sequences stages and writes deployment state.
- **No expert owns unrestricted infrastructure authority**: infrastructure access is only `agent.dispatch` with typed operations; the Runtime Agent re-validates authorization.
- **Constraints independent of model output**: quality gates, tool catalog, policy and the Agent's closed operation set are enforced by code. A model-assisted expert's output is just a draft artifact subject to the same gates.

## 4. Advanced / supporting roles

Architecture, Database, QA, DevOps, Observability and Documentation remain **engineering roles** ([`../roles.md`](../roles.md)). They may become supporting deployment experts only through a new contract in `schemas/experts/` and an ADR; they are not part of the V0.1 runtime pipeline.
