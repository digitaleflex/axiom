# Agent Failure Matrix (M5.16, #90)

> Integration test suite for the complete Runtime Agent contract. The suite
> lives in `services/agent/tests/` (package `tests`) and exercises the real
> `internal/...` packages end to end; only the OS/process boundary (Docker CLI,
> HTTP transport, time) is faked.

## Scope

- **Real packages:** `protocol`, `identity`, `security/auth`,
  `security/ownership`, `capabilities`, `runtime/docker`, `runtime/traefik`,
  `health`, `logs`, `heartbeat`, `dispatcher`, `state`, `recovery`.
- **Fakes:** a scripted in-memory Docker Runner (`simDocker`), an `httptest`
  Engine that records every signed request (`fakeEngine`), and temp dirs for
  identity/credential/state/Traefik dynamic config.
- **No external services.** Docker-dependent tests stay isolated in
  `internal/runtime/docker/integration_test.go` and are skipped unless
  `AXIOM_TEST_DOCKER=1`; this suite needs no Docker daemon.

## Failure matrix

| Scenario | Expected behavior | Test | Owning issue |
|---|---|---|---|
| registration | Identity generated, registration request signed with the bootstrap credential, issued agent ID + credential persisted, first signed heartbeat accepted (Engine 200) | `TestScenario_Registration` | #76 #77 #78 |
| authentication failure | Engine 401 stops the heartbeat loop with `heartbeat.ErrRevoked`; no retry | `TestScenario_AuthenticationFailure` | #77 #78 |
| heartbeat timeout | Agent keeps sending on schedule; staleness is Engine-derived (`server.EffectiveStatusAt`, StaleAfter 5m); the agent only stops on 401 | `TestScenario_HeartbeatTimeout` | #78 |
| capability discovery | Discovery never fails; absent Docker/Traefik reported unavailable (no caps); present tools yield `docker`, `docker_compose`, `traefik`, `tls` | `TestScenario_CapabilityDiscovery` | #79 |
| operation success | All six closed types (`CREATE_RUNTIME`, `NETWORK`, `START`, `VERIFY`, `STOP`, `REMOVE`) execute through the real adapters; VERIFY carries the probe report | `TestScenario_OperationSuccess` | #80 #83 #84 #85 |
| duplicate operation | Same operation ID returns the cached result; the adapter runs exactly once | `TestScenario_DuplicateOperation` | #80 |
| invalid operation | Unknown type (`PREPARE`, `RUN_SHELL`, `EXEC`) rejected with `UNKNOWN_OPERATION`; no adapter call | `TestScenario_InvalidOperation` | #80 #75 |
| runtime failure | Docker adapter's stable code surfaces verbatim through the dispatcher (`RUNTIME_IMAGE_MISSING`, `RUNTIME_DOCKER_FAILED`) | `TestScenario_RuntimeFailure` | #83 #80 |
| network failure | Traefik write/render error surfaces; no partial file is written and an existing config is not mutated | `TestScenario_NetworkFailure` | #84 #80 |
| restart recovery | Reopened store transitions `RUNNING` → `INTERRUPTED`; `CREATE_RUNTIME` needs reconciliation, `VERIFY` is resumable | `TestScenario_RestartRecovery` | #81 #82 |
| reconciliation | Unmanaged containers ignored; managed+terminal consistent; managed without terminal state = missing-state; unmanaged deployment = orphan; orphan removed only under `AllowCleanup` + allow-list | `TestScenario_Reconciliation` | #82 #89 |
| health failure | `health.Checker` against an always-503 server returns `*health.UnhealthyError` (status 503, matches `ErrUnhealthy`); through the dispatcher it reports `HEALTH_CHECK_FAILED` | `TestScenario_HealthFailure` | #85 |
| log streaming | Stream channel is bounded at `logs.StreamBuffer`; overflow drops oldest and queues the truncation marker; secrets are redacted before leaving the agent | `TestScenario_LogStreaming` | #86 |
| authorization violation | Dispatcher rejects a foreign-server operation with `FORBIDDEN` before execution; Docker adapter refuses an unmanaged container with `RUNTIME_NOT_MANAGED` | `TestScenario_AuthorizationViolation` | #75 #83 #89 |

