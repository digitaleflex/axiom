# Axiom Agent ↔ Engine Protocol

> Issue #75. Domain contract for all Runtime Agent communication. Machine types: `services/agent/internal/protocol/` (agent module), mirrored on the wire by `services/engine/internal/agentclient/` (engine module). Transport and credential decisions: [ADR-0008](../adr/0008-agent-engine-communication.md) (Accepted, 2026-10-09).
>
> This document defines **what** is said. The transport is fixed (agent-initiated long-poll HTTPS, §0) but it does not change these messages: the package doc states the same structs serve any transport (`services/agent/internal/protocol/protocol.go:1-9`).

## 0. Transport

The agent initiates every exchange. The Engine never opens a connection towards an agent, and no inbound port is required on a user server.

| Endpoint | Direction | Behaviour |
|---|---|---|
| `POST /api/v1/agent/poll` | agent → Engine | holds up to 25s; `204` if no operation is pending, `200` with one `Operation` otherwise |
| `POST /api/v1/agent/result` | agent → Engine | `Acknowledgement` and `Result` |

Registration, rotation and heartbeat are on their existing routes (`services/engine/internal/api/api.go:200-203`). The poll and result routes are decided by ADR-0008 but **not yet registered** in the router: dispatch cannot be exercised end to end until they are.

A second transport exists and is retained for one configuration only: the agent's own listener, whose address `AXIOM_AGENT_LISTEN_ADDR` is refused unless it is a loopback address or `AXIOM_AGENT_ALLOW_PUBLIC_LISTENER=true` is set (`services/agent/internal/config/config.go:210-221`). That listener serves `POST /api/v1/agent/operations` (`services/agent/internal/config/config.go:131`, `services/engine/internal/agentclient/client.go:92`) and is the path for a **self-hosted loopback deployment only**. It is not the hosted transport.

Because both legs are ordinary HTTPS, the agent's outbound hardening applies to poll and result unchanged: 64 KiB request and response limits, endpoint allow-list, `https` only outside loopback, timeout policy (`services/agent/internal/security/transport/transport.go:18-32`, `transport.go:71-92`).

## 1. Principles

1. The agent initiates every exchange (works behind NAT; the Engine never dials servers).
2. The Engine authorizes every operation; the agent re-validates before executing.
3. Operations are closed, typed values — there is no shell, command string or script payload anywhere in this protocol.
4. Server identity is bound to agent identity: an operation naming another server is rejected.
5. Every message carries a protocol version, a unique message ID, a timestamp and (when caused by a user action) a correlation ID.
6. Retried work is idempotent: operation IDs embed deployment, step and attempt.
7. Every operation names the application it deploys, not only the deployment.

## 2. Versioning

- Current version: `2` (`protocol.Version`), minimum supported: `1` (`protocol.MinVersion`) — `services/agent/internal/protocol/protocol.go:29-32`.
- At registration both sides declare `[min, max]`; they use the highest common version (`Compatible`, `protocol.go:35-37`).
- A message with an unsupported version is rejected with `VERSION_MISMATCH`; the versions themselves never break parsing (envelope first, `protocol.go:88-91`).
- There is no V2: `applicationId` became mandatory without an envelope bump. `Version` and `MinVersion` are both 1 (`protocol.go:30-31`), so a V1 peer parses and negotiates; the refusal is the scope check (§6), not the envelope version.
- **Accepted risk:** `applicationId` is mandatory while the envelope version does not move, so a V1 peer cannot be refused at parse time. No V1 agent is deployed, so there is no installed base to break. When one exists, the check must be version-gated.
- Engine-side mirror: `agentclient.ProtocolVersion = 1` (`services/engine/internal/agentclient/client.go:85`) and `agentauth.ProtocolVersion = 1` (`services/engine/internal/agentauth/service.go:36`); all three constants agree, so nothing must be reconciled before the first dispatch.

## 3. Envelope

Every message starts with:

```json
{ "protocol": 2, "messageId": "msg_…", "sentAt": "2026-10-09T10:00:00Z", "correlationId": "req_…" }
```

- `protocol`: `1` or `2`, otherwise `VERSION_MISMATCH` (`protocol.go:88-91`).
- `messageId`: unique per transmission (`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`, `protocol.go:66`), used for dedupe and tracing.
- `sentAt`: RFC3339. Receivers reject messages more than `MaxClockSkew` (5 min) in the future or older than 24h (`protocol.go:61`, `protocol.go:95-97`); duplicates within the window are rejected by ID (`Dedupe`, `protocol.go:371-393`).
- `correlationId`: the API request (`req_[0-9a-f]{16}`, `protocol.go:70`) that caused the work; present on operations, acks, results and errors caused by user actions.

