package logs_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/internal/logs"
	"github.com/digitaleflex/axiom/services/engine/migrations"
)

// openIsolated connects to the test database inside an isolated schema so the
// test exercises a fresh migration without touching other data (same pattern
// as internal/database/servers_test.go).
func openIsolated(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	url := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	schema := "logs_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
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

// seedDeployment inserts the minimum rows for deployment_logs' FK chain.
func seedDeployment(t *testing.T, db *sql.DB, ctx context.Context) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO users (id) VALUES ('usr_1')`,
		`INSERT INTO github_connections (id, user_id) VALUES ('ghc_1', 'usr_1')`,
		`INSERT INTO repositories (id, connection_id, external_id, full_name, clone_url) VALUES ('repo_1', 'ghc_1', '1', 'acme/web', 'x')`,
		`INSERT INTO applications (id, repository_id, name) VALUES ('app_1', 'repo_1', 'web')`,
		`INSERT INTO servers (id, name, address, status) VALUES ('srv_1', 'srv-eu-1', '203.0.113.10', 'ready')`,
		`INSERT INTO deployment_plans (id, application_id, server_id, environment, ref, application_profile_version, fingerprint, body)
		 VALUES ('plan_1', 'app_1', 'srv_1', 'production', 'main', 1, 'sha256:x', '{}')`,
		`INSERT INTO deployments (id, application_id, server_id, environment, plan_id) VALUES ('dep_1', 'app_1', 'srv_1', 'production', 'plan_1')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func entry(deploymentID string, level logs.Level, step logs.Step, source logs.Source, msg string) logs.Entry {
	return logs.Entry{DeploymentID: deploymentID, Level: level, Step: step, Source: source, Message: msg}
}

func TestAppendListRoundTrip(t *testing.T) {
	db, ctx := openIsolated(t)
	seedDeployment(t, db, ctx)
	store := logs.NewPGStore(db, 0) // default cap

	entries := []logs.Entry{
		entry("dep_1", logs.LevelInfo, logs.StepBuild, logs.SourceBuild, "fetching source at abc1234"),
		entry("dep_1", logs.LevelError, logs.StepBuild, logs.SourceBuild, "build failed"),
		entry("dep_1", logs.LevelWarn, logs.StepStart, logs.SourceDeploy, "slow health check"),
	}
	if err := store.Append(ctx, entries...); err != nil {
		t.Fatalf("append: %v", err)
	}
	// IDs and timestamps were assigned.
	for _, e := range entries {
		if e.ID == "" || e.OccurredAt.IsZero() {
			t.Fatalf("entry not completed: %+v", e)
		}
	}

	got, next, err := store.List(ctx, "dep_1", logs.Filter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if next != "" {
		t.Fatalf("nextCursor = %q, want empty on a single page", next)
	}
	if len(got) != 3 {
		t.Fatalf("listed %d entries, want 3", len(got))
	}
	for i, want := range entries {
		if got[i].ID != want.ID || got[i].Message != want.Message || got[i].Level != want.Level ||
			got[i].Step != want.Step || got[i].Source != want.Source || got[i].DeploymentID != want.DeploymentID {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want)
		}
	}
	// Ordered by (occurred_at, id).
	for i := 1; i < len(got); i++ {
		if got[i-1].OccurredAt.After(got[i].OccurredAt) ||
			(got[i-1].OccurredAt.Equal(got[i].OccurredAt) && got[i-1].ID > got[i].ID) {
			t.Errorf("entries out of order at %d: %+v then %+v", i, got[i-1], got[i])
		}
	}

	n, err := store.Count(ctx, "dep_1")
	if err != nil || n != 3 {
		t.Fatalf("count = %d, %v", n, err)
	}
}

func TestAppendRedactsBeforePersistence(t *testing.T) {
	db, ctx := openIsolated(t)
	seedDeployment(t, db, ctx)
	store := logs.NewPGStore(db, 0)

	msg := "clone https://user:pass@github.com/acme/web.git password=hunter2 done"
	if err := store.Append(ctx, entry("dep_1", logs.LevelInfo, logs.StepBuild, logs.SourceBuild, msg)); err != nil {
		t.Fatalf("append: %v", err)
	}
	var raw string
	if err := db.QueryRowContext(ctx,
		`SELECT message FROM deployment_logs WHERE deployment_id = 'dep_1'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == msg {
		t.Fatal("raw message persisted without redaction")
	}
	if strings.Contains(raw, "hunter2") || strings.Contains(raw, "user:pass") {
		t.Fatalf("secret leaked into stored message: %q", raw)
	}
	if !strings.Contains(raw, logs.RedactionMarker) {
		t.Fatalf("redaction marker missing: %q", raw)
	}
}

func TestListFilters(t *testing.T) {
	db, ctx := openIsolated(t)
	seedDeployment(t, db, ctx)
	store := logs.NewPGStore(db, 0)

	entries := []logs.Entry{
		entry("dep_1", logs.LevelDebug, logs.StepBuild, logs.SourceBuild, "debug noise"),
		entry("dep_1", logs.LevelInfo, logs.StepBuild, logs.SourceBuild, "fetching source"),
		entry("dep_1", logs.LevelWarn, logs.StepBuild, logs.SourceBuild, "deprecated flag used"),
		entry("dep_1", logs.LevelError, logs.StepBuild, logs.SourceBuild, "build failed badly"),
		entry("dep_1", logs.LevelInfo, logs.StepStart, logs.SourceDeploy, "container started"),
		entry("dep_1", logs.LevelError, logs.StepVerify, logs.SourceRuntime, "health check failed"),
	}
	if err := store.Append(ctx, entries...); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Exact level set.
	got, _, err := store.List(ctx, "dep_1", logs.Filter{Levels: []logs.Level{logs.LevelError}})
	if err != nil || len(got) != 2 {
		t.Fatalf("level set = %d %v", len(got), err)
	}
	// Minimum severity.
	got, _, err = store.List(ctx, "dep_1", logs.Filter{MinLevel: logs.LevelWarn})
	if err != nil || len(got) != 3 {
		t.Fatalf("min level = %d %v", len(got), err)
	}
	// Step filter.
	got, _, err = store.List(ctx, "dep_1", logs.Filter{Step: logs.StepBuild})
	if err != nil || len(got) != 4 {
		t.Fatalf("step = %d %v", len(got), err)
	}
	// Source filter.
	got, _, err = store.List(ctx, "dep_1", logs.Filter{Source: logs.SourceRuntime})
	if err != nil || len(got) != 1 || got[0].Message != "health check failed" {
		t.Fatalf("source = %+v %v", got, err)
	}
	// Case-insensitive substring search.
	got, _, err = store.List(ctx, "dep_1", logs.Filter{Search: "FAILED"})
	if err != nil || len(got) != 2 {
		t.Fatalf("search = %d %v", len(got), err)
	}
	// Search wildcards are escaped, not interpreted.
	got, _, err = store.List(ctx, "dep_1", logs.Filter{Search: "%"})
	if err != nil || len(got) != 0 {
		t.Fatalf("unescaped wildcard = %d %v", len(got), err)
	}
	// Combined filters.
	got, _, err = store.List(ctx, "dep_1", logs.Filter{
		MinLevel: logs.LevelInfo, Step: logs.StepBuild, Source: logs.SourceBuild, Search: "source",
	})
	if err != nil || len(got) != 1 || got[0].Message != "fetching source" {
		t.Fatalf("combined = %+v %v", got, err)
	}
	// Unknown deployment: empty page.
	got, next, err := store.List(ctx, "dep_x", logs.Filter{})
	if err != nil || len(got) != 0 || next != "" {
		t.Fatalf("unknown deployment = %+v %q %v", got, next, err)
	}
}

func TestListCursorPagination(t *testing.T) {
	db, ctx := openIsolated(t)
	seedDeployment(t, db, ctx)
	store := logs.NewPGStore(db, 0)

	for i := 0; i < 10; i++ {
		if err := store.Append(ctx, entry("dep_1", logs.LevelInfo, logs.StepBuild, logs.SourceBuild, fmt.Sprintf("line %d", i))); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	var all []logs.Entry
	cursor := ""
	pages := 0
	for {
		page, next, err := store.List(ctx, "dep_1", logs.Filter{Limit: 4, Cursor: cursor})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		all = append(all, page...)
		pages++
		if next == "" {
			break
		}
		cursor = next
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(all) != 10 {
		t.Fatalf("collected %d entries, want 10", len(all))
	}
	for i, e := range all {
		if e.Message != fmt.Sprintf("line %d", i) {
			t.Errorf("entry %d = %q, want line %d", i, e.Message, i)
		}
	}
}

func TestAppendEnforcesRetentionCap(t *testing.T) {
	db, ctx := openIsolated(t)
	seedDeployment(t, db, ctx)
	store := logs.NewPGStore(db, 5)

	for i := 0; i < 8; i++ {
		if err := store.Append(ctx, entry("dep_1", logs.LevelInfo, logs.StepBuild, logs.SourceBuild, fmt.Sprintf("line %d", i))); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	n, err := store.Count(ctx, "dep_1")
	if err != nil || n != 5 {
		t.Fatalf("count = %d, %v; cap must bound retention", n, err)
	}
	got, _, err := store.List(ctx, "dep_1", logs.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	// The oldest three entries were deleted in the same transaction as the
	// inserts; the newest five survive.
	for i, e := range got {
		if e.Message != fmt.Sprintf("line %d", i+3) {
			t.Errorf("entry %d = %q, want line %d", i, e.Message, i+3)
		}
	}
}

func TestTrim(t *testing.T) {
	db, ctx := openIsolated(t)
	seedDeployment(t, db, ctx)
	store := logs.NewPGStore(db, 0)

	for i := 0; i < 10; i++ {
		if err := store.Append(ctx, entry("dep_1", logs.LevelInfo, logs.StepBuild, logs.SourceBuild, fmt.Sprintf("line %d", i))); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	deleted, err := store.Trim(ctx, "dep_1", 3)
	if err != nil || deleted != 7 {
		t.Fatalf("trim = %d, %v", deleted, err)
	}
	n, err := store.Count(ctx, "dep_1")
	if err != nil || n != 3 {
		t.Fatalf("count = %d, %v", n, err)
	}
	got, _, err := store.List(ctx, "dep_1", logs.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range got {
		if e.Message != fmt.Sprintf("line %d", i+7) {
			t.Errorf("entry %d = %q, want line %d", i, e.Message, i+7)
		}
	}
	// Trimming to zero deletes everything.
	deleted, err = store.Trim(ctx, "dep_1", 0)
	if err != nil || deleted != 3 {
		t.Fatalf("trim all = %d, %v", deleted, err)
	}
}

func TestTableConstraints(t *testing.T) {
	db, ctx := openIsolated(t)
	seedDeployment(t, db, ctx)
	mustFail := func(name, q string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q); err == nil {
			t.Errorf("%s: expected constraint violation", name)
		}
	}
	mustFail("invalid level", `INSERT INTO deployment_logs (id, deployment_id, level, source) VALUES ('log_x', 'dep_1', 'TRACE', 'build')`)
	mustFail("invalid step", `INSERT INTO deployment_logs (id, deployment_id, level, step, source) VALUES ('log_x', 'dep_1', 'INFO', 'PREPARE', 'build')`)
	mustFail("invalid source", `INSERT INTO deployment_logs (id, deployment_id, level, source) VALUES ('log_x', 'dep_1', 'INFO', 'agent')`)
	mustFail("unknown deployment", `INSERT INTO deployment_logs (id, deployment_id, level, source) VALUES ('log_x', 'dep_x', 'INFO', 'build')`)
	// Empty step is valid (not step-correlated).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO deployment_logs (id, deployment_id, level, step, source, message) VALUES ('log_ok', 'dep_1', 'INFO', '', 'build', 'x')`); err != nil {
		t.Fatalf("empty step must be valid: %v", err)
	}
}

func TestConcurrentAppends(t *testing.T) {
	db, ctx := openIsolated(t)
	seedDeployment(t, db, ctx)
	store := logs.NewPGStore(db, 0)

	const workers = 8
	const perWorker = 25
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			entries := make([]logs.Entry, perWorker)
			for i := range entries {
				entries[i] = entry("dep_1", logs.LevelInfo, logs.StepBuild, logs.SourceBuild,
					fmt.Sprintf("worker %d line %d", w, i))
			}
			if err := store.Append(ctx, entries...); err != nil {
				errs <- fmt.Errorf("worker %d: %w", w, err)
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	n, err := store.Count(ctx, "dep_1")
	if err != nil || n != workers*perWorker {
		t.Fatalf("count = %d, %v; want %d", n, err, workers*perWorker)
	}
	got, next, err := store.List(ctx, "dep_1", logs.Filter{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if next != "" {
		t.Fatalf("nextCursor = %q, want empty", next)
	}
	if len(got) != workers*perWorker {
		t.Fatalf("listed %d, want %d", len(got), workers*perWorker)
	}
	seen := map[string]bool{}
	for i := 1; i < len(got); i++ {
		if got[i-1].OccurredAt.After(got[i].OccurredAt) ||
			(got[i-1].OccurredAt.Equal(got[i].OccurredAt) && got[i-1].ID > got[i].ID) {
			t.Fatalf("entries out of order at %d", i)
		}
	}
	for _, e := range got {
		if seen[e.ID] {
			t.Fatalf("duplicate id %q", e.ID)
		}
		seen[e.ID] = true
	}
}
