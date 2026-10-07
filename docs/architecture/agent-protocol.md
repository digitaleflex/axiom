# Axiom Agent ↔ Engine Protocol v1

> Issue #75. Domain contract for all Runtime Agent communication. Machine types: `services/agent/internal/protocol/` (agent module). Transport selection: [ADR-0008](../adr/0008-agent-engine-communication.md) (proposed).
>
> This document defines **what** is said. **How** bytes move (long-poll HTTPS, WebSocket, gRPC stream) is a transport decision and must not change these messages.

## 1. Principles

1. The agent initiates every exchange (works behind NAT; the Engine never dials servers).
2. The Engine authorizes every operation; the agent re-validates before executing.
3. Operations are closed, typed values — there is no shell, command string or script payload anywhere in this protocol.
4. Server identity is bound to agent identity: an operation naming another server is rejected.
5. Every message carries a protocol version, a unique message ID, a timestamp and (when caused by a user action) a correlation ID.
6. Retried work is idempotent: operation IDs embed deployment, step and attempt.

## 2. Versioning

- Current version: `1` (`protocol.Version`), minimum supported: `1`.
- At registration both sides declare `[min, max]`; they use the highest common version (`Compatible`).
- A message with an unsupported version is rejected with `VERSION_MISMATCH`; the versions themselves never break parsing (envelope first).
- Breaking changes require a new version; additive optional fields do not.

## 3. Envelope

Every message starts with:

```json
{ "protocol": 1, "messageId": "msg_…", "sentAt": "2026-10-07T10:00:00Z", "correlationId": "req_…" }
```

- `messageId`: unique per transmission (`[A-Za-z0-9_.-]{1,128}`), used for dedupe and tracing.
- `sentAt`: RFC3339. Receivers reject messages more than 5 minutes in the future or older than 24h; duplicates within the window are rejected by ID (`Dedupe`).
- `correlationId`: the API request (`req_…`) that caused the work; present on operations, acks, results and errors caused by user actions.

## 4. Identity & Registration

The Engine issues the agent ID at registration; the agent stores it alongside its server binding.

### RegistrationRequest (agent → Engine, once, with the bootstrap credential on the transport)

```json
{ "protocol": 1, "messageId": "…", "sentAt": "…",
  "serverId": "srv_…", "agentVersion": "0.1.3", "capabilities": ["docker", "traefik"] }
```

### RegistrationResponse (Engine → agent)

```json
{ "protocol": 1, "messageId": "…", "sentAt": "…",
  "agentId": "agent_…", "serverId": "srv_…", "negotiated": 1, "heartbeatIntervalSeconds": 30 }
```

### AgentIdentity (every later message)

```json
{ "agentId": "agent_…", "serverId": "srv_…" }
```

## 5. Heartbeat (#78)

Agent → Engine on the negotiated interval:

```json
{ "protocol": 1, "messageId": "…", "sentAt": "…",
  "agentId": "agent_…", "serverId": "srv_…", "status": "READY",
  "capabilities": ["docker", "traefik"], "cpuCount": 4, "memoryMb": 8192, "diskFreeMb": 50000 }
```

`status` is the agent-observed state (`READY`, `DEGRADED`, `OFFLINE`). The Engine derives staleness from arrival time; a missing heartbeat is an Engine-side timeout, never an agent message.

## 6. Operation Dispatch (Engine → agent, executed by #80)

```json
{ "protocol": 1, "messageId": "…", "sentAt": "…", "correlationId": "req_…",
  "operationId": "op_dep_01J…_CREATE_RUNTIME_1", "type": "CREATE_RUNTIME",
  "deploymentId": "dep_…", "serverId": "srv_…",
  "payload": { "imageRef": "sha256:…", "container": "axiom-app-1", "port": 3000 } }
```

### Closed operation set (V0.1)

| Type | Payload | Effect |
|---|---|---|
| `CREATE_RUNTIME` | `imageRef`, `container`, `port` | create (not start) the runtime |
| `NETWORK` | `container`, `proxy`, `domain`, `tls`, `port` | route hostname with TLS |
| `START` | `container` | start the runtime |
| `VERIFY` | `domain`, `path`, `timeoutSeconds` | probe until success or timeout |
| `STOP` | `container` | stop the runtime (rollback/cancel path) |
| `REMOVE` | `container` | remove the runtime and its data |

