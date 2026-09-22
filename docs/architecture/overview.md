# Axiom Architecture Overview

Axiom is a deployment and cloud application hosting platform organized around a control plane, a deployment engine and bounded runtime agents.

## System architecture
Developer
↓
Public Web / Authentication / Onboarding
↓
Axiom Cloud Console
↓
Axiom Engine / Control Plane
├── Auth / Authorization
├── GitHub Integration
├── Repository Analyzer
├── Application Profile
├── Deployment Planner
├── Build Engine
├── Deployment Executor
├── PostgreSQL Persistence
├── Observability
└── Security / Governance
↓
Runtime Agent
├── Registration / Identity
├── Heartbeat / Capabilities
├── Authorized Dispatcher
├── Docker Runtime Adapter
├── Traefik Network Adapter
├── Health
├── Logs
└── Metrics
↓
VPS / Infrastructure
├── Docker
├── Traefik
└── Axiom-managed application runtimes

## Canonical deployment pipeline
GitHub → Repository/Ref → Repository Snapshot → Repository Analyzer → Application Profile → Deployment Plan → Build Engine → Deployment Executor → Runtime Agent → Docker Runtime → Traefik / Domain / TLS → Health Check → LIVE

## Control-plane rule
Cloud Console, CLI and future clients communicate only with the Engine API.
Clients never directly communicate with Docker, Traefik, PostgreSQL, Runtime Agents or deployment servers.

## Runtime boundary
The Runtime Agent executes only authorized operations represented by versioned contracts. It is not the orchestration brain and does not contain application business logic.

## Agent system
Axiom expert agents and coding-agent workflow are development and governance mechanisms. They are not the end-user product model.

## Future architecture
Axiom may add microVM isolation, richer service networking, additional infrastructure providers and advanced orchestration later. Such capabilities require explicit architecture and contract updates before becoming implementation requirements.