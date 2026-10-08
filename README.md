# Axiom

> **Transform a GitHub repository into a live application.**

Axiom is an automated application deployment and hosting platform designed to connect the developer experience of Git-based platforms with infrastructure and runtime management.

The core workflow is simple:

```text
GitHub
  ↓
Repository
  ↓
Repository Analyzer
  ↓
Stack Detection
  ↓
Application Profile
  ↓
Deployment Plan
  ↓
Build
  ↓
Runtime
  ↓
Traefik / Networking
  ↓
Domain + SSL
  ↓
Health Check
  ↓
LIVE
```

## Product vision

Axiom combines three layers:

- **Developer Experience** — GitHub integration, repositories, deployments, logs, domains and a cloud console.
- **Application Platform** — automatic build, runtime, container and deployment management.
- **Infrastructure Platform** — servers, networking, reverse proxy, SSL and infrastructure-aware execution.

The objective is to make deployment infrastructure progressively transparent to the developer while keeping execution observable and controllable.

### What Axiom is not

Axiom is **not primarily a generic AI software factory**.

AI agents and expert services can be used internally for repository analysis, stack detection, deployment planning, security and infrastructure decisions. They support the deployment engine rather than replacing the core deployment workflow.

## Current architecture

```text
                         AXIOM
                           │
                  ┌────────▼────────┐
                  │ Deployment      │
                  │ Engine          │
                  └────────┬────────┘
                           │
          ┌────────────────┼────────────────┐
          │                │                │
          ▼                ▼                ▼
      Analyzer          Planner          Executor
          │                │                │
          └────────────────┼────────────────┘
                           │
                    Expert Services
                           │
          ┌────────────────┼────────────────┐
          ▼                ▼                ▼
       Security          DevOps       Infrastructure
                           │
                    ┌──────▼──────┐
                    │ Application │
                    │ Runtime     │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │ Server / VPS│
                    └─────────────┘
```

## Core deployment flow

Axiom's target V0.1 workflow is:

1. Connect a GitHub account.
2. List repositories accessible to the user.
3. Select a repository.
4. Analyze the repository.
5. Detect language, framework, package manager, build/start commands, port and runtime requirements.
6. Generate an application profile.
7. Generate a deployment plan.
8. Select a target server.
9. Build and package the application.
10. Deploy it to the selected runtime.
11. Configure networking, domain and SSL.
12. Run health checks.
13. Expose the application as live.

Example:

```text
Connect GitHub
      ↓
Select Repository
      ↓
"Next.js · TypeScript · pnpm · port 3000"
      ↓
Select VPS
      ↓
Deploy
      ↓
Build
      ↓
Container
      ↓
Traefik
      ↓
SSL
      ↓
Health Check
      ↓
https://app.example.com
```

## Architecture status

The repository is past the foundation phase: analysis, profiling, planning and deployment state are implemented and wired into the running Engine. Execution is not.

At `main` = `d4dc7ce`:

- All three Go services (`engine`, `agent`, `orchestrator`) compile, `go vet` is clean, and the Go suite reports **564 passing tests, 0 failures, 2 skips** (`TestRealDockerBuild`, `TestGitHubToLive`) against PostgreSQL 16.
- The Cloud Console typechecks and builds (`tsc --noEmit && vite build`), but its test suite is **not green**: `vitest run` reports **31 passed / 9 failed**. These fragile tests were committed as-is in `471bebf`.
- No CI workflow runs the frontend, so that breakage is not caught automatically. `.github/workflows/` covers Go tests, schema contracts and agent readiness only.

### Wired in production

Constructed by the Engine composition root (`services/engine/internal/bootstrap/bootstrap.go`) and reachable over the HTTP API:

