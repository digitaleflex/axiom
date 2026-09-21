# Task Planning

The planner converts an approved objective into a dependency graph of bounded tasks.

## Rules

- Tasks are atomic enough to have one primary owner.
- Every task declares inputs, expected artifacts, capabilities and quality gates.
- Dependencies form a directed acyclic graph (DAG).
- Circular dependencies are rejected before execution.
- A task is READY only when all required dependencies are completed and required inputs are available and valid.
- Independent READY tasks may execute in parallel when concurrency and policy permit.
- Failure of a required dependency blocks downstream tasks unless an explicit alternative path exists.

## Scheduling priority

1. policy/security constraints
2. dependency readiness
3. required quality/approval gates
4. explicit task priority
5. deterministic creation order as the final tie-breaker

The planner must be deterministic for identical project state and inputs.

## Example

```text
Business Specification
        |
        v
Architecture
   /    |    \
  v     v     v
Backend Frontend Database
   \     |     /
    v    v    v
   Security + QA
        |
        v
DevOps / Infrastructure
        |
        v
   Runtime Agent
```

The planner describes work; it does not invent business requirements or bypass expert authority.