# Orchestrator Context

Context is assembled per task from authoritative project and execution sources.

## Context layers

1. project manifest
2. current task
3. validated input artifacts
4. relevant prior decisions/ADRs
5. repository snapshot or references
6. runtime state when explicitly required
7. sensitive context only when policy authorizes it

## Propagation rules

- Minimum necessary context only.
- Artifacts are referenced by immutable identity/version.
- Context is snapshotted for reproducibility.
- Secrets are references, not ordinary prompt/context values.
- Sensitive context access is explicit and auditable.
- Later mutable data cannot silently rewrite the historical context of an execution.

Each execution records a context snapshot identifier that can be associated with its produced artifacts and audit events.