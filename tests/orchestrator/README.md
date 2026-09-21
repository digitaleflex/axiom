# Orchestrator Tests

Tests validate contracts before real provider integrations.

## Scenario matrix

| Scenario | Expected result |
|---|---|
| valid dependency graph | READY tasks scheduled |
| circular dependency | planner rejects graph |
| missing artifact | task remains blocked |
| incompatible agent | routing rejects dispatch |
| unauthorized capability | policy denies |
| quality gate failure | revision required |
| required human approval | execution pauses |
| approval granted | execution resumes |
| approval denied | execution cancelled/blocked by policy |
| transient adapter failure | retry according to policy |
| unknown external outcome | reconciliation required |
| Orchestrator restart | persisted state recovered |
| valid reference workflow | terminal COMPLETED |

## Test layers

- unit: pure state/planning/routing/policy behavior
- contract: schemas and interface invariants
- integration: in-memory adapters together
- E2E: canonical multi-agent workflow with deterministic test doubles

No test should require a real LLM provider to validate core orchestration semantics.