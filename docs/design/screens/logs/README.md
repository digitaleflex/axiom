# Application Logs Screen v1.0

> Screen contract — issue #137. Also the canonical **Log Viewer** behavior reused by Deployment Progress/Logs tabs (#136).
>
> Depends on: Design DNA (#131, §14), Progressive Disclosure (#132, §6), Shell & Navigation (#133, §5.4).
> Contract sources: `docs/architecture/api-contract.md` §14–§15; #66 (logs & events), #86 (runtime logs & streaming), #102 (diagnostics API), #126 (secret protection).

## 1. Purpose

Read the logs of **this application in this environment** — build, deployment and runtime output — correlated to deployments.

State-dominant screen (no primary button); the live log is the hero.

Scope statement, always visible in the toolbar: "Logs from acme-web · Production". The UI never implies access to unrestricted host or server logs.

## 2. Shell

Variant **A**, sidebar "Logs", route `/apps/:applicationId/:environment/logs`.
Query state (navigation §5.4): `q`, `regex`, `severity`, `range`, `deployment`, `source`, `follow`.

## 3. Layout

```
Title: Logs  ■ Production
Toolbar: [🔍 Search  .*]  [Severity: ≥ info ▾]  [Source: all ▾]  [Deployment: current (#42) ▾]  [Last 1h ▾]   [● Live] [⏸] [Copy] [Download]
Scope:   Logs from acme-web · Production · bounded to the last 7 days

┌──────────────────────────────────────────────────────────────────────────┐
│ 14:05:12.381  INFO   runtime  #42  Server listening on :3000             │
│ 14:06:02.004  WARN   runtime  #42  Slow query (812 ms)                   │
│ 14:07:44.910  ERROR  runtime  #42  TypeError: Cannot read …  ▸ 14 lines  │
│ ── Deployment #43 started · 14:10:01 ─────────────────────────────────── │
│ 14:10:20.112  INFO   build    #43  pnpm install --frozen-lockfile        │
└──────────────────────────────────────────────────────────────────────────┘
                                                          ▼ 23 new lines
```

## 4. Line Anatomy

| Part | Presentation |
|---|---|
| Timestamp | mono, `--text-tertiary`, millisecond precision; timezone toggle (local/UTC) in toolbar overflow |
| Severity | fixed-width label `DEBUG/INFO/WARN/ERROR` + color: ERROR `--status-failed`, WARN `--status-warning`, INFO `--text-secondary`, DEBUG `--text-tertiary` — label always present |
| Source | `build`, `deploy`, `runtime` (user-intent labels, not component names) |
| Deployment | `#42` link → deployment (correlation) |
| Message | mono, `--surface-inset` background for the viewer, stable line height |

Deployment boundaries are rendered as separator rows ("Deployment #43 started / went LIVE / failed"), linking to the deployment.

## 5. Features (#137)

| Feature | Behavior | Contract dependency |
|---|---|---|
| Live stream | **Live** toggle on by default for `range` ending now; new lines append | runtime log stream (**gap**) |
| Pause / resume | ⏸ freezes the view; buffer counter "N new lines"; resume jumps to bottom | client |
| Follow | auto-scroll while at bottom; scrolling up pauses follow (not the stream) | client |
| Timestamps | §4 | — |
| Severity filter | minimum level selector | `level` param |
| Deployment / runtime correlation | deployment column + separators + `deployment` filter | deployment ID per line (**gap** for runtime logs) |
| Source filter | build / deploy / runtime | **gap** |
| Search | plain text by default; `.*` toggles regex **only where supported** by the API; otherwise the toggle is hidden | server-side search (**gap**); client-side search limited to loaded lines, labelled "Searching loaded lines" |
| Folding | multi-line entries (stack traces) collapsed to first line + "▸ N lines"; expand per entry or "Expand all" | Engine groups multi-line entries or client groups by continuation heuristics |
| Copy | per line (hover action), selection copy, "Copy visible" | client |
| Download | exports current filter result | **only if** API permits (**gap**); otherwise not rendered |
| Bounded history | scope line states retention; scrolling to top loads older pages via cursor until retention limit, then "Start of retained logs (7 days)" | `cursor`, `limit` |
| Secret redaction | Engine redacts before transmission (API §14); the UI renders `[REDACTED]` markers with a tooltip "Hidden by Axiom", never attempts to reconstruct | Engine |

## 6. Disclosure

| Level | Content |
|---|---|
| L2 | lines, severity, source, deployment correlation |
| L3 | expanded stack traces, raw metadata drawer per line (request ID, step) |
| L4 | runtime identifiers (container ID) inside line metadata drawer, labelled with context (`acme-web → Production → #42 → Runtime → Container`) |

## 7. States

| State | Behavior |
|---|---|
| Loading | 12 skeleton lines inside viewer |
| No logs in range | "No logs from acme-web · Production in the last 1h" + widen range |
| Not deployed | environment empty state (application §1.6) |
| Stream disconnected | toolbar Live indicator → WARNING "Reconnecting…"; lines kept; refetch gap on reconnect, separator "Gap while reconnecting (14:12:03–14:12:41)" if lines may be missing |
| Rate-limited / truncated | separator "Output truncated by Axiom (N lines dropped)" when the Engine reports it |

## 8. Performance & Accessibility

- Virtualized list; DOM bounded (e.g. ≤ 2,000 rendered rows); buffer cap with oldest-line eviction notice.
- Viewer is `role="log"` with `aria-live="off"` (too noisy); a separate polite announcement only for ERROR lines when the user enables "Announce errors".
- Keyboard: `/` focuses search, `Space` toggles pause when viewer focused, `j/k` move line focus, `Enter` expands folded entry.
- Respects reduced motion (no smooth scroll).

## 9. Contract Gaps

The API defines only **deployment** logs (`GET /deployments/{id}/logs`, params `step`, `level`, `cursor`, `limit`). Application Logs require:

| Need | Owner |
|---|---|
| Application/environment-scoped runtime logs endpoint (paged) | #86 / #102 / #117 |
| Runtime log stream (SSE) | #86 / #118 |
| Per-line `source` and `deploymentId` | #66 / #86 |
| Server-side text/regex search capability flag | #102 |
| Export/download permission | #102 / #127 |
| Retention window value | #86 |

Until available, Application Logs can be composed from deployment logs of recent deployments in the environment, labelled "Deployment logs only — runtime logs not yet available".

## 10. Acceptance Checklist

- [ ] Scope (application + environment) always visible; no host-log implication.
- [ ] Severity is text + color; timestamps mono.
- [ ] Every line correlated to a deployment where data allows.
- [ ] Regex, download shown only when supported.
- [ ] Redaction markers rendered verbatim.
- [ ] Filters and range survive refresh and sharing.
