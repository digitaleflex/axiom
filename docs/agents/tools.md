# Expert Tool Permission Model

Tool access is separate from role identity and capability declarations.

## Permission levels

| Level | Meaning |
|---|---|
| READ | Inspect permitted resources |
| WRITE | Modify permitted project resources |
| EXECUTE | Run an explicitly permitted operation |
| PRIVILEGED | Perform sensitive infrastructure/security operations |
| APPROVAL_REQUIRED | Operation requires explicit human or policy approval |

## Default policy

No expert receives unrestricted filesystem, shell, network, secret, repository, or infrastructure access by default.

## Tool categories

- repository: read/write source and configuration;
- filesystem: scoped workspace access;
- shell: controlled command execution;
- infrastructure: deployment and runtime operations;
- secrets: reference/access through a secret-management boundary;
- network: explicitly allowed external communication;
- external APIs: provider-specific credentials and scopes;
- observability: logs, metrics, traces and diagnostics.

## Sensitive operations

Examples include deleting resources, rotating credentials, changing access control, modifying production networking, destructive database operations, and production deployment changes.

These operations must have explicit authorization, audit records, and the applicable approval gate.

## Revocation

Permissions must be revocable independently of the agent implementation. A capability declaration never grants permission by itself.
