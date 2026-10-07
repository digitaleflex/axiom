package appconfig

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/internal/security/secrets"
	"github.com/digitaleflex/axiom/services/engine/migrations"
)

// newTestService connects to the test database inside an isolated schema and
// runs the full migration set, so each test exercises a fresh secrets table.
func newTestService(t *testing.T) (*Service, *sql.DB, context.Context) {
	t.Helper()
	dsn := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	schema := "appcfg_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`) })

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, err := sql.Open("pgx", dsn+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	box, err := secrets.NewBox(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Store: &secrets.EncryptedStore{DB: db, Box: box}}, db, ctx
}

func TestValidName(t *testing.T) {
	valid := []string{"DATABASE_URL", "API_KEY", "A", "_UNDER", "PORT_8080"}
	for _, n := range valid {
		if !ValidName(n) {
			t.Errorf("ValidName(%q) = false, want true", n)
		}
	}
	invalid := []string{"", "database-url", "1DATABASE", "DATABASE-URL", "DATABASE.URL", "DATABASE URL", "database"}
	for _, n := range invalid {
		if ValidName(n) {
			t.Errorf("ValidName(%q) = true, want false", n)
		}
	}
}

func TestSetValidatesName(t *testing.T) {
	svc, _, ctx := newTestService(t)
	for _, n := range []string{"database-url", "1DATABASE", "DATABASE-URL", ""} {
		if err := svc.Set(ctx, "app_1", n, "v", true); err == nil {
			t.Errorf("Set with name %q must fail", n)
		}
	}
}

func TestSetListDeleteRoundTrip(t *testing.T) {
	svc, _, ctx := newTestService(t)
	if err := svc.Set(ctx, "app_1", "DATABASE_URL", "postgres://db/app", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.Set(ctx, "app_1", "API_KEY", "gho_api_secret", true); err != nil {
		t.Fatal(err)
	}
	entries, err := svc.List(ctx, "app_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name != "API_KEY" || entries[1].Name != "DATABASE_URL" {
		t.Fatalf("list = %+v", entries)
	}
	for _, e := range entries {
		if !e.Secret || !e.IsSet || e.UpdatedAt.IsZero() {
			t.Fatalf("entry = %+v", e)
		}
	}

	if err := svc.Delete(ctx, "app_1", "API_KEY"); err != nil {
		t.Fatal(err)
	}
	entries, err = svc.List(ctx, "app_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "DATABASE_URL" {
		t.Fatalf("list after delete = %+v", entries)
	}
}

func TestListNeverLeaksValues(t *testing.T) {
	svc, db, ctx := newTestService(t)
	if err := svc.Set(ctx, "app_1", "DATABASE_URL", "postgres://db/app", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.Set(ctx, "app_1", "API_KEY", "gho_api_secret", true); err != nil {
		t.Fatal(err)
	}

	entries, err := svc.List(ctx, "app_1")
	if err != nil {
		t.Fatal(err)
	}
	// The JSON form of List carries no value field and no plaintext.
	rawJSON, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawJSON), "value") {
		t.Fatalf("list JSON leaks a value field: %s", rawJSON)
	}
	for _, secret := range []string{"postgres://db/app", "gho_api_secret"} {
		if strings.Contains(string(rawJSON), secret) {
			t.Fatalf("list JSON leaks plaintext: %s", rawJSON)
		}
	}
	// The raw database holds no plaintext either.
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM secrets WHERE value_sealed LIKE $1`, "%gho_api_secret%").Scan(&n); err != nil || n != 0 {
		t.Fatalf("plaintext found in database (n=%d, err=%v)", n, err)
	}
}

func TestResolveDelegatesToInjector(t *testing.T) {
	svc, _, ctx := newTestService(t)
	if err := svc.Set(ctx, "app_1", "DATABASE_URL", "postgres://db/app", true); err != nil {
		t.Fatal(err)
	}
	env, err := svc.Resolve(ctx, "app_1", []string{"DATABASE_URL"})
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 1 || env[0] != "DATABASE_URL=postgres://db/app" {
		t.Fatalf("env = %v", env)
	}

	// Missing required names produce a typed error listing names only.
	_, err = svc.Resolve(ctx, "app_1", []string{"DATABASE_URL", "STRIPE_KEY"})
	var missing *secrets.MissingRequiredError
	if !errors.As(err, &missing) {
		t.Fatalf("error type = %T, want *secrets.MissingRequiredError", err)
	}
	if len(missing.Missing) != 1 || missing.Missing[0] != "STRIPE_KEY" {
		t.Fatalf("missing = %v", missing.Missing)
	}
	if strings.Contains(err.Error(), "postgres://db/app") {
		t.Fatalf("error leaks values: %v", err)
	}
}

func TestScopesAreIsolatedPerApplication(t *testing.T) {
	svc, _, ctx := newTestService(t)
	if err := svc.Set(ctx, "app_1", "DATABASE_URL", "postgres://db/one", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.Set(ctx, "app_2", "DATABASE_URL", "postgres://db/two", true); err != nil {
		t.Fatal(err)
	}
	env1, err := svc.Resolve(ctx, "app_1", []string{"DATABASE_URL"})
	if err != nil {
		t.Fatal(err)
	}
	env2, err := svc.Resolve(ctx, "app_2", []string{"DATABASE_URL"})
	if err != nil {
		t.Fatal(err)
	}
	if env1[0] != "DATABASE_URL=postgres://db/one" || env2[0] != "DATABASE_URL=postgres://db/two" {
		t.Fatalf("scopes leaked: %v / %v", env1, env2)
	}
}
