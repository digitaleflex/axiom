# Orchestrator Lifecycle

## Run lifecycle

```text
CREATED -> PLANNING -> READY -> RUNNING
RUNNING -> WAITING_GATE -> RUNNING
RUNNING -> WAITING_APPROVAL -> RUNNING
RUNNING -> BLOCKED
RUNNING -> FAILED
FAILED -> RETRYING -> RUNNING
RUNNING -> COMPLETED
RUNNING -> CANCELLED
BLOCKED -> RUNNING | CANCELLED
WAITING_GATE -> RUNNING | REVISION_REQUIRED | FAILED
WAITING_APPROVAL -> RUNNING | CANCELLED
REVISION_REQUIRED -> PLANNING | RUNNING
```

## Task lifecycle

```text
PENDING -> READY -> DISPATCHED -> RUNNING
RUNNING -> SUCCEEDED | FAILED | BLOCKED | CANCELLED
FAILED -> RETRYING -> READY
SUCCEEDED -> VALIDATING -> COMPLETED
VALIDATING -> COMPLETED | REVISION_REQUIRED | REJECTED
REVISION_REQUIRED -> READY
```

## Invariants

1. Invalid transitions are rejected.
2. A task cannot become READY while a required dependency is unresolved.
3. A task cannot be dispatched without an authorized agent and compatible contract.
4. A gate failure cannot be bypassed by retrying the same task.
5. A required human approval cannot be replaced by an agent decision.
6. Terminal states are immutable except through an explicit recovery/reconciliation operation.
7. Every transition has a timestamp, actor, correlation ID and reason.
8. Execution attempts have an idempotency key.

## Recovery

On restart, persisted state is authoritative. In-flight tasks are reconciled using execution leases and idempotency keys. The Orchestrator must never assume that a process restart means that the external operation did not happen.

## Cancellation

Cancellation is explicit. The Orchestrator records who requested it and whether downstream execution was successfully stopped. Cancellation does not erase history.