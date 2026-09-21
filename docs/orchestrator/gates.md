# Quality Gates, Approvals and Escalation

## Gate types

- contract validation
- artifact validation
- security policy
- automated tests
- role-specific quality gate
- human approval

## Gate result

```text
PENDING -> PASSED
PENDING -> FAILED
FAILED -> REVISION_REQUIRED
REVISION_REQUIRED -> PENDING
PENDING -> BLOCKED
```

A failed gate prevents downstream progression until its required revision or approval path is completed.

## Human approval

Human approval is mandatory when the project policy marks an operation as approval-required. Typical examples include privileged infrastructure changes, destructive operations, production-impacting deployment and exceptional access to sensitive resources.

Approval records contain requester, approver, action, scope, decision, timestamp, reason and correlation ID.

Agents cannot approve their own critical output.

## Escalation

Escalation is explicit when:

- no eligible agent exists
- policy denies execution
- a required gate repeatedly fails
- an external dependency remains unavailable
- a human decision is required
- execution exceeds configured limits

Escalation preserves the current state and evidence.