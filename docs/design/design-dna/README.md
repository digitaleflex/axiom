# AXIOM DESIGN DNA v1.0

> Canonical visual language for Axiom Cloud Console.
>
> Status: implementation contract for V0.1
>
> Product principle: **Progressive Disclosure Infrastructure**.

## 1. Design Character

Axiom must feel:

- premium
- technical
- credible
- modern
- precise
- calm
- powerful
- developer-first
- infrastructure-grade

The interface must communicate technical competence without forcing infrastructure complexity onto the user.

### Core rule

**Application context first. Infrastructure detail on demand.**

Axiom should expose the simplest useful representation first and allow deliberate drill-down into deployment, runtime, networking, health and infrastructure details.

---

## 2. Visual Principles

1. **Progressive disclosure** — information becomes more technical as the user drills down.
2. **Operational clarity** — every deployment state must be immediately recognizable.
3. **Technical density with hierarchy** — high information density is allowed, visual hierarchy is mandatory.
4. **Semantic consistency** — status meaning is never communicated by color alone and never changes between screens.
5. **Calm infrastructure** — avoid gratuitous glow, gradients and decorative effects.
6. **Technical precision** — identifiers, logs, ports, timestamps and machine-generated values use technical typography.
7. **Environment anchoring** — Production, Staging and Preview remain visible in the relevant product chrome.
8. **No imitation** — do not reproduce the visual identity or interaction patterns of Vercel, Linear, Railway, Render, AWS or other competitors.

---

## 3. Color System

Axiom uses a dark technical foundation with restrained accent usage.

### Brand accent

| Token | Value | Usage |
|---|---|---|
| `--axiom-accent` | `#C5F441` | Primary Axiom accent, key actions, active emphasis |
| `--axiom-accent-strong` | `#D5FF63` | Hover/emphasis |
| `--axiom-accent-muted` | `rgba(197,244,65,.14)` | Accent surfaces |
| `--axiom-accent-focus` | `rgba(197,244,65,.32)` | Focus ring |

The accent must not be used as a substitute for semantic status colors.

### Surface hierarchy

| Token | Role |
|---|---|
| `--surface-0` | application background |
| `--surface-1` | primary panels |
| `--surface-2` | elevated cards / secondary panels |
| `--surface-3` | popovers / drawers / overlays |
| `--surface-inset` | code, log and technical inset areas |

Recommended base values:

```css
--surface-0: #080A0C;
--surface-1: #0D1013;
--surface-2: #12161A;
--surface-3: #171C21;
--surface-inset: #07090B;
```

### Text hierarchy

```css
--text-primary: #F3F5F7;
--text-secondary: #A8B0B8;
--text-tertiary: #747E88;
--text-disabled: #4E565E;
```

### Borders

```css
--border-subtle: rgba(255,255,255,.07);
--border-default: rgba(255,255,255,.11);
--border-strong: rgba(255,255,255,.17);
```

---

## 4. Semantic Status System

Status semantics are canonical across every screen.

| State | Semantic | Token |
|---|---|---|
| LIVE | healthy / operational | `--status-live` |
| BUILDING | building / probing / processing | `--status-building` |
| QUEUED | preview / waiting | `--status-queued` |
| FAILED | failure / crash | `--status-failed` |
| INACTIVE | superseded / inactive | `--status-inactive` |
| WARNING | degraded / attention required | `--status-warning` |

Recommended values:

```css
--status-live: #35D07F;
--status-building: #F5B942;
--status-queued: #5EA7FF;
--status-failed: #F05252;
--status-inactive: #7C858F;
--status-warning: #F59E4A;
```

### Accessibility rule

Status must always be communicated through at least two channels:

- color + text
- color + icon
- color + shape/state indicator

Never rely on color alone.

---

## 5. Typography

### UI typography

Primary UI family:

- Geist Sans where available
- system sans fallback

### Technical typography

Use JetBrains Mono / Geist Mono for:

