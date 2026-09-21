# Orchestrator Security

Security policy is enforced before dispatch and cannot be bypassed by retries, routing or escalation.

## Authorization chain

```text
Principal
  ↓
Project access
  ↓
Task authorization
  ↓
Agent capability
  ↓
Tool permission
  ↓
Sensitive resource policy
  ↓
Human approval when required
  ↓
Dispatch
```

## Rules

- Deny by default for capabilities and sensitive resources.
- Every execution is attributable to a principal and task.
- Privileged operations require explicit policy authorization.
- Approval-required operations cannot be converted into ordinary operations by an agent.
- Secrets are never copied into ordinary audit events or artifacts.
- Authorization failures are explicit, observable and auditable.

The Orchestrator coordinates policy enforcement; the actual privileged operation remains owned by the appropriate infrastructure/runtime boundary.