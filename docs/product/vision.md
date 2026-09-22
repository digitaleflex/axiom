# Axiom Vision

## Product definition
Axiom is a developer-first deployment and cloud application hosting platform that transforms a GitHub repository into a running, observable application on infrastructure chosen by the user.

> **Axiom transforms a GitHub repository into a deployed application.**

Axiom combines a low-friction developer experience with explicit operational transparency. It hides unnecessary infrastructure complexity by default while keeping deployment decisions, runtime state, logs, health and infrastructure context inspectable when needed.

## Core deployment pipeline

GitHub → Repository → Repository Analysis → Stack Detection → Application Profile → Deployment Plan → Build → Runtime → Networking → Domain / SSL → Health Check → LIVE

## Target architecture

Developer → Axiom Cloud Console → Axiom Engine / Control Plane → Runtime Agent → VPS / Infrastructure

The Engine contains GitHub Integration, Repository Analyzer, Application Profile, Deployment Planner, Build Engine, Deployment Executor, Security/Governance and Observability.
The Runtime Agent contains bounded Docker runtime, Traefik networking, health, logs and metrics capabilities.

## Core principles
1. Git-native deployment.
2. Progressive disclosure: simple defaults first, technical detail on demand.
3. Operational transparency: deployment states, steps, logs and health remain observable.
4. Bounded automation: Engine and Runtime Agent execute explicit contracts, not arbitrary commands.
5. Infrastructure abstraction without infrastructure blindness.
6. Deterministic deployment planning.
7. Security by boundary.

## Axiom product surfaces
- Public website and acquisition
- Authentication
- Onboarding
- Cloud Console
- Application operations
- Infrastructure/server operations
- Workspace/account management
- Billing and commercial surfaces
- System/error states

Billing and advanced commercial capabilities are product surfaces, but remain outside the V0.1 technical deployment contract.

## Future vision
Axiom may later support additional runtime isolation technologies, broader infrastructure providers, advanced networking, richer observability, previews, rollbacks and commercial capabilities. These are future capabilities unless added to a versioned contract.

## Non-goals for V0.1
- Generic autonomous software factory
- Kubernetes-first platform
- Unrestricted remote shell
- Arbitrary Docker control exposed to clients
- Multi-cloud orchestrator
- Agent marketplace

The internal agent system supports bounded platform responsibilities; it is not the primary end-user product model.