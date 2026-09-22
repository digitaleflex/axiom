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

The repository is currently in the **foundation and persistence phase**.

### Implemented foundation

- Go Engine bootstrap
- Configuration loading
- Structured logging with `slog`
- HTTP server foundation
- Health and readiness endpoints
- PostgreSQL connection layer
- PostgreSQL connection pooling configuration
- Migration runner
- Initial relational schema
- Repository/persistence layer
- Transaction helper
- PostgreSQL integration-test suite
- Local PostgreSQL development environment

### Persistence model currently implemented

The current V0.1 persistence foundation contains:

```text
User
 └── GitHubConnection
      └── Repository
           └── Application
                └── Deployment
                     └── Server
```

The persistence layer is intentionally being expanded incrementally. The complete deployment domain still needs additional entities such as environments, deployment steps, domains, logs and agent-related state.

## Repository structure

```text
axiom/
├── docs/
│   ├── product/
│   ├── architecture/
│   ├── agents/
│   ├── orchestrator/
│   └── adr/
│
├── schemas/
│   ├── axiom.yaml
│   ├── agent.yaml
│   ├── artifact.yaml
│   └── task.yaml
│
├── services/
│   └── engine/
│       ├── cmd/
│       │   └── engine/
│       │       └── main.go
│       ├── internal/
│       │   ├── config/
│       │   ├── database/
│       │   ├── httpserver/
│       │   └── logger/
│       ├── migrations/
│       ├── go.mod
│       └── README.md
│
└── tests/
    ├── contracts/
    ├── agents/
    ├── integration/
    └── e2e/
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
docker compose -f infra/docker-compose.dev.yml up -d postgres
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

**Current phase:** Foundation → Persistence → API

**Current focus:** complete the authoritative PostgreSQL domain model, validate persistence with a real PostgreSQL environment, then expose the platform through the REST API and WebSocket layer.

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
