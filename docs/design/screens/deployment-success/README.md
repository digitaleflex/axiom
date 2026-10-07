# Deployment Success Screen v1.0

> Screen contract — issue #136.
>
> Depends on: Deployment state model (`docs/design/screens/deployment-progress/README.md` §1), Design DNA (#131), Progressive Disclosure (#132), Shell & Navigation (#133).
> Contract sources: `docs/architecture/api-contract.md` §11, §13, §15, §16, #65, #66.

## 1. Purpose & action

Prove that the application is **live and healthy** — not merely built — and point to the next useful action.

Primary action: **Open application** (external, new tab).

## 2. Shell

- Variant **D**, tab **Summary** when status is `LIVE`; route `/apps/:applicationId/:environment/deployments/:deploymentId/summary`.
- Landing tab for LIVE deployments (navigation §5.3).
- Environment chip shown and locked.
- If this deployment is later superseded, the same URL renders the Summary with an INACTIVE "Superseded by #43" notice (no success hero).

## 3. Layout

```
Breadcrumbs: Workspace › acme-web › [■ Production] › Deployments › #42
Title:       Deployment #42  ■ Production  ● Live                [View logs] [Open application ↗]

┌ LIVE hero (L1) ─────────────────────────────────────────────────────────┐
│ ● Live at  https://app.acme.dev   [Copy]                                 │
│ Health check passed · GET / → 200 in 84 ms · checked 14:05:02            │
└──────────────────────────────────────────────────────────────────────────┘
┌ Deployment (L2) ──────────────┐ ┌ Runtime (L2) ─────────────────────────┐
│ Commit   3f9c2a1  fix: header…│ │ Server   srv-eu-1                      │
│ Ref      main                 │ │ Runtime  Running · port 3000            │
│ Duration 3:12                 │ │ Strategy Container image                │
│ Started  14:01:50 by jane     │ │ ▸ Technical details                     │
│ ID       dep_01J8…  [Copy]    │ └─────────────────────────────────────────┘
└───────────────────────────────┘
┌ Sequence (collapsed, L2) ✓ Build 1:12 · ✓ Create Runtime 0:09 · ✓ Configure Network 0:31 · ✓ Start 0:06 · ✓ Verify 0:14 ┐

Next:  [Add a custom domain]  [View metrics]  [Go to application overview]
```

## 4. Content

| Requirement (#136) | Source | Presentation | Level |
|---|---|---|---|
| LIVE state | deployment `status: LIVE` | pill + hero with `--status-live` icon + "Live" text | L1 |
| URL | `url` from status event / deployment | large link, copy button, opens new tab | L1 |
| Health result (proof) | `GET /deployments/{id}/health`: `status`, `http.statusCode`, `http.latencyMs`, check time | hero sub-line; values mono | L1 |
| Deployment ID | deployment resource | human number in title; `dep_…` mono + copy | L2 |
| Commit | deployment ref/commit | SHA mono + message | L2 |
| Duration | `startedAt` → `completedAt` | mono `m:ss` | L2 |
| Target runtime | health `runtime.status`, plan runtime strategy, port, server | Runtime card | L2 |
| Next useful actions | — | §5 | L1/L2 |
| Image, digest, container ID | runtime detail | inside "Technical details" | L3/L4 |

### Health is mandatory

The hero states health **only** from the health result. If the health endpoint is unavailable or returns a non-HEALTHY result after LIVE:

- Hero keeps "Live" (Engine status) but the health line becomes WARNING: "Health check now failing — GET / → 503" with link to Health tab.
- If no health result exists at all: "Health result unavailable" (WARNING) — never an implied pass.

## 5. Actions

| Action | Placement | Condition |
|---|---|---|
| **Open application** | header primary + hero link | always when URL present |
| View logs | header secondary → Application Logs filtered to this deployment | always |
| Add a custom domain | Next row → Application Domains | when only an Engine default domain is used |
| View metrics | Next row → Application Metrics | always |
| Go to application overview | Next row | always |
| Rollback | header overflow | **only if** the Engine exposes rollback for this deployment (**gap**, no rollback endpoint in API contract); otherwise not rendered |

## 6. First-deployment variant

When this is the first LIVE deployment of the application (end of the setup flow):

- Hero sub-copy adds: "acme-web is now running in Production."
- Stepper no longer shown (setup complete); APPLICATION sidebar sections become enabled (shell §2.2).
- Next row prioritizes "Go to application overview".

## 7. Accessibility

- Hero is a `region` labelled "Deployment result".
- External link announces "opens in a new tab".
- No celebratory animation; a single state transition fade respecting reduced motion.

## 8. Acceptance Checklist

- [ ] LIVE shown only from Engine status; health shown only from health result.
- [ ] URL, deployment ID, commit, duration, health, runtime, next actions all present.
- [ ] Missing/failing health never displayed as passed.
- [ ] Rollback not rendered unless supported by contract.
- [ ] Superseded deployments do not show the success hero.
