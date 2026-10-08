# Parallel Execution Plan — Remaining V0.1 Work

> Goal: finish V0.1 with maximum parallel agent throughput and zero file-ownership collisions.
> Rule: one path has one active owner. Lanes below are collision-free by construction.
> Sizes: S < 1h, M = 1–3h, L = 3–6h of agent work. Estimates assume the fixer profile.

## 0. Done (close-out only, owner: maintainer)

M0 (#25–#27), M1 (#28–#33), M2 (#34–#39), M3 engine (#57–#68), design (#131–#142).
Action: close issues with a status comment each; keep epics (#13–#17) open until their last child lands.

## Status

- **Wave 1 — DONE** (A #76/#77 `977b0b1`, B #89/#79 `d13f3e4`, C #83 `87061f3`, D #124/#121 `0a11717`, E #119 `ffefc1d`, F #104/#129-draft `7f786d5`)
- **Wave 2 — DONE** (G #78/#80 `5cf22ab`, H #84/#85/#86 `529244c`, I #122/#120 `36e99b9`, J #101 `220ea66`; follow-up correlation IDs `7b232de`)
- **Wave 3 — DELIVERED** (K #81/#82 agent state + recovery, L #87/#103 metrics, M #125 auth, N #126 secrets)
- **Wave 4 — DELIVERED** (O #88 agent network security, P #123 compose preset, Q #127/#128 authz + audit). R (#102 diagnostics API) is **NOT** delivered: `services/engine/internal/observability/diagnostics/` does not exist.

Wave 3 and Wave 4 code is present and tested, but the GitHub issues are still open and — more importantly — much of it is not reachable from a running binary. See "Known production gap" below before concluding anything about operational state.

## Wave 1 — launch now (5 lanes, no inter-dependencies)

| Lane | Issues | Owned paths (exclusive) | Size | Notes |
|---|---|---|---|---|
| A — Agent identity & auth | #76, #77 (sequential) | `services/agent/internal/identity/`, `services/agent/internal/security/auth/`, `services/engine/internal/agentauth/` (new), `services/engine/migrations/009_agent_credentials.sql` (new) | L | Engine side included in-lane (registration endpoint, credential verify/rotate/revoke). Bootstrap wiring of new endpoints by lane owner. No other lane touches these paths. |
| B — Agent ownership + capabilities | #89 then #79 (sequential) | `services/agent/internal/security/ownership/`, `services/agent/internal/capabilities/` | M | #89 first (label scheme + naming constraints that #83/#84 will consume). No Docker/exec yet. |
| C — Docker adapter | #83 | `services/agent/internal/runtime/docker/` | L | Biggest single task. Constraint: stdlib only (agent `go.mod` has no deps) → Docker CLI via bounded argv (same pattern as engine `ExecBuilder`), never shell. Reads `ownership` (#89, Wave-1 output) and `protocol` (done). |
| D — axiom.yaml parser + Node presets | #124 (axiom.yaml parser, `internal/manifest/`) then #121 (Node/Next presets, `internal/runtime/presets/node/`) (sequential) | `services/engine/internal/manifest/` (new), `services/engine/internal/runtime/presets/` (new), `services/engine/internal/build/dockerfile.go` (templates only) | M | Manifest parser vs JSON schema (#31, frozen); precedence Override › manifest › detected › default. Wiring into `analysis.Service` = small, in-lane touch of `internal/analysis/service.go` (declare it). |
| E — Console shell | #119 | `apps/cloud/**` (shell, routing, API client, auth boundary, tokens→CSS) | L | Greenfield (80-line bootstrap today). Zero collision with Go work. Uses interim `AXIOM_API_TOKEN` until #125. |
| F — Docs (runbook + threat draft) | #104, #129-draft | `docs/operations/`, `docs/security/` | S | Zero code collision. #129 finalized after #88/#89. |

## Wave 2 — starts when Wave 1 lanes land (dependencies in brackets)

| Lane | Issues | Owned paths | Size | Needs |
|---|---|---|---|---|
| G — Heartbeat & dispatcher | #78 then #80 | `services/agent/internal/heartbeat/`, `services/agent/internal/dispatcher/` (+ engine liveness query: `internal/server/` read-side only) | L | #76 (identity), #77 (auth), #79 (caps) |
| H — Traefik + health + logs | #84, #85, #86 (one agent, sequential) | `services/agent/internal/runtime/traefik/`, `services/agent/internal/health/`, `services/agent/internal/logs/` | L | #89 labels, #80 interface (read protocol doc if #80 late; program to `protocol.Operation`) |
| I — Docker preset + console workflow | #122, #120 | `services/engine/internal/runtime/presets/docker/`, `apps/cloud/features/deploy/` | M | #83 (build semantics), #119 (shell) |
| J — Structured logging | #101 | `services/engine/internal/observability/logging/`, logging convention doc | S | none (engine-only; agent adopts later) |

## Wave 3

| Lane | Issues | Owned paths | Size | Needs |
|---|---|---|---|---|
| K — Agent state + recovery | #81, #82 | `services/agent/internal/state/`, `services/agent/internal/recovery/` | M | #80, #83 |
| L — Metrics (agent + engine endpoint) | #87, #103 | `services/agent/internal/metrics/`, `services/engine/internal/observability/metrics/`, `/metrics` wiring in `httpserver` (declare) | M | #78 |
| M — User auth | #125 | `services/engine/internal/auth/`, `internal/api` auth middleware (declare exclusive during lane) | L | #71 (done) |
| N — Secrets store | #126 | `services/engine/internal/security/secrets/` (extend), `migrations/012_secrets.sql` | M | #77 |

## Wave 4

| Lane | Issues | Owned paths | Size | Needs |
|---|---|---|---|---|
| O — Network security | #88 | `services/agent/internal/security/transport/` + engine TLS posture | M | #75 (done), #77 |
| P — Compose preset | #123 | `services/engine/internal/runtime/presets/compose/`, build+executor compose path (declare both during lane) | M | #83, #89 |
| Q — Authz + audit | #127 then #128 | `services/engine/internal/authz/`, `services/engine/internal/audit/`, `migrations/013_audit.sql`, `internal/api` enforcement (declare exclusive during lane) | L | #125, #126 |
| R — Diagnostics API | #102 | `services/engine/internal/observability/diagnostics/`, additive API routes | M | #66 (done), #86 |

## Wave 5 — release

- #90 agent integration tests (after G/H/K), #105–#106 CLI (anytime, new dir, P1-post-V0.1 — deprioritize).
- #107 reference app: maintainer decision (docs).
- #108/#109 reference VPS + dry run: **human-gated** (real server, DNS, agent install).
- #110/#111 release gates, #112 evidence docs (parallelizable late), #113 tagging (human).
- #69/#71 verify-and-close; close epics #13–#18.

## Known production gap

**Do not read the shipped agent and build packages as an operational runtime.** Waves 1 to 4 landed a large amount of code, but the path from a deployment record to a running application is broken at two wiring points.

### Breakpoint 1 — Engine bootstrap does not construct the executor

`services/engine/internal/bootstrap/bootstrap.go` never builds `build.Engine` (`services/engine/internal/build`) nor `executor.Runner` (`services/engine/internal/executor`). Both packages exist and are unit-tested, but nothing instantiates them at runtime.

Consequences:

- build, image packaging, runtime start, Traefik/networking, health check and domain/SSL are **not reachable** from a running Engine (#98 secure build workspace, #99 image build pipeline, #100 execution orchestration);
- a deployment created through the API **stays `PENDING`** — it never reaches `LIVE` or `FAILED`;
- the SSE deployment event stream (#118) is wired but carries no execution events.

### Breakpoint 2 — the agent binary does not mount the M5 packages

`services/agent/cmd/agent/main.go:21` instantiates only `agent.NewRuntime`, and `agent.Run` does nothing but log every 30 seconds. The M5 agent packages — `identity`, `heartbeat`, `dispatcher`, `state`, `recovery`, `docker`, `traefik`, `health`, `logs`, `capabilities`, `protocol`, `ownership`, `auth` — are delivered and tested (#75 protocol contract, #89 ownership boundary) but **never mounted in the binary**. The register → heartbeat → dispatch → recover acceptance path therefore has no production entry point. This is already recorded in `docs/architecture/agent-failure-matrix.md:91-96`.

There is also **no Engine-to-Agent HTTP client**, so Engine and Agent cannot talk to each other in production.

### What that means for the roadmap

- Wave 3 (#125 auth, #126 secrets) and Wave 4 (#88 transport security, #123 compose preset, #127/#128 authz + audit) are delivered on the Engine and Agent sides, but security enforcement and preset selection are only as strong as the wiring above allows.
- Wave 5 cannot start meaningfully before Breakpoints 1 and 2 are closed: without an executor there is no `#110` end-to-end gate to pass, and without an operational agent there is no `#109` reference dry run.
- Nothing in Waves 1 to 4 should be reported as "the agent is operational" or "deployments go LIVE" on the basis of code presence alone.

## Human-gated (cannot be delegated)

VPS provisioning (#108/#109), GitHub OAuth App credentials (real connect test), production `AXIOM_API_TOKEN`/`AXIOM_SECRET_KEY`, release tag (#113), closing design epic #130 (needs visual validation).

## Launch protocol per lane

Each lane gets: exclusive OWNED paths, READ list, FORBIDDEN list, `gh issue view` numbers, DB env (`AXIOM_TEST_DATABASE_URL`), `gofmt` + `go test -race` verification, no-commit rule (maintainer reviews, wires, commits). API-surface changes (`internal/api`, `migrations/*` numbering, contract docs) are declared in the lane and applied by the lane owner only.
