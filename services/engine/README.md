# Axiom Engine

The Engine is the backend foundation for Axiom's deployment platform.

## Local PostgreSQL

Start the development database:

```bash
docker compose -f docker-compose.dev.yml up -d
```

Default connection:

```text
postgres://axiom:axiom@localhost:5432/axiom?sslmode=disable
```

## Run the engine

```bash
export DATABASE_URL='postgres://axiom:axiom@localhost:5432/axiom?sslmode=disable'
go run ./cmd/engine
```

Migrations are applied automatically during startup when the database is configured.

## Integration tests

```bash
export AXIOM_TEST_DATABASE_URL='postgres://axiom:axiom@localhost:5432/axiom?sslmode=disable'
go test ./...
```

The PostgreSQL integration tests are skipped when `AXIOM_TEST_DATABASE_URL` is not set. This keeps the default unit-test run independent from an external database while making the integration path explicit.
