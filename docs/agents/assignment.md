# Axiom Agent Assignment

## Objective

Assign the smallest suitable GitHub custom agent to each READY issue.

## Assignment algorithm

1. Read the issue.
2. Identify milestone and domain.
3. Read the Agent Task Specification.
4. Resolve dependencies.
5. Resolve ownership.
6. Select the specialist agent.
7. Assign the issue to the agent.
8. Monitor the generated branch/PR.
9. Validate the PR against the task contract.

## Routing matrix

| Work domain | Agent |
|---|---|
| Architecture / ADR / contracts | architecture |
| Go Engine / orchestration | engine |
| GitHub integration / analyzer / planner | deployment |
| Runtime / Docker / Traefik | runtime |
| Security / auth / secrets | security |
| PostgreSQL / persistence | database |
| Cloud Console / frontend | frontend |
| Tests / release validation | qa |

## Parallelization

Tasks may run in parallel when:

- their dependencies are satisfied
- their owned paths do not overlap
- they do not mutate the same contract simultaneously
- integration order is known

Example:

```
M5 Protocol
     ↓
Identity ─────── Docker
     ↓             ↓
Authentication ─ Traefik
     ↓             ↓
Dispatcher ───── Health
     \             /
      State / Recovery
            ↓
       Integration
```

## When not to parallelize

Do not run tasks in parallel when they:

- edit the same contract
- modify the same ownership surface
- depend on an unfinished schema
- depend on an unfinished API
- require a shared architectural decision
- can create incompatible migrations

## Assignment evidence

Every agent task should leave a trace containing:

- issue number
- selected agent
- dependencies
- ownership
- branch/PR
- tests
- validation result
- final handoff
