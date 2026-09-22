# Axiom Agent Readiness

The readiness validator turns the documented multi-agent workflow into an automated GitHub gate.

## Checks

### Agent issues
- required task sections;
- agent, priority, and deadline metadata;
- resolvable owned paths;
- blocking dependency states;
- overlapping active ownership boundaries.

### Pull requests
- an unambiguous linked issue;
- linked issue has the agent label;
- linked issue has resolvable owned paths;
- every changed file is inside the ownership boundary;
- linked issue is not blocked, rejected, or cancelled.

## Fail closed

The validator rejects a task when it cannot prove that a requested change is in scope. It does not infer architecture or widen ownership.

## Ownership source

The validator first reads OWNED PATHS from the issue body. For the current migration backlog it falls back to docs/architecture/agent-work-map.md.

## Workflow states

Recommended labels: agent:ready, agent:in-progress, agent:review, agent:integration, agent:done, agent:blocked, agent:rejected, agent:cancelled.

The first validator version is read-only with respect to workflow state: it fails the check rather than silently changing labels.

## Security

The workflow reads repository metadata and issue/PR data through GITHUB_TOKEN. It does not execute repository code, deployment commands, Docker commands, or arbitrary commands supplied by an issue.
