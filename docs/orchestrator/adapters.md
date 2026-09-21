# Orchestrator Adapters

The core Orchestrator depends on interfaces, not concrete providers.

## Adapter boundaries

- `AgentExecutor`: dispatches a task to an expert agent.
- `StateStore`: persists orchestration state.
- `ArtifactStore`: stores and retrieves immutable artifact versions.
- `EventPublisher`: publishes structured events.
- `ApprovalProvider`: resolves human approval requests.
- `PolicyEngine`: evaluates authorization decisions.
- `Clock`: provides time for deterministic tests.
- `IDGenerator`: provides identifiers for deterministic tests.

## Rules

Provider-specific SDKs, LLM clients, database drivers and infrastructure APIs belong behind adapters. The orchestration core must remain testable with in-memory implementations.

Adapter failures are represented as explicit domain outcomes. They must not silently mutate task state.