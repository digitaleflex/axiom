# Deployment Failure Screen v1.0

> Screen contract — issue #136.
>
> Depends on: Deployment state model (`docs/design/screens/deployment-progress/README.md` §1), Deployment Plan failure boundaries (`docs/design/screens/deployment-plan/README.md` §6), Design DNA (#131), Progressive Disclosure (#132, §9 Failure Disclosure), Shell & Navigation (#133).
> Contract sources: `docs/architecture/api-contract.md` §11–§14, #65, #66, #117 (error contract).

## 1. Purpose & action

Explain **where** and **why** the deployment stopped, what is still running, and how to recover — without requiring the user to read raw logs.

Primary action: **Retry** when allowed; otherwise **Edit configuration**.

No generic failure banner: every failure screen names the failed step and a cause.

## 2. Shell

- Variant **D**, tab **Summary** when status is `FAILED`; route `/apps/:applicationId/:environment/deployments/:deploymentId/summary`.
- Landing tab for FAILED deployments (navigation §5.3). Toast "Deployment #42 failed" links here.
- Environment chip shown and locked.

## 3. Layout

```
Breadcrumbs: Workspace › acme-web › [■ Production] › Deployments › #42
Title:       Deployment #42  ■ Production  ⬣ Failed        [Edit configuration] [Retry ▸]

┌ Failure (L1) ───────────────────────────────────────────────────────────┐
│ ⬣ Stopped at Verify                                                      │
│ The application did not respond to the health check.                     │
│ GET / → connection refused after 5 attempts · exit code — · 14:05:44     │
│ ✓ The current Production deployment (#41) is still serving traffic.      │
└──────────────────────────────────────────────────────────────────────────┘
┌ What to check (L2) ─────────────────────────────────────────────────────┐
│ • The app listens on port 3000 (profile: Default). Is that correct?      │
│   [Edit port]                                                            │
│ • Start command: pnpm start                                              │
└──────────────────────────────────────────────────────────────────────────┘
┌ Sequence (L2) ✓ Build · ✓ Create Runtime · ✓ Configure Network · ✓ Start · ⬣ Verify ┐
┌ Relevant logs (L3) — step VERIFY, errors first ─────────── [Open full logs] ┐
│ 14:05:12 INFO   Server listening on http://0.0.0.0:8080                   │
│ 14:05:44 ERROR  Health check GET :3000/ → connection refused (5/5)        │
└──────────────────────────────────────────────────────────────────────────┘
```

## 4. Content

| Requirement (#136) | Source | Presentation | Level |
|---|---|---|---|
| Failed state | `status: FAILED` | pill + failure panel, FAILED semantic, octagon icon + "Failed" text | L1 |
| Failed phase | step with `status: FAILED` (`GET /deployments/{id}/steps`) | "Stopped at {step label}" | L1 |
| Concise explanation | Engine error `code` → human message (§5) | one sentence | L1 |
| Exit / error code | failed step or error payload | mono; shows "—" when absent, never omitted silently | L1/L3 |
| Timestamp | step `completedAt` / failure event | mono | L1 |
| Impact | plan rollback metadata (plan §6) | "still serving" / "nothing published" line | L1 |
| Actionable diagnostics | error code → checklist (§5) linking to the field to fix | L2 |
| Relevant logs | `GET /deployments/{id}/logs?step={FAILED_STEP}&level=error` (last 50 lines, then context) | log viewer excerpt, stack traces folded | L3 |
| Retry / redeploy | §6 | header primary | L1 |
| Configuration correction path | Edit configuration / Edit profile field deep links | L1/L2 |
| Rollback path | only if supported | §6 | L1 |
| Container/image IDs, raw probe data | runtime detail | "Technical details" | L4 |

### Disclosure order (progressive-disclosure §9)

L1 panel answers *where / why / impact / what next*. L2 gives the failed step context and checks. L3 shows logs and command/exit code. L4 holds identifiers and raw telemetry. Logs are never the first or only explanation.

## 5. Error Explanations

The explanation and checklist come from the Engine error contract (#117) — **the UI does not parse logs to guess a cause**.

Mapping based on the canonical error classes of `docs/architecture/api-contract.md` §18. The UI branches on `error.code` only; `error.message` may be shown as secondary detail, never parsed. `error.requestId` is shown at L3 (mono, copy) for support.

| Error code (API §18) | Typical step | L1 explanation | Checklist → correction path |
|---|---|---|---|
| `BUILD_FAILED` | Build | "The build failed (exit code {n})." | build command, package manager, lockfile → Edit profile; build config values → Edit configuration |
| `RUNTIME_FAILED` | Create Runtime / Start | "The application couldn't start (exit code {n})." | start command, port, required config values → Edit configuration; server capacity → Change server |
| `HEALTH_CHECK_FAILED` | Verify | "The application did not respond to the health check." | port, health path, start command |
| `DEPLOYMENT_NOT_ELIGIBLE` | before Build | "The selected server can't run this deployment." | server readiness/capabilities → Change server |
| `DEPLOYMENT_INVALID_STATE` | any | "This deployment was stopped because its state changed." | View deployments |
| `POLICY_DENIED` | any | "Axiom's security policy blocked this deployment." | `error.details` summary; contact workspace owner |
| `INTERNAL_ERROR` | any | "Axiom hit an internal error at {step}." | Retry; requestId for support |
| unknown code | any | "Axiom stopped this deployment at {step}." + raw code (mono) | Open full logs |

Network/domain failures currently have no dedicated error class; they surface as `RUNTIME_FAILED` or unknown until #64 / #117 add one. Finer-grained sub-causes (e.g. dependency install vs compile) require `error.details` keys from #117 and are not inferred by the UI.

Unknown codes are shown raw; the screen still names the step and impact.

## 6. Actions

| Action | Behavior | Condition |
|---|---|---|
| **Retry** | regenerate a plan from the same inputs (plan is single-use) → if fingerprint identical and READY, confirm then deploy; else open the new plan for review | failure is retryable (Engine flag — **gap**; fallback: always offered, plan validation decides) |
| Edit configuration | → `/apps/:id/setup/configure` prefilled with this deployment's inputs; field anchor from checklist | always |
| Edit profile field | → Profile with field focused | when checklist targets a profile field |
| Open full logs | → Logs tab `?step={step}&severity=error` | always |
| Rollback | restore previous LIVE deployment | **only if** supported by the contract (**gap**); when traffic was never switched, rollback is unnecessary and the impact line says so |

Production retry follows the Production deploy confirmation (plan §8).

## 7. Accessibility

- Failure panel uses `role="alert"` only on first live transition; on page load it is a labelled region (no repeated alerts on refresh).
- Error code and timestamps are text, copyable.
- Folded stack traces are buttons with `aria-expanded`.

## 8. Acceptance Checklist

- [ ] Failed step and concise cause visible without opening logs.
- [ ] Impact line states what is still running (from rollback metadata, or explicitly unknown).
- [ ] Exit/error code shown where available, "—" otherwise.
- [ ] Explanations sourced from Engine error codes, never log parsing.
- [ ] Retry, configuration correction and logs reachable; rollback only when supported.
- [ ] No generic "Something went wrong" banner.
