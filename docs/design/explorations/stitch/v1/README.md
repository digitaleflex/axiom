# Stitch V1 — Exploration Archive

> Issue #142. Historical exploration. Status: **archived, non-normative**.

## 1. Provenance

| Item | Value |
|---|---|
| Tool | Google Stitch |
| Input prompt | `docs/design/master-prompts/04-stitch-exploration.md` + screen prompts `docs/design/master-prompts/05-screen-prompts/01–20` (V1 versions, see git history before #142) |
| Upstream context | Product UI Brief, UX/IA, Design DNA draft (pre-#131 freeze) |
| Exports | `exports/` (to be added by the design owner; none committed at archive time) |

To keep V1 reproducible and auditable, the exact prompt text is recoverable from git history of `docs/design/master-prompts/` at the commit preceding #142, and exported assets must be committed under `exports/` with their generation date in the filename (`YYYY-MM-DD-<screen>.png|html`).

## 2. Observations to Preserve (#142)

| Observation | Where it lives now (validated) |
|---|---|
| Strong technical infrastructure aesthetic | Design DNA §1–§2 |
| High information density | Design DNA §6, progressive disclosure §6 |
| Semantic status colors | Design DNA §4, deployment-progress §1.2 |
| Technical typography | Design DNA §5, accessibility §8 |
| Operational telemetry | screens/metrics |
| Deployment-focused visual language | Design DNA §13, screens/deployment-* |

## 3. Audit Findings → Corrections

| # | V1 finding | Correction | Validated in |
|---|---|---|---|
| V1-1 | No Deployment Configuration screen | added | `screens/deployment-configuration/` (#135) |
| V1-2 | No Deployment Plan screen | added as first-class artifact | `screens/deployment-plan/` (#135) |
| V1-3 | No Application Logs screen | added | `screens/logs/` (#137) |
| V1-4 | Application Profile weak | evidence/confidence/provenance model | `screens/application-profile/`, `screens/analysis/` (#134) |
| V1-5 | Progressive disclosure implicit | explicit L1–L4 contract | `ux/progressive-disclosure.md` (#132) |
| V1-6 | Kubernetes/SRE-first vocabulary | V0.1 vocabulary rules | `ux/navigation/` §4, `screens/infrastructure/` §1 |
| V1-7 | Metrics not runtime-aware | ownership layers + runtime-aware rules | `screens/metrics/` (#137) |
| V1-8 | Raw infrastructure over application context | application-first shell & disclosure | `screens/shell/` (#133) |
| V1-9 | Technical depth must remain | drill-down L3/L4 everywhere | progressive disclosure §2 |
| V1-10 | Plan steps not matching contract ("Prepare", "Build Image") | render `steps[]` only | `screens/deployment-plan/` §4 |
| V1-11 | Rollback / Abort shown unconditionally | only when contract supports | deployment-success §5, deployment-progress §7 |