## 4. Identity & Registration

The Engine issues the agent ID at registration; the agent stores it alongside its server binding.

### RegistrationRequest (agent → Engine, once, with the bootstrap credential on the transport)

```json
{ "protocol": 2, "messageId": "…", "sentAt": "…",
  "serverId": "srv_…", "agentVersion": "0.1.3", "capabilities": ["docker", "traefik"] }
```

`serverId` and both capability fields are required; `agentId` is absent because the Engine issues it (`protocol.go:146-157`).

### RegistrationResponse (Engine → agent)

```json
{ "protocol": 2, "messageId": "…", "sentAt": "…",
  "agentId": "agent_…", "serverId": "srv_…", "negotiated": 2, "heartbeatIntervalSeconds": 30 }
```

### AgentIdentity (every later message)

```json
{ "agentId": "agent_…", "serverId": "srv_…" }
```

`agentId` matches `^agent_[0-9a-f]{24}$` (`protocol.go:71`); `serverId` matches the generic ID form (`protocol.go:126-134`).

## 5. Heartbeat (#78)

Agent → Engine on the negotiated interval:

```json
{ "protocol": 2, "messageId": "…", "sentAt": "…",
  "agentId": "agent_…", "serverId": "srv_…", "status": "READY",
  "capabilities": ["docker", "traefik"], "cpuCount": 4, "memoryMb": 8192, "diskFreeMb": 50000 }
```

`status` is the agent-observed state (`READY`, `DEGRADED`, `OFFLINE`; `protocol.go:187-192`). The Engine derives staleness from arrival time; a missing heartbeat is an Engine-side timeout, never an agent message.

## 6. Operation Dispatch (Engine → agent)

```json
{ "protocol": 2, "messageId": "…", "sentAt": "…", "correlationId": "req_…",
  "operationId": "op_dep_01J…_CREATE_RUNTIME_1", "type": "CREATE_RUNTIME",
  "deploymentId": "dep_…", "applicationId": "app_…", "serverId": "srv_…",
  "payload": { "imageRef": "sha256:…", "container": "axiom-app-1", "port": 3000 } }
```

`deploymentId` and `applicationId` are two different resources and two different Docker label values: `axiom.deployment` and `axiom.application` (`services/agent/internal/security/ownership/ownership.go:22-23`). A deployment is one attempt to roll one application out to one server. Ownership assertions check **both** (`ownership.go:139-149`), so an operation that cannot name its application cannot be attributed to one.

### Closed operation set

Six types, no more (`protocol.go:41-57`):

| Type | Payload | Effect |
|---|---|---|
| `CREATE_RUNTIME` | `imageRef`, `container`, `port` | create (not start) the runtime |
| `NETWORK` | `container`, `proxy`, `domain`, `tls`, `port` | route hostname with TLS |
| `START` | `container` | start the runtime |
| `VERIFY` | `domain`, `path`, `timeoutSeconds` | probe until success or timeout |
| `STOP` | `container` | stop the runtime (rollback/cancel path) |
| `REMOVE` | `container` | remove the runtime and its data |

### Validation order (agent, before anything executes)

Implemented in `Operation.Validate` (`protocol.go:216-240`):

1. Envelope: version, freshness, message ID, correlation ID format (`protocol.go:88-102`).
2. `type` is in the closed set — anything else is `UNKNOWN_OPERATION` (`protocol.go:220-222`).
3. `operationId` matches `^op_dep_[0-9a-f]{24}_[A-Z_]+_[0-9]+$`, i.e. `op_<deploymentID>_<STEP>_<attempt>` (`protocol.go:69`, `protocol.go:223-225`).
4. `deploymentId` matches `^dep_[0-9a-f]{24}$` (`protocol.go:67`, `protocol.go:226-228`).
5. **`applicationId` is present and matches `^app_[0-9a-f]{24}$`** — otherwise `INCOMPLETE_SCOPE` (`protocol.go:68`, `protocol.go:232-234`). It is never defaulted or inferred.
6. `serverId` equals the agent's bound server — otherwise `ErrBinding` (`protocol.go:236-238`).
7. Payload matches the type's schema (`protocol.go:259-282`).

