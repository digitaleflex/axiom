# Axiom Agent Work Lifecycle

This document defines the lifecycle used to dispatch autonomous coding agents safely.

## Canonical states

`DRAFT → READY → IN_PROGRESS → REVIEW → INTEGRATION → DONE`

Exception states:

- `BLOCKED`: a dependency, contract or ownership condition prevents execution.
- `REJECTED`: implementation or acceptance criteria are not satisfied.
- `CANCELLED`: task is intentionally abandoned or superseded.

## State ownership

| State | Meaning | Entry requirements | Exit condition |
|---|---|---|---|
| DRAFT | Task is being specified | Issue exists | READY checklist passes |
| READY | Agent may start | Dependencies, ownership and contract gates pass | Agent claims task |
| IN_PROGRESS | Agent is implementing | Agent has claimed the issue | Implementation + tests complete |
| REVIEW | Implementation awaits review | Scoped commit/PR exists | Review accepted or changes requested |
| INTEGRATION | Approved work is being integrated | Review passed, dependencies still valid | Integration tests pass |
| DONE | Work is complete | Integration and acceptance gates pass | Terminal |
| BLOCKED | Cannot safely proceed | Missing dependency/contract/ownership | Blocking condition resolved |
| REJECTED | Work does not satisfy contract | Review/integration failure | Returned to IN_PROGRESS |
| CANCELLED | No longer required | Explicit project decision | Terminal |

## READY gate

An issue can enter READY only when all are true:

- [ ] Objective is unambiguous.
- [ ] BLOCKS dependencies are complete or explicitly waived.
- [ ] READ/CONSUME dependencies are identified.
- [ ] OWNED PATHS are declared.
- [ ] No active issue owns the same implementation paths.
- [ ] FORBIDDEN PATHS are declared.
- [ ] Shared contracts are identified.
- [ ] Acceptance criteria are testable.
- [ ] Test strategy is defined.
- [ ] Definition of Done exists.
- [ ] No architecture decision requires agent guessing.

If any condition fails, the issue remains BLOCKED/DRAFT.

## Agent claim gate

Before coding, the agent must verify:

1. The issue is READY.
2. No dependency changed since READY.
3. No ownership collision exists.
4. The target branch/ref is current enough for the declared ownership map.
5. The agent understands the contract it consumes.
6. The agent can identify the exact files it may modify.

If any check fails, the agent must not start implementation.

## IN_PROGRESS rules

While implementing:

- Modify only OWNED PATHS.
- Do not silently expand scope.
- Do not modify another task's owned path.
- Do not change shared contracts without transferring ownership.
- Add tests with the implementation.
- Keep secrets and credentials out of source, logs and commits.
- If a cross-boundary change becomes necessary, stop and report BLOCKED rather than taking ownership implicitly.

## REVIEW gate

Review must verify:

- scope matches the issue;
- owned-path boundary was respected;
- forbidden paths were not modified;
- acceptance criteria are demonstrably satisfied;
- tests cover relevant success and failure cases;
- no unauthorized contract changes exist;
- security boundaries remain intact;
- commit/PR is scoped and understandable.

Review outcomes:

- ACCEPT → INTEGRATION
- CHANGES_REQUESTED → IN_PROGRESS
- BLOCKED → BLOCKED

## INTEGRATION gate

Integration is a separate responsibility from implementation.

Verify:

- branch/commit can be integrated cleanly;
- shared contracts remain compatible;
- dependent tasks still match their declared inputs;
- relevant unit/integration tests pass;
- no ownership collision was introduced;
- documentation is synchronized where required.

Only after these checks may the task become DONE.

## Handoff contract

Every completed task must report:

### Implementation
What was implemented.

### Files changed
Exact paths.

### Contracts consumed
Contract names/versions.

### Tests
Commands actually executed and their results.

### Known limitations
Anything intentionally deferred.

### Integration notes
Anything downstream agents must know.

### Follow-up
Remaining work, if any.

## Automation principle

The workflow should eventually be enforced mechanically by CI or a repository automation:

`issue metadata → readiness validator → agent claim → PR boundary validator → tests → review → integration → DONE`

Human review remains authoritative for architectural or security exceptions.

## Collision policy

The system must fail closed.

If ownership is ambiguous, duplicated or missing:

**Do not dispatch the agent.**

Mark the task BLOCKED and require the ownership map to be corrected.

## No false completion

An issue must never be marked DONE merely because code exists.

DONE requires evidence:
- implementation,
- tests,
- review,
- integration,
- acceptance criteria.