Scenario coverage: **14 / 14** from issue #90, plus one gap-documentation test
(`TestGap_DockerErrorLacksStableCodeMethod`).

## Mapping to M5 acceptance criteria (#18)

| M5 acceptance criterion | Covered by |
|---|---|
| An Agent can register | `TestScenario_Registration` |
| …receive an authorized deployment operation | `TestScenario_OperationSuccess`, `TestScenario_AuthorizationViolation`, `TestScenario_InvalidOperation` |
| …execute it safely | `TestScenario_OperationSuccess`, `TestScenario_RuntimeFailure`, `TestScenario_NetworkFailure`, `TestScenario_HealthFailure`, `TestScenario_LogStreaming` |
| …report the result | `TestScenario_OperationSuccess`, `TestScenario_DuplicateOperation`, `TestScenario_RuntimeFailure` |
| …and recover after restart | `TestScenario_RestartRecovery`, `TestScenario_Reconciliation` |
| Each critical failure mode has an automated test | This matrix |
| Tests do not require a production VPS | Fakes only at the OS/network boundary |
| Docker-dependent tests are isolated and explicitly marked | `internal/runtime/docker/integration_test.go` (skipped unless `AXIOM_TEST_DOCKER=1`) |

## Gaps found (reported, not fixed — outside this lane's owned paths)

1. **No production `dispatcher.Adapter` bridge exists.**
   `services/agent/internal/dispatcher/dispatcher.go:120-127` defines the
   adapter interface, but nothing in the module implements it over the real
   `runtime/docker`, `runtime/traefik` and `health` packages. The integration
   harness supplies this seam (`bridge` in `tests/harness_test.go`). Until a
   production bridge lands, the dispatcher has no wired runtime.

2. **`*docker.Error` does not implement `dispatcher.ErrorCoder`.**
   The Docker adapter exposes its stable code as the struct field
   `Error.Code` (`services/agent/internal/runtime/docker/docker.go:52-58`) but
   has no `ErrorCode() string` method, which is what
   `dispatcher.ErrorCoder` requires (`dispatcher.go:132`). Consequently
   `classifyError` (`dispatcher.go:438-448`) degrades every raw Docker failure
   to `INTERNAL`. A bridge must map `Code` → `ErrorCode()`; the harness does,
   and `TestGap_DockerErrorLacksStableCodeMethod` documents the raw deficiency.
   Fix belongs to `runtime/docker` (#83), which is out of this lane.

3. **`dispatcher.CreateParams` carries no deployment/server identity.**
   `dispatcher.go:76-80` passes only `ImageRef`/`Container`/`Port`, while
   `docker.CreateSpec` (`docker.go:189-206`) needs `DeploymentID`,
   `ApplicationID` and `ServerID` to stamp the canonical ownership labels.
   The bridge must be configured with the deployment out-of-band. Consider
   threading the deployment through the adapter boundary so the bridge cannot
   mislabel resources.

4. **Traefik and health errors have no canonical machine codes.**
   The Traefik adapter returns typed sentinels
   (`traefik.go:46-61`) and the health package returns
   `*health.UnhealthyError`; neither defines a wire `errorCode`. The protocol
   only names `RUNTIME_IMAGE_MISSING` / `HEALTH_CHECK_FAILED` as examples
   (`protocol.go:297-299`). The harness maps Traefik failures to
   `NETWORK_CONFIG_FAILED` and health failures to `HEALTH_CHECK_FAILED`;
   a canonical code set should be defined by #84/#85.

5. **`cmd/agent/main.go` is not wired to the M5 packages.**
   `services/agent/cmd/agent/main.go:21-22` still builds the old
   `agent.LocalRuntime` skeleton and never constructs identity, auth,
   heartbeat, dispatcher, state or recovery. The M5 acceptance path
   (register → heartbeat → dispatch → recover) therefore has no production
   entry point yet; this lane wires the real packages only in tests.

## Running the suite

```sh
cd services/agent
gofmt -w .
go test -race -count=1 ./...
```

`TestScenario_LogStreaming` uses a large source payload with a 1 ms poll
interval to force overflow deterministically; all other tests are
sleep-free apart from 1 ms polling used to observe asynchronous heartbeats and
the streaming producer.
