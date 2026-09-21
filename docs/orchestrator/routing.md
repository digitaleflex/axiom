# Agent Routing

Routing selects an expert agent that can legally and technically execute a task.

## Matching criteria

A candidate must satisfy all of:

- required role
- required capabilities
- compatible agent contract version
- required tool permissions
- required context access
- policy constraints
- runtime availability

## Decision sequence

```text
Task
  ↓
Required role/capabilities
  ↓
Registered agents
  ↓
Contract compatibility
  ↓
Permission/policy check
  ↓
Context compatibility
  ↓
Availability/concurrency
  ↓
Authorized dispatch
```

No scoring mechanism may compensate for a hard policy violation. An agent with a higher capability match is still ineligible if it lacks required authorization.

## Rejection

If no eligible agent exists, the task becomes BLOCKED or ESCALATED with a structured reason. The Orchestrator must not silently route the task to an unrelated role.