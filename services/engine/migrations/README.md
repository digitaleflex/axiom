# Engine Migrations

> Issue #115. PostgreSQL is the Engine's single primary datastore ([ADR-0003](../../../docs/adr/0003-postgresql-persistence.md)).

## Discipline

1. Migrations are embedded SQL files applied in lexical order at startup (`migrations.Run`), each in its own transaction, recorded in `schema_migrations`.
2. **Never edit an applied migration.** Every change is a new file `NNN_description.sql`.
3. Migrations must be safe to re-run on an already-migrated database (`IF NOT EXISTS` where possible).
4. Identifiers are opaque `TEXT` (API contract §3); clients never infer structure from IDs.
5. Enumerations are enforced with `CHECK` constraints matching the API contract and domain code.
6. Every migration is covered by `migrations_test.go` (fresh schema + constraints), run with `AXIOM_TEST_DATABASE_URL`.

## V0.1 model & state ownership

| Table | Purpose | Sole writer (package) |
|---|---|---|
| `users` | identity | auth (#125) |
| `github_connections` | GitHub account connection metadata (no tokens in plain columns) | GitHub integration (#91) |
| `repositories` | discovered repositories | GitHub integration (#92) |
| `applications` | deployable projects (several per repository allowed) | applications API (#117) |
| `servers` | registered servers, liveness, capabilities | `internal/server` (#76–#79) |
| `deployment_plans` | immutable, single-use plans with fingerprint | planner (#97) |
| `deployments` | authoritative deployment state, number, URL, error code | `internal/deployment` only (state machine, ADR-0010) |
| `deployment_steps` | per-step status (`BUILD`…`VERIFY`) | Deployment Engine / executor (#100) |
| `deployment_events` | ordered event log per deployment (`seq`) for SSE replay | `internal/deployment` (#116, #118) |
| `idempotency_keys` | durable idempotency (`scope`, `key`, request hash → resource) | `internal/deployment` (#116) |
| `domains` | hostnames per application/environment, one primary | domains (#64) |

## Key constraints

- `deployments.status` ∈ canonical states; default `PENDING`.
- `environment` ∈ `production`, `staging`, `preview`.
- One deployment per plan; unique deployment `number` per application.
- Steps limited to contract step names; events unique per `(deployment_id, seq)`.
- Hostnames lowercase, globally unique; one primary per application/environment.
- Servers referenced by deployments cannot be deleted (`RESTRICT`).
