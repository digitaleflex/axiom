# Application Overview & Application Deployments Screens v1.0

> Screen contracts — issue #137.
>
> Depends on: Design DNA (#131), Progressive Disclosure (#132), Shell & Navigation (#133), Deployment state model (`docs/design/screens/deployment-progress/README.md` §1, #136).
> Contract sources: `docs/architecture/api-contract.md` §6, §11, §16, §17; #66, #102.
> Related: `docs/design/screens/logs/`, `docs/design/screens/metrics/`, `docs/design/screens/domains/`.

All application screens are **scoped to one application and one environment** (navigation §2). Switching environment keeps the section and clears deployment context (navigation §10).

---

## 1. Application Overview

### 1.1 Purpose & action

Operational home for one application in one environment: is it live, where, which version, is it healthy.

Primary action: **Deploy** → `/apps/:id/setup/configure` prefilled for the current environment.

### 1.2 Shell

Variant **A**, sidebar "Overview", route `/apps/:applicationId/:environment/overview`. Environment chip switchable.

### 1.3 Layout & priority

Content order follows #137 priority:

```
Title:  acme-web  ■ Production  ● Live                         [Deploy ▸]
Meta:   acme/web ↗ · Next.js · srv-eu-1

┌ Status (L1) ────────────────────────────────────────────────────────────┐
│ ● Live at https://app.acme.dev  [Copy] [Open ↗]                          │
│ Current deployment #42 · 3f9c2a1 "fix: header overflow" · 2h ago by jane │
│ Health  ● Healthy · GET / → 200 in 84 ms · checked 30s ago               │
└──────────────────────────────────────────────────────────────────────────┘
┌ Runtime (L2) ─────────────────┐ ┌ Resources (L2) — last 1h ─────────────┐
│ Running · port 3000            │ │ CPU    12 %   ▁▂▂▃▂▁                  │
│ Container image · Node 20      │ │ Memory 312 MB / 1 GB                  │
│ Server srv-eu-1 ● READY        │ │ [View metrics]                        │
└────────────────────────────────┘ └────────────────────────────────────────┘
┌ Domains (L2) ──────────────────┐ ┌ Recent deployments (L2) ──────────────┐
│ app.acme.dev  Primary · HTTPS ✓│ │ ● #42 Live     3f9c2a1  3:12  2h ago   │
│ [Manage domains]               │ │ ⬣ #41 Failed   a71e0d9  0:48  5h ago   │
└────────────────────────────────┘ │ ○ #40 Superseded …       [All]         │
                                   └────────────────────────────────────────┘
```

### 1.4 Content

| Priority (#137) | Source | Level |
|---|---|---|
| Application identity | application name, repository, framework | L1 |
| Environment | chip | L1 |
| LIVE status | current deployment status for this environment | L1 |
| URL | primary domain / deployment URL | L1 |
| Current deployment | number, commit, age, initiator → link to deployment | L1 |
| Health | `GET /deployments/{current}/health` | L1 |
| Runtime summary | runtime status, strategy, port, server | L2 |
| Recent deployments | last 3–5 for this environment → link to deployment | L2 |
| High-level resource usage | CPU %, memory used/limit — sparkline, from metrics (§ metrics) | L2 |

### 1.5 Health vs status vs domain

Three independent signals, never merged into one indicator:

| Signal | Meaning | Where |
|---|---|---|
| Deployment status | outcome of the last deployment (LIVE/FAILED/…) | title pill |
| Health | is the running application responding now | Status card health line |
| Domain state | is the hostname routed with valid TLS | Domains card (see domains §3) |

Example: deployment LIVE, health WARNING "GET / → 503 since 14:20", domains OK — all shown side by side.

### 1.6 States

| State | Behavior |
|---|---|
| Not deployed in this environment | empty state: "acme-web isn't deployed to Staging yet" + why it matters + **Deploy to Staging** |
| Deployment in progress | Status card shows in-progress deployment with current step and link to Progress; previous LIVE still shown as "Currently serving #41" |
| Last deployment failed, previous still live | title pill shows serving state (LIVE, #41); WARNING strip "Deployment #42 failed at Verify" → Failure |
| Health failing | health line WARNING/FAILED with link to deployment Health tab |
| Server offline | Runtime card WARNING "Server srv-eu-1 is offline — status may be stale" |
| Loading | skeleton cards; title from URL |

---

## 2. Application Deployments

### 2.1 Purpose

Chronological history of deployments for the application in the current environment, with direct paths to logs and health.

Primary action: **Deploy**.

### 2.2 Shell

Variant **A**, sidebar "Deployments", route `/apps/:applicationId/:environment/deployments`. Query: `status`, `page`.

### 2.3 Table

| Column (#137) | Content | Mono |
|---|---|---|
| Status | pill (text + icon) | — |
| Deployment | `#42` + opaque ID on hover/copy | yes |
| Commit / ref | short SHA + message (truncated) + ref | SHA, ref |
| Environment | chip (abbreviated) — shown only when "All environments" filter is active | — |
| Target server | name | — |
| Duration | `m:ss`; running: ticking | yes |
| Started | relative; absolute on hover | yes |
| Initiator | avatar + name | — |
| Actions | row overflow (§2.4) | — |

- Sorted newest first; the currently serving deployment is marked "Serving" (text tag) even if not the newest.
- Row click → deployment (state-dependent landing tab, navigation §5.3).
- Filters: status (multi), environment scope ("This environment" default / "All environments" — the only place cross-environment data appears; scope shown in a visible filter chip).

### 2.4 Row actions

| Action | Target | Condition |
|---|---|---|
| Inspect | deployment Summary | always |
| View logs | deployment Logs tab | always |
| View health | deployment Health tab | deployment reached VERIFYING or later |
| Redeploy | regenerate plan from this deployment's inputs → Plan review | user authorized (#127); not for in-progress rows |
| Rollback | restore this deployment | **only if** rollback is in the contract (**gap**, #65 / #100); otherwise not rendered |

Actions the user is not authorized to perform are hidden, not disabled (authorization model #127), except where the absence would be confusing — then disabled with reason.

### 2.5 States

| State | Behavior |
|---|---|
| Empty | "No deployments in Production yet" + Deploy |
| Filtered empty | "No failed deployments" + Clear filters |
| Loading | 10 skeleton rows |
| Live updates | new deployments and status changes update rows in place (from events); a new top row is announced "New deployment #43 started" |

Responsive (< `--bp-md`): rows become two-line cards: status + #number + age / commit + duration; server and initiator move to the row detail.

---

## 3. Contract Mapping & Gaps

| UI need | Contract | Status |
|---|---|---|
| Deployments per application | `GET /applications/{id}/deployments` (`page`, `limit`, `status`) | available; **gap**: `environment` filter, server, initiator, duration fields |
| Current / serving deployment per environment | — | **gap** (#71 / #117) |
| Health of current deployment | `GET /deployments/{id}/health` | available |
| Resource summary | — | **gap**, see metrics §6 |
| Rollback | — | **gap** (#65 / #100) |

## 4. Acceptance Checklist

- [ ] Overview answers live? / where? / which version? / healthy? above the fold.
- [ ] Deployment status, health and domain state shown as distinct signals.
- [ ] Every deployment row links to its logs and health.
- [ ] Everything scoped to the selected environment unless the user opts into "All environments".
- [ ] Unauthorized or unsupported actions not rendered.
