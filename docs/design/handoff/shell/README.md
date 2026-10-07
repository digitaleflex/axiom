# Cloud Console Shell — Frontend Handoff v1.0

> Implementation contract for the Cloud Console shell — issue #133.
>
> Consumed by: #119 (Application Shell & Auth), #72 (Cloud Console — Vite + React), #120 (GitHub → Deploy Workflow).
>
> Normative sources (this document does not override them):
> - `docs/design/design-dna/README.md`
> - `docs/design/tokens/axiom.css`
> - `docs/design/ux/progressive-disclosure.md`
> - `docs/design/ux/navigation/README.md`
> - `docs/design/screens/shell/README.md`

## 1. Scope

This handoff defines **what** the frontend must implement for the shell and **which invariants** must be tested. It does not prescribe a component library. The final, design-complete frontend contract is #141; this document is the shell slice of it.

---

## 2. Layout Regions

| Region | Element | Landmark | Notes |
|---|---|---|---|
| Skip link | `<a href="#content">` | — | first focusable element |
| Sidebar | `<nav aria-label="Primary">` | navigation | 240px / 64px / drawer |
| Context bar | `<header>` | banner | sticky, 48px |
| Page header | inside `<main>` | — | breadcrumbs `<nav aria-label="Breadcrumb">` |
| Content | `<main id="content" tabindex="-1">` | main | receives focus after route change |
| Activity drawer | `<aside aria-label="Activity">` | complementary | modal on < `--bp-lg` |
| Toast region | `aria-live="polite"` container | — | failures use `role="alert"` |

Layout uses CSS grid: `grid-template-columns: var(--shell-sidebar-width) 1fr; grid-template-rows: 48px 1fr;`.

---

## 3. Tokens

The shell consumes only tokens from `docs/design/tokens/axiom.css`. The following **layout constants** are shell-local and must not be used to express semantics:

```css
:root {
  --shell-sidebar-width: 240px;
  --shell-sidebar-rail-width: 64px;
  --shell-contextbar-height: 48px;
  --shell-drawer-width: 400px;
}
```

| Use | Token |
|---|---|
| App background | `--surface-0` |
| Sidebar | `--surface-1` |
| Active nav item / hover | `--surface-2` |
| Drawers, menus, Production chip fill | `--surface-3` |
| Active location indicator, focus ring, nav progress bar | `--axiom-accent` (`--focus-ring-*` for focus) |
| Status pills, banners, toasts | `--status-*` only |
| Technical values (IDs, SHAs, timestamps) | mono family, `--text-sm` |

Forbidden: new color tokens for environments, using `--status-*` for environment, using `--axiom-accent` for status.

---

## 4. Components

| Component | Responsibility | Key props / API |
|---|---|---|
| `AppShell` | regions, responsive mode, skip link, focus management | `variant: 'workspace' \| 'setup' \| 'application' \| 'deployment' \| 'infrastructure'` |
| `Sidebar` | groups, active item, rail/drawer modes | derived from route; collapse preference persisted per user |
| `ContextBar` | workspace, application switcher, environment chip, global actions | derived from route |
| `ApplicationSwitcher` | searchable combobox of applications | ARIA combobox pattern |
| `EnvironmentChip` | single environment representation | `environment`, `state?: 'not-deployed'`, `locked?`, `size: 'sm' \| 'md'`, `abbreviated?` |
| `EnvironmentMenu` | switch environment | uses `EnvironmentChip` + `StatusPill` rows |
| `PageHeader` | breadcrumbs, title, status, actions, tabs/stepper, return affordance | `primaryAction` (max 1), `secondaryActions` (overflow after 2) |
| `Breadcrumbs` | context hierarchy | built from route, not history |
| `SetupStepper` | 5-step setup flow | `current`, `completed[]` |
| `DeploymentTabs` | Summary · Plan · Progress · Logs · Health · Runtime | links with `aria-current` |
| `ActivityDrawer` | workspace events | filter `all \| in-progress \| failed` |
| `ActiveDeploymentsIndicator` | in-progress count | hidden at 0 |
| `ToastRegion` | terminal/actionable notifications | suppression rule §7 |
| `ShellBanner` | engine unreachable, GitHub disconnected | WARNING semantic |

All components consume semantic tokens and the canonical status vocabulary (Design DNA §12).

---

## 5. Routing Contract

Implement the route table from `docs/design/ux/navigation/README.md` §5.1 exactly.

Requirements:

