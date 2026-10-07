# Deployment Progress Screen v1.0

> Screen contract — issue #136. Also defines the **canonical deployment state model** reused by Deployment Success and Deployment Failure.
>
> Depends on: Design DNA (#131, §4, §13, §14), Progressive Disclosure (#132, §8–§10), Shell & Navigation (#133), Deployment Plan (`docs/design/screens/deployment-plan/README.md`, #135).
> Contract sources: `docs/architecture/api-contract.md` §11–§16, #65 (state machine), #66 (logs & events), #118 (SSE stream).
>
> Related: `docs/design/screens/deployment-success/README.md`, `docs/design/screens/deployment-failure/README.md`.

## 1. Canonical State Model

### 1.1 Two vocabularies, one screen

| Vocabulary | Values (contract) | Rendered as |
|---|---|---|
| Deployment **status** (API §12) | `PENDING`, `ANALYZING`, `PLANNING`, `BUILDING`, `DEPLOYING`, `VERIFYING`, `LIVE`, `FAILED` | status pill in page header (L1) |
| Plan **steps** (API §10, §13) | `BUILD`, `CREATE_RUNTIME`, `NETWORK`, `START`, `VERIFY` | execution sequence (shared `PlanSequence`, plan §4) |

The status is authoritative for the deployment outcome. Steps explain *where* the deployment is. The UI never derives the status from steps (e.g. all steps completed ≠ LIVE); LIVE is shown only when the Engine reports `LIVE`.

### 1.2 Status → presentation

| Status | Pill label | Semantic token | Icon | Copy (L1 sentence) |
|---|---|---|---|---|
| `PENDING` | Queued | `--status-queued` | clock | "Waiting to start" |
| `ANALYZING` | Analyzing | `--status-building` | spinner* | "Checking the repository at `{sha}`" |
| `PLANNING` | Planning | `--status-building` | spinner* | "Preparing the execution plan" |
| `BUILDING` | Building | `--status-building` | spinner* | "Building the application image" |
| `DEPLOYING` | Deploying | `--status-building` | spinner* | "Starting the application on `{server}`" |
| `VERIFYING` | Verifying | `--status-building` | spinner* | "Checking the application responds" |
| `LIVE` | Live | `--status-live` | filled circle-check | "Live at `{url}`" |
| `FAILED` | Failed | `--status-failed` | octagon-x | "Stopped at {step}: {short cause}" |
| `CANCELLED`† | Cancelled | `--status-inactive` | slashed circle | "Cancelled by {user}" |
| unknown | the raw value | `--status-inactive` | dot | "Status reported by Axiom: `{value}`" |

\* Spinner replaced by a static icon under `prefers-reduced-motion`.
† `CANCELLED` is not in the API §12 state machine although `POST /deployments/{id}/cancel` exists — **gap for #65 / #117**. Until defined, a cancelled deployment is rendered from whatever terminal status the Engine returns.

Every status is text + icon + color (Design DNA §4). The pill never appears without its label.

### 1.3 Step states

From `GET /deployments/{id}/steps` (`status`) and step events. Rendered with the Design DNA §13 vocabulary:

| Step status | Indicator | Text |
|---|---|---|
| queued (not started) | hollow circle, `--text-tertiary` | "Queued" |
| `RUNNING` | BUILDING semantic, animated ring | "In progress · 0:42" |
| `COMPLETED` | neutral check, `--text-secondary` | "Done · 1:12" |
| `FAILED` | FAILED semantic, octagon-x | "Failed · 0:08" |
| skipped | INACTIVE, dash | "Skipped" |
| cancelled | INACTIVE, slashed circle | "Cancelled" |

Completed steps use **neutral**, not `--status-live`: green is reserved for the deployment being LIVE.

### 1.4 Status ↔ step correspondence (presentation only)

| Status | Steps typically active |
|---|---|
| `BUILDING` | `BUILD` |
| `DEPLOYING` | `CREATE_RUNTIME`, `NETWORK`, `START` |
| `VERIFYING` | `VERIFY` |
| `PENDING`, `ANALYZING`, `PLANNING` | none — the sequence shows all steps queued, the precondition row reflects the status |

Used only to place the "current" emphasis when step data lags behind a status event. Step data, when present, always wins.

---

## 2. Purpose & action

Let the user follow a running deployment and understand, without reading logs, what Axiom is doing right now.

Dominant element: **the current step** (state-dominant screen). Action: **Cancel** where the Engine permits.

## 3. Shell

- Variant **D**, tab **Progress**; route `/apps/:applicationId/:environment/deployments/:deploymentId/progress`.
- Landing tab for non-terminal statuses (navigation §5.3).
- Environment chip shown and locked.
- When the deployment reaches a terminal state while this tab is open, the page **stays on Progress** and shows the terminal banner (§6) with a link to Summary. No automatic navigation (the user may be reading logs).

## 4. Layout

```
Breadcrumbs: Workspace › acme-web › [■ Production] › Deployments › #42
Title:       Deployment #42  ■ Production  ◐ Deploying · 2:41          [Cancel]
Meta:        acme/web · main · 3f9c2a1 "fix: header overflow" · srv-eu-1 · started by jane 14:02:11

┌ Current step (L1, dominant) ────────────────────────────────────────────┐
│ ◐ Configure Network                                     step 3 of 5     │
│ Routing app.acme.dev with HTTPS · 0:18                                  │
│ Latest: "Certificate requested for app.acme.dev"                        │
└──────────────────────────────────────────────────────────────────────────┘

Sequence                                   Live events / logs
 ✓ Analyze        precondition              [Events | Logs]  [Follow ●] [Filter ▾]
 ✓ Build          Done · 1:12  ▸            14:03:20  BUILD completed
 ✓ Create Runtime Done · 0:09  ▸            14:03:29  CREATE_RUNTIME completed
 ◐ Configure Net. In progress · 0:18        14:03:31  NETWORK started
 ○ Start          Queued                    14:03:40  Certificate requested…
 ○ Verify         Queued
                                  → LIVE
```

- Desktop ≥ `--bp-lg`: sequence left (≈ 40%), feed right.
- Below `--bp-lg`: current step card, then sequence, then feed (collapsible, collapsed by default on < `--bp-md`).

## 5. Content

| Requirement (#136) | Presentation | Level |
|---|---|---|
| Current step visually dominant | current-step card: largest type in content, BUILDING semantic, step name, explanation (plan §4 template), step elapsed, latest event message | L1 |
| Overall state | status pill + elapsed total in title | L1 |
| Elapsed duration | total in title (`2:41`, mono, ticking each second from `startedAt`); per-step durations in sequence | L1/L2 |
| Deployment ID | human number in title; opaque `dep_…` in meta overflow (copy) | L2 / L3 |
| Repository / ref / commit | meta line, mono SHA + commit message (truncated) | L2 |
| Environment | chip in title and context bar | L1 |
| Target server | meta line, link to server (with `?from=`) | L2 |
| Completed steps inspectable | expanding a step shows its timestamps, events and a "View logs for this step" link (`?step=BUILD`) | L2/L3 |
| Live event / log feed | right panel, Events (default) or Logs | L2/L3 |
| Health verification | Verify step expanded automatically while running: check target, attempts, last result | L2 |
| Cancellation | Cancel button (§7) | L1 |
| Terminal state | banner + pill (§6) | L1 |

### 5.1 Events feed (default)

- One line per event from the stream: time (mono, `--text-tertiary`), step label, human message. Event types from API §14: state changes, step started/completed/failed, build output references, runtime events, health-check results, policy decisions, final outcome.
- Health-check results render as "Health check: GET / → 200 in 84 ms" (mono values).

### 5.2 Logs view

- Log viewer per Design DNA §14 / progressive-disclosure §6: monospace, timestamps, severity, search, step filter, copy, foldable stack traces.
- Query state: `q`, `severity`, `step`, `follow` (navigation §5.4).
- **Follow** pins to bottom; scrolling up pauses follow and shows "▼ N new lines" jump button.
- Secrets are redacted by the Engine (API §14); the UI renders redaction markers as-is, never attempts reconstruction.

## 6. Terminal Transitions

| Transition | On this tab |
|---|---|
| → `LIVE` | current-step card becomes a LIVE banner: "Live at app.acme.dev" + **Open application** + "View summary"; Verify step shows passing health result; no toast (shell §5.2 suppression) |
| → `FAILED` | card becomes FAILED banner: failed step + concise cause + **Inspect failure** (→ Summary/Failure) + Retry; failed step auto-expanded; logs filtered to failed step |
| → cancelled | INACTIVE banner: "Deployment cancelled at {step}" + "What still runs" line from rollback metadata |

Transitions are announced once via live region (§10). Elapsed timer stops at the Engine's `completedAt`, not at client receipt time.

## 7. Cancellation

- Visible while status is non-terminal **and** the Engine allows cancel. Allowed-ness should come from a `cancellable` flag (**gap**, #65 / #117). Fallback: show Cancel for non-terminal statuses and handle `409` with "This deployment can no longer be cancelled (now in {step})".
- Secondary/destructive style; never the visual hero.
- Confirmation dialog: "Cancel deployment #42?" + what happens (from rollback metadata, e.g. "The current Production deployment keeps serving."). Production requires typed application name.
- After confirm: button shows "Cancelling…", disabled; state updates only from Engine events.

## 8. Realtime & Reconnect Safety

Algorithm (implementation detail in `docs/design/handoff/shell/README.md` §7 and `docs/design/handoff/deployment/README.md`):

1. On mount: fetch snapshot `GET /deployments/{id}` + `GET /deployments/{id}/steps` + recent events; render.
2. Open SSE `GET /deployments/{id}/events/stream`.
3. Apply events idempotently: ignore events that would move a step or status backwards; terminal status is final.
4. On disconnect: show inline "Reconnecting…" chip on the feed header (WARNING semantic, text), keep last known state, ticking timer marked "~" (approximate).
5. On reconnect: refetch snapshot (step 1), then resume stream (with `Last-Event-ID` if the Engine supports event IDs — **gap**, #118).
6. If the stream fails repeatedly: fall back to polling the snapshot every 5s; feed header says "Live updates paused — refreshing every 5s".
7. Refresh / deep link at any moment renders the same state from the snapshot.

The UI never shows a step as completed or the deployment as LIVE based on client inference or timeouts.

## 9. States

| State | Behavior |
|---|---|
| Loading | header skeleton with known IDs from URL; sequence skeleton |
| `PENDING` | current-step card: "Queued — waiting for {reason if provided}"; all steps queued |
| `ANALYZING` / `PLANNING` | current-step card reflects status; precondition row active |
| Running steps | §4 |
| Reconnecting | §8 |
| Terminal | §6 |
| Not found / no access | System state in content (shell §9) |

## 10. Accessibility

- Sequence is an ordered list; current step `aria-current="step"`.
- Live region (polite) announces: step started, step completed, status changes; terminal LIVE/FAILED announced assertively once. Log lines are **not** announced.
- Timer has `aria-hidden` ticking text plus a static accessible label updated per step ("Deploying, elapsed 2 minutes 41 seconds" on focus).
- Feed is a `log` role region; follow toggle is a switch with label.

## 11. Acceptance Checklist

- [ ] Status pills and step indicators always include text + icon.
- [ ] Current step is the most prominent element; state readable without logs.
- [ ] Completed steps expandable with timestamps, events and step-filtered logs.
- [ ] Elapsed duration, deployment ID, repo/ref/commit, environment, server present.
- [ ] Cancel only where permitted; 409 handled.
- [ ] Refresh and reconnect reproduce the Engine state; no client-inferred LIVE.
- [ ] Unknown statuses/steps rendered raw, never dropped.
