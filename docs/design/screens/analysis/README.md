# Repository Analysis & Evidence Model v1.0

> Screen + cross-cutting evidence/confidence contract — issue #134.
>
> Depends on: Design DNA (#131), Progressive Disclosure (#132), Shell & Navigation (#133).
> Contract sources: `docs/architecture/api-contract.md` §7–§8, #58 / #94 (analyzer evidence & confidence), #95 (Application Profile schema).
>
> Related screens: `docs/design/screens/github/`, `docs/design/screens/repository/`, `docs/design/screens/application-profile/`.

## 1. The Understanding Phase

```
GitHub Connection ──► Repository List ──► Repository Detail ──► Repository Analysis ──► Application Profile ──► Server Selection (#135+)
   /github             /repositories       /repositories/:id     /apps/:id/setup/analysis  /apps/:id/setup/profile
   connect             find repo           choose ref · Analyze  observe · understand      review · confirm · Continue
```

Goal: **the user understands Axiom's interpretation of their repository before anything is built.**

Phase-wide rules:

1. Every detected fact shows *why* Axiom believes it (evidence) and *how sure* it is (confidence).
2. Nothing assumed is presented as detected. Every value carries a provenance (§4).
3. Ambiguous and unsupported results are explicit, named states — never hidden behind a default.
4. Analysis reads repository files only; it never executes project code. The UI says so where trust matters (Repository Detail, Analysis).
5. Evidence is available on demand (L2/L3) and never dominates the main reading path (L1).

---

## 2. Evidence & Confidence Model (canonical, reused by Application Profile)

### 2.1 Evidence record

Each detected fact is backed by one or more evidence records.

| Field | Presentation | Level |
|---|---|---|
| Source type | label + icon: Manifest (`package.json`, `go.mod`), Lockfile, Dockerfile, Compose file, Framework config, Script, Source file, `axiom.yaml` | L2 |
| Path | mono, copyable, relative to repository root, e.g. `apps/web/package.json` | L2 |
| Excerpt | `--surface-inset` code block, line numbers, max 6 lines, matched lines highlighted with `--axiom-accent-muted` | L3 |
| Effect | **Supports** or **Conflicts** (text + icon) | L2 |
| Explanation | one sentence, e.g. "`next` is declared in dependencies" | L2 |
| Rule / analyzer version | mono, e.g. `node.framework.next@1.2.0` | L4 |

Examples of evidence the UI must be able to render (from #134): `package.json` dependency, lockfile presence (`pnpm-lock.yaml`), `Dockerfile` `EXPOSE 8080`, framework config (`next.config.mjs`), detected script (`"build": "next build"`), source evidence (`server.listen(3000)`).

### 2.2 Confidence bands

The Engine returns confidence in `[0, 1]` (#94). The UI never shows a bare number by default; it shows a **band label**. The exact percentage is L3 (shown inside the evidence drawer, mono, e.g. `0.97`).

| Band | Range (presentation default) | Label | Indicator | Requires user action |
|---|---|---|---|---|
| High | ≥ 0.90 | Detected | neutral check icon, `--text-secondary` | no |
| Medium | 0.60 – 0.89 | Likely — review | WARNING semantic, outlined icon | review suggested, not blocking |
| Low | < 0.60 | Uncertain — confirm | WARNING semantic, filled icon | confirmation required (blocking) |

- Thresholds are presentation defaults; if #94 defines canonical thresholds, they replace these.
- High confidence deliberately uses **no color**: confidence is not deployment health and must not borrow `--status-live`.
- Bands are always text + icon (Design DNA §4).

### 2.3 Result states per fact

| State | Meaning | Presentation | Blocking |
|---|---|---|---|
| Detected | one consistent conclusion | value + band | only if Low |
| Ambiguous | evidence supports ≥2 conclusions (e.g. `package-lock.json` and `pnpm-lock.yaml`) | "Ambiguous" WARNING + candidate values as a choice list, each with its evidence | yes, until user chooses |
| Not detected | no evidence found | "Not detected" + what Axiom looked for + action (enter value / add `axiom.yaml`) | yes, if required for deployment |
| Unsupported | detected, but outside V0.1 presets (#96) | "Not supported in V0.1" + detected value + reason + supported alternatives | yes |
| Not applicable | irrelevant to the detected strategy (e.g. package manager for a Dockerfile-only repo) | hidden at L1; listed as "Not applicable" at L3 | no |

### 2.4 Whole-analysis outcome

| Outcome | Condition | L1 message |
|---|---|---|
| Ready | no blocking fact | "Axiom understood this repository." |
| Needs review | ≥1 blocking Ambiguous / Low / Not detected fact | "Axiom needs you to confirm N details." |
| Unsupported | strategy cannot resolve to a V0.1 preset | "This repository can't be deployed by Axiom V0.1 yet." + reason + docs link |
| Failed | analysis could not run (access lost, ref missing, snapshot error) | "Analysis couldn't complete." + cause + Retry |

Unsupported must never silently become deployable (#94 acceptance criterion).

---

## 3. Repository Analysis Screen

### 3.1 Shell

- Variant **S**, setup step 1 "Analyze" (`docs/design/screens/shell/README.md` §8).
- Route `/apps/:applicationId/setup/analysis`; the analysis being shown is the latest for the application unless `?analysis=:analysisId` is present (shareable, survives refresh).
- Environment chip not shown (environment is chosen at step 4).

### 3.2 Layout

```
Breadcrumbs: Workspace › acme-web › Setup
Stepper:     ● Analyze  ○ Profile  ○ Server  ○ Configure  ○ Plan
Title:       Analyzing acme/web                     [Continue ▸] (disabled until done)
Meta:        ref main · 3f9c2a1 · read-only analysis — no code is executed

┌ Stages ─────────────────────────────┐ ┌ Findings ─────────────────────────────┐
│ ✓ Fetch repository snapshot          │ │ Language     TypeScript   Detected     │
│ ✓ Inspect manifests                  │ │ Framework    Next.js 14   Detected  ⓘ3 │
│ ◐ Detect runtime & framework         │ │ Package mgr  …            (pending)    │
│ ○ Detect build & start commands      │ │ Build cmd    …                         │
│ ○ Detect services & configuration    │ │ Port         …                         │
│ ○ Resolve deployment strategy        │ │ Strategy     …                         │
└──────────────────────────────────────┘ └───────────────────────────────────────┘
```

Findings fill in as stages complete; each row shows value, band, and an evidence count (`ⓘ3`) that opens the evidence drawer.

### 3.3 Stages

| Stage | Produces |
|---|---|
| Fetch repository snapshot | ref resolved to commit SHA |
| Inspect manifests | list of recognized files (L3: file list) |
| Detect runtime & framework | language, runtime version, framework |
| Detect build & start commands | package manager, build, start commands |
| Detect services & configuration | port, services, configuration requirements, health strategy |
| Resolve deployment strategy | container strategy / preset (#96), supported or not |

Stage states use the deployment step vocabulary: completed, current, queued, failed, skipped (Design DNA §13). Status colors: current = BUILDING, completed = neutral check, failed = FAILED, skipped = INACTIVE.

### 3.4 Disclosure

| Level | Content |
|---|---|
| L1 | repository, ref, overall outcome, primary action |
| L2 | stages, findings with bands, evidence counts |
| L3 | evidence drawer (path, excerpt, effect), recognized file list, exact confidence |
| L4 | analyzer rule IDs and version, snapshot ID |

### 3.5 States

| State | Behavior | Primary action |
|---|---|---|
| Running | stages progress; findings appear incrementally | Continue (disabled, label "Analyzing…") |
| Ready | all stages completed; outcome banner neutral | **Continue** → Profile |
| Needs review | outcome banner WARNING; blocking facts listed with links | **Review profile** → Profile (blocking facts are resolved there) |
| Unsupported | outcome panel replaces findings emphasis; detected stack + reason + supported stacks (Node/Next.js, Vite, Go, Docker — per #96) | **Choose another repository**; secondary: "Add axiom.yaml" docs |
| Failed | failed stage marked FAILED with cause | **Retry analysis**; secondary: change ref |
| Re-analysis | when opened from an existing application: previous profile shown greyed for comparison; changed facts marked "Changed" | Continue |

Leaving during Running is safe: analysis continues server-side; returning shows current state.

### 3.6 Accessibility

- Stage list is an ordered list; the current stage has `aria-current="step"`.
- Stage completion and final outcome are announced through a polite live region (one announcement per stage, not per finding).
- Evidence drawer: dialog semantics, focus trapped, Escape closes, focus returns to the evidence trigger.

---

## 4. Value Provenance (canonical, reused by Application Profile)

Every profile value displays exactly one provenance tag.

| Provenance | Tag | Meaning |
|---|---|---|
| Detected | "Detected" + band | inferred from evidence |
| Declared | "axiom.yaml" | declared in the repository manifest |
| Default | "Default" | preset default with no evidence, e.g. port `3000` for Next.js |
| Override | "Override" | entered by the user in Axiom |

Precedence (highest first): **Override › Declared › Detected › Default**. This mirrors the intended `axiom.yaml` precedence (#96 / #124); if #124 freezes a different order, this table follows it.

A Default is never labelled Detected. When a higher-precedence source replaces a lower one, the replaced value stays visible at L3 ("Detected `npm start`, overridden").

---

## 5. Contract Mapping & Gaps

| UI need | Contract | Status |
|---|---|---|
| Start analysis for a ref | `POST /applications/{id}/analysis` | available |
| Analysis result with evidence & confidence | `GET /applications/{id}/analysis/{analysisId}` | available; record shape pending #94 |
| Stage-level progress | — | **gap**: needs analysis `status` + per-stage state (SSE or pollable). Fallback: indeterminate stage list driven by `status`, findings shown on completion |
| Ambiguous candidates per fact | — | **gap**: #94 must expose alternatives with their evidence |
| Unsupported reason & supported alternatives | — | **gap**: #96 must expose machine-readable reason codes |
| Analyzer version / rule ID | — | pending #94 |

## 6. Acceptance Checklist

- [ ] Every finding shows value, provenance and band; evidence reachable in one interaction.
- [ ] No bare confidence number at L1/L2.
- [ ] High confidence uses no status color; Medium/Low use WARNING with text.
- [ ] Ambiguous, Not detected and Unsupported are distinct, named, and blocking where specified.
- [ ] "No code is executed" stated on the Analysis screen.
- [ ] Refresh during and after analysis restores the same analysis.
