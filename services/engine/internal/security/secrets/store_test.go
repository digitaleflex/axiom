package secrets

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/migrations"
)

// newTestStore connects to the test database inside an isolated schema and
// runs the full migration set, so each test exercises a fresh secrets table.
func newTestStore(t *testing.T) (EncryptedStore, *sql.DB, context.Context) {
	t.Helper()
	dsn := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	schema := "secrets_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
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
	box, err := NewBox(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return EncryptedStore{DB: db, Box: box}, db, ctx
}

func TestPutGetRoundTrip(t *testing.T) {
	store, _, ctx := newTestStore(t)
	if err := store.Put(ctx, "application:app_1", "DATABASE_URL", "postgres://db/app"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, "application:app_1", "DATABASE_URL")
	if err != nil || got != "postgres://db/app" {
		t.Fatalf("get = %q, %v", got, err)
	}
	exists, err := store.Exists(ctx, "application:app_1", "DATABASE_URL")
	if err != nil || !exists {
		t.Fatalf("exists = %v, %v", exists, err)
	}
}

func TestCiphertextContainsNoPlaintext(t *testing.T) {
	store, db, ctx := newTestStore(t)
	plaintext := "gho_super_secret_token"
	if err := store.Put(ctx, "application:app_1", "TOKEN", plaintext); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT value_sealed FROM secrets WHERE scope = $1 AND name = $2`,
		"application:app_1", "TOKEN").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(plaintext)) {
		t.Fatal("stored ciphertext contains plaintext")
	}
	if string(raw) == plaintext {
		t.Fatal("stored value is plaintext")
	}
}

func TestAADBindingWrongScopeFails(t *testing.T) {
	store, _, ctx := newTestStore(t)
	if err := store.Put(ctx, "application:app_1", "TOKEN", "gho_secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "application:app_2", "TOKEN"); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong scope must fail with ErrDecrypt, got %v", err)
	}
	if _, err := store.Get(ctx, "application:app_1", "OTHER"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing name must fail with ErrNotFound, got %v", err)
	}
}

func TestUpsertUpdatesValue(t *testing.T) {
	store, db, ctx := newTestStore(t)
	if err := store.Put(ctx, "application:app_1", "DATABASE_URL", "postgres://db/v1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "application:app_1", "DATABASE_URL", "postgres://db/v2"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, "application:app_1", "DATABASE_URL")
	if err != nil || got != "postgres://db/v2" {
		t.Fatalf("get after upsert = %q, %v", got, err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM secrets WHERE scope = $1 AND name = $2`,
		"application:app_1", "DATABASE_URL").Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d (err=%v), want 1 after upsert", n, err)
	}
}

func TestListNeverLeaksValues(t *testing.T) {
	store, db, ctx := newTestStore(t)
	if err := store.Put(ctx, "application:app_1", "DATABASE_URL", "postgres://db/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "application:app_1", "API_KEY", "gho_api_secret"); err != nil {
		t.Fatal(err)
	}

	metas, err := store.ListNames(ctx, "application:app_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 || metas[0].Name != "API_KEY" || metas[1].Name != "DATABASE_URL" {
		t.Fatalf("list = %+v", metas)
	}
	for _, m := range metas {
		if m.UpdatedAt.IsZero() {
			t.Fatalf("meta %s has zero UpdatedAt", m.Name)
		}
	}

	// The JSON form of the metadata carries no value field.
	rawJSON, err := json.Marshal(metas)
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

func TestDelete(t *testing.T) {
	store, _, ctx := newTestStore(t)
	if err := store.Put(ctx, "application:app_1", "TOKEN", "gho_secret"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, "application:app_1", "TOKEN"); err != nil {
		t.Fatal(err)
	}
	exists, err := store.Exists(ctx, "application:app_1", "TOKEN")
	if err != nil || exists {
		t.Fatalf("exists after delete = %v, %v", exists, err)
	}
	if _, err := store.Get(ctx, "application:app_1", "TOKEN"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete must be ErrNotFound, got %v", err)
	}
}

func TestNoSecretInLogs(t *testing.T) {
	store, _, ctx := newTestStore(t)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	plaintext := "gho_never_log_me"
	if err := store.Put(ctx, "application:app_1", "TOKEN", plaintext); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "application:app_1", "TOKEN"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListNames(ctx, "application:app_1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, "application:app_1", "TOKEN"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), plaintext) {
		t.Fatalf("plaintext leaked into logs: %s", buf.String())
	}
}

func TestErrorsNeverContainPlaintext(t *testing.T) {
	// Put without a box fails before touching the database.
	s := EncryptedStore{DB: nil, Box: nil}
	err := s.Put(context.Background(), "application:app_1", "TOKEN", "gho_secret")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "gho_secret") {
		t.Fatalf("error contains plaintext: %v", err)
	}

	// A decrypt failure never contains the plaintext.
	store, _, ctx := newTestStore(t)
	sealed, _ := store.Box.Seal([]byte("gho_distinctive_plaintext"), []byte("application:app_1/WRONG"))
	_, err = store.Box.Open(sealed, []byte("application:app_1/TOKEN"))
	if !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong aad must fail with ErrDecrypt, got %v", err)
	}
	if strings.Contains(err.Error(), "gho_distinctive_plaintext") {
		t.Fatalf("decrypt error contains plaintext: %v", err)
	}
}

func TestMigrationCreatesSecretsTable(t *testing.T) {
	_, db, ctx := newTestStore(t)
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'secrets')`).
		Scan(&exists); err != nil || !exists {
		t.Fatalf("secrets table missing (err=%v)", err)
	}

	// UNIQUE(scope, name): a duplicate insert must fail.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO secrets (secret_id, scope, name, value_sealed) VALUES ('sec_1', 'application:app_1', 'TOKEN', '\x01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO secrets (secret_id, scope, name, value_sealed) VALUES ('sec_2', 'application:app_1', 'TOKEN', '\x01')`); err == nil {
		t.Fatal("duplicate (scope, name) must be rejected")
	}
	// CHECK constraints: empty scope/name must fail.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO secrets (secret_id, scope, name, value_sealed) VALUES ('sec_3', '', 'TOKEN', '\x01')`); err == nil {
		t.Fatal("empty scope must be rejected")
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO secrets (secret_id, scope, name, value_sealed) VALUES ('sec_4', 'application:app_1', '', '\x01')`); err == nil {
		t.Fatal("empty name must be rejected")
	}
}
