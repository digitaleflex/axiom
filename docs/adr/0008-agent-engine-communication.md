# ADR-0008 — Agent ↔ Engine communication model

- **Status:** Accepted
- **Date:** 2026-10-09
- **Issue:** #75, #77, #88

## Context

Runtime Agents run on user servers, often behind NAT/firewalls. The Engine must remain authoritative and operations must be replay-resistant. Two things had to be settled before any code could be wired: the transport, and the credential direction that the existing credential store cannot satisfy by itself.

The domain contract is fixed in [`docs/architecture/agent-protocol.md`](../architecture/agent-protocol.md) with machine types in `services/agent/internal/protocol/`.

The constraint that decided the credential question: `services/engine/internal/agentauth` stores credential **hashes** only and returns plaintext exactly once at issue time (`services/engine/internal/agentauth/service.go:1-8`), so after registration the Engine holds nothing it could present to an agent. The inbound listener reflects this — its production authenticator `refuseInbound` returns `ErrUnauthenticated` for every request (`services/agent/internal/bootstrap/listener.go:48-52`), and on the Engine side the injected `CredentialProvider` seam could only refuse with the stable code `AGENT_NO_CREDENTIAL` (`services/engine/internal/agentclient/client.go`) — since the decision landed, that seam is non-blocking and the code reports a missing per-agent signing key instead (`client.go:55-61`, `client.go:637-655`).

## Decision

### Transport: agent-initiated long-poll HTTPS

The Agent initiates every exchange. The Engine **never** opens a connection towards an agent, and no inbound port is required on the user server.

Two Engine endpoints carry the dispatch channel:

| Endpoint | Direction | Behaviour |
|---|---|---|
| `POST /api/v1/agent/poll` | agent → Engine | holds the request up to 25s; answers `204` when no operation is pending, or `200` with one `protocol.Operation` |
| `POST /api/v1/agent/result` | agent → Engine | acknowledgement (`protocol.Acknowledgement`) and execution result (`protocol.Result`) |

Registration, rotation and heartbeat stay on their existing routes (`services/engine/internal/api/api.go:200-203`). The two poll/result routes are part of this decision and are not yet registered in the router; until they are, dispatch cannot be exercised.

The agent's own listener remains loopback by default: `AXIOM_AGENT_LISTEN_ADDR` is rejected unless it is a loopback address or `AXIOM_AGENT_ALLOW_PUBLIC_LISTENER=true` is set (`services/agent/internal/config/config.go:205-222`). This listener is not the dispatch channel of the decision above; it is the Engine→Agent adapter that remains usable for a **self-hosted, loopback-only** deployment.

`agentclient` (`POST /api/v1/agent/operations`, `services/engine/internal/agentclient/client.go:104`) is that self-hosted loopback mode and nothing else. It signs every operation with `X-Axiom-Signature` and sets `X-Timestamp` and `X-Nonce` on every request, plus `Authorization` and `X-Credential-Version` when a bearer credential exists (`client.go:527-556`), and admits `http://` only for a loopback host outside production (`client.go:677-692`). It is kept, tested and retained as a supported configuration; it is not the transport for a hosted deployment.

Because both endpoints are ordinary HTTPS requests, the existing TLS path on the agent→Engine leg (`services/agent/internal/security/transport`) applies unchanged to poll and result.

### Authentication: one credential, one direction

