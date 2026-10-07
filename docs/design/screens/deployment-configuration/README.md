# Deployment Configuration Screen v1.0

> Screen contract — issue #135.
>
> Depends on: Design DNA (#131), Progressive Disclosure (#132), Shell & Navigation (#133), Application Profile (`docs/design/screens/application-profile/README.md`, #134).
> Contract sources: `docs/architecture/api-contract.md` §9–§10, #95 (profile), #97 (plan validation), #60 (planner).
> Handoff: `docs/design/handoff/deployment/README.md`.

## 1. Purpose & action

Collect the **inputs** the planner needs — target, environment, domain, runtime parameters, configuration values — with safe defaults visible, and nothing executed.

Primary action: **Review plan** → generates a plan and opens Deployment Plan.

Configuration never deploys. Only the Deploy action on the Plan screen starts a deployment.

## 2. Shell

- Variant **S**, setup step 4 "Configure"; route `/apps/:applicationId/setup/configure`.
- Same route is used for subsequent deployments (Deploy from Application Overview): form prefilled from the last plan for the current environment; steps 1–3 shown as completed links.
- Environment chip appears in the context bar **as soon as an environment is selected** (shell §8).
- Unsaved form state is kept in session storage per application so refresh does not lose input; secret values are **never** stored client-side (§5.4).

## 3. Layout

```
Breadcrumbs: Workspace › acme-web › Setup
Stepper:     ✓ Analyze  ✓ Profile  ✓ Server  ● Configure  ○ Plan
Title:       Configure deployment                         [Review plan ▸]

┌ Target (L1) ────────────────────────────────────────────────────────────┐
│ Environment   ( ■ Production )  ( ◧ Staging )  ( □ Preview )            │
│ Server        srv-eu-1 · ● READY · 4 vCPU · 8 GB          [Change]      │
│ Source        acme/web · main · 3f9c2a1                   [Change ref]  │
└──────────────────────────────────────────────────────────────────────────┘
┌ Access (L1) ────────────────────────────────────────────────────────────┐
│ Domain        [ app.acme.dev            ]  HTTPS · certificate automatic │
└──────────────────────────────────────────────────────────────────────────┘
┌ Configuration values (L1/L2) ───────────────────────────────────────────┐
│ DATABASE_URL       required   [ •••••••• set ]       Secret            │
│ NEXTAUTH_SECRET    required   [ Missing      ]       Secret            │
│ [+ Add variable]                                                         │
└──────────────────────────────────────────────────────────────────────────┘
┌ Runtime (L2) — from profile ────────────────────────────────────────────┐
│ Port 3000 (Default)   Health check GET / (Default)   Start pnpm start   │
└──────────────────────────────────────────────────────────────────────────┘
▸ Advanced options (L3)
```

## 4. Field Matrix

| Group | Field (#135) | Default | Source / provenance | Editable here | Required | Planner input |
|---|---|---|---|---|---|---|
| Target | Environment | none on first deployment (explicit choice); current environment on redeploy | user | yes | yes | **gap** — not in plan request |
| Target | Server | selection from step 3 | user | via Change → Server Selection (#138) | yes | `serverId` |
| Target | Repository / ref | ref analyzed in step 1 | user | via Change ref (triggers re-analysis if commit changed) | yes | `ref` |
| Access | Domain | Engine-provided default domain if offered; else empty | user / Engine | yes | per Engine policy | `domain` |
| Access | TLS | Automatic | Engine | no (status only) | — | derived |
| Config | Environment variables / secrets | names from profile configuration requirements | profile + user | values yes | required names must be set | **gap** |
| Runtime | Port | profile value | profile provenance tag | yes (writes profile Override) | yes | profile |
| Runtime | Health check | profile health strategy | profile provenance tag | yes (path, expected status) | yes | `healthCheck` (plan response) |
| Runtime | Build / start commands | profile values | profile | read-only here; "Edit in profile" link | — | profile |
| Advanced | Resources (CPU / memory limits) | none (unlimited within server) | user | only when server capability supports limits | no | **gap** |
| Advanced | Runtime options (restart policy, runtime version pin) | preset defaults | preset (#96) | only options exposed by the preset | no | **gap** |

Rules:

- Every default is visible and labelled "Default" or with its provenance tag; nothing is applied invisibly.
- Fields not exposed by the backend contract are **not rendered** (not disabled-and-invented). Rows marked *gap* appear only once the contract provides them.
- Port and health check edits are profile overrides; the UI says so ("Saved to profile as Override").

## 5. Interactions

### 5.1 Environment

- Three option cards using the environment chip (shell §4) with one-line meaning: Production "Serves your users", Staging "Pre-production validation", Preview "Temporary review build".
- Each card shows the current deployment status in that environment ("● LIVE · deployed 3d ago" / "Not deployed").
- Selecting Production when a LIVE deployment exists shows an inline notice: "This will replace the live Production deployment after verification succeeds." (copy subject to the plan's rollback boundary, see Plan §6).

### 5.2 Server

- Shows name, readiness (READY / DEGRADED / OFFLINE with status semantics), key capacity.
- DEGRADED: WARNING notice, allowed with explicit acknowledgement checkbox. OFFLINE: blocking error, Review plan disabled.
- Change returns to Server Selection with `?from=` and comes back with state preserved.

### 5.3 Domain

- Inline format validation (hostname, no scheme, no path).
- If the Engine reports DNS status: "DNS points to srv-eu-1 ✓" or WARNING "DNS not pointing to this server yet — the deployment can proceed, HTTPS will activate once DNS resolves." Wording follows the Engine's actual policy.
- Domain already used by another application/environment → blocking error naming the owner if the user can see it.

### 5.4 Configuration values (secrets boundary)

- Rows: name (mono), required/optional, value input, type (Secret / Plain).
- Required names come from the profile's configuration requirements; users may add variables.
- **Secret values are write-only**: once saved, shown as `•••••••• set` with Replace and Remove actions; never revealed, never prefilled, never stored in browser storage, never included in URLs, logs or plan views.
- Plain values may be displayed.
- Missing required values block Review plan, listed in the error summary.
- Paste of `.env` content supported: parsed into rows, preview before applying, values default to Secret.

### 5.5 Review plan

1. Client validation; on failure, an error summary at the top links to each invalid field (focus moves to the summary).
2. Persist configuration, then `POST /applications/{id}/deployment-plans`.
3. Button loading "Generating plan…".
4. On success navigate to `/apps/:id/setup/plan/:planId`.
5. On backend validation errors (#97: invalid port/domain, unsupported capability), map errors to fields; unmapped errors appear in the summary.

## 6. Disclosure

| Level | Content |
|---|---|
| L1 | environment, server, source, domain, missing required values, primary action |
| L2 | configuration values, port, health check, commands (read-only), server capacity |
| L3 | advanced options, resources, runtime options, full SHA |
| L4 | — (no infrastructure internals on this screen) |

## 7. States

| State | Behavior |
|---|---|
| Loading | form skeleton; actions disabled |
| Incomplete | Review plan enabled; pressing it shows the error summary (do not disable silently) |
| Generating plan | form read-only; button loading |
| Server became OFFLINE | blocking banner; Change server |
| Profile changed since analysis | banner "Profile updated — review before continuing" with link |
| Save / generate error | form preserved; error summary |

## 8. Accessibility

- Environment cards are a radio group (`fieldset` + `legend` "Environment").
- Error summary uses `role="alert"`, links move focus to fields; each field has `aria-invalid` + `aria-describedby`.
- Secret inputs `autocomplete="off"`, `type="password"` with an explicit "Show while typing" toggle that only affects the current unsaved input.

## 9. Acceptance Checklist

- [ ] All #135 configuration areas present or explicitly gated on contract availability.
- [ ] Every default visible with provenance.
- [ ] Environment is an explicit choice on first deployment.
- [ ] Secret values write-only end-to-end in the UI.
- [ ] Advanced options collapsed by default.
- [ ] Nothing is deployed from this screen.
