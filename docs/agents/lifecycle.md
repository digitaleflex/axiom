# Axiom Agent Lifecycle

## 1. DRAFT

A task exists conceptually but is not executable by an autonomous coding agent.

Required work:

- define objective
- identify dependencies
- define contracts
- define owned paths
- define forbidden paths
- define tests
- define acceptance criteria

## 2. READY

The task has passed the Axiom readiness rules.

A task is READY only when:

- required sections exist
- `agent`, priority, and deadline metadata exist
- dependencies are ready or explicitly non-blocking
- ownership is non-overlapping with active work
- contracts are identified
- acceptance criteria are testable

## 3. IN_PROGRESS

A GitHub agent has been assigned and is actively implementing the task.

Rules:

- work only within the ownership boundary
- do not silently expand scope
- do not rewrite unrelated architecture
- report blockers instead of guessing
- keep changes reviewable

## 4. REVIEW

The agent has produced a pull request.

Validation must cover:

- changed files are within ownership
- issue scope is respected
- relevant tests exist and are executed where possible
- security constraints are respected
- architecture contracts remain coherent
- no unverified completion claims exist

## 5. INTEGRATION

The PR is technically acceptable and is being integrated with the current branch.

Integration verifies:

- dependency compatibility
- migrations/contracts compatibility
- cross-agent assumptions
- CI status
- collision resolution

## 6. DONE

The task is complete only when:

- acceptance criteria are satisfied
- evidence is recorded
- PR is merged
- no unresolved blocker remains
- issue is updated with the final handoff

## Exception states

### BLOCKED

Use when a dependency, contract, credential, environment, or architectural decision prevents safe progress.

### REJECTED

Use when the implementation fails scope, architecture, security, or quality requirements.

### CANCELLED

Use when the task is intentionally abandoned or superseded.

## Non-negotiable rule

An agent must never convert uncertainty into an architectural decision without recording the decision or escalating it.
