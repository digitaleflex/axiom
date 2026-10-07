package audit_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/internal/audit"
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

	schema := "audit_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
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
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, ctx
}

func TestPGStoreRecordAndList(t *testing.T) {
	db, ctx := openFresh(t)
	store := audit.NewPGStore(db)
	svc := audit.NewService(store)

	base := audit.Event{
		ActorID: "usr_1", ActorName: "Jane", Action: "deployment.create",
		TargetType: "deployment", TargetID: "dep_1", Result: audit.ResultOK,
		RequestID: "req_1", CorrelationID: "corr_1", DeploymentID: "dep_1",
		OwnerID: "usr_1",
		Details: map[string]string{"environment": "production"},
	}
	e1 := base
	e1.OccurredAt = time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	if err := svc.Record(ctx, e1); err != nil {
		t.Fatal(err)
	}
	e2 := base
	e2.Action = "deployment.cancel"
	e2.TargetID = "dep_2"
	e2.OccurredAt = time.Date(2026, 10, 7, 11, 0, 0, 0, time.UTC)
	if err := svc.Record(ctx, e2); err != nil {
		t.Fatal(err)
	}

	// Newest first.
	items, err := svc.List(ctx, audit.Filter{})
	if err != nil || len(items) != 2 {
		t.Fatalf("list = %v, %v", items, err)
	}
	if items[0].Action != "deployment.cancel" || items[1].Action != "deployment.create" {
		t.Fatalf("order = %v", items)
	}
	if items[0].ID == "" || items[0].OccurredAt.IsZero() {
		t.Fatalf("event must have ID and timestamp: %+v", items[0])
	}

	// Actor filter.
	items, err = svc.List(ctx, audit.Filter{ActorID: "usr_1"})
	if err != nil || len(items) != 2 {
		t.Fatalf("actor filter = %v, %v", items, err)
	}
	items, err = svc.List(ctx, audit.Filter{ActorID: "usr_2"})
	if err != nil || len(items) != 0 {
		t.Fatalf("actor filter miss = %v, %v", items, err)
	}

	// Target filter.
	items, err = svc.List(ctx, audit.Filter{TargetType: "deployment", TargetID: "dep_1"})
	if err != nil || len(items) != 1 {
		t.Fatalf("target filter = %v, %v", items, err)
	}

	// Owner scoping.
	items, err = svc.List(ctx, audit.Filter{OwnerID: "usr_1"})
	if err != nil || len(items) != 2 {
		t.Fatalf("owner scope = %v, %v", items, err)
	}
	items, err = svc.List(ctx, audit.Filter{OwnerID: "usr_2"})
	if err != nil || len(items) != 0 {
		t.Fatalf("owner scope miss = %v, %v", items, err)
	}
}

func TestPGStoreIndexesExist(t *testing.T) {
	db, ctx := openFresh(t)
	for _, idx := range []string{
		"idx_audit_events_actor",
		"idx_audit_events_target",
		"idx_audit_events_occurred_at",
		"idx_audit_events_owner",
	} {
		var exists bool
		err := db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = current_schema() AND indexname = $1)`, idx).Scan(&exists)
		if err != nil || !exists {
			t.Errorf("index %s missing (err=%v)", idx, err)
		}
	}
}

func TestPGStoreRejectsInvalidResult(t *testing.T) {
	db, ctx := openFresh(t)
	store := audit.NewPGStore(db)
	e := audit.Event{ActorID: "usr_1", Action: "x", TargetType: "t", TargetID: "i", Result: "bogus"}
	if err := store.Record(ctx, e); err == nil {
		t.Fatal("invalid result must be rejected by the CHECK constraint")
	}
}

func TestNoSecretInDatabase(t *testing.T) {
	db, ctx := openFresh(t)
	store := audit.NewPGStore(db)
	svc := audit.NewService(store)

	// A key=value secret: logs.Redact's KV pattern recognizes it.
	secret := "hunter2"
	e := audit.Event{
		ActorID: "usr_1", Action: "github.disconnect", TargetType: "github_connection",
		TargetID: "ghc_1", Result: audit.ResultError, ErrorCode: "token_rejected",
		OwnerID: "usr_1",
		Details: map[string]string{"reason": "connect failed password=" + secret},
	}
	if err := svc.Record(ctx, e); err != nil {
		t.Fatal(err)
	}

	// Raw scan: the secret must not appear anywhere in the table, in any
	// column or in the JSONB details.
	var raw string
	if err := db.QueryRowContext(ctx,
		`SELECT coalesce(id,'') || ' ' || coalesce(actor_id,'') || ' ' || coalesce(actor_name,'') || ' ' ||
		        coalesce(action,'') || ' ' || coalesce(target_type,'') || ' ' || coalesce(target_id,'') || ' ' ||
		        coalesce(result,'') || ' ' || coalesce(error_code,'') || ' ' || coalesce(request_id,'') || ' ' ||
		        coalesce(correlation_id,'') || ' ' || coalesce(deployment_id,'') || ' ' || coalesce(owner_id,'') || ' ' ||
		        coalesce(details::text,'') FROM audit_events`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, secret) {
		t.Fatalf("secret found in audit_events row: %q", raw)
	}
	if !strings.Contains(raw, "[REDACTED]") {
		t.Fatalf("redaction marker missing from row: %q", raw)
	}
}