### Validation order (agent, before anything executes)

1. Envelope (version, freshness, format).
2. Type is in the closed set — anything else is rejected with `UNKNOWN_OPERATION`.
3. `operationId` matches `op_<deploymentID>_<STEP>_<attempt>`.
4. `deploymentId` is well-formed; `serverId` equals the agent's bound server — otherwise rejected (identity binding).
5. Payload matches the type's schema.

### Correspondence with the Engine executor

`services/engine/internal/executor` builds the same envelope (`Operation{OperationID, CorrelationID, DeploymentID, ServerID}` + typed request). The transport adapter (#80) translates it 1:1 into this protocol. Field names are identical on both sides by design; the two Go modules cannot share the package (separate modules, ownership boundary), so this table is the conformance contract:

| Executor (`services/engine`) | Protocol (`services/agent`) |
|---|---|
| `Operation.OperationID/CorrelationID/DeploymentID/ServerID` | `Operation.operationId/correlationId/deploymentId/serverId` |
| `CreateRuntimeRequest{ImageRef, Container, Port}` | `CREATE_RUNTIME{imageRef, container, port}` |
| `NetworkRequest{Container, Proxy, Domain, TLS, Port}` | `NETWORK{container, proxy, domain, tls, port}` |
| `StartRequest{Container}` | `START{container}` |
| `HealthCheckRequest{Domain, Path, TimeoutSeconds}` | `VERIFY{domain, path, timeoutSeconds}` |

## 7. Acknowledgement & Result (agent → Engine)

Ack (immediate, after validation, before execution) distinguishes *rejected* from *running*:

```json
{ "operationId": "op_…", "deploymentId": "dep_…", "accepted": true }
```

Result (after execution):

```json
{ "operationId": "op_…", "deploymentId": "dep_…", "success": true,
  "health": { "statusCode": 200, "latencyMs": 84, "attempt": 1 },
  "finishedAt": "…" }
```

Failures carry a stable `errorCode` (`RUNTIME_IMAGE_MISSING`, `HEALTH_CHECK_FAILED`, …) and a human message without secrets. VERIFY results carry the probe report the Engine persists as `health.passed` / `health.failed` (#65).

## 8. Capability & Resource Reports

Capabilities ride on registration and heartbeat (`capabilities: ["docker", "traefik", …]` plus `cpuCount`, `memoryMb`, `diskFreeMb`). Full discovery rules are #79; the wire format is fixed here.

## 9. Error Envelope

```json
{ "protocol": 1, "messageId": "…", "sentAt": "…", "correlationId": "req_…",
  "code": "INVALID_MESSAGE", "message": "…", "relatedId": "op_…", "retryable": false }
```

Codes: `UNKNOWN_OPERATION`, `UNAUTHORIZED`, `FORBIDDEN`, `INVALID_MESSAGE`, `STALE_MESSAGE`, `REPLAYED`, `VERSION_MISMATCH`, `INTERNAL`.

## 10. Security Mapping

| #75 requirement | Mechanism |
|---|---|
| No arbitrary shell payload | closed type set; payloads are scalars, validated per type |
| Explicit operation type | `type` required; unknown rejected before execution |
| Server identity bound to agent identity | `serverId` checked against the bound identity on every operation |
| Engine authorization authoritative | agent executes only dispatched operations; policy gate (#67) precedes dispatch |
| Unknown types rejected | `UNKNOWN_OPERATION`, never executed |
| Replay resistance | message IDs deduped in-window; timestamps bounded; operation IDs deterministic per attempt (safe retry) |

## 11. Request/Response Lifecycle

```text
register → heartbeat… (agent-initiated, interval)
dispatch(Operation) → ack(accepted) → …execution… → result(success)
                      ↘ reject(UNKNOWN_OPERATION/UNAUTHORIZED/…) [no execution]
heartbeat stops → Engine marks liveness timeout (#78)
transport drops → agent reconnects and re-registers session (no state replay needed:
                 the Engine is authoritative; the agent reports current state)
```

## 12. Out of Scope (other issues)

Transport (#75 follow-up per ADR-0008), credential issuance/rotation (#77), heartbeat timeout policy (#78), capability discovery rules (#79), dispatcher implementation (#80), Docker (#83) and Traefik (#84) adapters, agent-side logs/metrics (#86/#87).
