# Responsive Behavior v1.0

> Issue #140. Responsive specification for patterns and for all 20 V0.1 screens.
>
> Breakpoints (Design DNA §7): `--bp-sm` 640 · `--bp-md` 768 · `--bp-lg` 1024 · `--bp-xl` 1280 · `--bp-2xl` 1536.
> Shell responsiveness: `docs/design/screens/shell/README.md` §7 (normative, not repeated here).

## 1. Device Classes

| Class | Range | Intent |
|---|---|---|
| Mobile | < 768 | monitor, approve, diagnose; full flows possible but optimized for observation (progress, logs, status, domains) |
| Tablet | 768–1279 | full product, condensed chrome |
| Desktop | ≥ 1280 | primary design target, dense technical layouts |

Every flow remains **possible** on mobile; dense configuration (Configure, Plan detail) is usable but not optimized.

## 2. Pattern Rules

| Pattern | Desktop | Tablet | Mobile |
|---|---|---|---|
| Side navigation | expanded / collapsible to rail | rail (1024–1279) / drawer (768–1023) | drawer |
| Dense tables | all columns | low-priority columns hidden behind row expansion; column priority declared per table | two-line cards; primary column + status on line 1; key metric/time on line 2; rest in expansion |
| Logs | full width viewer, toolbar single row | toolbar wraps to two rows | toolbar collapses to search + filter sheet; no-wrap with horizontal scroll inside viewer; wrap toggle prominent |
| Deployment timeline | vertical sequence + side feed | sequence above feed | current-step card pinned at top; sequence as compact horizontal stepper (icons + current label); feed collapsed |
| Modals | centered, max 560px (forms) / 720px (details) | same | full-screen sheet with sticky header and action bar |
| Drawers | right, 400px (activity) / 560px (detail) | same, overlay | full-screen sheet; Back button replaces close icon |
| Technical identifiers | full or middle-truncated | middle-truncated | middle-truncated, copy button always visible (no hover-only) |
| Charts | grid of 2–3 | 2 columns | 1 column; current value above chart; tooltip on tap; range selector as segmented control |
| Page header actions | primary + 2 secondary | primary + overflow | primary full-width below title; secondary in overflow |
| Forms | 2-column groups where natural | 1 column | 1 column; sticky bottom action bar with primary action |

### Column priority (tables)

Each table declares priority 1–3. Priority 1 always visible; 2 hidden < `--bp-lg`; 3 hidden < `--bp-xl`. Hidden columns are reachable in row expansion — never dropped.

### Hover-only affordances

Forbidden for essential actions. Copy buttons, row overflow and tooltips with essential info must be reachable by tap and keyboard.

## 3. Screen Matrix (20 screens)

| # | Screen | Tablet adaptation | Mobile adaptation |
|---|---|---|---|
| 1 | Dashboard | cards 2-col | single column; in-progress deployments first |
| 2 | GitHub Connection | access lists stack | account card + actions full-width |
| 3 | Repository List | Visibility, Default branch → expansion | two-line cards; search sticky |
| 4 | Repository Detail | cards stack | ref selector full-width sheet; Analyze sticky bottom |
| 5 | Repository Analysis | stages above findings | stages compact; findings list; evidence drawer full-screen |
| 6 | Application Profile | groups 2-col | groups single column; overrides inline; Continue sticky bottom |
| 7 | Server Selection | resources compress to bars | cards: name + state + eligibility; resources in expansion |
| 8 | Deployment Configuration | single column | single column; secrets rows stack name/value; Review plan sticky |
| 9 | Deployment Plan | summary above sequence | summary collapsible; steps expand in place; Deploy sticky |
| 10 | Deployment Progress | sequence above feed | current step pinned; compact stepper; feed collapsed; Cancel in header overflow |
| 11 | Deployment Success | cards stack | hero first, Open application full-width |
| 12 | Deployment Failure | cards stack | failure panel first; logs excerpt 10 lines with "Open full logs" |
| 13 | Application Overview | cards 2-col | status card first; others stacked |
| 14 | Application Deployments | server, initiator → expansion | two-line cards |
| 15 | Application Logs | toolbar 2 rows | filter sheet; viewer full height |
| 16 | Application Metrics | 2-col charts | 1-col charts; server section collapsed |
| 17 | Application Domains | checks collapse to overall + expansion | cards: hostname + overall; diagnostics in expansion; Add sticky |
| 18 | Server Overview | runtime/routing columns → expansion | cards: name + state + reason; resources line |
| 19 | Server Details | section tabs scroll | section select; diagnostics charts single column |
| 20 | Settings | section list left (narrow) | section select at top; Save sticky when dirty |

## 4. Invariants

1. Environment chip and application name never hidden (shell §7).
2. One primary action always reachable without scrolling (sticky bottom bar on mobile where needed).
3. Status labels never reduced to icon-only.
4. The shell never scrolls horizontally; only logs, code and wide tables scroll inside their container.
5. No content removed at smaller sizes — only moved behind disclosure.

## 5. Acceptance Checklist

- [ ] All 20 screens specified at tablet and mobile (§3).
- [ ] Tables declare column priority; hidden columns reachable.
- [ ] Logs, timelines, charts, identifiers follow §2 on mobile.
- [ ] No hover-only essential affordance.
- [ ] Verified at 375, 768, 1024, 1440 widths.
