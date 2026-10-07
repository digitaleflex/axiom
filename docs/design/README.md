# Axiom Design Documentation

Validated design documentation for the Axiom Cloud Console V0.1. **This folder is the frontend source of truth.** Generated explorations (Stitch) are archived under `explorations/` and never override it.

Start here: **[`handoff/README.md`](handoff/README.md)** — final frontend implementation contract (#141).

## Map

| Area | Path | Issue |
|---|---|---|
| Design DNA (visual language) | [`design-dna/`](design-dna/README.md) | #131 |
| Tokens (CSS) | [`tokens/axiom.css`](tokens/axiom.css) | #131, #140 (v1.0.1) |
| Progressive disclosure | [`ux/progressive-disclosure.md`](ux/progressive-disclosure.md) | #132 |
| Navigation & routes | [`ux/navigation/`](ux/navigation/README.md) | #133 |
| Shell | [`screens/shell/`](screens/shell/README.md), [`handoff/shell/`](handoff/shell/README.md) | #133 |
| GitHub, repositories, analysis, profile | [`screens/github/`](screens/github/README.md), [`screens/repository/`](screens/repository/README.md), [`screens/analysis/`](screens/analysis/README.md), [`screens/application-profile/`](screens/application-profile/README.md) | #134 |
| Configuration & plan | [`screens/deployment-configuration/`](screens/deployment-configuration/README.md), [`screens/deployment-plan/`](screens/deployment-plan/README.md), [`handoff/deployment/`](handoff/deployment/README.md) | #135 |
| Progress, success, failure | [`screens/deployment-progress/`](screens/deployment-progress/README.md), [`screens/deployment-success/`](screens/deployment-success/README.md), [`screens/deployment-failure/`](screens/deployment-failure/README.md) | #136 |
| Application operations | [`screens/application/`](screens/application/README.md), [`screens/logs/`](screens/logs/README.md), [`screens/metrics/`](screens/metrics/README.md), [`screens/domains/`](screens/domains/README.md) | #137 |
| Infrastructure | [`screens/infrastructure/`](screens/infrastructure/README.md), [`screens/servers/`](screens/servers/README.md) | #138 |
| Settings & system states | [`screens/settings/`](screens/settings/README.md), [`screens/system-states/`](screens/system-states/README.md), [`components/system/`](components/system/README.md) | #139 |
| Quality gate | [`accessibility/`](accessibility/README.md), [`responsive/`](responsive/README.md), [`interactions/`](interactions/README.md) | #140 |
| Final handoff | [`handoff/README.md`](handoff/README.md) | #141 |
| Explorations (non-normative) | [`explorations/stitch/`](explorations/stitch/README.md) | #142 |
| Generation prompts (non-normative) | [`master-prompts/`](master-prompts/README.md) | — |

## Rules

1. Validated docs here > generated visuals.
2. Backend contracts (`docs/architecture/`) > design docs for data and states.
3. Changes to a normative doc go through an issue and update the handoff register.
