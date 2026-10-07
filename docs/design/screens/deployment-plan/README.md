# Deployment Plan Screen v1.0

> Screen contract — issue #135.
>
> Depends on: Design DNA (#131, §13 Deployment Visualization), Progressive Disclosure (#132, §8 Deployment-Specific Disclosure), Shell & Navigation (#133), Deployment Configuration (`docs/design/screens/deployment-configuration/README.md`).
> Contract sources: `docs/architecture/api-contract.md` §10–§13, #60 (planner), #97 (plan validation & determinism).
> Handoff: `docs/design/handoff/deployment/README.md`.

## 1. Purpose & action

Make the Deployment Plan a first-class, reviewable object: show exactly how Axiom will turn the profile + configuration + server into an executable sequence, before anything runs.

Primary action: **Deploy** — enabled only when the plan is valid (§7).

## 2. Shell

- Variant **S**, setup step 5 "Plan"; route `/apps/:applicationId/setup/plan/:planId`.
- Environment chip shown and **locked** (shell §8). Changing environment means going back to Configure, which produces a new plan.
- The plan is immutable: the URL always identifies one plan; refresh shows the same plan.
- The same plan view is reused read-only in the Deployment **Plan** tab (`…/deployments/:deploymentId/plan`) without the Deploy action.

## 3. Layout

```
Breadcrumbs: Workspace › acme-web › Setup
Stepper:     ✓ Analyze  ✓ Profile  ✓ Server  ✓ Configure  ● Plan
Title:       Deployment plan  ■ Production  ● READY             [Edit configuration] [Deploy ▸]
Meta:        plan_01J8… · profile v3 · fingerprint 9a41…e2 · generated 1m ago

┌ Plan summary (L1/L2) ───────────────────────────────────────────────────┐
│ Source       acme/web · main · 3f9c2a1                                   │
│ Application  Next.js 14 · Node 20 · pnpm                                 │
│ Target       srv-eu-1 · Production                                       │
│ Access       app.acme.dev · HTTPS automatic                              │
│ Build        Container image built from source with pnpm                 │
│ Runtime      Single container · port 3000                                │
│ Health       HTTP GET / → 2xx                                            │
│ Rollback     Current LIVE deployment keeps serving if verification fails │
└──────────────────────────────────────────────────────────────────────────┘

Execution sequence
  ✓ Analyze            completed · analysis_7f… · 3f9c2a1            (precondition)
  ──────────────────────────────────────────────────────────────────────
  1 Build              Build a container image from main@3f9c2a1      ▸
  2 Create Runtime     Prepare the runtime on srv-eu-1                ▸
  3 Configure Network  Route app.acme.dev with HTTPS                  ▸
  4 Start              Start the application on port 3000             ▸
  5 Verify             Check GET / responds before going LIVE         ▸
                                                                 → LIVE
```

## 4. Steps — Contract Alignment

**The UI renders the steps returned by the plan (`steps[]`), in returned order. It never adds, removes, renames or reorders planner steps.**

| Contract code (API §10) | Display label | Short explanation template |
|---|---|---|
| `BUILD` | Build | Build a container image from `{ref}@{sha}` |
| `CREATE_RUNTIME` | Create Runtime | Prepare the runtime on `{server}` |
| `NETWORK` | Configure Network | Route `{domain}` with HTTPS |
| `START` | Start | Start the application on port `{port}` |
| `VERIFY` | Verify | Check `{healthCheck}` responds before going LIVE |
| *unknown code* | the code itself, mono | "Step defined by the planner" — never dropped |

**Analyze** is shown as a completed **precondition** above the sequence, sourced from the analysis that produced the profile version. It is visually separated (divider, no step number) because it is not a planner step. This satisfies the canonical progression (Design DNA §13) without inventing a plan step.

> The visual prompt `docs/design/master-prompts/05-screen-prompts/09-deployment-plan.md` mentions "Prepare" and "Build Image". Those are **not** contract steps and must not be rendered. That prompt should be aligned when #142 (Stitch V2 baseline) is executed.

## 5. Step Detail

Each step row expands (disclosure triangle, keyboard operable). Content per step:

| Field (#135) | Presentation | Level |
|---|---|---|
| Status | Planned (plan view) — in deployment views: queued / current / completed / failed / skipped / cancelled (Design DNA §13) | L1 |
| Short explanation | template in §4 | L1 |
| Relevant technical detail | step-specific, below | L3 |
| Dependencies | "Requires: Build" — derived from contract order or explicit planner dependencies if provided | L2 |
| Failure boundary | what is affected if this step fails (§6) | L2 |

Step-specific technical detail (L3), rendered only from plan data:

| Step | Detail |
|---|---|
| Build | build strategy, package manager, build command, base runtime, image reference (mono) |
| Create Runtime | runtime strategy, resources if set, configuration variable **names** (secret values never shown) |
| Configure Network | domain, TLS mode, internal port, routing target |
| Start | start command, port, restart policy |
| Verify | health check type, path, expected status, timeout/retries if provided |

L4 (expert, collapsed inside L3 under "Technical identifiers"): image digest when known, internal routing identifiers, plan schema version. Internal component names (e.g. Docker, Traefik) may appear here as labelled values, never as step names (navigation §4).

## 6. Failure & Rollback Boundary

Failure boundaries and rollback copy are **rendered from plan rollback metadata** (#97). The table below is reference copy for the expected V0.1 behavior; it must be validated against #97 / #100 and replaced by planner-provided values where they differ.

| Step fails | Reference failure boundary copy |
|---|---|
| Build | "Nothing changes on the server. The current deployment keeps serving." |
| Create Runtime | "The new runtime is discarded. The current deployment keeps serving." |
| Configure Network | "Traffic is not switched. The current deployment keeps serving." |
| Start | "The new runtime is stopped. The current deployment keeps serving." |
| Verify | "The deployment does not go LIVE. The current deployment keeps serving." |

First deployment in an environment: replace "The current deployment keeps serving" with "Nothing is published."

If the plan provides no rollback metadata, the summary shows "Rollback: not specified by this plan" (WARNING) — the UI does not claim a rollback guarantee it cannot source.

## 7. Plan Validity & Deploy Gating

| Plan state | Source | UI | Deploy |
|---|---|---|---|
| Generating | client, during POST | skeleton sequence | disabled |
| READY | `status: READY` | status pill neutral "Ready" | **enabled** |
| Invalid | validation errors (#97) | FAILED notice listing errors, each linking to the Configure field | disabled |
| Stale | inputs changed since generation (profile version, server state, configuration) — detected by fingerprint/profile version mismatch | WARNING "This plan is out of date" + **Regenerate plan** | disabled |
| Target unavailable | server OFFLINE at review time | blocking notice + Change server | disabled |
| Already executed | plan used by a deployment | link "View deployment #42" | hidden |

Disabled Deploy always shows its reason as visible text next to the button.

### Determinism (#97)

- Meta shows plan ID, profile version and fingerprint (mono, copyable, L3 in tooltip/expand).
- Regenerating with identical inputs yields the same fingerprint; the UI can state "Same as previous plan" when fingerprints match.
- The UI never mutates a plan; Edit configuration → new plan → new URL.

## 8. Deploy

1. Production: confirmation dialog — title "Deploy acme-web to Production?", summary (ref, server, domain), rollback line, typed application name. Staging/Preview: no typed confirmation; single click.
2. `POST /applications/{id}/deployments` with `planId` and a client-generated `Idempotency-Key` bound to the plan ID (re-click or retry cannot create a second deployment).
3. On `202`: navigate to `/apps/:id/:environment/deployments/:deploymentId/progress`.
4. On conflict (plan stale / already executed): show the corresponding state from §7.

## 9. Disclosure

| Level | Content |
|---|---|
| L1 | environment, plan state, step list with explanations, primary action |
| L2 | plan summary, dependencies, failure boundaries |
| L3 | step technical detail, fingerprint, profile version |
| L4 | digests, internal routing identifiers, schema version |

## 10. Accessibility

- Execution sequence is an ordered list; precondition is a separate item labelled "Precondition".
- Step expanders are buttons with `aria-expanded` and `aria-controls`.
- Deploy confirmation is a modal dialog; focus starts on the text input; Escape cancels.

## 11. Acceptance Checklist

- [ ] Complete plan (summary + all steps) reviewable before Deploy.
- [ ] Steps rendered strictly from `steps[]`; Analyze shown only as a precondition.
- [ ] Every step has status, explanation, technical detail, dependencies, failure boundary.
- [ ] Rollback/failure claims sourced from plan metadata or explicitly marked unspecified.
- [ ] Deploy enabled only for READY, non-stale plans; reason shown otherwise.
- [ ] Fingerprint and profile version visible for determinism.
- [ ] Idempotent Deploy.
