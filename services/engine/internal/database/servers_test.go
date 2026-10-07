package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/server"
	"github.com/digitaleflex/axiom/services/engine/migrations"
)

func openIsolated(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	url := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	schema := "srv_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
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
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

func TestServerLifecycle(t *testing.T) {
	db, ctx := openIsolated(t)
	repos := NewRepositories(db)
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id) VALUES ('usr_1')`); err != nil {
		t.Fatal(err)
	}

	rec := server.Record{ID: "srv_1", Name: "srv-eu-1", Address: "203.0.113.10", OwnerID: "usr_1", Status: server.StatusPending}
	if err := repos.Servers.Create(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repos.Servers.Get(ctx, "srv_1")
	if err != nil || got.Status != server.StatusPending {
		t.Fatalf("get = %+v %v", got, err)
	}
	if err := repos.Servers.Rename(ctx, "srv_1", "srv-eu-2"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if got, _ := repos.Servers.Get(ctx, "srv_1"); got.Name != "srv-eu-2" {
		t.Fatalf("renamed = %+v", got)
	}
	if err := repos.Servers.Rename(ctx, "srv_x", "srv-3"); !errors.Is(err, server.ErrNotFound) {
		t.Fatalf("rename missing: %v", err)
	}

	items, total, err := repos.Servers.ListFiltered(ctx, "pending", 10, 0)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("filtered = %d %v %v", total, items, err)
	}
	if _, total, _ := repos.Servers.ListFiltered(ctx, "ready", 10, 0); total != 0 {
		t.Fatal("ready filter must be empty")
	}
	if _, total, _ := repos.Servers.ListFiltered(ctx, "", 10, 0); total != 1 {
		t.Fatal("unfiltered must return all")
	}

	// Health round-trip with capabilities.
	health := server.Health{Status: server.StatusReady, AgentVersion: "0.1.3",
		Capabilities: []server.Capability{server.CapabilityDocker, server.CapabilityTraefik},
		CPUCount:     4, MemoryMB: 8192, DiskFreeMB: 50000, LastSeenAt: time.Now().UTC().Format(time.RFC3339)}
	if err := repos.Servers.UpdateHealth(ctx, "srv_1", health); err != nil {
		t.Fatalf("health: %v", err)
	}
	if got, _ := repos.Servers.Get(ctx, "srv_1"); got.Status != server.StatusReady || len(got.Capabilities) != 2 || got.CPUCount != 4 || got.LastSeenAt == "" {
		t.Fatalf("after health = %+v", got)
	}

	// Removal guard: active deployment blocks, terminal ones do not.
	for _, q := range []string{
		`INSERT INTO github_connections (id, user_id) VALUES ('ghc_1', 'usr_1')`,
		`INSERT INTO repositories (id, connection_id, external_id, full_name, clone_url) VALUES ('repo_1', 'ghc_1', '1', 'acme/web', 'x')`,
		`INSERT INTO applications (id, repository_id, name) VALUES ('app_1', 'repo_1', 'web')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	seed := func(id, status string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO deployment_plans (id, application_id, server_id, environment, ref, application_profile_version, fingerprint, body)
			VALUES ('plan_`+id+`', 'app_1', 'srv_1', 'production', 'main', 1, 'sha256:`+id+`', '{"steps":["BUILD"]}')`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO deployments (id, application_id, server_id, environment, plan_id, status) VALUES ('dep_`+id+`', 'app_1', 'srv_1', 'production', 'plan_`+id+`', '`+status+`')`); err != nil {
			t.Fatal(err)
		}
	}
	seed("live", "LIVE")
	if n, _ := repos.Servers.ActiveDeployments(ctx, "srv_1"); n != 1 {
		t.Fatalf("active = %d", n)
	}
	if err := repos.Servers.Delete(ctx, "srv_1"); err == nil {
		t.Fatal("delete must be blocked by RESTRICT while deployments reference the server")
	}
	if _, err := db.ExecContext(ctx, `UPDATE deployments SET status = 'FAILED' WHERE id = 'dep_live'`); err != nil {
		t.Fatal(err)
	}
	if n, _ := repos.Servers.ActiveDeployments(ctx, "srv_1"); n != 0 {
		t.Fatalf("active after failure = %d", n)
	}
	// Plans still reference the server: friendly history error, not a 500.
	if err := repos.Servers.Delete(ctx, "srv_1"); !errors.Is(err, server.ErrHasHistory) {
		t.Fatalf("plan history must block deletion: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM deployments WHERE id = 'dep_live'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM deployment_plans WHERE id = 'plan_live'`); err != nil {
		t.Fatal(err)
	}
	if err := repos.Servers.Delete(ctx, "srv_1"); err != nil {
		t.Fatalf("delete without references: %v", err)
	}
	if _, err := repos.Servers.Get(ctx, "srv_1"); !errors.Is(err, server.ErrNotFound) {
		t.Fatalf("deleted get: %v", err)
	}
	if err := repos.Servers.Delete(ctx, "srv_1"); !errors.Is(err, server.ErrNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
}
