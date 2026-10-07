# Deployment Configuration & Plan — Frontend Handoff v1.0

> Implementation contract — issue #135.
>
> Consumed by: #120 (Cloud Console GitHub → Deploy workflow), #72.
>
> Normative sources:
> - `docs/design/screens/deployment-configuration/README.md`
> - `docs/design/screens/deployment-plan/README.md`
> - `docs/design/handoff/shell/README.md`
> - `docs/architecture/api-contract.md` §9–§13

## 1. Data Flow

```
Profile (#95) ─┐
Server (#138) ─┼─► Configuration form ──POST deployment-plans──► Plan (immutable, planId)
Config values ─┘                                                    │
                                                         POST deployments (planId, Idempotency-Key)
                                                                    ▼
                                                         Deployment (Progress tab)
```

- Configuration is client-side form state until persisted by the Engine.
- A plan is never edited; new inputs produce a new plan.
- The UI never talks to servers, runtime components or the database directly.

## 2. Components

| Component | Responsibility |
|---|---|
| `DeploymentConfigForm` | field groups, validation, error summary, session draft (non-secret only) |
| `EnvironmentPicker` | radio cards built on `EnvironmentChip`; shows current status per environment |
| `ServerSummary` | selected server readiness + capacity; Change → Server Selection with `from` |
| `DomainField` | format validation, Engine DNS status when available |
| `ConfigValuesEditor` | rows of name / required / value / type; `.env` paste preview; write-only secrets |
| `PlanSummary` | summary rows (source, application, target, access, build, runtime, health, rollback) |
| `PlanSequence` | precondition row + steps from `steps[]` |
| `PlanStep` | expandable step: status, explanation, dependencies, failure boundary, L3/L4 detail |
| `PlanStateNotice` | Invalid / Stale / Target unavailable / Already executed |
| `DeployButton` | gating, reason text, Production confirmation, idempotency |

`PlanSequence` and `PlanStep` are shared with Deployment Progress (#136): same component, live step statuses.

## 3. Step Label Mapping

Single source of truth in code, e.g. `stepLabels.ts`:

```ts
export const PLAN_STEP_LABELS: Record<string, string> = {
  BUILD: 'Build',
  CREATE_RUNTIME: 'Create Runtime',
  NETWORK: 'Configure Network',
  START: 'Start',
  VERIFY: 'Verify',
};
// Unknown codes: render the raw code in mono. Never drop, rename or reorder.
```

The same mapping is used for `GET /deployments/{id}/steps` (`name` field).

## 4. Secrets Rules (#126)

| Rule | Implementation |
|---|---|
| Write-only | after save, the UI only knows `{ name, isSet: true }` |
| No client persistence | secret inputs excluded from session drafts, query params, analytics, error reports |
| No echo | plan, logs, events, toasts show variable names only |
| Input hygiene | `type=password`, `autocomplete=off`, `spellcheck=false` |
| Transport | sent only in the dedicated Engine request body over the authenticated API |

## 5. Gating Logic

```text
canReviewPlan = environment && server.state != OFFLINE
                && (server.state != DEGRADED || degradedAcknowledged)
                && ref && requiredConfigValues.allSet && fieldsValid

canDeploy     = plan.status == 'READY'
                && !plan.stale            // profile version / fingerprint / server state unchanged
                && !plan.executed
                && server.state != OFFLINE
```

`Review plan` is not disabled when invalid; it validates and shows the error summary. `Deploy` is disabled with visible reason when `canDeploy` is false.

## 6. Idempotency

- `Idempotency-Key = "deploy:" + planId` (deterministic per plan) so double-clicks, retries and refresh cannot create two deployments from one plan.
- `POST deployment-plans` is guarded client-side against concurrent submission.

## 7. Contract Gaps (must be resolved before #120 ships)

| Gap | Needed by | Owner |
|---|---|---|
| `environment` in plan request/response and deployment | Configure environment, locked chip, routes | #71 / #117 / #97 |
| Configuration values API (write-only secrets) | ConfigValuesEditor | #117 / #126 |
| Plan `status` values beyond `READY` (invalid, with field-addressable errors) | PlanStateNotice | #97 / #117 |
| Plan fingerprint and staleness signal | determinism display, Stale state | #97 |
| Plan summary fields: build strategy, runtime strategy, domain/TLS, rollback metadata, per-step detail and failure boundary | PlanSummary, PlanStep | #60 / #97 |
| Optional step dependencies in `steps[]` (otherwise derived from order) | PlanStep | #60 |
| Server capabilities for resource limits / runtime options | Advanced options | #79 / #138 |
| Engine default domain and DNS status | DomainField | #64 |

Until a gap is closed, the corresponding UI element is **not rendered** (or rendered as "not specified by this plan" for rollback), never filled with invented data.

## 8. Test Requirements

| # | Test |
|---|---|
| D1 | Plan renders exactly `steps[]` in order; unknown code rendered raw; Analyze appears only as precondition. |
| D2 | Deploy disabled for non-READY, stale, executed plans, with reason text. |
| D3 | Double-click Deploy sends one request; retry reuses the same Idempotency-Key. |
| D4 | Secret value never appears in DOM after save, session storage, URL or plan view. |
| D5 | Production deploy requires typed application name; Staging/Preview do not. |
| D6 | Backend validation errors map to Configure fields and error summary. |
| D7 | Refresh on Configure restores non-secret draft; refresh on Plan shows the same plan. |
| D8 | Missing rollback metadata renders "not specified by this plan", not reference copy. |
| D9 | Keyboard-only: environment radio group, step expanders, confirmation dialog operable; axe clean. |
