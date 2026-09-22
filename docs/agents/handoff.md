# Axiom Agent Handoff Contract

Every completed agent task must provide a concise handoff in its PR or issue.

## Required format

### Summary

What was implemented.

### Scope

What was intentionally changed.

### Files

List the principal files created or modified.

### Contracts

List architecture/API/domain contracts consumed or changed.

### Tests

For each relevant test:

- command
- result
- environment limitations, if any

Never state that a test passed if it was not executed.

### Security

Describe security-sensitive behavior and validation.

### Ownership

Confirm that all changed files were within the declared ownership boundary.

### Known limitations

List incomplete behavior, stubs, assumptions, and follow-up work.

### Integration notes

List anything another agent or the integration owner must know.

### Status

One of:

- READY FOR REVIEW
- BLOCKED
- READY FOR INTEGRATION

## Handoff principle

The next agent must be able to continue from the handoff without reconstructing the previous agent's reasoning.
