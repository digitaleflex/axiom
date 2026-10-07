# Application Profile Screen v1.0

> Screen contract — issue #134.
>
> Depends on: Design DNA (#131), Progressive Disclosure (#132), Shell & Navigation (#133), Evidence model (`docs/design/screens/analysis/README.md` §2, §4).
> Contract sources: `docs/architecture/api-contract.md` §8, #95 (canonical Application Profile schema), #96 (stack presets).

## 1. Purpose

Show Axiom's complete interpretation of the application, let the user confirm or correct it, and gate progression on unresolved uncertainty.

Primary action: **Continue** → Server Selection.

## 2. Shell

- Variant **S**, setup step 2 "Profile".
- Route `/apps/:applicationId/setup/profile`.
- Re-entered from Application Overview for existing applications (read view with "Edit overrides").
- Environment chip: shown only when the target environment is already known (re-analysis of a deployed application); otherwise the meta line reads "Environment · chosen at Configure".

## 3. Layout

```
Breadcrumbs: Workspace › acme-web › Setup
Stepper:     ✓ Analyze  ● Profile  ○ Server  ○ Configure  ○ Plan
Title:       Application profile                          [Re-analyze] [Continue ▸]
Meta:        acme/web · main · 3f9c2a1 · analyzed 2m ago · profile v3

[Outcome banner — only when Needs review / Unsupported]

┌ Summary card (L1) ──────────────────────────────────────────────────────┐
│ Next.js 14 on Node 20 · pnpm · Docker image · port 3000                 │
│ "Axiom will build this app with pnpm and run it as a container."        │
└──────────────────────────────────────────────────────────────────────────┘

┌ Runtime ───────────┐ ┌ Build & start ─────────┐ ┌ Networking & health ──┐
│ Language  ...      │ │ Package mgr ...        │ │ Port   ...            │
│ Runtime   ...      │ │ Build cmd   ...        │ │ Health ...            │
│ Framework ...      │ │ Start cmd   ...        │ │                       │
└────────────────────┘ └────────────────────────┘ └───────────────────────┘
┌ Services ──────────────────────┐ ┌ Configuration requirements ──────────┐
└────────────────────────────────┘ └──────────────────────────────────────┘
▸ Evidence (12 sources)   ▸ Technical details
```

The summary sentence is generated from the profile; it is the L1 answer to "what will Axiom do?".

## 4. Field Matrix

Each field row: label · value (mono when technical) · provenance tag · band (if Detected) · evidence count · edit affordance (if overridable).

| Group | Field (#134) | Profile contract key (API §8 / #95) | Mono | Overridable in V0.1 | Required to continue |
|---|---|---|---|---|---|
| Source | Repository / ref | `repository`, `ref`, commit | yes | no (change via Re-analyze) | yes |
| Runtime | Language / runtime | `language`, runtime version | version only | no | yes |
| Runtime | Framework | `framework` | no | no | no (generic allowed) |
| Build & start | Package manager | `packageManager` | yes | yes (choice of supported) | if Node |
| Build & start | Build command | `buildCommand` | yes | yes | if strategy builds from source |
| Build & start | Start command | `startCommand` | yes | yes | if strategy builds from source |
| Networking | Port | `port` | yes | yes (1–65535) | yes |
| Networking | Health strategy | health check (path / TCP) | path yes | yes (path) | yes |
| Strategy | Container strategy | `containerStrategy` | no | no (derived by preset) | yes |
| Services | Services | `services[]` | names yes | select/deselect (Compose, #123) | no |
| Config | Configuration requirements | env var names, required files | yes | no (values set in Configure, #135) | no |
| Context | Target environment | when known | — | no (chosen in Configure) | no |
| Meta | Confidence / evidence | `confidence`, evidence | — | — | — |

Keys not yet present in the API example (`repository`, `ref`, runtime version, health strategy, configuration requirements) must be added by #95; this table is the UI's requirement list for that schema.

## 5. Interactions

### 5.1 Override

- Edit opens inline editor; saving marks the field **Override** and keeps the detected value visible beneath at L3 ("Detected: `npm start`").
- "Revert to detected" restores the lower-precedence value.
- Commands are validated against preset rules (#96). Unsafe or unsupported input shows an inline error with the reason; it cannot be saved.
- Overrides on a field whose evidence later changes (Re-analyze) are kept and flagged "Detected value changed".

### 5.2 Resolving blocking facts

- **Ambiguous** → radio list of candidates, each with its evidence summary; choosing one creates an Override.
- **Low confidence** → "Confirm" (accept as is) or edit.
- **Not detected** (required) → empty editor with guidance on what Axiom looked for.
- **Unsupported** → no resolution in-screen; Continue disabled; actions: Choose another repository, Re-analyze another ref, docs for `axiom.yaml`.

The outcome banner lists blocking items as links that scroll to and focus the field. Its counter updates live ("2 details to confirm").

### 5.3 Continue

Enabled only when no blocking fact remains. Disabled state explains why (tooltip and visible text under the button: "Confirm 2 details to continue"). Pressing Continue persists overrides, then navigates to `/apps/:id/setup/server`.

## 6. Disclosure

| Level | Content |
|---|---|
| L1 | summary sentence, outcome banner, primary action |
| L2 | field groups with value, provenance, band |
| L3 | evidence drawer, replaced lower-precedence values, exact confidence, Not applicable fields, recognized files |
| L4 | profile version ID, analysis ID, analyzer rule IDs |

Runtime-aware: only fields relevant to the resolved strategy are shown at L2 (progressive-disclosure §7). Example — Dockerfile-only repository: package manager and build/start commands are Not applicable; Dockerfile path and `EXPOSE` port are shown instead.

## 7. States

| State | Behavior |
|---|---|
| Loading | skeleton of groups; summary card skeleton |
| Ready | no banner; Continue enabled |
| Needs review | WARNING banner with blocking list; Continue disabled |
| Unsupported | banner + reason; groups shown read-only for transparency |
| Analysis outdated | banner "Repository changed since analysis (new commit on `main`)" + Re-analyze — only when the Engine exposes it |
| Save error | inline error on field; overrides not lost |

## 8. Synthetic Reference Data (for mockups)

- `acme/web`, ref `main`, commit `3f9c2a1`, Next.js 14.2, Node 20, pnpm 9, build `pnpm build`, start `pnpm start`, port `3000` (Default — no explicit evidence), health `GET /` (Default), container strategy Docker image, configuration requirements `DATABASE_URL`, `NEXTAUTH_SECRET` (from `.env.example`).
- Needs-review variant: both `package-lock.json` and `pnpm-lock.yaml` present → package manager Ambiguous.
- Unsupported variant: `acme/ml-api`, Python 3.11 / FastAPI → "Python isn't supported in V0.1. Supported: Node.js (Next.js, Vite), Go, Dockerfile."

## 9. Accessibility

- Each field row is a description list item (`<dt>`/`<dd>`); provenance and band are part of the accessible description.
- Disabled Continue is `aria-disabled` (still focusable) with the reason in `aria-describedby`.
- Radio candidates for Ambiguous facts form a `fieldset` with legend naming the field.

## 10. Acceptance Checklist

- [ ] Every field in §4 rendered with provenance; Default never shown as Detected.
- [ ] Summary sentence states what Axiom will do in plain language.
- [ ] Evidence reachable without leaving the screen; not shown by default.
- [ ] Continue gated on blocking facts with a visible reason.
- [ ] Overrides distinguishable from detected values and reversible.
- [ ] Configuration requirements show names only, never values.
- [ ] Fields map to #95 keys; gaps listed in §4 handed to #95.
