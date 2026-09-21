# Persistence, Idempotency and Recovery

The Orchestrator persists authoritative state independently from process memory.

## Required persisted state

- orchestration run
- tasks and dependencies
- execution attempts
- state transitions
- artifact references
- gate results
- approvals
- context snapshot references
- correlation IDs

## Idempotency

Every external execution receives an idempotency key derived from the logical task execution attempt. Retrying a request must allow the adapter/runtime boundary to distinguish a new operation from a replay.

## Recovery

On startup:

1. load non-terminal runs
2. identify stale execution leases
3. reconcile in-flight operations
4. preserve completed work
5. retry only according to policy
6. emit recovery events

The system must assume an external operation may have succeeded even when the Orchestrator process failed before receiving its response.

## Reconciliation

Unknown outcomes are not converted directly to FAILED. They enter an explicit reconciliation state so that destructive or non-idempotent actions are not duplicated.