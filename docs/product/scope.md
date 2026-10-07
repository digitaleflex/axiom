# Axiom Scope — Platform vs Domain Project

> Issue #25. Companion to [`vision.md`](vision.md). V0.1 limits: [`v0.1-scope.md`](v0.1-scope.md). Terms: [`glossary.md`](glossary.md).

## 1. What Axiom is (domain-independent)

Axiom is a **deployment platform**: it takes any supported application repository and turns it into a running, observable application on a server the user controls. Nothing in Axiom depends on what the deployed application does (shop, blog, API, SaaS). The deployed application is opaque business code; Axiom only understands how to build, run, route and verify it.

## 2. Responsibility Split

| Concern | Axiom Platform | Domain Project (the user's application) |
|---|---|---|
| Source code & business logic | never reads semantics; analyzes files only to detect stack | owns entirely |
| Stack detection, build, runtime, routing, TLS, health | owns (Engine + Runtime Agent) | may declare hints/overrides in `axiom.yaml` |
| Deployment state & history | owns (Engine is authoritative) | — |
| Application configuration values / secrets | stores and injects securely; never interprets | defines names and values |
| Business data, schemas, domain rules | never owns | owns |
| Product workflows & UX of the application | never owns | owns |
| Security & infrastructure constraints | owns; cannot be weakened by a domain project | can request, not override (see §4) |
| Observability of runtime (logs, health, resources) | collects and presents | emits logs/endpoints |
| Server / infrastructure | operates via Runtime Agent within bounded operations | provides the server (V0.1: user-owned VPS) |

**Replacement test:** replacing the deployed application with a completely different one must require no change to Axiom code, contracts or schemas — only a different repository and, optionally, a different `axiom.yaml`.

## 3. Canonical Terminology (summary)

Full definitions in [`glossary.md`](glossary.md).

| Term | Meaning in Axiom |
|---|---|
| **Platform** | Reusable Axiom foundation: Engine, Runtime Agent, Cloud Console, contracts |
| **Domain Project** | A concrete application deployed by Axiom; owns its business logic |
| **Orchestrator** | Coordination layer routing tasks between experts — in V0.1 the **Deployment Engine** is the canonical orchestration boundary |
| **Runtime** | Where the deployed application executes (V0.1: a Docker container or Compose services on a server) |
| **Agent** | Executable implementation of an expert role. Not to be confused with the **Runtime Agent** |
| **Runtime Agent** | Bounded Go process on a user server that executes Engine-authorized operations |
| **Expert** | Bounded specialist responsible for one decision or artifact (e.g. Stack Detector, Deployment Planner) |

## 4. Boundary Rules

1. Axiom must remain usable when the domain project is replaced by another project with different business requirements.
2. A domain project influences deployment only through **declared inputs** (repository content, `axiom.yaml`, configuration values, user choices in the Console) — never by altering platform behavior.
3. Security and infrastructure constraints (allowed operations, isolation, secret handling, health gating) are platform-owned and enforced by contracts, quality gates and, where designated, human approval.
4. The platform never executes repository code during analysis; code runs only inside the build and runtime boundaries defined by the deployment plan.

Detailed architectural boundaries: [`../architecture/boundaries.md`](../architecture/boundaries.md).

## 5. Non-goals

V0.1 non-goals are listed in [`v0.1-scope.md`](v0.1-scope.md#4-out-of-scope--non-goals).
