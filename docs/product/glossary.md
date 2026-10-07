# Axiom Glossary

> Issue #27. Canonical vocabulary for documentation, code, experts and humans. When a term has a code/API identifier, that identifier is canonical in code.

## 1. Platform & Framework Terms

| Term | Definition | Not to be confused with | Code / API |
|---|---|---|---|
| **Platform** | Reusable Axiom foundation (Engine, Runtime Agent, Cloud Console, contracts). | Domain Project | — |
| **Domain Project** | A concrete application deployed by Axiom; owns its business logic. | Domain (hostname) | — |
| **Expert** | Bounded specialist role responsible for one decision or artifact (e.g. Stack Detector, Deployment Planner). | Agent | `schemas/experts/*` |
| **Agent** | Executable implementation of an expert role. Always "expert agent" in prose when ambiguity is possible. | **Runtime Agent** | — |
| **Orchestrator** | Coordination layer routing tasks and artifacts between experts. In V0.1 the **Deployment Engine** is the canonical orchestration boundary. | Deployment Engine (concrete) | `services/orchestrator` |
| **Artifact** | Versioned, validated output exchanged between stages (analysis, profile, plan, image reference, result). | Build artifact (one artifact type) | `schemas/artifact.yaml` |
| **Contract** | Machine-readable agreement defining inputs, outputs and constraints. | — | `docs/architecture/contracts.md` |
| **Job** | Executable unit of work requested from the expert framework. Never used for user deployments. | Deployment | — |
| **Task** | Bounded piece of a job assigned to one expert. | Plan Step | — |
| **Capability** | Operation an expert or Runtime Agent is authorized and able to perform. | Server capability (Docker/Traefik availability) | — |
| **Runtime** | Where the deployed application executes. V0.1: a Docker container or Compose services on a server. | Runtime Agent | — |
| **Adapter** | Integration translating Axiom contracts to an external provider or tool (Docker, Traefik, GitHub). | — | — |
| **Provider** | External infrastructure, model or service consumed through an adapter. | — | — |
| **Project Manifest** | Optional `axiom.yaml` declaring deployment hints and overrides. | Application Profile (derived) | `axiom.yaml` |
| **Quality Gate** | Validation required before an artifact or task can advance. | Health Check | — |
| **Human Approval** | Explicit human authorization required for designated decisions or operations (e.g. Production deploy confirmation). | — | — |

## 2. Deployment Domain Terms

| Term | Definition | Not to be confused with | Code / API |
|---|---|---|---|
| **Workspace** | Account-level container for applications, servers and connections. Single implicit workspace in V0.1. | Domain Project | not in API v1 |
| **Application** | Deployable project derived from a repository, managed by Axiom. | Repository | `applicationId`, `/applications` |
| **Environment** | Isolated execution context of an application: `production`, `staging`, `preview`. | Runtime | `environment` (pending API field) |
| **Repository** | GitHub source repository accessible through a connection. | Application | `repositoryId` |
| **Ref** | Branch or tag resolved to an exact commit before analysis/build. | — | `ref` |
| **Analysis** | Read-only inspection of a repository snapshot; never executes code. | Build | `analysisId` |
| **Evidence** | Record explaining why a fact was detected (file, excerpt, effect). | — | analysis result |
| **Confidence** | Bounded score `[0,1]` of a detection; shown as bands in UI. | Health | `confidence` |
| **Application Profile** | Versioned, canonical description of how the application builds and runs (language, framework, commands, port, strategy). | Project Manifest (input) | `/applications/{id}/profile` |
| **Preset** | Explicit V0.1 build/runtime strategy a profile resolves to (Node/Next.js, generic Docker, Compose). | — | #96 |
| **Server** | User-provided machine registered with Axiom and operated by a Runtime Agent. | Node, Host | `serverId`, `/servers` |
| **Runtime Agent** | Bounded Go process on a server executing Engine-authorized operations only. | Agent (expert) | `services/agent` |
| **Engine** | Go control plane exposing `/api/v1`; authoritative owner of deployment state. Contains the **Deployment Engine**. | Orchestrator (generic) | `services/engine` |
| **Deployment Plan** | Immutable, deterministic, reviewable plan derived from profile + server + configuration. | Deployment | `planId`, `/deployment-plans` |
| **Plan Step** | One ordered step of a plan. | Task | `BUILD`, `CREATE_RUNTIME`, `NETWORK`, `START`, `VERIFY` |
| **Deployment** | One execution of a plan for an application in an environment. | Job | `deploymentId` (`dep_…`) |
| **Deployment Status** | Authoritative state of a deployment. | Step status | `PENDING`, `ANALYZING`, `PLANNING`, `BUILDING`, `DEPLOYING`, `VERIFYING`, `LIVE`, `FAILED` |
| **Health Check** | Verification that the running application responds; gates LIVE. | Quality Gate | `healthCheck`, `/deployments/{id}/health` |
| **LIVE** | Deployment status reached only after successful health verification. | "Running" | `LIVE` |
| **Domain** | Hostname routed to an application with TLS. | Domain Project | `/applications/{id}/domains` |

## 3. Deprecated / Avoid

| Avoid | Use instead | Reason |
|---|---|---|
| Node, Host (for a deployment target) | Server | single canonical name |
| Pod, Cluster, Namespace | — (not in V0.1) | not Kubernetes-first |
| Job / Pipeline (for a user deployment) | Deployment | Job belongs to expert framework |
| bare "agent" in UI or API | Runtime Agent (infra) / expert agent (framework) | naming collision |
| Container (as navigation/section label) | Runtime | implementation detail |
| Orchestrator state machine | Deployment Engine state machine | superseded (#40) |
| Software factory | — | non-goal |
