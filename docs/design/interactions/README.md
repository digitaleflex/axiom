# Interaction Rules v1.0

> Issue #140. Cross-screen interaction rules. Must not contradict API state authority: **the Engine is the authoritative owner of deployment, server and domain state** (API §12).
>
> Components: `docs/design/components/system/README.md`. Accessibility: `docs/design/accessibility/README.md`.

## 1. Loading & Processing Feedback

| Duration | Feedback |
|---|---|
| < 300ms | none (avoid flicker); button press state only |
| 300ms – 2s | button loading state (spinner + verb "Saving…") or region skeleton |
| > 2s known step | `ProcessingIndicator` naming the step |
| > 10s | add elapsed time and what happens if the user leaves ("You can leave — the deployment continues.") |

Buttons in loading state keep their width and are disabled to prevent duplicate submission.

## 2. Optimistic vs Authoritative State

| Category | Examples | Rule |
|---|---|---|
| **Authoritative only** (never optimistic) | deployment status, step status, LIVE, health, server state, domain DNS/TLS/routing, cancel result, plan validity, eligibility | UI shows "requested" state (e.g. "Cancelling…") until the Engine confirms via response/event. Never display a target state before confirmation. |
| **Optimistic allowed** (local, reversible, low risk) | UI preferences, sidebar collapse, log wrap, notification read state, filter changes | apply immediately; on failure revert and show `InlineNotice` |
| **Pessimistic with immediate echo** | rename server, save setting, add domain | input stays as entered with "Saving…"; on success mark saved; on failure keep input + error |

A 202 Accepted means *accepted*, not *done*: show the resource in its initial Engine state (e.g. `PENDING`), then follow events.

## 3. Confirmation Flows

| Operation class | Confirmation |
|---|---|
| Non-destructive, reversible | none |
| Generates a new artifact | none; result shown (e.g. new plan) |
| Starts execution (Deploy) | Staging/Preview: none beyond the Plan review; Production: typed confirmation |
| Destructive / visitor-impacting | `ConfirmDialog` destructive with impact list |
| Account-wide or Production destructive | typed confirmation |
| Privileged / security | privileged variant showing scope, target, "Audited" |

Never ask confirmation for something the user can undo trivially; never skip it for something they cannot.

## 4. Copy Interactions

- Copy buttons on all technical identifiers, URLs, DNS records, commands, log lines.
- Feedback: icon swaps to check + "Copied" for 1.5s; announced via polite live region ("Commit SHA copied").
- Copies the **full** value regardless of visual truncation; never includes line numbers or decorative prefixes.
- Secrets have no copy affordance after initial reveal.
- Clipboard failure: show "Couldn't copy — select the text manually" and select it.

## 5. Expand / Collapse

- Disclosure controls are buttons with `aria-expanded`; chevron rotates (no animation under reduced motion).
- State persistence: section expansion that reflects a shareable view (e.g. `?step=build`) goes in the URL; others persist per user per screen in local storage; L4 expanders always start collapsed.
- "Expand all / Collapse all" offered where ≥ 5 expandable items (plan steps, stack traces).
- Auto-expansion only for the item that needs attention (failed step, domain needing action); never steals focus.

## 6. Realtime & Reconnect

Canonical algorithm (deployment-progress §8):

1. Snapshot fetch → render.
2. Subscribe to SSE.
3. Apply events idempotently; ignore regressions; terminal states final.
4. On disconnect: keep state, show `ReconnectingChip`, mark timers approximate.
5. On reconnect: refetch snapshot, then resume.
6. Repeated failure: poll every 5s, labelled.

UI never advances state by timer or inference.

## 7. Stale Data

- Every polled/heartbeat-derived value has a freshness expectation; exceeding it shows `StaleMarker` with "as of {time}".
- Offline server data and disconnected-stream data are always marked stale.
- Stale data is never used to enable an action that depends on it (e.g. selecting a server whose READY state is stale requires refresh first).

## 8. Destructive-Operation Safeguards

1. Destructive actions never primary on a page; placed in overflow or Danger zone.
2. Button label = verb + target ("Remove app.acme.dev").
3. Dialog lists concrete impact and what is **not** affected ("Your running application keeps running").
4. Cancel is the default focus; Enter does not confirm destructive dialogs unless focus is on the confirm button.
5. Typed confirmation for Production or account-wide scope; comparison is exact, case-sensitive, trimmed.
6. Server-side authorization error (`FORBIDDEN`) handled in-dialog.
7. Success toast names the target; failure keeps the dialog open.

## 9. Acceptance Checklist

- [ ] No optimistic rendering of Engine-owned state.
- [ ] Feedback thresholds applied (§1).
- [ ] Confirmation matrix applied (§3).
- [ ] Copy always copies full value with announced feedback.
- [ ] Reconnect algorithm shared by all realtime views.
- [ ] Stale data marked and not used to enable dependent actions.
