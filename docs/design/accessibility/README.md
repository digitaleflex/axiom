# Accessibility Quality Gate v1.0

> Issue #140. Validates the Design DNA (#131), tokens (`docs/design/tokens/axiom.css`) and screen contracts (#133–#139) against WCAG 2.2 AA–oriented requirements.
>
> Companion: `docs/design/responsive/README.md`, `docs/design/interactions/README.md`.

## 1. Contrast Audit (computed)

Ratios computed with the WCAG relative-luminance formula from canonical token values. Targets: 4.5:1 normal text, 3:1 large text (≥ 18.66px bold / 24px) and non-text UI components (WCAG 1.4.3 / 1.4.11).

### 1.1 Text and status colors on surfaces

| Foreground | surface-0 | surface-1 | surface-2 | surface-3 | inset | Verdict |
|---|---|---|---|---|---|---|
| `--text-primary` | 18.14 | 17.46 | 16.63 | 15.69 | 18.25 | pass |
| `--text-secondary` | 9.03 | 8.69 | 8.28 | 7.81 | 9.09 | pass |
| `--text-tertiary` | 4.80 | 4.62 | **4.40** | **4.15** | 4.83 | **fails on surface-2/3** |
| `--text-disabled` | 2.66 | 2.56 | 2.44 | 2.30 | 2.68 | exempt (disabled), never for information |
| `--axiom-accent` | 15.48 | 14.89 | 14.19 | 13.39 | 15.57 | pass |
| `--status-live` | 9.90 | 9.52 | 9.07 | 8.56 | 9.95 | pass |
| `--status-building` | 11.24 | 10.81 | 10.30 | 9.72 | 11.30 | pass |
| `--status-queued` | 7.98 | 7.68 | 7.31 | 6.90 | 8.02 | pass |
| `--status-failed` | 5.70 | 5.48 | 5.22 | 4.93 | 5.73 | pass |
| `--status-inactive` | 5.30 | 5.10 | 4.85 | 4.58 | 5.33 | pass |
| `--status-warning` | 9.32 | 8.97 | 8.54 | 8.06 | 9.38 | pass |

### 1.2 Filled controls

| Pair | Ratio | Verdict |
|---|---|---|
| `--surface-0` text on `--axiom-accent` (primary button) | 15.48 | pass |
| `--surface-0` text on `--status-failed` (destructive button) | 5.70 | pass |
| white text on `--status-failed` | 3.48 | **fail** — do not use |

### 1.3 Non-text (borders, focus) on surface-1

| Token | Effective color | Ratio | Verdict |
|---|---|---|---|
| `--border-subtle` | `#1E2124` | 1.18 | decorative only |
| `--border-default` | `#282A2D` | 1.33 | decorative only |
| `--border-strong` | `#36393B` | 1.64 | decorative only |
| `--axiom-accent-focus` | `#485922` | **2.48** | **fails as focus indicator** |
| `--axiom-accent` (solid) | — | 14.89 | pass |

### 1.4 Resolutions (normative)

| # | Finding | Resolution |
|---|---|---|
| A1 | `--text-tertiary` < 4.5 on surface-2/3 | On surface-2, surface-3 and drawers/popovers, use `--text-secondary` for any informative text. `--text-tertiary` allowed there only for large text or purely decorative content. |
| A2 | Focus halo 2.48:1 | Focus indicator = **2px solid `--axiom-accent` outline, 2px offset** (`--focus-ring`). `--axiom-accent-focus` may be added as an outer halo, never alone. |
| A3 | No border reaches 3:1 | New token `--border-control: #6B747D` (4.02 on surface-1, 3.61 on surface-3) for boundaries that identify interactive controls: inputs, selects, checkboxes, radio cards, unselected toggles. `--border-*` remain for separators/cards. |
| A4 | White on failed 3.48 | Destructive buttons use `--surface-0` text on `--status-failed`. |
| A5 | Status pills | Status color is applied to icon + label text (all ≥ 4.5 on every surface) on a ≤ 14% tinted background; never colored text on a saturated fill. |

Tokens A2/A3 are added to `docs/design/tokens/axiom.css` as a v1.0.1 accessibility amendment (no existing token changed).

## 2. Status Without Color

Verified pattern for every stateful element (Design DNA §4):

| Element | Channels |
|---|---|
| Deployment status pill | color + icon shape + text label |
| Step indicator | icon shape (hollow / ring / check / octagon / dash / slash) + text |
| Environment chip | glyph shape + border style + text (no color) |
| Server state | color + icon + text + reason |
| Domain checks | ✓ / ✕ / ○ icon + text |
| Log severity | fixed-width text label + color |
| Chart thresholds | labelled line + text annotation |

