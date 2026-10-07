# Infrastructure Model — Server State, Capabilities & Eligibility v1.0

> Cross-cutting infrastructure presentation contract — issue #138.
>
> Depends on: Design DNA (#131, §15 Infrastructure Visualization), Progressive Disclosure (#132), Shell & Navigation (#133).
> Contract sources: `docs/architecture/api-contract.md` §9; #63 (runtime & server management), #76 (registration), #78 (heartbeat & liveness), #79 (capability & resource discovery), #85 (runtime health).
> Screens: `docs/design/screens/servers/README.md`.

V0.1 runtime model: **Engine → Runtime Agent → Docker → Traefik → Application** (V0.1 scope). Kubernetes, pods and service mesh are never presented as concepts, requirements or empty placeholders.

## 1. Vocabulary in Infrastructure Context

On application screens, internal components stay out of navigation and L1/L2 (navigation §4). On **infrastructure screens** the server *is* the subject, so components appear at L2 — always with a user-facing role label first:

| Role label (always shown) | Component (shown as value) | Example |
|---|---|---|
| Axiom agent | Runtime Agent version | "Axiom agent · v0.1.3 · connected" |
| Container runtime | Docker version | "Container runtime · Docker 26.1" |
| Multi-service support | Docker Compose version | "Multi-service support · Compose 2.27" |
| Routing | Traefik version | "Routing · Traefik 3.0" |
| HTTPS certificates | TLS capability | "HTTPS certificates · automatic" |

Navigation labels remain "Servers", never "Agents" or "Nodes".

## 2. Server State

| State | Rule (presentation; authoritative state from Engine #78) | Semantic | L1 copy |
|---|---|---|---|
| READY | agent connected, heartbeat fresh, required capabilities healthy | `--status-live` + "Ready" | "Ready for deployments" |
| DEGRADED | agent connected but a capability unhealthy, resources critically low, or heartbeat late | `--status-warning` + "Degraded" | "Running with issues: {primary reason}" |
| OFFLINE | heartbeat timed out / agent disconnected | `--status-failed` + "Offline" | "Not reachable since {lastSeen}" |
| PENDING | registered, awaiting first heartbeat | `--status-queued` + "Pending" | "Waiting for the agent to connect" |
| REVOKED | agent identity revoked | `--status-inactive` + "Revoked" | "Agent access revoked" |
| unknown | raw value | `--status-inactive` | "Status reported by Axiom: `{value}`" |

- Every state shows **last seen** (relative + absolute mono) — the user must know how fresh the data is.
- OFFLINE servers show last known resources marked stale, never as current.
- The state always carries its reason (DEGRADED/OFFLINE), never color alone.

## 3. Capability Model

Each capability row: role label · status · version (mono) · detail.

| Capability | Status values | Required for |
|---|---|---|
| Axiom agent | Connected / Late heartbeat / Disconnected; version; "Update available" (info) | all deployments |
| Container runtime | Available / Unavailable / Unhealthy | all V0.1 strategies |
| Multi-service support | Available / Not installed | Compose strategy (#123) |
| Routing | Available / Unavailable / Unhealthy | public domains |
| HTTPS certificates | Automatic / Unavailable | HTTPS domains |
| Architecture / OS | value (e.g. `linux/amd64`) | image compatibility |

Resources: CPU (cores, current %), memory (total, available), disk (total, free). Units always explicit.

## 4. Eligibility

A server is eligible for a deployment only when its agent, runtime capabilities and health satisfy the plan requirements (API §9). The UI shows eligibility **per application**, computed by the Engine, with reasons.

| Eligibility | Meaning | Selectable |
|---|---|---|
| Eligible | meets all requirements | yes |
| Eligible with warnings | meets requirements but DEGRADED or resources tight | yes, with acknowledgement |
| Not eligible | missing requirement | no — reasons listed |

Reason catalog (presentation copy; codes from Engine — **gap**, #79 / #97):

| Reason | Copy |
|---|---|
| Server offline | "Offline since 14:02 — the agent isn't reachable." |
| Container runtime unavailable | "Container runtime isn't available on this server." |
| Missing multi-service support | "This app uses multiple services; install Docker Compose on this server." |
| Routing unavailable | "Routing isn't available, so app.acme.dev can't be served." |
| Insufficient memory | "Needs ~1 GB free memory; 420 MB available." |
| Insufficient disk | "Needs ~3 GB free disk; 1.1 GB available." |
| Architecture mismatch | "Image targets `linux/amd64`; server is `linux/arm64`." |
| Agent version too old | "Agent v0.0.9 is older than the minimum v0.1.0." |

Every Not eligible server lists **all** failing requirements, each with a fix hint where applicable.

## 5. Axiom-managed vs Unknown Resources

Server details distinguish:

- **Axiom-managed**: runtimes created by Axiom deployments — linked to application · environment · deployment.
- **Other workloads on this server**: resources detected but not managed by Axiom — shown as an aggregate count/resource usage only (L3), never actionable, labelled "Not managed by Axiom".

## 6. Safe Actions Policy

Infrastructure screens never expose raw privileged host operations (shell, arbitrary container exec, reboot, package install) — V0.1 excludes arbitrary remote shell.

Allowed actions (subject to authorization #127 and contract availability):

| Action | Placement | Guard |
|---|---|---|
| Register server | Server Overview primary | — |
| View deployments on this server | navigation | — |
| Rename server | Details overflow | — |
| Rotate agent credential | Details → Agent section (L3) | confirmation; contract #77 |
| Revoke agent / remove server | Details overflow, destructive | blocked while Axiom-managed runtimes exist, with list; typed confirmation |

## 7. Register Server Flow (V0.1)

Dialog from Server Overview:

1. Name the server.
2. Engine returns a one-time bootstrap command/token (#76) — shown **once**, copy button, expiry time; never retrievable again.
3. "Waiting for the agent to connect…" (PENDING) with live state; success when first heartbeat arrives → READY/DEGRADED with capability summary.
4. Timeout/expiry: "Token expired — generate a new one".

## 8. Contract Gaps

| Need | Owner |
|---|---|
| Server state enum incl. DEGRADED reason, lastSeen | #78 / #117 |
| Capability list with status/version, resources | #79 / #117 |
| Per-application eligibility with reason codes | #79 / #97 |
| Runtimes on server linked to deployments; unmanaged aggregate | #63 / #81 |
| Server events (heartbeat transitions, capability changes) | #78 / #66 |
| Registration bootstrap token response | #76 |
| Credential rotation / revoke endpoints | #77 / #89 |
