# Engine structured logging & correlation

> Issue #101, [ADR-0006](../adr/0006-observability-standards.md). Convention and helpers:
> `services/engine/internal/observability/logging/`. JSON logger construction:
> `services/engine/internal/logger/`.
>
> Goal: a deployment is traceable across **API → Engine → Agent → runtime**
> using structured JSON logs, without ever exposing a credential.

This document defines the field names, severities, propagation rules and
redaction rules for every Engine log line. The `logging` package is the shared
implementation; call sites adopt it incrementally (see [§7](#7-adoption-todos)).

## 1. Principles

1. **Structured JSON only.** All services log through `log/slog` with the JSON
   handler built in `internal/logger.New`. No `fmt.Printf`, no unstructured
   `log` package.
2. **One identifier set.** Correlation fields use fixed camelCase keys:
   `requestId`, `correlationId`, `deploymentId`. They are constants in
   `logging` (`FieldRequestID`, …) so renames are compile-checked.
3. **Every line is attributable.** A line is emitted with `service` and
   `component` (from `logging.NewLogger`) and, where relevant, the operation
   name.
4. **Secrets never reach the sink.** Redaction runs before emission
   (`logging.Redact`, `logging.NewRedactingHandler`) and again before durable
   persistence (`internal/logs.Redact`). See [§6](#6-never-logged).
5. **IDs are tracing-compatible.** No OpenTelemetry in V0.1 (ADR-0006); the
   identifier set is designed to feed a tracer later without renaming fields.

## 2. Required fields by layer

| Layer | Emitter | Required fields |
| --- | --- | --- |
| API access log | `api.(*API).accessLog` (`internal/api/middleware.go:63`) | `requestId`, `method`, `path`, `status`, `durationMs` |
| API error (5xx) | `api.(*API).writeError` (`internal/api/http.go:28`) | `requestId`, `method`, `path`, `error` (redacted) |
| API audit | `api.(*API).audit` (`internal/api/http.go:121`) | `requestId`, `actor`, `action`, `target`, `result` |
| Executor step | `executor.(*PlanExecutor).Execute` / `step` (`internal/executor/executor.go:55`) | `deploymentId`, `correlationId`, `step`, `attempt`, `durationMs`, `result` |
| Build | `build.(*Engine).Build` (`internal/build/engine.go:127`) | `deploymentId`, `step=BUILD`, `source=build`, `level` (INFO/ERROR) |
| Agent operation | `agent.(*LocalRuntime)` (`services/agent/internal/agent/runtime.go:25`) | `deploymentId`, `correlationId`, operation name, target (container/domain) |
| Analysis | `analysis.(*Service)` (`internal/analysis/service.go:165`) | `analysisId`, `applicationId`, `commit`, `profileStatus`, `profileVersion`; failures add `errorCode`, `cause` |
| GitHub connection | `ghauth.(*Service)` (`internal/github/auth/service.go:174`) | `connectionId`, `account`, `userId`; failures add `error`, `reason` |
| Lifecycle | `bootstrap` / `cmd/engine` | `service`, `env`, `version`, `addr`, `database` |

Conventions:

- Correlation keys are camelCase in logs. The Agent currently uses snake_case
  (`deployment_id`) — adopting `logging` converges it on `deploymentId`
  ([§7](#7-adoption-todos)).
- `result` is a short machine code: `ok` on success, or the same stable error
  code the API/executor records (`BUILD_FAILED`, `HEALTH_CHECK_FAILED`, …).
  It is never a human sentence.
- `error` holds a redacted message; prefer a stable code in `result` and keep
  `error` for operator detail.

## 3. Severity guidance

| Level | When | Examples in the codebase |
| --- | --- | --- |
| **DEBUG** | High-volume diagnostics, disabled by default. Safe to drop. | `write response` failures (`internal/api/http.go:49`) |
| **INFO** | Normal lifecycle and successful operations: state changes, start/end of a step, audits, access logs. | `axiom engine started/stopped` (`bootstrap.go:217,240`); `http request` (`middleware.go:68`); `audit` (`http.go:126`); `analysis completed` (`analysis/service.go:165`); `github connection established` (`auth/service.go:174`); step `start`/`end` |
| **WARN** | Degradation or denial the system handled without failing: retries, policy denials, optional dependencies unavailable, rejected input that was expected to be rejected. | `deployment denied by policy` (`executor.go:87`); `step attempt failed, retrying` (`executor.go:243`); `record health failure` (`executor.go:162`); `github callback rejected` (`api/github.go:62`); `running without database` (`bootstrap.go:169`); `github grant revocation failed` (`auth/service.go:212`) |
| **ERROR** | An operation failed and needs attention; a 5xx is returned, a deployment failed, or a panic was recovered. | `request failed` for 5xx (`http.go:33`); `panic` (`middleware.go:81`); build `ERROR` log lines (`build/engine.go:147`); `analysis failed` (`analysis/service.go:225`); `execution finished with error` (`executor/runner.go:66`) |

Rules of thumb:

- A retried attempt logs **WARN**; only the final failure logs **ERROR**.
- A client error (4xx) is not an Engine failure: log at **INFO**/**DEBUG**,
  not **ERROR**.
- Never log the same failure at ERROR twice (e.g. executor *and* API) without
  different `component` values — duplicate ERROR lines hurt alerting.

## 4. Correlation propagation

```
client X-Request-ID ──▶ requestIDMiddleware (validate or generate req_<16hex>)
        │                        internal/api/middleware.go:28
        ▼
   requestId in ctx ──▶ access log / error envelope / audit
        │
        ▼  executor.Request.CorrelationID  (api → executor)
   correlationId (req_…)  ── generated if empty: deployment.NewID("req")
        │                        internal/executor/executor.go:63
        ▼
   protocol.Envelope.CorrelationID ──▶ Agent operation
        │                        services/agent/internal/protocol/protocol.go:75
        ▼
   Agent logs / results carry the same correlationId
```

Rules:

1. `requestId` is the API request identifier. `requestIDMiddleware` accepts a
   well-formed client `X-Request-ID` (`^[A-Za-z0-9._-]{1,64}$`) or generates
   `req_<16 hex>`, echoes it in the response and stores it in the context
   (`internal/api/middleware.go:24-38`).
2. `correlationId` is the execution identifier propagated to the Agent. The
   API passes the request ID as `executor.Request.CorrelationID`; the executor
   reuses it or generates one when empty (`executor.go:63-66`).
3. Every Agent operation carries it: `newOperation` sets
   `Operation.CorrelationID` (`executor.go:215-221`) and the protocol envelope
   validates the `req_[0-9a-f]{16}` shape
   (`services/agent/internal/protocol/protocol.go:60,88`). The agent must log
   the same value so Engine and agent lines join.
4. `deploymentId` ties every line of a deployment together even when the
   request ID is absent (e.g. background retries).
5. Context helpers: set with `logging.WithRequestID/WithCorrelationID/
   WithDeploymentID`, read with `logging.RequestID/CorrelationID/DeploymentID`,
   and attach to a logger with `logging.With(log, ctx)` or `logging.Attrs(ctx)`.
6. **Known gap:** persisted deployment events carry `deploymentId` and `seq`
   but not `correlationId` (`internal/deployment/event.go:20-28`,
   `internal/deployment/memstore.go:252-285`). End-to-end traceability from the
   SSE stream therefore currently relies on `deploymentId`. Adding
   `correlationId` to event data is tracked as a follow-up ([§7](#7-adoption-todos)).

## 5. Operation logging

For work that spans a measurable duration, emit a start/end pair with
`logging.LogStart` / `logging.LogEnd`:

```go
start := logging.LogStart(ctx, log, "BUILD", "step", "BUILD")
// … work …
logging.LogEnd(ctx, log, "BUILD", start, "ok", "step", "BUILD")
// or, on failure:
logging.LogEnd(ctx, log, "BUILD", start, "BUILD_FAILED", "step", "BUILD")
```

- `LogStart` logs `<op> start` at INFO.
- `LogEnd` logs `<op> end` with `durationMs` and `result`; INFO when `result`
  is `ok`/empty, ERROR otherwise.
- Both attach `requestId`/`correlationId`/`deploymentId` from `ctx`, so start
  and end join on `operation` and can be aggregated by `durationMs`.

## 6. Never logged

The following must never appear in any log line, at any level, even redacted
where the value itself is the secret:

- Authorization headers, bearer tokens, bootstrap credentials.
- Passwords, API tokens, OAuth client secrets, access/refresh tokens.
- Private keys (PEM blocks) and the secret encryption key (`cfg.SecretKey`).
- Credentials embedded in URLs (clone URLs, database URLs).
- Full environment variable dumps.
- Request or response **bodies**, headers and query strings — the access log
  deliberately logs only method, path, status and duration
  (`internal/api/middleware.go:61-62`).
- Raw source archives or repository contents.

Defences, in order:

1. **Don't log it.** The list above is a caller obligation.
2. **Redact free text** with `logging.Redact` (same regex set as
   `internal/logs.Redact`): bearer tokens, `password=`/`token=`/`secret=`
   pairs, JSON secret fields, URL credentials, PEM blocks → `[REDACTED]`.
3. **Redact structured attrs** with `logging.NewRedactingHandler`: it redacts
   the message and, by key, any attribute whose normalized name ends in
   `password`, `passwd`, `secret`, `token`, `apikey`, `credential`,
   `authorization` or `privatekey` (covers `apiToken`, `api_token`,
   `github-token`, `clientSecret`, …), recursing into groups and error values.
   Wrap the base handler once at the composition root.

The marker `[REDACTED]` is intentionally kept: readers can tell a value was
present without seeing it (API contract §14).

## 7. Sampling & retention

There is no log sampling in V0.1; severity filtering (`LOG_LEVEL`) and the
caps below bound volume instead.

- **Durable deployment logs** are capped at `logs.DefaultMaxEntries = 10_000`
  entries **per deployment**, trimmed in the same transaction as the inserts
  (`internal/logs/store.go:11-13,75-85`). The cap is configurable through
  `logs.NewPGStore(db, maxPerDeployment)`.
- **Durable log queries** default to 100 entries and cap at 1 000 per page
  (`internal/logs/store.go:15-18`).
- **Agent runtime log fetch** defaults to 100 lines, caps at 10 000
  (`services/agent/internal/logs/logs.go:55-58`); each message is truncated to
  8 KiB (`maxMessageBytes`) and streaming uses a 100-entry buffer with
  drop-oldest backpressure plus a truncation marker (`StreamBuffer`).
- **Request bodies** are bounded to 1 MiB at the API edge
  (`internal/api/http.go:15`); they are never logged.
- **SSE streams** are long-lived and never buffered to disk; the persisted
  event log is the source of truth.

## 8. Adoption TODOs

This lane introduces the convention and helpers only. The following call sites
should adopt them next (no behavior change expected):

1. `internal/executor/executor.go:67` — replace the manual
   `log.With("deploymentId", id, "correlationId", corr)` with
   `ctx = logging.WithCorrelationID(logging.WithDeploymentID(ctx, id), corr)`
   and `logging.With(log, ctx)`, so every downstream line inherits the IDs.
2. `internal/executor/executor.go:226` (`step`) — wrap each step with
   `logging.LogStart`/`LogEnd` to emit `durationMs` + `result` per attempt
   (today only the retry at line 243 is logged).
3. `internal/build/engine.go:144-162` and `internal/executor/runner.go:65` —
   emit BUILD start/end through `logging.LogStart`/`LogEnd` and use
   `logging.RedactError` for the `error` field.
4. `services/agent/internal/agent/runtime.go:26-58` — switch `deployment_id`
   to `deploymentId`, add `correlationId` (from the operation envelope), and
   build the logger with `logging.NewLogger("axiom-agent", "runtime")`.
5. `cmd/engine/main.go:28` / `internal/logger/logger.go` — wrap the base
   handler with `logging.NewRedactingHandler` so redaction is a logger
   property, not a per-call discipline.

Follow-up gap (needs a code change outside this lane):

6. `internal/deployment/event.go:20-28` + `internal/deployment/memstore.go` —
   add `correlationId` to `statusEventData`/`stepEventData` so the SSE stream
   and `GET /deployments/{id}/events` expose the full trace chain required by
   the acceptance criteria.
