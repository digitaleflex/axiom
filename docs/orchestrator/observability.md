# Orchestrator Observability

Every orchestration execution is traceable through structured events.

## Required identifiers

- `project_id`
- `run_id`
- `task_id`
- `execution_id`
- `artifact_id` when applicable
- `correlation_id`
- `actor_id`

## Event categories

- run.created
- run.state_changed
- task.created
- task.dispatched
- task.started
- task.completed
- task.failed
- task.retrying
- routing.decided
- gate.evaluated
- approval.requested
- approval.decided
- artifact.published
- execution.reconciled
- escalation.created

## Telemetry rules

Events must distinguish decision, execution and outcome. Sensitive values and secret material must never be emitted. Metrics and traces should reference IDs rather than embedding confidential payloads.

The event stream must be sufficient to reconstruct the lifecycle of a run without relying on ephemeral process logs.