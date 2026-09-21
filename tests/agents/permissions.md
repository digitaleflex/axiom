# Expert Permission Test Matrix

The implementation must prove that permissions are enforced independently of model intent.

| Scenario | Expected result |
|---|---|
| Expert uses undeclared capability | Reject |
| Expert writes outside declared scope | Reject |
| Expert requests secret without permission | Reject |
| Expert requests privileged operation without approval | Reject |
| Expert performs forbidden responsibility | Reject and record violation |
| Revoked permission is used | Reject |
| Valid read within scope | Allow |
| Valid write within scope | Allow |
| Valid sensitive operation with approval | Allow and audit |
| Runtime receives unauthorized command | Reject at runtime |

## Required evidence

- deterministic authorization result;
- actor/expert identity;
- capability identifier;
- resource/scope;
- approval reference when applicable;
- audit event for denied sensitive operations.
