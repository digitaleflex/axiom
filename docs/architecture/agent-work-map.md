# Axiom Agent Work Map

This map is the canonical ownership matrix for parallel autonomous work.

## Rules

- One implementation surface has one active owner.
- READ access does not imply write access.
- Contract changes must be made by the issue that owns the contract.
- Integration issues consume completed outputs; they do not silently take ownership of upstream code.
- If an issue needs a path not listed under OWNED, it must stop and request an ownership update.

| Issue | Area | OWNED PATHS | READ / CONSUME | FORBIDDEN |
|---|---|---|---|---|
| #75 | Agent protocol | services/agent/internal/protocol/, docs/architecture/agent-protocol.md | M1 contracts | Engine runtime, Docker, Traefik |
| #76 | Agent identity | services/agent/internal/identity/ | #75, server domain | Docker, API |
| #77 | Agent auth | services/agent/internal/security/auth/ | #75,#76 | Runtime adapters |
| #78 | Heartbeat | services/agent/internal/heartbeat/ | #75,#76 | Runtime adapters |
| #79 | Capabilities | services/agent/internal/capabilities/ | #75,#78 | Runtime adapters |
| #80 | Dispatcher | services/agent/internal/dispatcher/ | #75,#77,#79 | Docker/Traefik adapters |
| #81 | Local state | services/agent/internal/state/ | #75,#80 | Engine DB schema |
| #82 | Recovery | services/agent/internal/recovery/ | #81,#83,#84 | Engine orchestration |
| #83 | Docker | services/agent/internal/runtime/docker/ | #75,#80,#81 | Engine, Traefik |
| #84 | Traefik | services/agent/internal/runtime/traefik/ | #75,#80,#83 | Engine API |
| #85 | Health | services/agent/internal/health/ | #75,#80,#83 | API contract |
| #86 | Logs | services/agent/internal/logs/ | #75,#83 | Engine persistence |
| #87 | Metrics | services/agent/internal/metrics/ | #75,#78 | Product UI |
| #88 | Agent network security | services/agent/internal/security/transport/ | #75,#77 | Runtime adapters |
| #89 | Ownership | services/agent/internal/security/ownership/ | #75,#77,#80,#83,#84 | Engine schema |
| #90 | Agent integration tests | services/agent/tests/ | #75-89 | Production implementation |
| #91 | GitHub connection | services/engine/internal/github/auth/ | #71,#125 | Agent, DB migrations |
| #92 | GitHub discovery | services/engine/internal/github/repos/ | #91 | Analyzer/runtime |
| #93 | Snapshot | services/engine/internal/analyzer/snapshot/ | #92 | Agent, DB |
| #94 | Evidence | services/engine/internal/analyzer/evidence/ | #93 | Runtime |
| #95 | Profile schema | services/engine/internal/profile/ | #94 | Planner/executor |
| #96 | Presets | services/engine/internal/profile/presets/ | #95 | Agent |
| #97 | Plan validation | services/engine/internal/planner/validation/ | #95,#96 | Runtime adapters |
| #98 | Build workspace | services/engine/internal/build/workspace/ | #93 | Agent runtime |
| #99 | Build artifacts | services/engine/internal/build/artifacts/ | #98 | Agent runtime |
| #100 | Execution orchestration | services/engine/internal/executor/orchestration/ | #97,#99,#80 | Agent implementation |
| #114 | Engine DI | services/engine/cmd/, services/engine/internal/bootstrap/ | #69 | Domain internals |
| #115 | DB schema | services/engine/migrations/ | #70 | Agent, API handlers |
| #116 | Deployment persistence | services/engine/internal/database/deployment/ | #115 | Agent |
| #117 | REST API | services/engine/internal/api/ | #71,#116 | Agent runtime |
| #118 | SSE | services/engine/internal/api/sse/ | #75,#117 | Agent transport |
| #119 | Cloud shell | apps/cloud/ | #117,#125 | Engine internals |
| #120 | Cloud workflow | apps/cloud/features/deploy/ | #119,#117 | Agent implementation |
| #121 | Node/Next presets | services/engine/internal/runtime/presets/node/ | #95,#96 | Agent adapters |
| #122 | Docker preset | services/engine/internal/runtime/presets/docker/ | #83,#97 | Engine core |
| #123 | Compose preset | services/engine/internal/runtime/presets/compose/ | #83,#89 | Engine API |
| #124 | axiom.yaml | services/engine/internal/manifest/ | #31,#60 | Agent security |
| #125 | User auth | services/engine/internal/auth/ | #117 | Agent |
| #126 | Secrets | services/engine/internal/security/secrets/ | #77,#125 | Cloud UI secrets storage |
| #127 | Authorization | services/engine/internal/authz/ | #125,#126 | Agent runtime |
| #128 | Audit | services/engine/internal/audit/ | #66,#127 | Agent implementation |
| #129 | Threat model | docs/security/, services/engine/internal/security/abuse/ | #67,#88,#89 | Product feature code |
| #101 | Structured logging | services/engine/internal/observability/logging/ | #66 | UI |
| #102 | Diagnostics | services/engine/internal/observability/diagnostics/ | #101,#103 | Agent runtime |
| #103 | Metrics | services/engine/internal/observability/metrics/ | #101 | UI |
| #104 | Runbook | docs/operations/ | #101-103 | Runtime implementation |
| #107 | Reference app | docs/reference/ | #59,#19 | Engine implementation |
| #108 | Reference VPS | docs/reference/vps/ | #76,#79,#88 | Engine |
| #109 | Dry run | docs/reference/runs/ | #107,#108,#90 | Product implementation |
| #110 | E2E gate | tests/e2e/ | #68,#74 | Production implementation |
| #111 | Security regression | tests/security/ | #67,#77,#88,#89,#90 | Production implementation |
| #112 | Release evidence | docs/release/ | #110,#111 | Production implementation |
| #113 | Release candidate | .github/, docs/release/ | #110-112 | Domain implementation |

## Important note

The paths above are the intended ownership model. Existing repository structure may differ while the codebase is being migrated. If a declared path does not exist yet, create it only when the issue explicitly owns it. Do not relocate existing code merely to match this table without an integration task.
