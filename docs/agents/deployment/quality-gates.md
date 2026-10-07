# Deployment Expert Quality Gates & Evaluation

> Issue #38. Gates are declared in `quality_gates` of each expert contract and enforced by the Engine before handoff.

## 1. Gate pipeline (every artifact)

| Order | Gate | Check | Failure |
|---|---|---|---|
| 1 | Contract | producer contract allows this output type; inputs were `valid` | reject |
| 2 | Schema | envelope + payload validate (`artifact.schema.json`) | reject |
| 3 | Completeness | required payload fields present for the next stage | revise |
| 4 | Traceability | `trace.commit` consistent with `dependsOn` | reject |
| 5 | Expert gates | rules in the producer contract | per gate |
| 6 | Security | no secret values; policy allow (plans) | reject / escalate |
| 7 | Confidence | low-confidence / ambiguous required facts reported, never silently accepted | escalate to user |
| 8 | Human review | when configured (`overrides.human_review`) or policy requires | escalate |

## 2. Critical artifact criteria

| Artifact | Criteria |
|---|---|
| RepositoryAnalysis | every finding has evidence; confidence ∈ [0,1]; conflicts → ambiguous |
| ApplicationProfile | provenance per value; known preset or unsupported with reason; required fields for strategy |
| DeploymentPlan | canonical steps, no duplicates; capabilities satisfied; deterministic fingerprint; no secret values |
| BuildArtifact | built from plan commit; image digest present |
| HealthResult | status from actual probe; timestamp |
| DeploymentResult | LIVE only with HEALTHY result; FAILED with failed step + error code |
| SecurityReport | explicit allow/deny with findings |

## 3. Outcomes

| Outcome | Path |
|---|---|
| `valid` | handed to consumers |
| `revise` | returned to producer with reasons; new revision; max retries from `runtime_limits.max_retries`, then `rejected` |
| `reject` | artifact state `rejected` with `{gate, reason}`; deployment stage fails with error code |
| `escalate` | routed to escalation target (user, human approval, security expert); stage paused until resolved |

Invalid or unsafe output can never reach execution: executors accept only `valid` artifacts (artifacts.md R1).

## 4. Evaluation protocol

- **Golden fixtures** per expert (#95 tests): Node/Next.js, Vite, Go, Dockerfile, Compose, unsupported, conflicting lockfiles, monorepo.
- **Determinism tests**: same input twice → identical artifact fingerprint.
- **Gate tests**: each gate has a failing fixture.
- **Regression gate** before release (#111).
