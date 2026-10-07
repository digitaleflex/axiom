# Application Metrics Screen v1.0

> Screen contract — issue #137.
>
> Depends on: Design DNA (#131, §16 Data Visualization), Progressive Disclosure (#132, §7 Runtime-Aware Disclosure), Shell & Navigation (#133).
> Contract sources: #87 (metrics & telemetry), #103 (metrics endpoint & operational metrics).

## 1. Purpose

Answer "is this application healthy and does it have enough resources?" for the selected application and environment, with every chart tied to an operational signal.

State-dominant screen; no primary button.

## 2. Shell

Variant **A**, sidebar "Metrics", route `/apps/:applicationId/:environment/metrics`. Query: `range` (`15m`, `1h` default, `24h`, `7d` or `from`/`to`), `metric` (focused chart).

## 3. Ownership Layers

Metrics are grouped by **who owns the signal** (#137), in this order, each a labelled section:

| Layer | Owner (user-facing) | Signals (V0.1, where available) | Default level |
|---|---|---|---|
| Application | your application | health check status & latency, request rate / error rate / latency **only if the runtime exposes them** | L1/L2 |
| Runtime | the running instance of your app | CPU %, memory used / limit, restarts | L2 |
| Network | traffic reaching your app | ingress requests, response codes, throughput (bytes/s) | L2 |
| Server | srv-eu-1 (shared) | server CPU, memory, disk — labelled "Shared by N applications" | L3 (collapsed) |

- Section label wording avoids component names: "Runtime" not "Container", "Network" not "Traefik". Component names may appear in the L4 metadata of a chart ("Source: container runtime").
- Server metrics are collapsed by default and clearly marked as not specific to the application.

## 4. Runtime-Aware Rules

- Show only metrics the Engine reports for this application's runtime and strategy. No empty placeholder charts for unsupported metrics.
- Never show Kubernetes, pod, node-pool, mesh or eBPF metrics.
- Unsupported-but-common metrics (e.g. request latency for an app not instrumented) appear as one line under the Application section: "Request metrics aren't available for this runtime in V0.1." — not as empty charts.

## 5. Chart Rules (Design DNA §16)

| Rule | Implementation |
|---|---|
| Explicit units | axis + value labels: `%`, `MB`/`GB`, `req/s`, `ms`, `B/s` |
| Explicit time range | range in section header ("Last 1h · 1-min resolution") |
| Thresholds | memory limit drawn as a labelled line; health check failures as markers |
| Deployment correlation | vertical markers for deployments in range ("#42 LIVE", "#41 Failed") with tooltip + link |
| Current value | large numeric value (mono) beside each chart, with unit |
| Semantics | default series in `--text-secondary`/neutral; `--status-warning` / `--status-failed` only for threshold breaches, always with text annotation |
| No decoration | no gradients, no 3D, no area fills beyond 10% opacity |
| Shared crosshair | hovering one chart shows the same timestamp across charts |

Each chart has a one-line purpose caption, e.g. "Memory close to limit causes restarts."

## 6. Layout

```
Title: Metrics  ■ Production             [Last 1h ▾]
Application   Health ● Healthy 99.8 % checks passing · p95 check latency 91 ms
              [chart: health check latency, failure markers, deployment markers]
Runtime       CPU 12 %   [chart]        Memory 312 MB / 1 GB   [chart + limit line]   Restarts 0
Network       Requests 42 req/s [chart]  5xx 0.1 % [chart]
▸ Server srv-eu-1 · shared by 3 applications
```

Responsive: charts stack to one column below `--bp-lg`; current values remain visible above each chart.

## 7. Disclosure

| Level | Content |
|---|---|
| L1 | health summary sentence |
| L2 | application, runtime, network charts with current values |
| L3 | server section, per-metric resolution, raw values table toggle (accessible alternative) |
| L4 | metric source identifiers, sampling details |

## 8. States

| State | Behavior |
|---|---|
| Loading | chart skeletons with titles and units |
| No data yet | "Metrics appear a few minutes after the first deployment" |
| Gap in data | broken line segment + annotation "No data (server unreachable)" — never interpolated |
| Server offline | section WARNING; last values marked stale with timestamp |
| Not deployed | environment empty state |

## 9. Accessibility

- Every chart has a text summary (`aria-describedby`): current, min, max, trend over range.
- "View as table" toggle for each chart.
- Threshold breaches described in text, not only by line color.

## 10. Contract Gaps

No application metrics query endpoint exists in `docs/architecture/api-contract.md`. #103 targets Prometheus-compatible operational metrics. The UI needs:

| Need | Owner |
|---|---|
| Application/environment-scoped metric query (series, range, resolution) via Engine API | #87 / #103 / #117 |
| Capability list of available metrics per runtime | #87 |
| Deployment markers in range (can reuse deployments list) | #117 |
| Server sharing count | #79 / #138 |

The Console never queries a metrics backend directly; it goes through the Engine API (V0.1 scope boundary).

## 11. Acceptance Checklist

- [ ] Sections separated by ownership: application, runtime, network, server.
- [ ] Only runtime-supported metrics rendered; no empty or irrelevant charts.
- [ ] Every chart has units, range, purpose caption and deployment markers.
- [ ] Gaps shown, never interpolated.
- [ ] Server metrics marked as shared and collapsed by default.
