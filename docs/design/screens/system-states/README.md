# System States — Catalog & Coverage Matrix v1.0

> Full-page system states and per-screen state coverage — issue #139.
>
> Components: `docs/design/components/system/README.md`. Shell behavior: `docs/design/screens/shell/README.md` §9.

## 1. Full-Page States (`SystemStatePage`)

Rendered in the content area with the shell intact (context bar shows the last valid context, breadcrumbs truncated at the last valid segment).

| State | Trigger | Title / body | Recovery |
|---|---|---|---|
| Not found (404) | unknown route, unknown resource, unknown `:environment` | "We can't find that page." / "{object} doesn't exist or you may not have access." | Go to {parent} · Dashboard |
| Forbidden (403) | `FORBIDDEN`, `POLICY_DENIED` | "You don't have access to {object}." / who can grant it if known | Back · Dashboard |
| Unauthorized (401) | session missing/expired | not a page: redirect to sign-in with return path (#125) | Sign in |
| Server error (500) | `INTERNAL_ERROR` on the page's primary resource | "Something failed on Axiom's side." / request ID (mono, copy) | Retry · Status page |
| Service unavailable | Engine not ready | "Axiom is temporarily unavailable." / auto-retry countdown | Retry now |
| Maintenance | Engine maintenance signal | "Axiom is under maintenance until {time}." / "Your applications keep running." | Status page |
| Offline (client) | browser offline before first load | "You're offline." | Retry when connection returns (auto) |

Rule: a 404 for an unauthorized resource is acceptable (do not leak existence), but copy says "or you may not have access".

## 2. Domain-Specific States

| State | Where | Pattern | Recovery |
|---|---|---|---|
| GitHub disconnected | Workspace & Setup screens | `PageBanner` warning | Reconnect GitHub |
| No server available | Server Selection, Configure | `EmptyState` (no servers) / `InlineNotice` failed (none eligible) | Register server · reasons |
| Quota exceeded | create application / deploy | `InlineNotice` warning at the action + disabled action with reason | View limits (billing out of V0.1 scope: copy only) |
| Degraded server | Server screens, Overview runtime card | `InlineNotice` degraded | Server details |
| Partial failure | bulk operations, dashboard with some regions failing | `InlineNotice` partial listing succeeded / failed items; failed regions use `ErrorPanel` | Retry failed |
| Stale data | cards fed by heartbeats/metrics | `StaleMarker` | automatic |
| Reconnecting | Progress, Logs, activity drawer | `ReconnectingChip` | automatic |

## 3. Coverage Matrix — 20 Screens

L = loading, E = empty, F = failed region, S = stale/reconnecting, P = processing. Each cell names the specific state; "—" means not applicable.

| Screen | L | E | F | S / reconnect | P / other |
|---|---|---|---|---|---|
| Dashboard | skeleton cards | first-use: "Deploy your first application" | per-card `ErrorPanel` (partial) | stale server cards | in-progress deployments listed |
| GitHub Connection | skeleton account | not connected | callback error | — | redirecting |
| Repository List | skeleton rows | no connection / no access / no match | list error + Retry | stale when GitHub degraded | — |
| Repository Detail | header skeleton | empty repository | access lost | — | analyze submitting |
| Repository Analysis | stage skeleton | — | analysis failed | stage stream reconnecting | running stages |
| Application Profile | group skeleton | — | save error | analysis outdated | needs review, unsupported |
| Server Selection | row skeleton | no servers | list error | stale last-seen | — |
| Deployment Configuration | form skeleton | — | generate error | server became offline | generating plan |
| Deployment Plan | sequence skeleton | — | invalid plan | stale plan | — |
| Deployment Progress | header skeleton | — | not found | reconnecting / polling fallback | running |
| Deployment Success | hero skeleton | — | health unavailable | — | superseded |
| Deployment Failure | panel skeleton | — | — (is the failure state) | — | — |
| Application Overview | card skeleton | not deployed in env | health/metrics card error | stale server | deployment in progress |
| Application Deployments | row skeleton | none / filtered | list error | live row updates | — |
| Application Logs | line skeleton | no logs in range | load error | reconnecting + gap separator | truncated output |
| Application Metrics | chart skeleton | no data yet | query error | data gap annotation | — |
| Application Domains | row skeleton | no custom domain | list error | stale checks | DNS checking / TLS issuing |
| Server Overview | row skeleton | no servers | list error | stale rows | all offline banner |
| Server Details | section skeleton | — | section error | offline (stale data) | pending registration |
| Settings | section skeleton | no tokens / no sessions | save error | — | saving |

All screens additionally inherit: global offline, Engine unreachable, session expired, 403/404 (§1).

## 4. Copy Rules

- Lead with the effect on the user's application, not the internal cause ("Deployments can't start" before "agent disconnected").
- Use the application/environment/server names, never generic "resource".
- State durations and timestamps (mono).
- Never "Oops", never blame the user, never an unexplained error code at L1.

## 5. Acceptance Checklist

- [ ] Every screen has loading, empty (where meaningful) and failure states (§3).
- [ ] Every error state offers a next step.
- [ ] Full-page states keep the shell and last valid context.
- [ ] Domain-specific states use the shared components.