- Repository analysis and stack detection (`internal/analysis`, `internal/analyzer`)
- Application profile and runtime presets (`internal/profile`, `internal/runtime/presets`)
- Deployment plan generation (`internal/planner`)
- Deployment state, event bus and logs (`internal/deployment`, `internal/logs`)
- Servers, domains, GitHub connection, user authentication, authorization, audit, secrets (`internal/server`, `internal/domains`, `internal/github`, `internal/auth`, `internal/authz`, `internal/audit`, `internal/security/secrets`)
- Agent credentials on the Engine side (`internal/agentauth`)
- Metrics registry and SSE deployment event stream (`internal/observability/metrics`, `internal/api/sse`)
- PostgreSQL migrations 001-013 (`services/engine/migrations`)

### Delivered but not reachable: the build/executor breakpoint

`build.Engine` (`services/engine/internal/build`) and `executor.Runner` (`services/engine/internal/executor`) exist and are unit-tested, but **`bootstrap.New` never constructs them**. As a direct consequence:

- image build, runtime start, Traefik/networking, health check and domain/SSL are not reachable from a running Engine;
- a deployment created through the API stays `PENDING` — it never reaches `LIVE` or `FAILED`;
- the SSE stream exists but carries no execution events.

The same breakpoint exists on the agent side. `services/agent/cmd/agent/main.go:21` instantiates only `agent.NewRuntime`, and `agent.Run` does nothing but log every 30 seconds. The M5 agent packages (`identity`, `heartbeat`, `dispatcher`, `state`, `recovery`, `docker`, `traefik`, `health`, `logs`, `capabilities`, `protocol`, `ownership`, `auth`) are delivered and tested but **never mounted in the agent binary**. The repository already states this in `docs/architecture/agent-failure-matrix.md:91-96`.

There is also no Engine-to-Agent HTTP client, so Engine and Agent cannot talk to each other.

Two further packages are delivered and tested but **not adopted by production code**:

- `internal/observability/logging` — the correlation and redaction helpers have **zero production imports**. The Engine logs through plain `internal/logger` (bare `slog`), so no correlation ID reaches the application logs.
- `internal/runtime/presets/docker` — the generic Docker preset is never invoked by analysis, profile or build. A project shipping a valid `Dockerfile` does not get the intended preset validation.
- `services/agent/internal/metrics` — the registry is correct, but the agent exposes no HTTP endpoint, so agent metrics are never scraped.

In short: **analysis -> profile -> plan -> deployment record works and is live; execution does not.** Do not read the shipped agent and build packages as an operational runtime.

### Not present in the repository

