package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/migrations"
)

// openFresh connects to the test database inside an isolated schema so the
// test exercises a fresh migration without touching other data.
func openFresh(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	url := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	schema := "mig_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`) })

	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	db, err := sql.Open("pgx", url+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db, ctx
}

func TestFreshMigrationCreatesV01Schema(t *testing.T) {
	db, ctx := openFresh(t)
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Re-running is a no-op.
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	want := []string{"users", "github_connections", "repositories", "applications", "servers",
		"deployment_plans", "deployments", "deployment_steps", "deployment_events", "idempotency_keys", "domains",
		"audit_events"}
	for _, table := range want {
		var exists bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1)`, table).Scan(&exists)
		if err != nil || !exists {
			t.Errorf("table %s missing (err=%v)", table, err)
		}
	}

	var idType string
	if err := db.QueryRowContext(ctx, `SELECT data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'deployments' AND column_name = 'id'`).Scan(&idType); err != nil {
		t.Fatal(err)
	}
	if idType != "text" {
		t.Errorf("deployments.id type = %s, want text (opaque IDs)", idType)
	}
}

func TestSchemaConstraints(t *testing.T) {
	db, ctx := openFresh(t)
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustFail := func(name, q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err == nil {
			t.Errorf("%s: expected constraint violation", name)
		}
	}

	mustExec(`INSERT INTO users (id) VALUES ('usr_1')`)
	mustExec(`INSERT INTO github_connections (id, user_id) VALUES ('ghc_1', 'usr_1')`)
	mustExec(`INSERT INTO repositories (id, connection_id, external_id, full_name, clone_url) VALUES ('repo_1', 'ghc_1', '1', 'acme/web', 'https://github.com/acme/web.git')`)
	mustExec(`INSERT INTO applications (id, repository_id, name) VALUES ('app_1', 'repo_1', 'acme-web')`)
	mustExec(`INSERT INTO applications (id, repository_id, name) VALUES ('app_2', 'repo_1', 'acme-web-2')`) // several apps per repo
	mustExec(`INSERT INTO servers (id, name, address, status) VALUES ('srv_1', 'srv-eu-1', '203.0.113.10', 'ready')`)
	mustExec(`INSERT INTO deployment_plans (id, application_id, server_id, environment, ref, application_profile_version, fingerprint, body)
	          VALUES ('plan_1', 'app_1', 'srv_1', 'production', 'main', 1, 'sha256:x', '{}')`)
	mustExec(`INSERT INTO deployments (id, application_id, server_id, environment, plan_id, number) VALUES ('dep_1', 'app_1', 'srv_1', 'production', 'plan_1', 1)`)

	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM deployments WHERE id = 'dep_1'`).Scan(&status); err != nil || status != "PENDING" {
		t.Errorf("default status = %q (err=%v), want PENDING", status, err)
	}

	mustFail("lowercase status", `UPDATE deployments SET status = 'running' WHERE id = 'dep_1'`)
	mustFail("unknown environment", `INSERT INTO deployments (id, application_id, server_id, environment) VALUES ('dep_x', 'app_1', 'srv_1', 'dev')`)
	mustFail("plan single-use", `INSERT INTO deployments (id, application_id, server_id, environment, plan_id) VALUES ('dep_2', 'app_1', 'srv_1', 'production', 'plan_1')`)
	mustFail("duplicate deployment number", `INSERT INTO deployments (id, application_id, server_id, environment, number) VALUES ('dep_3', 'app_1', 'srv_1', 'production', 1)`)
	mustFail("unknown server status", `UPDATE servers SET status = 'up' WHERE id = 'srv_1'`)
	mustFail("non-contract step", `INSERT INTO deployment_steps (deployment_id, name, position) VALUES ('dep_1', 'PREPARE', 1)`)
	mustExec(`INSERT INTO deployment_steps (deployment_id, name, position) VALUES ('dep_1', 'BUILD', 1)`)
	mustExec(`INSERT INTO deployment_events (deployment_id, seq, id, type) VALUES ('dep_1', 1, 'evt_1', 'deployment.created')`)
	mustFail("duplicate event seq", `INSERT INTO deployment_events (deployment_id, seq, id, type) VALUES ('dep_1', 1, 'evt_2', 'x')`)
	mustExec(`INSERT INTO idempotency_keys (scope, key, request_hash, resource_id) VALUES ('deploy:app_1', 'k1', 'h', 'dep_1')`)
	mustFail("duplicate idempotency key", `INSERT INTO idempotency_keys (scope, key, request_hash, resource_id) VALUES ('deploy:app_1', 'k1', 'h2', 'dep_9')`)
	mustExec(`INSERT INTO domains (id, application_id, environment, hostname, is_primary) VALUES ('dom_1', 'app_1', 'production', 'app.acme.dev', true)`)
	mustFail("uppercase hostname", `INSERT INTO domains (id, application_id, environment, hostname) VALUES ('dom_2', 'app_1', 'production', 'App.acme.dev')`)
	mustFail("duplicate hostname", `INSERT INTO domains (id, application_id, environment, hostname) VALUES ('dom_3', 'app_2', 'staging', 'app.acme.dev')`)
	mustFail("two primary domains", `INSERT INTO domains (id, application_id, environment, hostname, is_primary) VALUES ('dom_4', 'app_1', 'production', 'www.acme.dev', true)`)
	mustFail("server in use", `DELETE FROM servers WHERE id = 'srv_1'`)

	// Cascade: deleting the application removes deployments, steps and events.
	mustExec(`DELETE FROM deployments WHERE id = 'dep_1'`)
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM deployment_steps WHERE deployment_id = 'dep_1'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("steps not cascaded: n=%d err=%v", n, err)
	}
}
