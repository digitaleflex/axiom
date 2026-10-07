# Axiom Cloud Console Navigation Model v1.0

> UX contract for Axiom V0.1 — issue #133.
>
> Governing principle: **Application context first. Infrastructure detail on demand.**
>
> Depends on:
> - `docs/design/design-dna/README.md` (#131)
> - `docs/design/ux/progressive-disclosure.md` (#132)
>
> Companion documents:
> - `docs/design/screens/shell/README.md` — shell anatomy and per-screen matrix
> - `docs/design/handoff/shell/README.md` — frontend implementation contract

## 1. Purpose

Define one navigation model shared by every Cloud Console screen so that the user always knows:

1. which workspace they are in
2. which application they are looking at
3. which environment (Production / Staging / Preview) they are acting on
4. which deployment or server they drilled into, where applicable

and can return from any drill-down without losing that context.

---

## 2. Context Hierarchy

Context is ordered and nested. A lower level never exists without its parents.

| Order | Context | Scope | Source of truth |
|---|---|---|---|
| 1 | Workspace | every authenticated screen | URL (implicit in V0.1, see §9) |
| 2 | Application | application and deployment screens | URL path `applicationId` |
| 3 | Environment | application and deployment screens | URL path `environment` |
| 4 | Deployment | deployment screens | URL path `deploymentId` |
| 5 | Server | infrastructure screens; referenced from deployment Runtime | URL path `serverId` |

Rules:

- The URL is the only source of truth for context. Client storage may only provide a *default* when the URL does not specify a value (§6).
- Changing a parent context resets all child contexts (changing application clears deployment; changing environment clears deployment).
- Server context is a reference, not a parent: navigating from a deployment to its server opens infrastructure context but keeps a "Back to Deployment" return target (§7).

---

## 3. Navigation Areas

The console has five navigation areas. Each area has a fixed set of sections.

### 3.1 Workspace

Entry level, no application selected.

| Section | Screen |
|---|---|
| Dashboard | Dashboard |
| Repositories | Repository List, Repository Detail |
| GitHub | GitHub Connection |

### 3.2 Application Setup (GitHub → Deployable)

Linear flow for a newly created application. Not a sidebar section; rendered as a stepper inside the page header.

| Step | Screen |
|---|---|
| 1 Analyze | Repository Analysis |
| 2 Profile | Application Profile |
| 3 Server | Server Selection |
| 4 Configure | Deployment Configuration |
| 5 Plan | Deployment Plan |

After **Deploy** on step 5 the user lands in the Deployment area (Progress).

### 3.3 Application (required model)

| Section | Screen |
|---|---|
| Overview | Application Overview |
| Deployments | Application Deployments |
| Logs | Application Logs |
| Metrics | Application Metrics |
| Domains | Application Domains |

### 3.4 Deployment (required model)

Opened from a deployment row, a notification, the setup flow, or a deep link.

| Tab | Content | Default disclosure |
|---|---|---|
| Summary | state, ref/commit, environment, URL, outcome (Success / Failure screens render here) | L1 |
| Plan | the deployment plan that produced this deployment | L2 |
| Progress | step timeline Analyze → Build → Create Runtime → Configure Network → Start → Verify | L1/L2 |
| Logs | deployment logs scoped to this deployment | L2/L3 |
| Health | verification checks and results | L2 |
| Runtime | runtime, image, port, server reference | L3 (L4 on expansion) |

### 3.5 Infrastructure (required model)

| Section | Screen |
|---|---|
| Servers | Server Overview |
| Server detail | Server Details (child of Servers, not a sidebar item) |
| Domains | workspace-wide domain list (links to Application Domains) |
| Settings | Settings |

---

## 4. Vocabulary Rules

Navigation labels describe **user intent**, never internal coupling.

| Allowed label | Forbidden in navigation labels |
|---|---|
| Servers | Agents, Nodes, Hosts |
| Runtime | Containers, Docker |
| Domains | Traefik, Routers, Ingress |
| Deployments | Jobs, Tasks, Pipelines |
| Health | Probes, Readiness |
| Logs | Streams, SSE |

Internal terms (Runtime Agent, Docker, Traefik, container ID, digest) may appear as **content** at L3/L4 per the progressive-disclosure contract, always with a label and parent context. They never become sections, tabs, breadcrumbs or route segments.

Kubernetes, service-mesh and eBPF terminology never appears in navigation.

---

## 5. Route Scheme

Routes are human-readable, stable and deep-linkable. Every screen is reachable directly by URL.

### 5.1 Canonical routes

| Route | Screen |
|---|---|
| `/` | redirect → `/dashboard` |
| `/dashboard` | Dashboard |
| `/github` | GitHub Connection |
| `/repositories` | Repository List |
| `/repositories/:repositoryId` | Repository Detail |
| `/apps/:applicationId/setup/analysis` | Repository Analysis |
| `/apps/:applicationId/setup/profile` | Application Profile |
| `/apps/:applicationId/setup/server` | Server Selection |
| `/apps/:applicationId/setup/configure` | Deployment Configuration |
| `/apps/:applicationId/setup/plan/:planId` | Deployment Plan |
| `/apps/:applicationId` | redirect → `/apps/:applicationId/:environment/overview` (§6) |
| `/apps/:applicationId/:environment/overview` | Application Overview |
| `/apps/:applicationId/:environment/deployments` | Application Deployments |
| `/apps/:applicationId/:environment/logs` | Application Logs |
| `/apps/:applicationId/:environment/metrics` | Application Metrics |
| `/apps/:applicationId/:environment/domains` | Application Domains |
| `/apps/:applicationId/:environment/deployments/:deploymentId` | redirect → state-dependent tab (§5.3) |
| `/apps/:applicationId/:environment/deployments/:deploymentId/:tab` | Deployment (`summary`, `plan`, `progress`, `logs`, `health`, `runtime`) |
| `/servers` | Server Overview |
| `/servers/:serverId` | Server Details |
| `/domains` | Infrastructure Domains |
| `/settings` | Settings (sub-sections as `/settings/:section`) |

`:environment` ∈ `production` | `staging` | `preview`.

### 5.2 Short deployment links

`/deployments/:deploymentId` is a permalink used by notifications, toasts and copied links. It resolves the deployment, then redirects to the canonical route with application and environment filled in. If resolution fails, show the Not Found system state with a path back to Dashboard.

### 5.3 State-dependent deployment landing

When the tab is omitted, the landing tab depends on deployment state:

| Deployment state | Landing tab |
|---|---|
| PENDING, ANALYZING, PLANNING, BUILDING, DEPLOYING, VERIFYING | `progress` |
| LIVE | `summary` (Deployment Success) |
| FAILED | `summary` (Deployment Failure) |
| CANCELLED / INACTIVE | `summary` |

An explicit tab in the URL is always honored regardless of state.

### 5.4 View state in query parameters

Filters and view state that must survive refresh and sharing are encoded as query parameters, never only in memory.

| Screen | Parameters |
|---|---|
| Application Deployments | `status`, `page` |
| Application Logs / Deployment Logs | `q`, `severity`, `range`, `step`, `follow` |
| Application Metrics | `range`, `metric` |
| Repository List | `q`, `connection` |
| Server Overview | `status` |

`range` uses relative tokens (`15m`, `1h`, `24h`, `7d`) or absolute ISO-8601 `from`/`to`.

Parameters that only affect the current session (e.g. a log line expanded, drawer scroll position) are not encoded.

---

## 6. Context Resolution & Persistence

### 6.1 Precedence

For each context level, the first available value wins:

1. URL path / query
2. Last-used value for this application (client storage, per user)
3. Product default

### 6.2 Defaults

| Context | Default |
|---|---|
| Environment | `production` if the application has a Production deployment; otherwise the environment of the most recent deployment; otherwise `production` |
| Logs/metrics range | `1h` |
| Deployment tab | §5.3 |

### 6.3 Invariants

- A value from client storage is **never** applied silently when the URL already specifies a value.
- When a default is applied, the URL is updated with `replace` (not `push`) so Back does not loop.
- Refresh on any canonical URL renders the same screen with the same application, environment, deployment, filters and range.
- An unknown `:environment` value renders the Not Found state, never a silent fallback to Production.
- An application with no deployment in the requested environment renders the Application screen's empty state for that environment (what is missing, why it matters, next action), not a redirect.

---

## 7. Drill-down & Return

Drill-down is reversible (progressive-disclosure §11).

```
Application (env)
  ↓  Deployments row
Deployment (tab)
  ↓  Runtime → server reference
Server Details
```

Rules:

- Every drill-down is a real navigation (`push`), so browser Back returns to the exact previous view including filters.
- Crossing from Deployment to Server Details shows a return affordance in the page header: `← Back to Deployment #42 · Production`. The return target is carried as `?from=` containing the originating canonical path (same-origin paths only).
- Drawers (L3/L4 detail) are **not** navigations unless they hold a shareable object; drawers that represent an object (log entry, step detail) may add a query parameter (e.g. `?step=build`) so the view is shareable.
- Closing a drawer restores focus to the element that opened it.

---

## 8. Breadcrumbs

Breadcrumbs express context hierarchy, not browsing history.

| Area | Pattern |
|---|---|
| Workspace | `Workspace › Repositories › acme/web` |
| Setup | `Workspace › acme-web › Setup` (step shown in stepper, not breadcrumb) |
| Application | `Workspace › acme-web › [Production] › Logs` |
| Deployment | `Workspace › acme-web › [Production] › Deployments › #42` |
| Infrastructure | `Workspace › Servers › srv-eu-1` |

- The environment segment renders as the environment chip (screens/shell §4), not plain text.
- The last segment is the current page and is not a link (`aria-current="page"`).
- Deployment segments use the human deployment number when available; the opaque ID (`dep_…`) is available via copy in the Summary tab.
- On narrow viewports, intermediate segments collapse into an overflow menu; the application and environment segments never collapse.

---

## 9. Workspace in V0.1

The V0.1 API (`docs/architecture/api-contract.md`) has no workspace resource. Therefore:

- The workspace selector is rendered in display-only form (name + avatar) with no switching.
- Workspace is not encoded in routes in V0.1.
- When multi-workspace support is introduced, routes gain a `/w/:workspaceSlug` prefix; all route segments after it are unchanged. Implementations must centralize route construction so this is a single change.

---

## 10. Environment Model

| Environment | Meaning | Anchoring |
|---|---|---|
| Production | serves end users | strongest anchoring; destructive actions require typed confirmation including the environment name |
| Staging | pre-production validation | standard anchoring |
| Preview | ephemeral / review builds | lightest anchoring; may be marked "Ephemeral" |

Rules:

- Environment is persistently visible in the context bar on every application and deployment screen (screens/shell §4).
- Switching environment keeps the current section (`/logs` stays `/logs`) and clears deployment context.
- Every confirmation, toast and notification about an operation names the environment.
- Environment is never communicated by color alone.

### Contract gap

The current API contract does not expose an environment field on applications, plans or deployments. This document assumes the Engine will expose a per-deployment `environment` (`production` | `staging` | `preview`) and allow listing deployments filtered by it. This must be resolved in #71 / #117 before #119 / #120 implement the shell. Multiple concurrent preview environments per application are **out of scope for V0.1**.

---

## 11. Acceptance Criteria (from #133)

| Criterion | Satisfied by |
|---|---|
| User always knows current application/environment | §2, §8, §10; shell context bar |
| Production/Staging/Preview persistently anchored where relevant | §10; shell §4 |
| Navigation does not expose Engine/Agent/Docker coupling | §4 |
| Shell works across all 20 screens | §3, §5.1; shell §8 matrix |
| Navigation state survives deep links and refresh | §5, §6 |
| Responsive behavior specified | shell §7 |

## 12. Non-Negotiable Rules

1. **The URL is the source of truth for context.**
2. **Never act on an environment the user cannot see.**
3. **Never silently fall back to Production.**
4. **Never name a navigation item after an internal component.**
5. **Never lose filters, range or context on Back or refresh.**