There is exactly one credential in the system: the one issued by `agentauth` (#77). It is the single agent identity for both roles — agent-to-Engine authentication and Engine-to-Agent authorization — but it is *presented* in one direction only. The agent presents it on poll and result exactly as it presents it on registration and heartbeat (`Authorization`, `X-Agent-ID`, `X-Timestamp`, `X-Nonce`, `services/agent/internal/security/auth/client.go:53-66`).

The Engine presents **no** credential of its own. There is nothing for it to present: it stores no plaintext (see Context). The Engine→Agent leg is therefore not authenticated by a shared secret but by the per-agent operation signing key below.

### Operation signing: per-agent HMAC-SHA256 key

Every dispatched operation is additionally signed by a key **per agent**. The key is managed by the Engine package `services/engine/internal/agentkey`, which stores it encrypted at rest with AES-256-GCM in the existing secret store (`services/engine/internal/security/secrets/box.go:1-2`, `store.go:16`), the same store `appconfig` uses under a scope prefix (`services/engine/internal/secrets/appconfig.go:17-22`), and scopes it `agent:<agentId>/operation-signing-key` (`ScopePrefix`/`KeyName`, `agentkey.go`). `Service.Issue` generates 32 bytes (`KeySize`) with `crypto/rand` and returns the plaintext key **once** — in the `operationSigningKey` field (lowercase hex) of the register and rotate responses (`services/engine/internal/api/agent.go:132`, `agent.go:176`, `agent.go:221-228`) — and never again; rotation reissues it the same way `agentauth` reissues a credential. `Service.SigningKey` decrypts it for signing only, and `agentclient` signs the exact bytes it sends (`services/engine/internal/agentclient/client.go:460`, `client.go:527-556`).

Canonical signing form, joined by `\n`:

```text
AXIOM-HMAC-V1\n<METHOD>\n<PATH>\n<AgentID>\n<Version>\n<Timestamp>\n<Nonce>\n<sha256(body)>
```

The lowercase hex HMAC-SHA256 goes in the `X-Axiom-Signature` header. Comparison is constant-time. The signature is verified **after** strict decoding and **before** dispatch: the agent refuses to execute anything it could not attribute to an authenticated operation.

**This is an assumed, deliberate exception to the hash-only rule of `agentauth`, and is accepted as such.** Signing requires a key; a verifier does not. A hash-only store can answer "is this token correct?" but can never answer "produce a signature for this operation". Rather than store the agent credential in reversible form — which would invert the property that makes revocation by hash and single-return plaintext possible — a dedicated signing key is introduced. The exposure is bounded: one key per agent, used only to sign Engine→Agent operations, never accepted by `agentauth`, never usable as a bearer credential against the Engine, and rotatable on its own. The cost is real and accepted: the secret store now holds a reversible value, so its confidentiality becomes a dependency of operation integrity on that leg.

### Resource scope: applicationId is mandatory

Every operation carries `applicationId`, distinct from `deploymentId` (`services/agent/internal/protocol/protocol.go:205-213`); the Engine resolves it from the deployment record and refuses rather than sending an operation without it (`services/engine/internal/agentclient/client.go:140-148`, `client.go:546-559`). The Docker ownership labels `axiom.application` and `axiom.deployment` are two distinct values (`services/agent/internal/security/ownership/ownership.go:22-23`, `NewLabels`, `ownership.go:96-103`), and every scope assertion checks both labels (`assertScope`, `ownership.go:139-149`). An operation whose `applicationId` is missing or malformed is refused with the stable code `INCOMPLETE_SCOPE` (`CodeIncompleteScope`, `protocol.go:361-365`), which is deliberately distinct from `INVALID_MESSAGE`: it is a security refusal, not a syntax error.

### Protocol version and the risk it creates

The wire version advertised by the agent module is `1` (`protocol.go:30`) and `MinVersion` is `1` (`protocol.go:31`). There is deliberately no bump: no V1 peer is deployed, and `applicationId` is enforced by the scope check rather than by an envelope version gate (`protocol.go:20-32`). The Engine side mirrors this exactly — `agentclient.ProtocolVersion = 1` (`client.go:85`) and `agentauth.ProtocolVersion = 1` (`services/engine/internal/agentauth/service.go:36`, used at `service.go:288` and `service.go:337`). All three constants agree, so there is no divergence to reconcile before the first dispatch. The residual risk is the opposite one: a mandatory `applicationId` with an unchanged envelope version cannot be refused at parse time, so once a V1 peer exists the check must become version-gated.

**Known risk, accepted on purpose:** making `applicationId` mandatory while the advertised version moves is a breaking change for any peer that sends operations without it. It is not breaking in practice because **no V1 agent is deployed yet** — there is no installed base to break. It is recorded here as a decision, not as an oversight: the moment a V1 agent exists, the mandatory `applicationId` must become version-gated instead of unconditional.

### mTLS

Deferred to #88. TLS already exists on the agent→Engine leg; the remaining work is client-certificate mutual authentication.

## Consequences

- Works behind NAT and through restrictive firewalls; the Engine never needs server credentials and never dials a user server.
- Requires agent reconnect/backoff and Engine-side liveness (#78), and an Engine-side queue with a 25s poll deadline to hold pending operations.
- The Engine's `agentclient` adapter and the agent's inbound listener remain in the tree for the loopback self-hosted mode, which keeps a second transport to keep correct.
- The Engine's secret store gains a reversible value (per-agent signing keys), with the confidentiality consequences stated above.
- The credential direction question is closed: one credential, agent→Engine only; Engine→Agent is authenticated by the signing key, not by a second credential.

## Alternatives considered

- **Engine dials the agent (reverse pull / inbound agent port):** rejected. It requires exposing a port on every user server, which is the single most common failure mode of self-hosted remote-execution products.
- **WebSocket or gRPC stream:** rejected for V0.1. Both require a stateful connection held open, which adds a connection-lifecycle problem the long-poll model does not have, for no benefit at this message rate. The protocol is transport-independent (`services/agent/internal/protocol/protocol.go:1-9`), so this remains revisitable without a message change.
- **Second shared credential for Engine→Agent:** rejected. It would require the Engine to store a reversible agent credential, i.e. exactly the property `agentauth` was built to avoid.
- **Bearer token instead of HMAC for Engine→Agent:** rejected. The single credential is already spent on the agent→Engine direction; reusing it would make one leak compromise both legs.