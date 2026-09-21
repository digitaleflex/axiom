# Expert Context and Memory Boundaries

Experts receive only the context required to perform their assigned task.

## Context layers

1. **Project context** — project identity, manifest, environment and active scope.
2. **Task context** — objective, constraints, dependencies and acceptance criteria.
3. **Artifact context** — validated inputs and relevant previous outputs.
4. **Decision context** — applicable ADRs and approved decisions.
5. **Repository context** — only the code and configuration required by the task.
6. **Runtime context** — only for roles explicitly authorized to inspect runtime state.
7. **Sensitive context** — secrets and protected data accessed through controlled references, never copied into ordinary artifacts.

## Rules

- Default access is least-privilege.
- Context must be attributable to a project and task.
- Sensitive context requires explicit authorization.
- Historical decisions must remain versioned and traceable.
- Context supplied to an expert should be reproducible for the same task execution where practical.
- Context should expire when the task or authorization expires.

## Memory boundaries

Agent memory must not become an uncontrolled source of authority. A remembered suggestion, preference, or previous output does not override the current project manifest, validated artifact, security policy, or approved decision.

## Context precedence

```text
Security / Governance constraints
            ↓
Approved architecture decisions
            ↓
Validated task + project contracts
            ↓
Validated artifacts
            ↓
Historical context / suggestions
```

When sources conflict, the higher-level constraint wins and the conflict should be surfaced to the Orchestrator.