Step 5 is a **security refusal**, not a syntax error: `ErrIncompleteScope` is a distinct sentinel (`protocol.go:112-116`) mapping to the stable code `INCOMPLETE_SCOPE` (`CodeIncompleteScope`, `protocol.go:361-365`), so a client can tell "the Engine sent an incomplete operation" from "the agent could not run it".

### Correspondence with the Engine executor

`services/engine/internal/executor` builds the same operation and `agentclient` translates it 1:1 (`services/engine/internal/agentclient/client.go:260-275`). The two Go modules cannot share the package (separate modules, ownership boundary), so this table is the conformance contract:

| Executor (`services/engine`) | Protocol (`services/agent`) |
|---|---|
| `Operation.OperationID/CorrelationID/DeploymentID/ServerID` | `Operation.operationId/correlationId/deploymentId/serverId` |
| operation `ApplicationID` | `Operation.applicationId` (`client.go:271-273`) |
| `CreateRuntimeRequest{ImageRef, Container, Port}` | `CREATE_RUNTIME{imageRef, container, port}` |
| `NetworkRequest{Container, Proxy, Domain, TLS, Port}` | `NETWORK{container, proxy, domain, tls, port}` |
| `StartRequest{Container}` | `START{container}` |
| `HealthCheckRequest{Domain, Path, TimeoutSeconds}` | `VERIFY{domain, path, timeoutSeconds}` |

The Engine refuses to put a malformed operation on the wire at all (`validate`, `client.go:655-687`; `applicationID`, `client.go:546-559`): no operation ID, deployment ID, server ID or application ID means the request never leaves the Engine.

## 7. Acknowledgement & Result (agent → Engine)

Ack (immediate, after validation, before execution) distinguishes *rejected* from *running* (`protocol.go:311-317`):

```json
{ "protocol": 2, "messageId": "…", "sentAt": "…",
  "operationId": "op_…", "deploymentId": "dep_…", "accepted": true }
```

Result (after execution, `protocol.go:320-332`):

```json
{ "protocol": 2, "messageId": "…", "sentAt": "…",
  "operationId": "op_…", "deploymentId": "dep_…", "success": true,
  "errorCode": "", "message": "",
  "health": { "statusCode": 200, "latencyMs": 84, "attempt": 1 },
  "finishedAt": "2026-10-09T10:00:05Z" }
```

