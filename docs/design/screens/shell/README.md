# Axiom Cloud Console Shell v1.0

> Canonical application shell shared by all Cloud Console screens — issue #133.
>
> Depends on:
> - `docs/design/design-dna/README.md` (#131)
> - `docs/design/ux/progressive-disclosure.md` (#132)
> - `docs/design/ux/navigation/README.md` (navigation model, routes, context rules)
>
> Implementation contract: `docs/design/handoff/shell/README.md`

## 1. Anatomy

```
┌───────────┬──────────────────────────────────────────────────────────┐
│           │ TOP CHROME                                               │
│           │ [Workspace ▾] / [Application ▾]  [■ Production ▾]   ◔ 🔔 ◯│
│  SIDEBAR  ├──────────────────────────────────────────────────────────┤
│           │ PAGE HEADER                                              │
│  Workspace│ Workspace › acme-web › [■ Production] › Deployments › #42 │
│  Applic.  │ Deployment #42      ● BUILDING         [Cancel] [Open ▸] │
│  Infra    │ Summary  Plan  Progress  Logs  Health  Runtime           │
│           ├──────────────────────────────────────────────────────────┤
│           │ CONTENT                                                  │
│  ─────    │                                                          │
│  Settings │                                                          │
│  [User]   │                                                          │
└───────────┴──────────────────────────────────────────────────────────┘
```

Regions, in DOM and focus order:

1. **Skip link** — "Skip to content".
2. **Sidebar** — primary navigation (`<nav aria-label="Primary">`).
3. **Top chrome** — context bar: workspace, application, environment, global actions.
4. **Page header** — breadcrumbs, title, status, page-level primary action, section tabs or setup stepper.
5. **Content** — `<main id="content">`.
6. **Overlays** — activity drawer, detail drawers, dialogs, toasts.

---

## 2. Sidebar

### 2.1 Structure

```
AXIOM

WORKSPACE
  Dashboard
  Repositories
  GitHub

APPLICATION          ← visible only when an application is in context
  acme-web ▾           (application name, opens application switcher)
  Overview
  Deployments
  Logs
  Metrics
  Domains

INFRASTRUCTURE
  Servers
  Domains

────────
  Settings
  [avatar] Jane Doe ▾
```

### 2.2 Rules

- Width: 240px expanded, 64px rail (§7).
- Surface: `--surface-1`, right border `--border-subtle`.
- Group labels: `--text-xs`, uppercase, `--text-tertiary`. Group labels are not interactive.
- Item: 32px height, `--space-3` horizontal padding, icon 16px + label `--text-md`.
- Active item: `--surface-2` background, `--text-primary`, 2px leading indicator in `--axiom-accent`, `aria-current="page"`. The accent marks *location*, never status.
- Inactive item: `--text-secondary`; hover `--surface-2`.
- The APPLICATION group appears when the route contains an application (application, deployment and setup areas). During setup only the application name is shown; the five sections are disabled until a first deployment exists, with tooltip "Available after the first deployment".
- Sidebar items carry no status colors. A single neutral count badge is allowed (e.g. GitHub "Reconnect" warning uses the WARNING semantic with icon + text, per Design DNA §4).
- No item is labelled after an internal component (navigation §4).

---

## 3. Top Chrome (Context Bar)

Height 48px, `--surface-0`, bottom border `--border-subtle`. Sticky.

### 3.1 Left — context path

`[Workspace ▾] / [Application ▾] / [Environment chip ▾]`

| Control | Behavior |
|---|---|
| Workspace | Display-only in V0.1 (navigation §9). Name + avatar. |
| Application | Combobox listing applications with search; each row shows name, repository (`owner/repo` in mono) and the Production status pill. Selecting keeps the current section and environment if that environment exists, else applies the environment default. Hidden outside application context. |
| Environment | Environment chip with menu (§4). Hidden outside application context. |

### 3.2 Right — global actions

| Control | Behavior |
|---|---|
| Active deployments indicator | Visible only when ≥1 deployment is in progress in the workspace. Shows BUILDING semantic icon + count ("2 deploying"). Opens activity drawer filtered to in-progress. |
| Activity (bell) | Opens activity drawer (§5). Unread count badge. Icon-only, `aria-label="Activity, 3 unread"`. |
| New application | Secondary button "New application" → `/repositories`. Hidden below `--bp-md`. |
| User menu | Avatar; opens Profile, Settings, Sign out. |

The context bar never contains a page-level primary action; that belongs to the page header so each screen has exactly one dominant action (progressive-disclosure §5).

---

## 4. Environment Chip

The environment chip is the single visual representation of environment everywhere: context bar, breadcrumbs, deployment rows, confirmations, toasts.

### 4.1 Encoding (no new colors)

Environment is not a status and must not reuse status colors. It is encoded by **text + glyph shape + border style**, using existing neutral tokens only.

| Environment | Glyph | Chip treatment |
|---|---|---|
| Production | ■ filled square | `--surface-3` fill, `--border-strong` solid 1px, `--text-primary`, label weight 600 |
| Staging | ◧ half-filled square | transparent fill, `--border-default` solid 1px, `--text-primary` |
| Preview | □ hollow square | transparent fill, `--border-default` dashed 1px, `--text-secondary`, optional suffix "Ephemeral" |

- Label is always visible text ("Production"), never abbreviated in the context bar. In dense tables the abbreviations `PROD`/`STG`/`PREV` are allowed with full name in `title`/accessible name.
- Height 24px, `--radius-sm`, `--text-sm`.
- When the requested environment has no deployment, the chip shows the environment with a trailing "· not deployed" in `--text-tertiary`.

### 4.2 Environment menu

- Lists Production, Staging, Preview in that fixed order.
- Each row: chip + current deployment status pill (LIVE / BUILDING / FAILED / "Not deployed").
- Selecting navigates per navigation §10 (keep section, clear deployment).
- Keyboard: opens with Enter/Space/ArrowDown; arrow keys move; Escape closes and returns focus.

### 4.3 Production safeguards

On application and deployment screens in Production:

- The page header title row shows the chip inline next to the title.
- Destructive dialogs (cancel deployment, remove domain, rollback) restate "Production" in the title and require typing the application name.

---

## 5. Notifications & Activity

### 5.1 Activity drawer

- Right-side drawer, 400px, `--surface-3`, opened from bell or active-deployments indicator.
- Lists recent workspace events, newest first: deployment state transitions, GitHub connection changes, server liveness changes.
- Each entry: status icon + text, application, environment chip, relative time (mono, absolute time on hover), link to the canonical route.
- Filters: All / In progress / Failed.
- Fed by the Engine event stream; no polling of runtime components.

### 5.2 Toasts

- Shown only for **terminal or actionable** events relevant to the user: deployment LIVE, deployment FAILED, GitHub disconnected, server offline.
- Not shown for intermediate steps (those live in Progress and the activity drawer).
- Not shown for an event whose deployment is the one currently open on screen (the page already shows it).
- Bottom-right, max 3 stacked, auto-dismiss 6s for success, persistent for failure until dismissed.
- Announced via `aria-live="polite"`; failures via `role="alert"`.
- Always contain application + environment chip and a link ("View deployment").

---

## 6. Page Header

Sits at the top of content, `--space-6` padding, separated by `--border-subtle`.

```
Breadcrumbs
Title  [Env chip]  [Status pill]                 [Secondary] [Primary]
Meta line (mono where technical): ref · commit · updated 2m ago
Tabs  |  or  Setup stepper
```

| Element | Rule |
|---|---|
| Breadcrumbs | navigation §8 |
| Title | `--text-2xl`, weight 600 |
| Status pill | Design DNA §4; only when the page represents a stateful object |
| Primary action | exactly one; `Primary Button` |
| Secondary actions | max 2 visible; more go into an overflow menu |
| Meta line | `--text-sm`, `--text-secondary`; technical values in mono with copy |
| Tabs | Application sections (Overview…Domains) or Deployment tabs (Summary…Runtime); underline indicator in `--axiom-accent`; `role="tablist"` semantics only if content swaps in place, otherwise links with `aria-current` |
| Setup stepper | 5 steps Analyze · Profile · Server · Configure · Plan; completed steps are links; future steps disabled |
| Return affordance | `← Back to Deployment #42 · Production` when `?from=` is present (navigation §7) |

Application area: the Application section tabs are rendered **in the sidebar**, not duplicated in the page header. Deployment area: deployment tabs render in the page header.

---

## 7. Responsive Behavior

| Viewport | Sidebar | Context bar | Page header |
|---|---|---|---|
| ≥ `--bp-xl` (1280) | expanded 240px; user may collapse to rail (persisted per user) | full | full |
| `--bp-lg`–`--bp-xl` (1024–1279) | rail 64px, icons + tooltips, group labels hidden; expands as overlay on hover/focus | full | full |
| `--bp-md`–`--bp-lg` (768–1023) | hidden; menu button in context bar opens it as a modal drawer | workspace collapses to avatar; app + env remain | secondary actions collapse to overflow |
| < `--bp-md` (768) | modal drawer | menu · application (truncated) · environment chip · bell | breadcrumbs collapse to parent link "‹ acme-web"; tabs become horizontally scrollable; primary action full-width below title |

Invariants at every size:

- **The environment chip is never hidden** on application and deployment screens.
- The application name is never hidden (may truncate with full name accessible).
- Touch targets ≥ 40×40px below `--bp-lg`.
- Logs and tables scroll horizontally inside their container; the shell never scrolls horizontally.
- Opening the sidebar drawer traps focus; Escape closes it.

---

## 8. Shell Matrix — 20 V0.1 Screens

Shell variants:

- **W** Workspace — no application context
- **S** Setup — application context, stepper, environment set at step 4
- **A** Application — application + environment, sidebar sections
- **D** Deployment — application + environment + deployment, header tabs
- **I** Infrastructure — no application context (optional return affordance)

| # | Screen | Variant | Route | Env chip | Sidebar active | Header primary action |
|---|---|---|---|---|---|---|
| 1 | Dashboard | W | `/dashboard` | per-row only | Dashboard | New application |
| 2 | GitHub Connection | W | `/github` | — | GitHub | Connect GitHub |
| 3 | Repository List | W | `/repositories` | — | Repositories | Select repository (row) |
| 4 | Repository Detail | W | `/repositories/:id` | — | Repositories | Analyze |
| 5 | Repository Analysis | S (1) | `/apps/:id/setup/analysis` | not shown | app name | Continue |
| 6 | Application Profile | S (2) | `/apps/:id/setup/profile` | not shown | app name | Continue |
| 7 | Server Selection | S (3) | `/apps/:id/setup/server` | not shown | app name | Select server |
| 8 | Deployment Configuration | S (4) | `/apps/:id/setup/configure` | selected here; shown once chosen | app name | Review plan |
| 9 | Deployment Plan | S (5) | `/apps/:id/setup/plan/:planId` | shown, locked | app name | Deploy |
| 10 | Deployment Progress | D | `…/deployments/:depId/progress` | shown, locked | Deployments | Cancel (when permitted) |
| 11 | Deployment Success | D | `…/deployments/:depId/summary` (LIVE) | shown, locked | Deployments | Open application |
| 12 | Deployment Failure | D | `…/deployments/:depId/summary` (FAILED) | shown, locked | Deployments | Retry |
| 13 | Application Overview | A | `/apps/:id/:env/overview` | shown, switchable | Overview | Deploy |
| 14 | Application Deployments | A | `/apps/:id/:env/deployments` | shown, switchable | Deployments | Deploy |
| 15 | Application Logs | A | `/apps/:id/:env/logs` | shown, switchable | Logs | — (state-dominant: live log) |
| 16 | Application Metrics | A | `/apps/:id/:env/metrics` | shown, switchable | Metrics | — (state-dominant: health) |
| 17 | Application Domains | A | `/apps/:id/:env/domains` | shown, switchable | Domains | Add domain |
| 18 | Server Overview | I | `/servers` | — | Servers | Register server |
| 19 | Server Details | I | `/servers/:id` | — (return affordance shows env when present) | Servers | — (state-dominant: server health) |
| 20 | Settings | I | `/settings/:section` | — | Settings | Save (when dirty) |

"Locked" means the chip is visible but its menu is disabled: a deployment belongs to exactly one environment, so switching environment from a deployment navigates to that environment's Application Overview instead.

---

## 9. Shell States

| State | Shell behavior |
|---|---|
| Loading (initial) | Shell chrome renders immediately; context bar shows application/environment from the URL as skeleton labels; content shows screen skeleton. No full-page spinner. |
| Loading (navigation) | 2px top progress bar in `--axiom-accent`; previous content stays until next is ready (max 300ms before skeleton). |
| Empty | Shell unchanged; content shows empty state (progressive-disclosure §10). |
| Engine unreachable | Persistent banner under context bar, WARNING semantic: "Axiom is reconnecting… Data may be out of date." Live streams paused indicator. |
| GitHub disconnected | Banner on Workspace and Setup screens with "Reconnect GitHub" action. |
| Not found / forbidden | Shell with context bar (parents still valid) and the System State in content; breadcrumbs truncated at the last valid segment. |
| Session expired | Redirect to sign-in carrying the canonical return path. |

---

## 10. Acceptance Checklist

- [ ] All 20 screens map to one variant and one canonical route (§8).
- [ ] Application and environment are visible on every A, D and S(4–5) screen at all viewports.
- [ ] Environment encoding uses text + glyph + border; no new color tokens.
- [ ] Exactly one primary action per page header, never in the context bar.
- [ ] No navigation label names an internal component.
- [ ] Toasts limited to terminal/actionable events and always name the environment.
- [ ] Responsive table (§7) implemented with the stated invariants.
- [ ] Shell states (§9) defined for loading, offline, not found and session expiry.
