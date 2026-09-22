# Axiom Agent Operating System

Axiom uses specialized expert roles plus GitHub Copilot custom agents to execute bounded engineering work through issues and pull requests.

## Core documents

- [Roles](./roles.md) — responsibilities and ownership boundaries.
- [Capabilities](./capabilities.md) — bounded actions available to experts.
- [Context](./context.md) — context and memory boundaries.
- [Tools](./tools.md) — tool permission model.
- [Quality Gates](./quality-gates.md) — validation and approval model.
- [Expert Specifications](./specs/README.md) — reference expert contracts.
- [Lifecycle](./lifecycle.md) — DRAFT → DONE execution lifecycle.
- [Assignment](./assignment.md) — routing issues to the appropriate agent.
- [Handoff](./handoff.md) — required completion evidence.

## GitHub-native execution

Repository-level custom agents live under `.github/agents/`. GitHub Copilot cloud agent can use these profiles when starting a task or assigning an agent to an issue.

Axiom adds governance around that capability:

```
Architecture / Contracts
        ↓
GitHub Issue
        ↓
Agent Task Specification
        ↓
READY gate
        ↓
GitHub Custom Agent
        ↓
Owned files
        ↓
Pull Request
        ↓
Scope + Tests + Security
        ↓
Architecture Review
        ↓
Integration
        ↓
DONE
```

## Operating principle

The GitHub agent is the implementation executor. Axiom's architecture, issue contracts, ownership map, readiness validator, CI, and human review remain the governance layer.

Agents do not redefine global architecture implicitly.

Every meaningful handoff produces evidence. Sensitive operations remain subject to explicit authorization and security controls.
