# Stitch Explorations (non-normative)

> Issue #142. **Nothing in this folder is a source of truth.** Validated decisions live in `docs/design/**` (see `docs/design/handoff/README.md` §1). Generated screens here are inspiration and audit material only.

## Structure

| Path | Content |
|---|---|
| [`v1/`](v1/README.md) | Archive record of Stitch V1 exploration: observations preserved, audit findings, export location convention |
| [`v2/`](v2/README.md) | V2 exploration baseline: corrected prompt, required screens, validation procedure |

## Repository Policy

1. Stitch exports (HTML, PNG, ZIP) are stored only under `explorations/stitch/v{N}/exports/`.
2. Exports are never imported by `apps/cloud` and never referenced by frontend code.
3. A generated screen becomes a **validated reference** only after passing the V2 validation procedure (`v2/README.md` §4); it is then linked from `docs/design/handoff/README.md` §7.
4. If a generated screen contradicts a validated spec, the spec wins and the discrepancy is recorded in the version's findings table — the spec is not silently changed.
5. Explorations must not redefine architecture, API, states or runtime model.
