# Operational metrics

> Issues #87 (Agent) and #103 (Engine), [ADR-0006](../adr/0006-observability-standards.md).
> Registries: `services/agent/internal/metrics/` and
> `services/engine/internal/observability/metrics/`. Engine route:
> `GET /metrics` (`services/engine/internal/httpserver/server.go`).

Both services expose **Prometheus text exposition** (`text/plain; version=0.0.4`)
from a small in-process registry. There is no third-party metrics client: the
registry, histograms and formatter are stdlib-only, so the Agent keeps its
zero-dependency contract and the Engine adds nothing to its dependency set.

## 1. Endpoint

| Service | Endpoint | Implementation |
| --- | --- | --- |
| Engine | `GET /metrics` | `httpserver.NewHandler` mounts `metrics.Handler(registry)`; override the default empty registry with `httpserver.WithMetrics(reg)` |
| Agent | not served (no HTTP listener) | `Registry.WriteText` / `Registry.Text`; the Engine/observability layer consumes the snapshot |

The Engine route is registered with the Go 1.22 method pattern `GET /metrics`;
`/health` (liveness) and `/ready` (readiness) are unchanged.

## 2. Metric names & labels

### Agent (`axiom_agent_`)

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `axiom_agent_uptime_seconds` | gauge | — | Seconds since the process started |
| `axiom_agent_heartbeat_age_seconds` | gauge | — | Seconds since the last successful heartbeat |
| `axiom_agent_operations_total` | counter | `type`, `result` | Completed operations |
| `axiom_agent_operation_duration_seconds` | histogram | `type` | Operation duration (seconds) |
| `axiom_agent_runtimes` | gauge | `state` | Managed runtimes by state |
| `axiom_agent_resources` | gauge | `resource` | Snapshot: `cpu`, `memory_mb`, `disk_free_mb` |
| `axiom_agent_docker_errors_total` | counter | `code` | Docker errors by stable code |

### Engine (`axiom_engine_`)

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `axiom_engine_http_requests_total` | counter | `method`, `status` | API requests |
| `axiom_engine_http_request_duration_seconds` | histogram | `method` | API request duration |
| `axiom_engine_deployments` | gauge | `state` | Deployments by state |
| `axiom_engine_operation_duration_seconds` | histogram | `step` | Execution step duration |
| `axiom_engine_operation_failures_total` | counter | `code` | Operation failures by stable code |
| `axiom_engine_heartbeat_age_seconds` | gauge | `server` | Last heartbeat age per server |
| `axiom_engine_runtimes` | gauge | `state` | Runtimes by state |
| `axiom_engine_resources` | gauge | `resource` | Snapshot: `cpu`, `memory_mb`, `disk_free_mb` |

Histogram buckets default to the conventional Prometheus set
(`.005 .01 .025 .05 .1 .25 .5 1 2.5 5 10` seconds); buckets are injected at
registration and rendered cumulatively with an implicit `+Inf`.

## 3. Cardinality & secrecy rules

- **Label keys are a fixed allow-list.** Registration returns an error for any
  key outside it, for duplicates, and for empty keys. Agent allow-list:
  `type`, `result`, `state`, `resource`, `code`. Engine adds `method`,
  `status`, `step`, `server`.
- **Deployment/request/correlation IDs are never label values.** They are
  unbounded; using them would make series cardinality grow with every
  deployment or request. Registration rejects `deploymentId`, `requestId`,
  `correlationId` (and snake_case variants) with a dedicated error. The Engine
  also rejects `path`/`url`/`query`. Use a bounded dimension instead.
- **Label values are bounded.** Each value is truncated to 64 runes and marked
  with `…` (U+2026). At runtime only declared label keys are read, so a caller
  cannot introduce new series through an unexpected key.
- **No secrets are representable.** Metric values are `float64`; there is no
  string-value metric and no label key in the allow-list that could carry a
  credential. The exposition is asserted to contain only numeric values.
- **`server` cardinality** (Engine `heartbeat_age_seconds`) is bounded by the
  number of registered servers, which is operator-managed rather than
  request-driven, so it is an acceptable label.

## 4. Determinism

Output is byte-stable: metric families are sorted by name, and samples within a
family are sorted by their encoded label set. This makes snapshots safe to
golden-test and to diff.

## 5. Measurement wiring TODOs

This lane delivers the registry, the formatter and the route. The registries
currently have no producers; wiring real measurements is a follow-up. Exact
call sites:

1. **API request count + duration** — `internal/api/middleware.go:63`
   (`(*API).accessLog`). Increment `axiom_engine_http_requests_total{method,status}`
   with `rec.status` and observe
   `axiom_engine_http_request_duration_seconds{method}` with
   `time.Since(start).Seconds()`. The `API` struct needs the `*metrics.Registry`
   injected (via `buildAPIDeps`, `internal/bootstrap/bootstrap.go`).
2. **Execution step duration + failures** — `internal/executor/executor.go:226`
   (`(*PlanExecutor).step`), with the overall run at `executor.go:55`
   (`(*PlanExecutor).Execute`). Observe
   `axiom_engine_operation_duration_seconds{step}` per attempt and increment
   `axiom_engine_operation_failures_total{code}` with the stable error code.
3. **Agent heartbeat ingestion** — `internal/api/agent_heartbeat.go:40`
   (`(*API).agentHeartbeat`). On success set
   `axiom_engine_heartbeat_age_seconds{server}` to 0 and refresh
   `axiom_engine_runtimes{state}` / `axiom_engine_resources{resource}` from the
   ingested snapshot; a background sweep should recompute heartbeat age so
   stale servers age without new requests.
4. **Agent-side producers** — `services/agent/internal/agent/runtime.go` and
   `services/agent/internal/heartbeat/heartbeat.go` should update uptime,
   heartbeat age, operation counters/durations, runtime counts and Docker
   error codes. The snapshot is then shipped to (or scraped by) the Engine.
5. **Composition root** — `internal/bootstrap/bootstrap.go:73` should build one
   `metrics.NewEngineRegistry()` and pass it both to the API/executor and to
   `httpserver.WithMetrics(reg)` so producers and the endpoint share a registry:

   ```go
   registry, _ := metrics.NewEngineRegistry()
   handler := httpserver.NewHandler(cfg, db, api.New(deps), httpserver.WithMetrics(registry))
   ```