Test: every screen in grayscale must remain fully interpretable (QA step in #141).

## 3. Keyboard

### 3.1 Global

| Key | Action |
|---|---|
| Tab / Shift+Tab | regions in order: skip link → sidebar → context bar → page header → content → overlays |
| Enter / Space | activate |
| Escape | close menu / drawer / dialog, return focus to trigger |
| Arrow keys | within menus, comboboxes, radio groups, tabs (roving tabindex) |
| `/` | focus search in Logs and Repository List (only when focus is not in a text field) |

No keyboard traps except intentional modal focus containment. All shortcuts single-key only when focus is outside text inputs and can be disabled in Settings → General (WCAG 2.1.4).

### 3.2 Critical flows (must pass keyboard-only)

1. Sign in → Dashboard
2. Connect GitHub → Repository List → Repository Detail → Analyze
3. Profile: resolve ambiguous fact → Continue
4. Server Selection → Configure (environment radio, secrets entry) → Review plan
5. Plan: expand steps → Deploy → Production typed confirmation
6. Progress: follow logs, pause, cancel with confirmation
7. Failure: open logs, Edit configuration, Retry
8. Switch environment from context bar; open activity drawer; follow notification
9. Domains: add domain, copy DNS record, remove with confirmation

## 4. Focus

- `:focus-visible` only; mouse clicks don't show ring on buttons, inputs always show it.
- Ring: `outline: 2px solid var(--axiom-accent); outline-offset: 2px;` on all surfaces (≥ 13:1).
- Inside dense tables/logs, focused row gets ring + `--surface-2` background.
- Focus never lost on route change (moves to `<main>`), on async content replacement (stays on equivalent control) or on item removal (moves to next item, else list heading).

## 5. Screen Readers

| Need | Rule |
|---|---|
| Icon-only buttons | `aria-label` with action + target ("Copy commit SHA 3f9c2a1") |
| Status | accessible name includes state text; decorative icons `aria-hidden` |
| Live updates | polite region for step/status transitions; assertive only for terminal FAILED/LIVE once; logs not announced |
| Tables | real `<table>` with `<th scope>`; sortable headers expose `aria-sort` |
| Charts | text summary + data table alternative |
| Technical identifiers | full value in accessible name even when visually truncated |
| Page title | `{Page} · {Application} · {Environment} · Axiom` |

## 6. Touch Targets

- ≥ 24×24 CSS px everywhere (WCAG 2.5.8); **≥ 40×40** for primary/destructive actions and all controls below `--bp-lg`.
- Row actions in dense tables: overflow button 32×32 on desktop, whole-row tap + 40×40 overflow on touch.

## 7. Text Scaling & Reflow

- Layout works at 200% zoom and at 320 CSS px width (WCAG 1.4.10) without two-dimensional scrolling, **except** logs, code excerpts and wide data tables, which scroll horizontally inside their own container (permitted exception).
- Type uses `rem`; line-height in unitless values; no fixed-height text containers.
- Text spacing override (1.5 line-height, 0.12em letter, 0.16em word) must not clip content.

## 8. Long Identifiers & Log Wrapping

| Content | Rule |
|---|---|
| SHAs, IDs, digests | middle truncation (`sha256:9a41…e2c0`) with full value in tooltip, accessible name and copy |
| URLs / hostnames | end truncation only below `--bp-md`; never truncate the hostname part |
| Paths | start truncation (`…/apps/web/package.json`) to keep filename |
| Log lines | default **no-wrap** with horizontal scroll; "Wrap lines" toggle (persisted per user); wrapped lines indent continuation by 2ch |
| Commands | wrap at spaces in profile/plan views; never in copy payload |

## 9. Error Association

- Each invalid field: `aria-invalid="true"`, message linked via `aria-describedby`, message text starts with the field name.
- Forms with ≥ 2 errors show an error summary (`role="alert"`) with links to fields; focus moves to it on submit.
- Errors never conveyed by red border alone: icon + text.

## 10. Reduced Motion

Under `prefers-reduced-motion: reduce`:

| Motion | Replacement |
|---|---|
| spinners / animated ring | static icon + "In progress" text |
| drawer / toast slide | instant appear / fade ≤ 100ms |
| progress bar animation | stepwise width change |
| log auto-scroll smoothing | instant jump |
| skeleton shimmer | static block |

A user setting in Settings → General ("Reduce motion") overrides the OS preference in both directions.

## 11. Acceptance Checklist

- [ ] A1–A5 applied in components and tokens.
- [ ] Grayscale review passes for all 20 screens.
- [ ] Nine critical flows pass keyboard-only.
- [ ] Focus ring ≥ 3:1 everywhere.
- [ ] 200% zoom and 320px reflow verified.
- [ ] Reduced-motion replacements implemented.
- [ ] axe-core: zero serious/critical violations on all screens.
