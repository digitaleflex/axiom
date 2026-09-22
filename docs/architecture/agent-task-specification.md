# Axiom Agent Task Specification

## Purpose

This document is the canonical execution contract for autonomous coding agents working on Axiom.

An issue is **READY FOR AGENT** only when its task specification defines the objective, dependencies, owned files, forbidden files, contracts, tests, acceptance criteria and handoff rules.

## Agent operating rules

1. Read the issue completely before changing code.
2. Read every declared READ dependency before implementation.
3. Modify only files under OWNED PATHS unless the issue explicitly grants an additional path.
4. Never modify FORBIDDEN PATHS.
5. Never invent or silently change an architecture contract. If a required contract is missing or contradictory, stop and report BLOCKED.
6. Do not refactor unrelated code.
7. Do not change public APIs, schemas or shared contracts owned by another active issue.
8. Keep changes atomic and reviewable.
9. Add/update tests for every behavior changed.
10. Run the declared verification commands when the local environment permits them. Never claim a test passed if it was not actually executed.
11. Commit only the files belonging to the issue.
12. Report exactly what changed, what was verified, what remains, and any integration concern.

## Dependency semantics

- **BLOCKS**: must be completed before implementation can safely begin.
- **READS**: authoritative context that may be consumed but not modified.
- **IMPLEMENTS**: files/contracts owned by this issue.
- **CONSUMES**: interfaces produced elsewhere.
- **INTEGRATES WITH**: downstream components that consume the result.

## Collision boundary

Every implementation issue MUST declare:

### OWNED PATHS
Files/directories the agent is authorized to create or modify.

### READ-ONLY PATHS
Files that may be inspected but must not be modified.

### FORBIDDEN PATHS
Explicitly protected areas owned by other tasks.

If two active issues own the same implementation path, the work map is invalid and must be corrected before parallel execution.

## Contract freeze

Shared contracts are integration boundaries. An agent must not modify:
- PostgreSQL schema owned by M4 schema tasks
- REST/API contract owned by M4 API tasks
- Agent protocol owned by M5.1
- Security policy owned by M5.3/M5.14/M5.15 and M7
- Deployment state machine owned by deployment-domain tasks

unless the issue explicitly transfers ownership.

## Definition of Done

- Implementation complete within OWNED PATHS.
- No forbidden-path changes.
- Unit/integration tests added or updated.
- Existing relevant tests remain compatible.
- Error paths and security constraints covered.
- Public contract changes documented in the owning contract issue.
- No secrets or credentials committed/logged.
- Verification commands executed where possible.
- Commit is scoped to the issue.
- Handoff report added to the issue/PR.

## Handoff report

Use:

### Implementation
- ...

### Files changed
- ...

### Contracts consumed
- ...

### Tests
- Command:
- Result:

### Known limitations
- ...

### Integration notes
- ...

### Follow-up
- ...

## READY FOR AGENT checklist

- [ ] Objective is unambiguous
- [ ] BLOCKS dependencies are resolved
- [ ] READS dependencies are named
- [ ] OWNED PATHS are exclusive
- [ ] FORBIDDEN PATHS are explicit
- [ ] Contracts are identified
- [ ] Acceptance criteria are testable
- [ ] Test strategy is defined
- [ ] Definition of Done is applicable
- [ ] No architecture guessing is required

## Parallel execution model

Axiom uses:

Architecture/contracts
→ domain task
→ owned implementation surface
→ tests
→ integration

Agents may work in parallel only when their OWNED PATHS do not overlap and their BLOCKS dependencies are satisfied.

Collision resistance is a design property, not a guarantee. Integration remains a separate controlled step.
