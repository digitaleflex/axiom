# Stitch V2 — Exploration Baseline

> Issue #142. Status: **baseline defined; outputs pending validation**. Non-normative.

## 1. Purpose

Generate V2 visual explorations that **conform** to the validated design contract, so validated frames can be linked as screenshot references in `docs/design/handoff/README.md` §7.

## 2. Required Inputs (attach to every Stitch session)

1. Product brief & scope: `docs/product/vision.md`, `docs/product/v0.1-scope.md`
2. UX/IA: `docs/design/ux/navigation/README.md`, `docs/design/ux/progressive-disclosure.md`
3. Design DNA + tokens: `docs/design/design-dna/README.md`, `docs/design/tokens/axiom.css` (v1.0.1)
4. V1 audit: `../v1/README.md` §3
5. The target screen spec from `docs/design/screens/**`

## 3. V2 Master Prompt

```
You are generating high-fidelity UI frames for AXIOM Cloud Console V0.1.

Hard constraints (do not violate):
- Use only the attached Design DNA tokens; no new colors. Environment
  (Production/Staging/Preview) is shown by text + glyph + border style, never color.
- Status is always text + icon + color, using the canonical vocabulary:
  Queued, Analyzing, Planning, Building, Deploying, Verifying, Live, Failed.
- Deployment plan steps are exactly: Build, Create Runtime, Configure Network,
  Start, Verify — with Analyze shown as a completed precondition. No other steps.
- Application context first. Infrastructure detail (Docker, Traefik, agent,
  container IDs) only in collapsed technical sections, never in navigation.
- No Kubernetes, pods, service mesh or eBPF concepts.
- Show Rollback or Cancel only where the attached screen spec allows it.
- Persistent context bar: workspace / application / environment chip.
- One primary action per screen, as named in the screen spec.
- Use realistic synthetic data from the screen spec (acme/web, Next.js 14,
  srv-eu-1, app.acme.dev, commit 3f9c2a1).
- Do not imitate Vercel, Linear, Railway, Render or AWS.

Generate the screen: <SCREEN NAME>
Follow this specification exactly: <PASTE docs/design/screens/<screen>/README.md>
Render desktop (1440) and mobile (375) frames, default state plus the
states listed in its "States" section.
```

## 4. Validation Procedure

A V2 frame is validated when a reviewer confirms, per frame:

| Check | Source |
|---|---|
| Layout and content match the screen spec | `screens/**` |
| Tokens only; contrast rules A1–A5 respected | accessibility §1 |
| Status/environment without color passes grayscale review | accessibility §2 |
| Disclosure levels correct (no L3/L4 as hero) | progressive disclosure |
| No invented steps, states, capabilities or actions | handoff §6 |
| Mobile frame matches responsive matrix | responsive §3 |

Record each validated frame in the table below and in handoff §7. Rejected frames stay in `exports/` with the failing check noted.

| Screen | Frame file | Validated by | Date | Notes |
|---|---|---|---|---|
| _none yet_ | | | | |

## 5. Order

Generate in roadmap order: shell → GitHub/analysis/profile → configuration/plan → progress/success/failure → application operations → servers → settings/system states.
