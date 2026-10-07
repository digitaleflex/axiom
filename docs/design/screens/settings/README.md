# Settings Screen v1.0

> Screen contract — issue #139.
>
> Depends on: Shell & Navigation (#133), System components (`docs/design/components/system/README.md`), GitHub screen (`docs/design/screens/github/README.md`).
> Contract sources: `docs/architecture/api-contract.md` §2 (authentication); #125 (authentication & session), #126 (secret protection), #127 (authorization & ownership), #128 (audit trail), #129 (threat model).

## 1. Purpose

Manage account, workspace, access and security. Settings are secondary to deployment work and are organized so that security-sensitive operations are isolated and unmistakable.

## 2. Shell

Variant **I**, sidebar "Settings", route `/settings/:section` (default `general`). Section nav is a vertical list inside content (desktop) / select (mobile). Each section has its own **Save** shown only when dirty (shell matrix #20); leaving with unsaved changes prompts.

## 3. Sections (#139)

| Section | Route | Content | V0.1 status |
|---|---|---|---|
| General | `general` | workspace name, default environment for new apps, timezone display (local/UTC) | workspace display-only (navigation §9) |
| Account | `account` | name, email, avatar; sign out | contract `GET /auth/me` |
| GitHub | `github` | embeds GitHub connection list (same component as `/github`) | available |
| Authentication & sessions | `sessions` | current session, other active sessions (device, location if provided, last active), sign out other sessions | **gap** #125 |
| API access | `api` | personal API tokens: name, scope, created, last used, expiry; create / revoke | **gap** — rendered only when contract exists |
| Notifications | `notifications` | which events notify (deployment LIVE / FAILED, server offline, GitHub disconnected) and where (in-app; email if supported) | in-app only until contract |
| Security | `security` | security overview: recent security events from audit trail (sign-ins, token creation, credential rotation, failed auth), 2FA if supported | **gap** #128 |
| Infrastructure | `infrastructure` | server registration defaults, agent minimum version notice; links to Servers | links only in V0.1 |
| Advanced | `advanced` | danger zone: delete application(s), leave/delete workspace (when supported) | per contract |

Terminology follows the security contracts: "Session", "Sign out", "API token", "Revoke", "Audit log" — no provider-specific identity terms (API §2: identity provider is an implementation detail).

Sections whose backend contract does not exist are **not rendered** (not shown disabled), except Security which shows "Security events will appear here" only when #128 provides the feed.

## 4. Security-Sensitive UI (#139)

| Rule | Implementation |
|---|---|
| Never reveal secrets/tokens | API token shown once on creation via `SecretOnceField`; list shows name, scope, last 4 chars **only if the contract provides a non-secret identifier**, otherwise just name |
| Destructive ops require explicit confirmation | `ConfirmDialog` destructive/typed (revoke token, sign out all sessions, delete application, disconnect GitHub) |
| Privileged actions show scope and target | `ConfirmDialog` privileged: e.g. "Create API token · Scope: deployments:write · Applies to: all applications in Acme" |
| Session/auth failures are clear | expired session → sign-in redirect with return path; failed re-auth for sensitive actions shows reason |
| Audit-relevant operations distinguishable | privileged actions carry an "Audited" tag (icon + text) next to the button and the dialog line "This action is recorded in the audit log." |
| Recent re-authentication | if the Engine requires step-up for sensitive actions, prompt inline before the dialog (**gap** #125) |
| UI visibility ≠ authorization | hidden controls are convenience only; every action handles `FORBIDDEN` (#127) |

## 5. Danger Zone

- Separate section at the bottom of Advanced, bordered with `--status-failed` at 40% opacity, title "Danger zone".
- Each item: what it does, what is lost, who is affected, a destructive button naming the target.
- Deleting an application with running deployments lists environments and running deployments and requires typing the application name; copy states whether running containers are stopped (per Engine contract).

## 6. States

Loading: section skeleton. Empty: "No API tokens yet — create one to use the Axiom API" / "No other active sessions". Save error: `InlineNotice` failed, input preserved. Saving: button loading. Forbidden: `InlineNotice` "Only workspace owners can change this."

## 7. Acceptance Checklist

- [ ] Nine sections organized per §3; unsupported contracts not rendered.
- [ ] Tokens revealed once only; never re-displayed.
- [ ] Destructive actions isolated in Danger zone with typed confirmation where scope is broad.
- [ ] Privileged actions show scope, target and "Audited".
- [ ] Terminology matches #125–#128.