Failures carry a stable `errorCode` (`RUNTIME_IMAGE_MISSING`, `HEALTH_CHECK_FAILED`, …) and a human message without secrets. VERIFY results carry the probe report the Engine persists as `health.passed` / `health.failed` (#65); a VERIFY acknowledged as successful without a health report is treated as malformed, never as an implied pass (`client.go:350-368`).

On the Engine side, an ack that accepts without a result is not an error — the agent took the work (`client.go:401-419`); a response naming another deployment, another operation or another protocol version is refused (`client.go:526-538`).

## 8. Capability & Resource Reports

Capabilities ride on registration and heartbeat (`capabilities: ["docker", "traefik", …]` plus `cpuCount`, `memoryMb`, `diskFreeMb`, `protocol.go:170-178`). Full discovery rules are #79; the wire format is fixed here.

## 9. Error Envelope

```json
{ "protocol": 2, "messageId": "…", "sentAt": "…", "correlationId": "req_…",
  "code": "INCOMPLETE_SCOPE", "message": "…", "relatedId": "op_…", "retryable": false }
```

Codes (`protocol.go:352-366`): `UNKNOWN_OPERATION`, `UNAUTHORIZED`, `FORBIDDEN`, `INVALID_MESSAGE`, `STALE_MESSAGE`, `REPLAYED`, `VERSION_MISMATCH`, `INTERNAL`, `INCOMPLETE_SCOPE`.

Transport-level failures on the Engine side use a separate, agentclient-owned set (`client.go:50-65`, the `Error` code set): `AGENT_NO_CREDENTIAL`, `AGENT_NOT_FOUND`, `AGENT_UNREACHABLE`, `AGENT_TIMEOUT`, `AGENT_OPERATION_REJECTED`, `AGENT_OPERATION_FAILED`, `AGENT_MALFORMED_RESPONSE`, `AGENT_RESPONSE_TOO_LARGE`, `AGENT_INSECURE_ENDPOINT`, `AGENT_INTERNAL`, `AGENT_NO_APPLICATION` (a deployment whose application cannot be resolved is refused before the wire, `client.go:546-559`).

## 10. Authentication and Signing

### Agent → Engine: the one credential

There is exactly one credential in the system. Issued by `agentauth` (#77), it authenticates the agent on register, rotate, heartbeat, poll and result. It is sent as a bearer token and never in a body. The agent's signing client sets `Authorization`, `X-Agent-ID`, `X-Timestamp` and `X-Nonce` on every outgoing request (`services/agent/internal/security/auth/client.go:53-66`); the Engine rejects skewed timestamps beyond `MaxClockSkew` (5 min) and replayed nonces within `NonceTTL` (10 min) (`services/engine/internal/agentauth/service.go:28-32`, enforced at `services/engine/internal/api/agent_heartbeat.go:52` and `services/engine/internal/api/agent.go:133`).

Only three routes bypass the user authenticator (`services/engine/internal/api/agent.go:17-18`); `GET /api/v1/agent/status` is user-authenticated.

The Engine holds no credential to present back: `agentauth` stores hashes only and returns plaintext exactly once (`services/engine/internal/agentauth/service.go:1-8`).

### Engine → agent: per-agent operation signing

Every dispatched operation is signed by a per-agent HMAC-SHA256 key, managed by a new Engine package `services/engine/internal/agentkey` (**not yet written**), stored AES-256-GCM encrypted in the existing secret store (`services/engine/internal/security/secrets/box.go:1-2`) and scoped `agent:<agentId>/operation-signing-key`. Plaintext is returned once, at registration.

Canonical form, `\n`-joined:

```text
AXIOM-HMAC-V1\n<METHOD>\n<PATH>\n<AgentID>\n<Version>\n<Timestamp>\n<Nonce>\n<sha256(body)>
```

This leg already carries `Authorization`, `X-Credential-Version`, `X-Timestamp` and `X-Nonce` on the loopback path (`client.go:488-496`); the signature is additive on top of those.

The hex signature travels in `X-Axiom-Signature` and is compared in constant time. Verification happens **after strict decoding and before dispatch** — an operation the agent cannot attribute to an authenticated message is never executed.

**This is an assumed, documented exception to the hash-only rule of `agentauth`.** Signing needs a key; a verifier needs no key. A hash-only store can answer "is this token valid?" but can never produce a signature. Storing the agent credential in reversible form was rejected; a dedicated, single-purpose signing key is used instead, and the cost is accepted openly: the secret store now holds a reversible value whose confidentiality becomes a dependency of operation integrity on this leg.

mTLS is deferred to #88; TLS on the agent→Engine leg already exists.

## 11. Security Mapping

| #75 requirement | Mechanism |
|---|---|
| No arbitrary shell payload | closed type set; payloads are scalars, validated per type (`protocol.go:259-282`) |
| Explicit operation type | `type` required; unknown rejected before execution |
| Server identity bound to agent identity | `serverId` checked against the bound identity on every operation |
| Application ownership attributable | `applicationId` mandatory; `axiom.application` and `axiom.deployment` checked separately (`ownership.go:139-149`) |
| Engine authorization authoritative | agent executes only dispatched operations; policy gate (#67) precedes dispatch |
| Unknown types rejected | `UNKNOWN_OPERATION`, never executed |
| Replay resistance | message IDs deduped in-window; timestamps bounded; operation IDs deterministic per attempt (safe retry) |
| Engine cannot be spoofed by a replayed operation | per-agent HMAC over method, path, agent, version, timestamp, nonce and body digest |

## 12. Request/Response Lifecycle

```text
register → heartbeat… (agent-initiated, interval)
long-poll  → 204 (nothing pending) | 200 Operation
           → validate (strict decode, then signature)
           → ack(accepted) → …execution… → result(success)
             ↘ reject(UNKNOWN_OPERATION/INCOMPLETE_SCOPE/UNAUTHORIZED/…) [no execution]
heartbeat stops → Engine marks liveness timeout (#78)
transport drops → agent reconnects and re-registers (no state replay needed:
                  the Engine is authoritative; the agent reports current state)
```

## 13. Out of Scope (other issues)

Credential issuance/rotation (#77, implemented — `services/engine/internal/agentauth/`), heartbeat timeout policy (#78), capability discovery rules (#79), the poll/result route registration and the Engine-side dispatch queue, `agentkey` implementation, Docker (#83) and Traefik (#84) adapters, agent-side logs/metrics (#86/#87), mTLS (#88).