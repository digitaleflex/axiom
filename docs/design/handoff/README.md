# Axiom Cloud Console — Final Design Handoff & Frontend Implementation Contract v1.0

> Issue #141. Single entry point for the frontend implementation agent (#119, #72, #120).
>
> **Rule: if it is not specified in `docs/design/**`, the frontend agent does not decide it — it records the question and stops that item.** Validated GitHub documentation is the source of truth; Stitch/visual explorations are not (#142).

## 1. Source-of-Truth Order

When two sources disagree, the higher one wins:

1. `docs/architecture/api-contract.md` and backend contracts (data, states, endpoints)
2. `docs/design/accessibility/README.md` (v1.0.1 token amendment)
3. `docs/design/design-dna/README.md` + `docs/design/tokens/axiom.css`
4. `docs/design/ux/progressive-disclosure.md`, `docs/design/ux/navigation/README.md`
5. `docs/design/interactions/README.md`, `docs/design/responsive/README.md`
6. `docs/design/components/**`, `docs/design/screens/**`
7. `docs/design/handoff/**` (this folder — consolidates, does not override)
8. Visual explorations and master prompts (inspiration only)

## 2. Screen Inventory

Every canonical screen has a handoff spec:

| # | Screen | Route | Spec |
|---|---|---|---|
| 1 | Dashboard | `/dashboard` | `screens/shell/` §8, `screens/system-states/` §3 — **see §8 D-1** |
| 2 | GitHub Connection | `/github` | `screens/github/` |
| 3 | Repository List | `/repositories` | `screens/repository/` §1 |
| 4 | Repository Detail | `/repositories/:id` | `screens/repository/` §2 |
| 5 | Repository Analysis | `/apps/:id/setup/analysis` | `screens/analysis/` |
| 6 | Application Profile | `/apps/:id/setup/profile` | `screens/application-profile/` |
| 7 | Server Selection | `/apps/:id/setup/server` | `screens/servers/` §1 |
| 8 | Deployment Configuration | `/apps/:id/setup/configure` | `screens/deployment-configuration/` |
| 9 | Deployment Plan | `/apps/:id/setup/plan/:planId` | `screens/deployment-plan/` |
| 10 | Deployment Progress | `…/deployments/:depId/progress` | `screens/deployment-progress/` |
| 11 | Deployment Success | `…/deployments/:depId/summary` (LIVE) | `screens/deployment-success/` |
| 12 | Deployment Failure | `…/deployments/:depId/summary` (FAILED) | `screens/deployment-failure/` |
| 13 | Application Overview | `/apps/:id/:env/overview` | `screens/application/` §1 |
| 14 | Application Deployments | `/apps/:id/:env/deployments` | `screens/application/` §2 |
| 15 | Application Logs | `/apps/:id/:env/logs` | `screens/logs/` |
| 16 | Application Metrics | `/apps/:id/:env/metrics` | `screens/metrics/` |
| 17 | Application Domains | `/apps/:id/:env/domains` | `screens/domains/` |
| 18 | Server Overview | `/servers` | `screens/servers/` §2 |
| 19 | Server Details | `/servers/:id` | `screens/servers/` §3 |
| 20 | Settings | `/settings/:section` | `screens/settings/` |

Shell for all: `screens/shell/`, `ux/navigation/`, `handoff/shell/`. Responsive per screen: `responsive/` §3. States per screen: `screens/system-states/` §3.

## 3. Component Inventory

Components consume semantic tokens only (Design DNA §12). Variants and states are mandatory.

| Component | Variants | States | Spec |
|---|---|---|---|
| Button | primary, secondary, ghost, destructive | default, hover, focus-visible, active, disabled (`aria-disabled` + reason), loading | Design DNA §10, accessibility A4 |
| Input / Select / Combobox | text, password (secret), search, select, combobox | default, focus, invalid, disabled, read-only | accessibility §9, A3 |
| StatusPill | one per canonical status + unknown | static, live-updating | `screens/deployment-progress/` §1.2 |
| StepIndicator | queued, running, completed, failed, skipped, cancelled | — | `screens/deployment-progress/` §1.3 |
| EnvironmentChip | production, staging, preview; sm/md; abbreviated; locked; not-deployed | — | `screens/shell/` §4 |
| ConfidenceBadge | high, medium, low | — | `screens/analysis/` §2.2 |
| ProvenanceTag | detected, axiom.yaml, default, override | — | `screens/analysis/` §4 |
| EvidenceDrawer | — | loading, empty, loaded | `screens/analysis/` §2.1 |
| PlanSequence / PlanStep | plan (static), deployment (live) | collapsed, expanded | `screens/deployment-plan/` §4–§5, `handoff/deployment/` |
| LogViewer | deployment, application | loading, empty, live, paused, reconnecting, truncated | `screens/logs/` |
| MetricChart | line, bar (counts) | loading, no data, gap, stale | `screens/metrics/` §5 |
| DataTable | standard, dense | loading, empty, filtered-empty, error | `responsive/` §2 |
| TechnicalValue | id, sha, digest, path, url, command | truncated (middle/start/end), copied | accessibility §8, interactions §4 |
| SecretField / SecretOnceField | write-only, one-time reveal | empty, set, replacing | `handoff/deployment/` §4, `components/system/` §6 |
| Sidebar, ContextBar, PageHeader, Breadcrumbs, SetupStepper, DeploymentTabs, ActivityDrawer | per shell | per shell | `handoff/shell/` |
| System components (Skeleton, EmptyState, InlineNotice, PageBanner, ErrorPanel, StaleMarker, ReconnectingChip, SystemStatePage, Toast, ConfirmDialog) | per spec | per spec | `components/system/` |

## 4. API → UI Semantics Map

| API field / state | UI semantic | Spec |
|---|---|---|
| Deployment `status` (PENDING…LIVE/FAILED) | StatusPill + L1 sentence | deployment-progress §1.2 |
| Step `name` (BUILD…VERIFY) | PlanStep label mapping; unknown raw | deployment-plan §4 |
| Step `status` | StepIndicator | deployment-progress §1.3 |
| Health `status`, `http.statusCode`, `latencyMs` | Health proof line | deployment-success §4 |
| Error `code` (API §18) | ErrorPanel / failure explanation | components/system §3.3, deployment-failure §5 |
| Profile fields + `confidence` | field rows + ConfidenceBadge + ProvenanceTag | application-profile §4 |
| Plan `status`, `steps`, `healthCheck` | plan validity, sequence | deployment-plan §7 |
| SSE events | realtime algorithm | interactions §6 |
| Opaque IDs (`dep_…`) | TechnicalValue, never parsed | api-contract §3 |

## 5. Contract Gap Register (backend dependencies)

These are **not design decisions left open**; the design specifies behavior both with and without the data. UI elements depending on a gap are not rendered until the contract exists.

| Gap | UI impact | Owner | Severity for #120 |
|---|---|---|---|
| `environment` on plans/deployments + filtering | env context everywhere | #71 / #117 / #97 | **HIGH (blocking)** |
| Write-only configuration values API | Configure secrets | #117 / #126 | **HIGH (blocking for apps needing env vars)** |
| Plan invalid status, field errors, fingerprint, rollback metadata, per-step detail | Plan states, failure boundary | #60 / #97 | MEDIUM |
| Application profile keys (repository, ref, runtime version, health, config requirements) | Profile fields | #95 | MEDIUM |
| Evidence records, ambiguity candidates, analysis stages | Analysis/Profile detail | #94 | MEDIUM |
| `CANCELLED`, `cancellable`, SSE event IDs | Progress cancel/reconnect | #65 / #118 | MEDIUM |
| Server state/capabilities/eligibility reasons | Server Selection | #78 / #79 | MEDIUM |
| Runtime logs, metrics query, domain status | Logs/Metrics/Domains | #86 / #87 / #64 | LOW for V0.1 journey (fallbacks specified) |
| Rollback, sessions, API tokens, audit feed | actions/settings sections | #65 / #100 / #125 / #126 / #128 | LOW (not rendered) |

Each item is also listed in the "Contract Gaps" section of the relevant screen doc.

## 6. Prohibited Implementation Shortcuts

The frontend agent must not:

1. **Invent backend endpoints**, fields or query parameters. Use only `docs/architecture/api-contract.md`; anything else waits for the gap owner.
2. **Invent deployment states or steps.** Render unknown values raw.
3. **Invent infrastructure capabilities** or eligibility rules client-side.
4. **Call Docker, Traefik, Runtime Agents, servers or databases from the browser.** All data via the Engine `/api/v1`.
5. Replace validated components with generic SaaS kits or default library styling (a headless library is acceptable if styled by tokens).
6. Introduce screen-specific colors, spacing or radii bypassing tokens; no hard-coded hex values outside `tokens/axiom.css`.
7. Render optimistic Engine-owned state (interactions §2).
8. Store secrets/tokens in browser storage, URLs, logs or analytics.
9. Communicate status by color alone.
10. Use internal component names in navigation labels.
11. Hide context (application/environment) at any breakpoint.
12. Parse error messages or logs to derive causes.

## 7. Screenshot References

No visual asset is validated yet. Per #142, Stitch V1 is archived as exploration and V2 outputs are linked here **only after validation** against this contract. Until then, the specifications in `docs/design/screens/**` (including ASCII layouts) are the only valid references.

| Screen | Validated asset |
|---|---|
| all | _pending V2 validation (#142)_ |

## 8. Design Issue Register

| ID | Severity | Issue | Resolution |
|---|---|---|---|
| D-1 | MEDIUM | Dashboard had no dedicated spec beyond shell/system states | Resolved below (§9) |
| D-2 | HIGH | Focus ring and control borders below WCAG non-text contrast | Resolved — accessibility A2/A3, tokens v1.0.1 |
| D-3 | MEDIUM | Visual prompt 09 listed non-contract plan steps ("Prepare", "Build Image") | Resolved — plan renders `steps[]`; prompt aligned in #142 |
| D-4 | MEDIUM | Visual prompts listed Rollback actions without contract support | Resolved — rendered only when contract exists |
| D-5 | LOW | Workspace switcher without workspace resource | Resolved — display-only in V0.1 |

**No unresolved CRITICAL/HIGH design issues remain.** Open HIGH items in §5 are backend contract dependencies, tracked by their owners.

## 9. Dashboard Spec (resolves D-1)

- Variant **W**, route `/dashboard`, primary action **New application**.
- Order: (1) in-progress deployments (Progress cards: app, env chip, current step, elapsed); (2) attention list — failed latest deployments, failing health, offline servers, GitHub needs attention (each `InlineNotice` with link); (3) applications grid — name, Production status pill + URL, last deployment age, env chips for other environments; (4) servers summary strip ("3 servers · 2 ready · 1 offline").
- Empty (first use): `EmptyState` "Deploy your first application" → Connect GitHub (if not connected) or Choose repository.
- Disclosure: L1 cards; L2 links; no technical identifiers on dashboard.
- Responsive: per `responsive/` §3 row 1. States: per `system-states/` §3 row 1.
- Data: `GET /applications`, deployments per application, `GET /servers`, `GET /github/connections`.

## 10. Frontend Readiness Checklist

- [ ] Tokens imported from `docs/design/tokens/axiom.css` (incl. v1.0.1).
- [ ] Route builders per `handoff/shell/` §5.
- [ ] Components in §3 implemented with all variants/states before screens.
- [ ] Tests: shell T1–T11, deployment D1–D9, accessibility checklist, responsive widths 375/768/1024/1440.
- [ ] Every gap-dependent element hidden until its contract lands.