1. **Centralized route builders.** No component concatenates paths. Provide typed builders, e.g.:
   ```ts
   routes.app.logs({ applicationId, environment, query: { severity: 'error', range: '1h' } })
   routes.deployment({ applicationId, environment, deploymentId, tab: 'progress' })
   routes.deploymentPermalink({ deploymentId })
   ```
   This keeps the future `/w/:workspaceSlug` prefix a single change (navigation §9).
2. **Typed route params.** `environment` is the union `'production' | 'staging' | 'preview'`; any other value renders Not Found.
3. **Query state.** Filters/range listed in navigation §5.4 are read from and written to the URL. Filter changes use `replace`; drill-downs use `push`.
4. **Redirects** (`/`, `/apps/:id`, deployment without tab, `/deployments/:id`) use `replace`.
5. **Return target.** `?from=` accepts only same-origin relative paths that match a known route; anything else is ignored.

---

## 6. Context Resolution

Pseudocode for application/deployment routes:

```text
environment =
  route.params.environment
  ?? storage.lastEnvironment[applicationId]          // only when URL lacks it
  ?? (app.hasProductionDeployment ? 'production'
      : app.latestDeployment?.environment ?? 'production')

on resolved default → navigate(replace, canonicalUrl)
on route change     → storage.lastEnvironment[applicationId] = environment
on app change       → keep section; keep environment if it exists for new app; clear deployment
on env change       → keep section; clear deployment
```

Client storage is per user, holds only identifiers and preferences (last environment per application, sidebar collapsed), and never holds secrets or tokens.

---

## 7. Realtime & Notifications

- The shell subscribes to Engine events only (API contract §15). It never talks to Runtime Agents, Docker, Traefik or servers directly (V0.1 scope success criteria).
- Toast emission: on `LIVE`, `FAILED`, GitHub disconnected, server offline.
- Suppress a toast when the event's `deploymentId` equals the deployment currently on screen.
- On stream disconnect: show `ShellBanner` "Axiom is reconnecting…", mark live views as paused, reconnect with backoff, then refetch current resources to reconcile.

---

## 8. Accessibility

- Route change: move focus to `<main>` and update `document.title` as `{Page} · {Application} · {Environment} · Axiom` (omit missing parts).
- Active sidebar item and breadcrumb current page use `aria-current="page"`.
- `EnvironmentChip` accessible name includes the full environment name (e.g. "Environment: Production"); glyph is `aria-hidden`.
- Icon-only buttons have `aria-label`; unread counts are part of the label.
- Drawers/dialogs trap focus, close on Escape, restore focus to their trigger.
- `prefers-reduced-motion`: disable drawer slide and nav progress animation (instant state change).
- Visible focus: `outline: var(--focus-ring-width) solid var(--focus-ring-color); outline-offset: var(--focus-ring-offset);` (solid accent, ≥13:1). `--axiom-accent-focus` may only be an additional outer halo (accessibility §1.4 A2).

---

## 9. Test Requirements

Minimum automated coverage for #119 / #72:

| # | Test |
|---|---|
| T1 | Every route in navigation §5.1 renders the expected shell variant and active sidebar item. |
| T2 | Refresh on an Application Logs URL with `q`, `severity`, `range` restores identical filters, application and environment. |
| T3 | Unknown `:environment` renders Not Found; no redirect to Production. |
| T4 | Stored last environment is ignored when URL specifies an environment. |
| T5 | Switching environment from `/logs` stays on `/logs` and clears deployment context. |
| T6 | `/deployments/:id` redirects to canonical route; tab chosen by state per navigation §5.3. |
| T7 | Environment chip is visible at 375px, 768px, 1024px and 1440px on A and D screens. |
| T8 | No navigation label matches the forbidden vocabulary (navigation §4). |
| T9 | Toast suppressed for the deployment currently on screen; shown otherwise with environment chip. |
| T10 | Production destructive dialog requires typed application name. |
| T11 | Keyboard-only: skip link, sidebar, environment menu, activity drawer fully operable; axe reports no violations on shell. |

---

## 10. Open Dependencies

| Item | Owner | Blocking |
|---|---|---|
| Per-deployment `environment` field and filtering in API | #71 / #117 | environment chip data, §6 resolution |
| Human deployment number (`#42`) in deployment resource | #71 / #116 | breadcrumbs (fallback: short opaque ID) |
| Workspace-level event stream (beyond per-deployment SSE) | #118 | activity drawer, active-deployments indicator (fallback: subscribe per open in-progress deployment) |
| Workspace resource | post-V0.1 | workspace switcher (display-only in V0.1) |
