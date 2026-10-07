# Application Domains Screen v1.0

> Screen contract — issue #137. Also covers the workspace-wide Infrastructure → Domains list (navigation §3.5).
>
> Depends on: Design DNA (#131), Progressive Disclosure (#132), Shell & Navigation (#133).
> Contract sources: `docs/architecture/api-contract.md` §17 (domains), #64 (networking & domains), #84 (network adapter).

## 1. Purpose & action

Manage the hostnames serving this application in this environment and make routing and certificate problems obvious.

Primary action: **Add domain**.

## 2. Shell

- Variant **A**, sidebar "Domains", route `/apps/:applicationId/:environment/domains`.
- Workspace list: variant **I**, route `/domains` — same table with an Application + Environment column, rows linking here.

## 3. Domain State Model

Domain state is **separate from application health** (#137). A domain row shows three independent checks, then a derived overall state.

| Check | Values | Meaning |
|---|---|---|
| DNS | Pointing ✓ / Not pointing / Checking / Unknown | hostname resolves to the target server |
| TLS certificate | Valid (expires in N days) / Issuing / Expiring soon (< 14 days) / Failed / Not started | HTTPS certificate state |
| Routing | Active / Pending / Failed | requests for the hostname reach the application |

| Overall | Rule | Semantic |
|---|---|---|
| Active | DNS ✓ + TLS valid + Routing active | `--status-live` + "Active" |
| Pending | any check in progress, none failed | `--status-building` + "Pending" |
| Action needed | DNS not pointing, or TLS expiring soon | `--status-warning` + "Action needed" |
| Failed | TLS failed or routing failed | `--status-failed` + "Failed" |
| Removing | delete in progress | `--status-inactive` + "Removing" |

Note: "Active" means the domain is routed with valid TLS; it says nothing about whether the application responds — that is health (application §1.5).

Label vocabulary: "Routing", "Certificate" — not "Traefik", "router", "Let's Encrypt" (API §17: these remain implementation details). Provider names may appear at L4 as labelled values (e.g. "Issuer: Let's Encrypt R11").

## 4. Layout

```
Title: Domains  ■ Production                                     [Add domain]

Domain               Overall          DNS            Certificate             Routing   
app.acme.dev  ★Primary ● Active       ✓ Pointing     ✓ Valid · 71 days       ✓ Active   ⋯
www.acme.dev          ▲ Action needed ✕ Not pointing ○ Waiting for DNS       ○ Pending  ⋯
  └ Point www.acme.dev to 203.0.113.10 (A record) or app.acme.dev (CNAME). [Copy] [Check again]
acme-web.axiom.run    ● Active        ✓ Managed      ✓ Valid                  ✓ Active     (default)
```

- Rows needing action expand automatically with an inline diagnostic and fix (§5).
- Primary domain marked with ★ + "Primary" text.
- Engine-provided default domain (if any) marked "default", cannot be removed.

## 5. Diagnostics (health / propagation)

| Situation | Inline diagnostic | Actions |
|---|---|---|
| DNS not pointing | expected record type/value (mono, copy) vs observed value; "DNS changes can take up to 48h" | Copy record, Check again |
| DNS checking | last check time, next check | Check again |
| TLS issuing | "Issuing certificate — usually under a minute once DNS is correct" | — |
| TLS failed | concise reason from Engine (e.g. "Validation request couldn't reach the server") + what to verify | Retry issuance (if supported) |
| TLS expiring soon | expiry date; renewal status | Renew (if supported) |
| Routing failed | "Requests for this hostname aren't reaching acme-web" + link to deployment Health | View deployment |

L3 drawer per domain: full check history (timestamps, results), certificate details (issuer, valid from/to, SANs — mono), routing target (`acme-web · Production · #42 · port 3000`). L4: internal routing identifiers.

## 6. Actions (safe configuration)

| Action | Behavior | Guard |
|---|---|---|
| Add domain | dialog: hostname input (validated: no scheme/path, valid hostname, not already used) → `POST /applications/{id}/domains`; then show DNS instructions immediately | conflict error names the owning application if visible to the user |
| Set as primary | marks primary (URL used across console) | **gap**: not in API |
| Remove | destructive confirm: "Remove app.acme.dev? Visitors using this address will get an error." Production primary domain: typed hostname confirmation | cannot remove default domain; removing the primary requires choosing a new primary first |
| Check again | triggers re-check | **gap**: not in API |

Unauthorized actions hidden (#127). All actions name the environment in confirmations.

## 7. States

| State | Behavior |
|---|---|
| No custom domain | default domain row + empty-state hint: "Add your own domain to serve acme-web at your address" |
| Not deployed | "Domains activate after the first deployment to Production"; Add domain still allowed (pending routing) if Engine permits |
| Loading | skeleton rows |
| Engine unreachable | shell banner; states marked stale |

## 8. Accessibility

- Each check is text + icon; overall state text + icon + color.
- Inline diagnostics associated to the row (`aria-describedby`); auto-expansion does not move focus.
- DNS record values in copyable mono fields with labelled copy buttons.

## 9. Contract Gaps

`docs/architecture/api-contract.md` §17 provides list/add/remove with `hostname` only. Needed:

| Need | Owner |
|---|---|
| Per-domain environment, DNS status (expected vs observed), TLS status/expiry/issuer, routing status | #64 / #84 / #117 |
| Primary flag + set-primary | #64 / #117 |
| Re-check / retry issuance endpoints | #64 |
| Engine default domain | #64 |
| Workspace-wide domain list | #117 |

## 10. Acceptance Checklist

- [ ] Domain state (DNS / Certificate / Routing) visually separate from application health.
- [ ] Problems expand inline with the exact fix.
- [ ] No component names in labels; providers only at L4.
- [ ] Destructive actions explain visitor impact and name the environment.
- [ ] Default domain cannot be removed; primary cannot be removed without replacement.
