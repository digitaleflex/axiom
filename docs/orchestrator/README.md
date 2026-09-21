# Axiom Orchestrator

The Orchestrator is Axiom's coordination layer. It transforms an approved project objective into bounded tasks, routes those tasks to authorized expert agents, validates artifacts and quality gates, tracks state, and coordinates recovery or escalation.

## Responsibilities

- plan tasks and dependencies
- select compatible expert agents
- assemble minimum required context
- manage artifact handoffs
- enforce quality, security and approval gates
- persist authoritative orchestration state
- publish audit and observability events
- recover interrupted execution

## Explicit non-responsibilities

The Orchestrator does not own business rules, application implementation, infrastructure execution or the intelligence of individual expert agents. It coordinates those capabilities through contracts.

## Specifications

- [Architecture](./architecture.md)
- [Lifecycle](./lifecycle.md)
- [Task planning](./task-planning.md)
- [Routing](./routing.md)
- [Context](./context.md)
- [Artifacts](./artifacts.md)
- [Gates](./gates.md)
- [Recovery](./recovery.md)
- [Security](./security.md)
- [Observability](./observability.md)
- [Adapters](./adapters.md)
- [Reference workflow](./reference-workflow.md)
- [M3 completion gate](./m3-gate.md)

## Design principle

> The Orchestrator coordinates authority; it does not become the authority for every domain.

Every decision must remain attributable to the business expert, technical expert, policy engine, quality gate or human approval that owns it.