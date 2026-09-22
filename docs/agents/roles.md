# Axiom Expert Roles

Axiom combines domain experts with GitHub custom agents. The role defines responsibility; the GitHub agent profile defines how that role is executed.

| Role | Primary responsibility | Typical GitHub agent |
|---|---|---|
| Business Domain | Requirements, domain rules, workflows | architecture |
| Architect | System architecture, boundaries, ADRs | architecture |
| Backend / Engine | APIs, services, Go Engine | engine |
| Deployment | GitHub integration, analyzer, profiles, planner | deployment |
| Runtime / Infrastructure | Docker, Traefik, runtime lifecycle | runtime |
| Frontend | Cloud Console, deployment UX | frontend |
| Database | Schema, migrations, persistence | database |
| Security | Auth, secrets, authorization, threat model | security |
| QA | Tests, regression, E2E, release evidence | qa |

## GitHub agent boundaries

### Architecture Agent
Owns architecture contracts, ADRs and cross-component coherence. It must not silently implement unrelated features.

### Engine Agent
Owns Go Engine composition, orchestration primitives and deployment-domain integration.

### Deployment Agent
Owns GitHub integration, repository analysis, application profiles, deployment plans and build orchestration.

### Runtime Agent
Owns runtime execution, Docker, Traefik, health, logs and agent-side lifecycle. It must preserve host ownership boundaries and must not introduce arbitrary host command execution.

### Frontend Agent
Owns the Cloud Console and frontend integration with documented backend contracts.

### Database Agent
Owns PostgreSQL schema, migrations, indexes, persistence and durable idempotency.

### Security Agent
Owns authentication, authorization, credentials, secrets and abuse controls. Security-sensitive work is fail-closed.

### QA Agent
Owns integration tests, E2E tests, regression suites and release evidence.

## Coordination rules

- Every decision has an accountable role.
- Every implementation task has an explicit owned path.
- Cross-domain changes require explicit integration.
- Security constraints apply regardless of which agent requested the change.
- An agent cannot expand its authority merely because it has tool access.