- Git SHAs
- deployment IDs
- container/image IDs
- ports
- timestamps
- exit codes
- logs
- paths
- commands
- technical identifiers
- machine-generated values

### Type scale

```css
--text-xs: 0.6875rem;
--text-sm: 0.8125rem;
--text-md: 0.875rem;
--text-lg: 1rem;
--text-xl: 1.125rem;
--text-2xl: 1.375rem;
--text-3xl: 1.75rem;
--text-4xl: 2.25rem;
```

Default body size: `0.875rem`.

Technical metadata should generally use `0.75rem`–`0.8125rem`.

---

## 6. Spacing and Density

Use a 4px base spacing unit.

```css
--space-1: 4px;
--space-2: 8px;
--space-3: 12px;
--space-4: 16px;
--space-5: 20px;
--space-6: 24px;
--space-8: 32px;
--space-10: 40px;
--space-12: 48px;
--space-16: 64px;
```

### Density rules

- compact controls for operational interfaces
- generous spacing around major section boundaries
- tables prioritize scanability over decorative padding
- logs prioritize readable line density
- deployment timelines prioritize state recognition
- dashboards may be dense, but primary actions must remain visually dominant

---

## 7. Grid and Layout

Desktop console baseline:

- persistent navigation
- top environment/context chrome where relevant
- content area with a maximum readable width
- technical views may use wider layouts when data density requires it

Use responsive layouts rather than fixed desktop-only geometry.

Primary breakpoints:

```css
--bp-sm: 640px;
--bp-md: 768px;
--bp-lg: 1024px;
--bp-xl: 1280px;
--bp-2xl: 1536px;
```

---

## 8. Radius

Axiom should remain technical rather than playful.

```css
--radius-sm: 4px;
--radius-md: 6px;
--radius-lg: 8px;
--radius-xl: 12px;
--radius-pill: 999px;
```

Use larger radii selectively for dialogs, major cards and status pills.

---

## 9. Elevation and Shadows

Elevation should communicate hierarchy, not decoration.

Prefer:

- surface contrast
- subtle borders
- restrained shadows

Avoid:

- large diffuse shadows
- excessive glow
- neon bloom
- glassmorphism as a default surface treatment

Recommended:

```css
--shadow-sm: 0 1px 2px rgba(0,0,0,.22);
--shadow-md: 0 8px 24px rgba(0,0,0,.28);
--shadow-lg: 0 16px 48px rgba(0,0,0,.34);
```

---

## 10. Focus and Interaction States

Every interactive control must define:

- default
- hover
- focus-visible
- active
- disabled
- loading where applicable
- destructive confirmation where applicable

Keyboard focus must remain visible against dark surfaces.

Focus indication must not depend exclusively on a subtle color shift.

---

## 11. Motion

Motion communicates system state.

### Use motion for

- deployment progression
- state transitions
- log arrival
- health changes
- panel transitions
- confirmation feedback

### Avoid

- decorative looping animations
- excessive parallax
- animated gradients
- motion that delays user actions

### Timing

Use short transitions for ordinary UI interaction and slightly longer transitions for major state changes.

Respect `prefers-reduced-motion`.

---

## 12. Canonical Component Foundations

The following components must consume semantic tokens rather than screen-specific values:

- Primary Button
- Secondary Button
- Ghost Button
- Destructive Button
- Input
- Select
- Card
- Table
- Badge
- Status Indicator
- Progress
- Timeline
- Log Viewer
- Metric Card
- Navigation
- Modal
- Drawer
- Toast
- Empty State
- Error State

### Component rule

A component must not define a competing semantic color vocabulary.

For example, a deployment component uses `status-live`; it does not invent `deployment-green`.

---

## 13. Deployment Visualization

The canonical deployment progression is:

```
Analyze
  ↓
Build
  ↓
Create Runtime
  ↓
Configure Network
  ↓
Start
  ↓
Verify
  ↓
LIVE
```

The UI must distinguish:

- completed
- current
- queued
- failed
- skipped
- cancelled