- Deployment diagnostics API (#102)
- Axiom CLI (#105, #106)
- V0.1 end-to-end and security/failure release gates (#110, #111)
- Release evidence documentation (#112)

### Persistence model currently implemented

```text
User
 └── GitHubConnection
      └── Repository
           └── Application
                └── Deployment
                     └── Server
```

Migrations 001-013 add sessions, secrets, audit and deployment logs/correlation on top of that chain. The deployment domain still lacks additional entities such as environments and deployment steps.

## Repository structure

```text
axiom/
├── apps/
│   └── cloud/                  # React + Vite console (src/, vite.config.ts)
│
├── docs/
│   ├── adr/
│   ├── agents/
│   ├── architecture/
│   ├── design/
│   ├── operations/
│   ├── orchestrator/
│   ├── product/
│   ├── roadmap/
│   └── security/
│
├── schemas/
│   ├── examples/
│   ├── experts/
│   ├── axiom.yaml
│   ├── agent.yaml
│   ├── artifact.yaml
│   ├── task.yaml
│   └── *.schema.json
│
├── services/
│   ├── engine/                 # cmd/engine, internal/*, migrations/, docker-compose.dev.yml
│   ├── agent/                  # cmd/agent, internal/*, tests/
│   └── orchestrator/           # cmd/orchestrator, internal/* (framework, see ADR-0010)
│
├── tests/
│   ├── agents/
│   ├── contracts/
│   ├── e2e/
│   ├── integration/
│   └── orchestrator/
│
└── .github/workflows/          # Go tests, schema contracts, agent readiness
```

## Technology direction

### Engine

- **Go**
- HTTP API
- PostgreSQL
- `database/sql`
- **pgx v5** PostgreSQL driver
- Structured logging with `log/slog`

### Platform

The planned platform includes:

- GitHub integration
- Repository analysis
- Stack detection
- Deployment planning
- Build execution
- Runtime management
- Docker/container support
- Server management
- Traefik networking
- Domain and SSL management
- Health checks
- Deployment logs and events
- Cloud console
- WebSocket-based realtime updates
- CLI

## Development

### Prerequisites

- Go 1.25+
- Docker / Docker Compose
- PostgreSQL for integration tests

### Start PostgreSQL locally

From the repository root:

```bash
docker compose -f services/engine/docker-compose.dev.yml up -d postgres
```

The development database is exposed locally on port `5432`.

### Configure the Engine

Copy the development environment example:

```bash
cp .env.example .env
```

The Engine uses `DATABASE_URL` for its runtime PostgreSQL connection.

For integration tests, configure:

```bash
export AXIOM_TEST_DATABASE_URL="postgres://axiom:axiom@localhost:5432/axiom?sslmode=disable"
```

Then run:

```bash
cd services/engine
go test ./...
```

If `AXIOM_TEST_DATABASE_URL` is not defined, PostgreSQL integration tests are skipped rather than silently using another database.

## Engineering principles

Axiom is being developed around several principles:

- **Deployment first** — the primary product workflow is GitHub → application → infrastructure → live.
- **Automation without opacity** — automation should expose the decisions it makes.
- **Infrastructure as a product** — servers, runtime, networking and deployment state are first-class platform concepts.
- **Deterministic execution** — deployment plans should be reproducible and inspectable.
- **Security by boundary** — repository, build, runtime and infrastructure permissions must remain separated.
- **Observable operations** — deployments need explicit states, logs, events and health checks.
- **Incremental architecture** — implement the smallest reliable platform slice before expanding the system.
- **AI as an expert layer** — AI assists analysis and decisions where useful; it does not replace deterministic platform primitives.

## Roadmap

The high-level path toward the first usable release is:

```text
M0  Product Definition
 ↓
M1  Architecture & Contracts
 ↓
M2  Expert Framework
 ↓
M3  Deployment Engine
 ↓
M4  Base Platform
 ↓
M5  Runtime Agent
 ↓
M6  Deployment Templates & Runtime Presets
 ↓
M7  Security & Governance
 ↓
M8  Observability & Operations
 ↓
M9  CLI
 ↓
M10 First Reference Deployment
 ↓
M11 GitHub → LIVE
```

The immediate engineering sequence inside the current platform phase is:

```text
M4.1 Go Engine Foundation
        ↓
M4.2 PostgreSQL Data Model & Persistence
        ↓
M4.3 REST API + WebSocket
        ↓
GitHub Integration
        ↓
Repository Analyzer
        ↓
Stack Detection
        ↓
Deployment Plan
        ↓
Build / Runtime
        ↓
Deployment Executor
        ↓
Traefik / Domain / SSL
        ↓
Health Checks
        ↓
GitHub → LIVE
```

## Project status

**Current phase:** M6/M7/M8 delivery — the code is largely landed, the execution path is not yet wired.

**Current focus:** close the build/executor breakpoint (`build.Engine` and `executor.Runner` are not constructed by the Engine bootstrap) and the agent wiring breakpoint (the M5 packages are not mounted in the agent binary), so that a deployment created through the API can actually reach `LIVE`. The Go suite is green; the Cloud Console test suite is not (31 passed / 9 failed) and is not covered by CI.

Axiom's first meaningful milestone is not a generic AI demonstration. It is a reliable deployment path:

> **GitHub repository → detected application → deployment plan → running application.**

## Documentation

Architecture and product decisions live under `docs/`.

Start with:

- `docs/product/vision.md`
- `docs/product/v0.1-scope.md`
- `docs/product/glossary.md`
- `docs/architecture/overview.md`
- `docs/architecture/boundaries.md`
- `docs/architecture/contracts.md`
- `docs/adr/README.md`

## License

License information will be added when the project licensing decision is finalized.
