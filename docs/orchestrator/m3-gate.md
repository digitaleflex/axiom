# M3 Completion Gate

M3 is complete only when the Orchestrator is specified, implemented at its core, tested and reviewable.

## Required

- [ ] lifecycle state machine defined and tested
- [ ] task planner and dependency engine defined and tested
- [ ] agent routing defined and tested
- [ ] context propagation defined and tested
- [ ] artifact registry/handoff defined and tested
- [ ] quality and approval gates enforced
- [ ] persistence/idempotency/recovery defined and tested
- [ ] security policy enforced
- [ ] observability/audit events defined
- [ ] adapters isolated from core
- [ ] canonical reference workflow passes with test doubles
- [ ] integration and E2E scenarios pass
- [ ] documentation complete
- [ ] no P0 security or contract gap remains

## Exit condition

M3 may transition to M4 only after the gate is reviewed and all mandatory checks are satisfied. Known limitations must be explicitly documented rather than silently accepted.