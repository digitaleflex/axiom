package database_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/internal/database"
	"github.com/digitaleflex/axiom/services/engine/migrations"
)

func TestPostgreSQLPersistence(t *testing.T) {
	url := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := database.Open(ctx, stdlib.GetDefaultDriver(), database.Config{
		URL:             url,
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := migrations.Run(ctx, db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repos := database.NewRepositories(db)
	ids := testIDs()
	defer cleanupTestData(t, ctx, db, ids.user)

	if err := repos.Users.Create(ctx, ids.user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repos.GitHubConnections.Create(ctx, ids.connection, ids.user); err != nil {
		t.Fatalf("create github connection: %v", err)
	}
	if err := repos.Repositories.Create(ctx, ids.repository, ids.connection, "repo-1", "digitaleflex/example", "https://github.com/digitaleflex/example.git"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if err := repos.Applications.Create(ctx, ids.application, ids.repository, "example"); err != nil {
		t.Fatalf("create application: %v", err)
	}
	if err := repos.Servers.Create(ctx, ids.server, "local-test", "127.0.0.1"); err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := repos.Deployments.Create(ctx, ids.deployment, ids.application, ids.server, "production"); err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	if err := repos.Deployments.SetStatus(ctx, ids.deployment, "running"); err != nil {
		t.Fatalf("set deployment status: %v", err)
	}

	deployment, err := repos.Deployments.Get(ctx, ids.deployment)
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if deployment.Status != "running" {
		t.Fatalf("expected deployment status running, got %q", deployment.Status)
	}

	user, err := repos.Users.Get(ctx, ids.user)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if user.ID != ids.user || user.CreatedAt.IsZero() {
		t.Fatalf("unexpected user: %+v", user)
	}
}

func TestTransactionRollback(t *testing.T) {
	url := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := sql.Open(stdlib.GetDefaultDriver(), url)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := migrations.Run(ctx, db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	id := "00000000-0000-0000-0000-000000000099"
	_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)

	err = database.Tx(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO users (id) VALUES ($1)`, id); err != nil {
			return err
		}
		return context.Canceled
	})
	if err == nil {
		t.Fatal("expected transaction error")
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = $1`, id).Scan(&count); err != nil {
		t.Fatalf("check rollback: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected rollback, found %d rows", count)
	}
}

type testIDSet struct {
	user, connection, repository, application, server, deployment string
}

func testIDs() testIDSet {
	return testIDSet{
		user:        "00000000-0000-0000-0000-000000000001",
		connection:  "00000000-0000-0000-0000-000000000002",
		repository:  "00000000-0000-0000-0000-000000000003",
		application: "00000000-0000-0000-0000-000000000004",
		server:      "00000000-0000-0000-0000-000000000005",
		deployment:  "00000000-0000-0000-0000-000000000006",
	}
}

func cleanupTestData(t *testing.T, ctx context.Context, db *sql.DB, userID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Logf("cleanup test data: %v", err)
	}
}
