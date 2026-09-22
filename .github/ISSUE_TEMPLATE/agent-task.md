---
name: Autonomous Agent Task
about: Create a collision-resistant task for an autonomous coding agent
title: "[AGENT] "
labels: "agent"
assignees: ""
---

## Objective

<!-- One unambiguous outcome. -->

## Agent Assignment

### Recommended agent
<!-- architecture | engine | deployment | runtime | database | security | frontend | qa -->

-

### Why this agent
-

### Alternative agent
-

## Scope

### In scope
-

### Out of scope
-

## Dependencies

### BLOCKS
-

### READ / CONSUME
-

### INTEGRATES WITH
-

## Collision Boundary

### OWNED PATHS
<!-- Only these paths may be modified. -->
-

### READ-ONLY PATHS
-

### FORBIDDEN PATHS
-

## Contracts

### Consumes
-

### Produces
-

### Contract version / source
-

## Implementation Requirements

-

## Security Constraints

-

## Test Strategy

### Unit
-

### Integration
-

### Failure / Security
-

## Acceptance Criteria

- [ ]

## Definition of Done

- [ ] Implementation is complete.
- [ ] Only OWNED PATHS were modified.
- [ ] Tests were added/updated.
- [ ] Executed verification is reported truthfully.
- [ ] Acceptance criteria are satisfied.
- [ ] No unauthorized contract changes.
- [ ] Handoff is complete.

## Handoff

### Implementation
-

### Files changed
-

### Contracts consumed
-

### Tests actually executed
-

### Known limitations
-

### Integration notes
-

### Follow-up
-

## Agent Readiness

- [ ] Objective unambiguous
- [ ] Recommended agent selected
- [ ] Dependencies resolved
- [ ] Ownership exclusive
- [ ] Forbidden paths explicit
- [ ] Contracts identified
- [ ] Acceptance criteria testable
- [ ] Test strategy defined
- [ ] No architecture guessing required

## Execution

After the issue passes the readiness gate:

1. Assign the selected GitHub custom agent.
2. Let the agent create/update its branch and pull request.
3. Validate changed files against OWNED PATHS.
4. Run relevant tests and CI checks.
5. Review security and contract compatibility.
6. Integrate only after the task reaches REVIEW/INTEGRATION.
