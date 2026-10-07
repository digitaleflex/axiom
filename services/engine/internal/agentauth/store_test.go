package agentauth

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

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

	schema := "agentauth_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
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
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, ctx
}

func seedServer(t *testing.T, db *sql.DB, ctx context.Context, id, status string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id) VALUES ('usr_1') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO servers (id, name, address, status) VALUES ($1, $2, $3, $4)`,
		id, "srv-"+id, "203.0.113.10", status); err != nil {
		t.Fatal(err)
	}
}

func TestMigration009CreatesAgentTables(t *testing.T) {
	db, ctx := openIsolated(t)
	for _, table := range []string{"agent_identities", "agent_bootstrap_tokens"} {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1)`, table).Scan(&exists); err != nil || !exists {
			t.Fatalf("table %s missing (err=%v)", table, err)
		}
	}
}

func TestPGStoreBootstrapTokenLifecycle(t *testing.T) {
	db, ctx := openIsolated(t)
	seedServer(t, db, ctx, "srv_1", "pending")
	store := NewPGStore(db)
	now := time.Now().UTC()

	if err := store.SaveBootstrapToken(ctx, "hash_valid", "srv_1", now.Add(15*time.Minute)); err != nil {
		t.Fatalf("save: %v", err)
	}
	bt, err := store.ConsumeBootstrapToken(ctx, "hash_valid", now)
	if err != nil || bt.ServerID != "srv_1" {
		t.Fatalf("consume = %+v %v", bt, err)
	}
	if _, err := store.ConsumeBootstrapToken(ctx, "hash_valid", now); !errors.Is(err, ErrTokenUsed) {
		t.Fatalf("double consume err = %v, want ErrTokenUsed", err)
	}
	if _, err := store.ConsumeBootstrapToken(ctx, "hash_missing", now); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("missing token err = %v, want ErrTokenInvalid", err)
	}
	if err := store.SaveBootstrapToken(ctx, "hash_expired", "srv_1", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeBootstrapToken(ctx, "hash_expired", now); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expired token err = %v, want ErrTokenExpired", err)
	}
}

func TestPGStoreIdentityLifecycle(t *testing.T) {
	db, ctx := openIsolated(t)
	seedServer(t, db, ctx, "srv_1", "pending")
	store := NewPGStore(db)
	now := time.Now().UTC()
	grace := now.Add(RotationGrace)

	id := Identity{
		AgentID: "agent_0123456789abcdef01234567", ServerID: "srv_1", AgentVersion: "0.1.0",
		Status: "active", CreatedAt: now, CredentialVersion: 1,
		CredentialExpiresAt: now.Add(CredentialTTL), credentialHash: hashToken("ac_first"),
	}
	if err := store.CreateIdentity(ctx, id); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := store.IdentityByServer(ctx, "srv_1")
	if err != nil || got.AgentID != id.AgentID || got.credentialHash != id.credentialHash {
		t.Fatalf("by server = %+v %v", got, err)
	}
	if _, err := store.IdentityByAgent(ctx, "agent_missing"); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("missing identity err = %v, want ErrIdentityNotFound", err)
	}
	// server_id is UNIQUE: a second identity on the same server must fail.
	if err := store.CreateIdentity(ctx, Identity{
		AgentID: "agent_aaaaaaaaaaaaaaaaaaaaaaaa", ServerID: "srv_1", Status: "active",
		CreatedAt: now, CredentialVersion: 1, CredentialExpiresAt: now.Add(CredentialTTL), credentialHash: "x",
	}); err == nil {
		t.Fatal("duplicate server binding must be rejected")
	}

	// Rotation updates current + previous hash and grace expiry.
	id.CredentialVersion = 2
	id.prevCredentialHash = id.credentialHash
	id.prevExpiresAt = &grace
	id.credentialHash = hashToken("ac_second")
	if err := store.UpdateIdentity(ctx, id); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = store.IdentityByAgent(ctx, id.AgentID)
	if err != nil || got.CredentialVersion != 2 || got.prevCredentialHash != hashToken("ac_first") || got.prevExpiresAt == nil {
		t.Fatalf("after update = %+v %v", got, err)
	}

	if err := store.RevokeIdentity(ctx, id.AgentID, now); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got, _ = store.IdentityByAgent(ctx, id.AgentID)
	if got.Status != "revoked" || got.RevokedAt == nil {
		t.Fatalf("after revoke = %+v", got)
	}
	if err := store.RevokeIdentity(ctx, "agent_missing", now); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("revoke missing err = %v", err)
	}
}

func TestPGStoreCascadesOnServerDelete(t *testing.T) {
	db, ctx := openIsolated(t)
	seedServer(t, db, ctx, "srv_1", "pending")
	store := NewPGStore(db)
	now := time.Now().UTC()
	if err := store.CreateIdentity(ctx, Identity{
		AgentID: "agent_0123456789abcdef01234567", ServerID: "srv_1", Status: "active",
		CreatedAt: now, CredentialVersion: 1, CredentialExpiresAt: now.Add(CredentialTTL), credentialHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM servers WHERE id = 'srv_1'`); err != nil {
		t.Fatalf("delete server: %v", err)
	}
	if _, err := store.IdentityByAgent(ctx, "agent_0123456789abcdef01234567"); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("identity must cascade on server delete, err = %v", err)
	}
}