Deployment progress should expose enough detail to explain what Axiom is doing without exposing raw infrastructure complexity by default.

---

## 14. Logs

Logs are technical content, not ordinary body text.

Rules:

- monospace typography
- stable line height
- timestamps visually distinguishable from message body
- severity/status recognizable
- copy action available
- search/filter available where relevant
- long stack traces foldable
- raw exit codes preserved
- technical identifiers remain copyable

---

## 15. Infrastructure Visualization

Infrastructure is progressively disclosed.

### Level 1 — Application

- application
- environment
- status
- URL
- deployment

### Level 2 — Deployment

- commit/ref
- build
- runtime
- health
- deployment events

### Level 3 — Runtime

- container
- image
- port
- resource usage
- runtime state

### Level 4 — Network / Infrastructure

- host/server
- routing
- Traefik
- domain/TLS
- health probes
- technical identifiers

V0.1 must not visually imply Kubernetes-first or service-mesh-first architecture.

---

## 16. Data Visualization

Charts must prioritize operational interpretation.

Use:

- clear axes
- readable units
- explicit time range
- meaningful legends
- semantic status markers
- restrained visual treatment

Avoid decorative charts without an operational purpose.

Metrics must make units explicit:

- CPU %
- memory MB/GB
- disk GB
- network throughput
- request latency
- health status

---

## 17. Environment Anchoring

Where an application/environment context exists, the interface should maintain a persistent contextual indicator for:

- Production
- Staging
- Preview

The environment must remain visually distinguishable throughout deployment and operations flows.

---

## 18. Iconography

Icons must be:

- simple
- technical
- consistent in stroke/weight
- recognizable at small sizes

Icons supplement labels; they do not replace critical textual status.

Avoid decorative icon collections.

---

## 19. Accessibility

Minimum requirements:

- keyboard navigability
- visible focus
- semantic HTML
- sufficient text/background contrast
- status not communicated by color alone
- reduced-motion support
- readable technical text
- usable controls at responsive sizes
- screen-reader labels for icon-only actions

---

## 20. CSS Token Contract

The implementation source of truth is the semantic token layer.

Example:

```css
:root {
  --axiom-accent: #C5F441;
  --axiom-accent-strong: #D5FF63;
  --axiom-accent-muted: rgba(197,244,65,.14);
  --axiom-accent-focus: rgba(197,244,65,.32);

  --surface-0: #080A0C;
  --surface-1: #0D1013;
  --surface-2: #12161A;
  --surface-3: #171C21;
  --surface-inset: #07090B;

  --text-primary: #F3F5F7;
  --text-secondary: #A8B0B8;
  --text-tertiary: #747E88;
  --text-disabled: #4E565E;

  --border-subtle: rgba(255,255,255,.07);
  --border-default: rgba(255,255,255,.11);
  --border-strong: rgba(255,255,255,.17);

  --status-live: #35D07F;
  --status-building: #F5B942;
  --status-queued: #5EA7FF;
  --status-failed: #F05252;
  --status-inactive: #7C858F;
  --status-warning: #F59E4A;
}
```

---

## 21. Non-Negotiable Constraints

- No screen-specific semantic colors.
- No competing token systems.
- No arbitrary visual rules introduced by individual screens.
- No Kubernetes-only terminology in foundational UI.
- No unrestricted glow/neon treatment.
- No decorative gradients as the primary visual language.
- No status communicated only through color.
- No technical identifiers rendered as ordinary prose when copyability matters.
- No visual design that hides operational state.
- No component may bypass the canonical semantic token layer.

## 22. Acceptance Gate

This Design DNA is considered implementation-ready when:

1. Frontend agents can implement the same component from the same tokens.
2. Two implementations preserve the same semantic meaning.
3. All canonical status states map to one semantic vocabulary.
4. Technical typography is consistent.
5. Density rules are reproducible.
6. Screen-specific designs can reference tokens without inventing new foundations.
7. The visual language scales across the 20 V0.1 screens and future 30+ screen surface.
