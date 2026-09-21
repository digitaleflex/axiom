# Axiom Expert Agent Framework

The expert framework defines specialized responsibilities that collaborate through explicit contracts and artifacts.

## Core documents

- [Roles](./roles.md) — ownership and responsibility boundaries.
- [Capabilities](./capabilities.md) — bounded actions available to experts.
- [Context](./context.md) — context and memory boundaries.
- [Tools](./tools.md) — tool permission model.
- [Quality Gates](./quality-gates.md) — validation and approval model.
- [Expert Specifications](./specs/README.md) — reference expert contracts.

## Execution principle

Experts do not directly redefine the global workflow. The orchestrator selects an expert according to declared role, capability, dependencies, and quality gates.

Every meaningful handoff produces a typed artifact. Sensitive operations remain subject to authorization and, where required, human approval.

## v0.1 objective

The framework must be implementable without coupling expert definitions to a specific business project or AI provider.
