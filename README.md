# Axiom

**Platform Engineering Platform.**

Axiom simplifies infrastructure management by providing a unified platform for deployment, automation, monitoring, networking and operations.

## Architecture

```
apps/
├── core/     ← Admin interface
└── cloud/    ← Client interface (future)

services/
└── engine/   ← Backend Go (source of truth)

agents/
└── agent/    ← Go agent (local operations)

cli/          ← Axiom CLI
sdk/          ← SDKs (Go, TypeScript, Python)
drivers/      ← Docker, Kubernetes, etc.
packages/     ← Shared TS libraries
infrastructure/ ← Docker, Traefik, PostgreSQL configs
docs/         ← RFCs, ADRs, architecture docs
```

## Getting Started

```bash
# Clone and bootstrap
git clone https://github.com/axiom/axiom-paas.git
cd axiom-paas
make bootstrap

# Start infrastructure
make docker-up

# Run development server
make dev
```

## Stack

| Layer       | Technology        |
|-------------|-------------------|
| Backend     | Go                |
| Frontend    | Next.js / React   |
| Database    | PostgreSQL        |
| Cache       | Redis             |
| Messaging   | NATS              |
| Proxy       | Traefik           |
| Containers  | Docker Compose    |

## License

MIT
