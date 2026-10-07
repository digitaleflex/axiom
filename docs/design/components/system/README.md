# System State Components v1.0

> Reusable component contract for non-happy-path UI — issue #139.
>
> Depends on: Design DNA (#131), Progressive Disclosure (#132, §10), Shell (#133, §9).
> Used by: every Cloud Console screen. Screen docs reference these components instead of defining their own state UI.
> Catalog of full-page states: `docs/design/screens/system-states/README.md`.

## 1. Principles

1. **One component per state class.** Screens configure content; they never restyle states.
2. **Every non-success state answers: what happened, what it affects, what to do next.**
3. **Context is preserved.** States render inside the shell; parent context (application, environment) stays visible.
4. **Text + icon + color.** No state relies on color alone (Design DNA §4).
5. **No secrets, no raw stack traces at L1.** Technical detail (request ID, error code) is L3, copyable.

## 2. Component Set

| Component | States covered (#139) | Scope | Semantic |
|---|---|---|---|
| `Skeleton` | loading | region | neutral `--surface-2` blocks, no shimmer under reduced motion |
| `ProcessingIndicator` | processing | inline / region | `--status-building`, names the known step ("Building…"), never a bare spinner when the step is known |
| `EmptyState` | empty | region | neutral |
| `InlineNotice` | success, warning, degraded, failed, partial failure | inline within a section | per variant |
| `PageBanner` | degraded, offline, reconnecting, stale data (page-wide) | under context bar | per variant |
| `StaleMarker` | stale data | value / card | `--text-tertiary` + clock icon + "as of 14:02" |
| `ReconnectingChip` | reconnecting | live region headers (feeds, logs) | `--status-warning` |
| `ErrorPanel` | failed (region could not load) | region | `--status-failed` |
| `SystemStatePage` | unauthorized, forbidden, not found, server error, maintenance, service unavailable, offline | content area (shell kept) | per variant |
| `Toast` | success / failure notifications | global | shell §5.2 |
| `ConfirmDialog` | destructive / privileged confirmation | modal | §5 |

## 3. Anatomy & Props

### 3.1 EmptyState

```
[icon 24px]
Title — what is missing            ("No deployments in Staging yet")
Body — why it matters (1–2 lines)
[Primary action]  [Secondary link]
```

Props: `title`, `description`, `primaryAction?`, `secondaryAction?`, `variant: 'first-use' | 'no-results' | 'not-deployed'`. `no-results` always includes "Clear filters".

### 3.2 InlineNotice / PageBanner

```
[icon] Title. Short impact sentence.        [Action]  [×]
       ▸ Details (L3: code, requestId)
```

| Variant | Token | Icon | Dismissible |
|---|---|---|---|
| success | `--status-live` | check-circle | yes |
| info | `--text-secondary` | info | yes |
| warning | `--status-warning` | triangle | yes (re-appears if condition persists on next load) |
| degraded | `--status-warning` | half-circle | no while condition holds |
| failed | `--status-failed` | octagon | no |
| partial | `--status-warning` | split-square | no — lists what succeeded and what failed |

Background: token at ~10% opacity on `--surface-1`, 1px border at ~40%, text `--text-primary`. Never full-saturation fills.

### 3.3 ErrorPanel

Replaces a failed region only (rest of page keeps working). Content: "Couldn't load {object}" + cause from error code + **Retry** + L3 details (code, requestId, time). Branches on `error.code` (API §18), never on message.

| Error code | Default copy | Action |
|---|---|---|
| `UNAUTHORIZED` | session expired → handled globally (redirect, §4) | — |
| `FORBIDDEN` / `POLICY_DENIED` | "You don't have access to this {object}." | Back / Ask an owner |
| `NOT_FOUND` | "This {object} doesn't exist or was removed." | Back to parent |
| `RATE_LIMITED` | "Too many requests. Retrying in {n}s." | auto-retry countdown |
| `CONFLICT` / `DEPLOYMENT_INVALID_STATE` | "This changed while you were viewing it." | Refresh |
| `VALIDATION_FAILED` / `INVALID_REQUEST` | field-mapped errors (forms) | fix fields |
| `INTERNAL_ERROR` / network | "Axiom couldn't complete this request." | Retry |

### 3.4 StaleMarker & ReconnectingChip

- Stale: shown when data age exceeds the freshness expectation of its source (e.g. server metrics > 2× heartbeat interval) or when the realtime connection is down.
- Reconnecting: shows attempt state; after N failures switches to "Live updates paused — refreshing every 5s" (deployment-progress §8).

## 4. Global Handling

| Condition | Handling |
|---|---|
| `UNAUTHORIZED` on any request | redirect to sign-in with return path; toast after re-auth "Session expired — signed in again" |
| Browser offline | `PageBanner` offline: "You're offline. Showing data as of 14:02." Mutating actions disabled with reason |
| Engine unreachable | `PageBanner` degraded "Axiom is reconnecting…" (shell §9) |
| Maintenance (Engine signal) | `SystemStatePage` maintenance if blocking; banner if read-only mode |

## 5. Destructive & Privileged Confirmation

`ConfirmDialog` variants:

| Variant | When | Behavior |
|---|---|---|
| standard | reversible or low impact | title + impact + Cancel / Confirm |
| destructive | irreversible or user-visible impact (remove domain, disconnect GitHub, remove server, revoke session, delete application) | Destructive button style (`--status-failed` fill, text "Remove …" — never "OK"); impact list; Cancel is default focus |
| typed | Production impact or account-wide scope | destructive + type the exact target name to enable |
| privileged | security-relevant operations (rotate credential, create API token, change security settings) | shows **scope** (what it affects) and **target** (exact resource, mono ID) + "This action is recorded in the audit log." |

Rules:

- The action verb in the button names the operation and target ("Remove app.acme.dev").
- Destructive buttons are never the primary button on a page; they live in overflow menus or a dedicated "Danger zone" section.
- After success, a toast confirms and names the target; failure keeps the dialog open with the error.

## 6. Secrets in UI

- Secrets and tokens are never rendered after creation. One-time reveals (new API token, bootstrap token) use `SecretOnceField`: value + Copy + "You won't be able to see this again." + expiry; leaving the dialog requires "I've copied it".
- Masked placeholders (`•••• set`) never encode length or prefix of the real value.

## 7. Accessibility

- `InlineNotice` failed / `PageBanner` offline use `role="alert"` on first appearance only; others `role="status"`.
- `ProcessingIndicator` sets `aria-busy` on the region.
- `ConfirmDialog`: `role="alertdialog"` for destructive/typed; focus Cancel; Escape cancels.
- Skeletons are `aria-hidden`; the region announces "Loading {object}" once.

## 8. Acceptance Checklist

- [ ] All 14 #139 states map to a component above.
- [ ] Screens consume components; no per-screen state styling.
- [ ] Error copy branches on API error codes.
- [ ] Destructive/privileged dialogs show impact, scope and target; Production/account scope requires typing.
- [ ] One-time secret reveal pattern used for every token.
