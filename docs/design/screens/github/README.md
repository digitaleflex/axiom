# GitHub Connection Screen v1.0

> Screen contract — issue #134.
>
> Depends on: Design DNA (#131), Progressive Disclosure (#132), Shell & Navigation (#133).
> Contract sources: `docs/architecture/api-contract.md` §4, #91 (GitHub App/OAuth connection boundary), #126 (token & secret protection).

## 1. Purpose & action

Connect GitHub so Axiom can read repositories, and make clear what Axiom can and cannot access.

Primary action: **Connect GitHub** (not connected) / **Browse repositories** (connected).

## 2. Shell

- Variant **W**, sidebar "GitHub", route `/github`.
- Return from the GitHub authorization callback lands on `/github?result=connected|denied|error`; the parameter is removed with `replace` after the result is shown.

## 3. Layout — connected

```
Breadcrumbs: Workspace › GitHub
Title:       GitHub                                     [Browse repositories ▸]

┌ Connection ───────────────────────────────────────────────────────────┐
│ (avatar) acme  · Organization        ● Connected                       │
│ Connected 12 Sep 2026 by jane · Repository access: 14 selected         │
│                                   [Manage access on GitHub ↗] [⋯ Disconnect] │
└────────────────────────────────────────────────────────────────────────┘
┌ What Axiom can access ─────────────────┐ ┌ What Axiom never does ────────┐
│ ✓ Read repository contents             │ │ ✕ Push code or open PRs        │
│ ✓ Read metadata (branches, tags)       │ │ ✕ Execute code during analysis │
│                                        │ │ ✕ Display or export tokens     │
└────────────────────────────────────────┘ └────────────────────────────────┘
[+ Connect another account]
```

## 4. Content

| Element | Content | Level |
|---|---|---|
| Account | avatar, login, type (User / Organization) | L1 |
| Status | Connected / Needs attention / Disconnected (status pill semantics: LIVE / WARNING / INACTIVE, always with text) | L1 |
| Repository access | "All repositories" or "N selected" + link to manage on GitHub | L2 |
| Permissions | human-readable list (read contents, read metadata) | L2 |
| Raw scopes / installation ID | mono, copyable | L3 |
| Connected at / by | date (mono on hover), user | L2 |

**The token is never displayed, partially or masked.** No "copy token" affordance exists (#126).

## 5. States

| State | Behavior | Primary action |
|---|---|---|
| Not connected | empty state: what connecting enables, the permission list, and "No code is executed during analysis" | **Connect GitHub** |
| Redirecting | button loading "Opening GitHub…"; full-page navigation to GitHub | — |
| Connected (callback) | success confirmation in-page: "acme is connected · 14 repositories available" | **Browse repositories** |
| Denied by user | neutral notice "GitHub access was not granted. Nothing was connected." | Connect GitHub |
| Callback error / invalid state | FAILED notice with normalized cause (#91), no technical stack | Try again |
| Needs attention | access revoked or suspended on GitHub; WARNING + impact ("New deployments for 3 applications are blocked") | **Reconnect** |
| Disconnected | account row INACTIVE | Connect GitHub |

## 6. Disconnect

Destructive dialog:

- Title "Disconnect acme?"
- Impact list: repositories no longer accessible; analyses and new deployments for listed applications blocked; **running applications keep running**.
- Confirmation by typing the account login.
- After success: toast + account row Disconnected.

## 7. Accessibility

- Status uses text + icon; permissions lists use ✓/✕ icons with text, icons `aria-hidden`.
- External links ("↗") announce "opens GitHub in a new tab".
- Callback result announced via polite live region on load.

## 8. Contract Mapping & Gaps

| UI need | Contract | Status |
|---|---|---|
| Connect | `POST /github/connections` | available |
| List connections | `GET /github/connections` | available; **gap**: needs account type, repository access mode/count, status (`active` / `needs_attention`), connectedAt/By |
| Disconnect | `DELETE /github/connections/{id}` | available |
| Applications impacted by a connection | — | **gap**: needed for Needs attention / Disconnect impact |

## 9. Acceptance Checklist

- [ ] Access and non-access are both stated explicitly.
- [ ] Token never rendered in any form.
- [ ] Every callback outcome (connected, denied, error) has a distinct state.
- [ ] Disconnect states that running applications are unaffected.
