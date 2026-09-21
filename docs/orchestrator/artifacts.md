# Artifact Registry

Artifacts are the durable exchange objects between orchestration stages.

## Artifact requirements

Every artifact has:

- immutable ID
- type
- schema/version
- project ID
- task ID
- producer
- intended consumers
- dependencies
- validation status
- creation timestamp
- provenance metadata

## Lifecycle

```text
CREATED -> VALIDATING -> VALID
                     \-> INVALID
VALID -> SUPERSEDED
VALID -> REVISED -> VALID
```

An artifact version is immutable after publication. A revision creates a new version and preserves a `supersedes`/lineage reference.

## Handoff

A task may consume only artifacts that satisfy its declared input contract and validation requirements. The registry is the source of truth for artifact identity and lineage; agents must not communicate critical state solely through untracked free-form messages.