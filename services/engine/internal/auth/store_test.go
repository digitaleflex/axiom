package auth

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

	schema := "auth_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
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

func TestMigration011CreatesSessionSchema(t *testing.T) {
	db, ctx := openIsolated(t)
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'sessions')`).Scan(&exists); err != nil || !exists {
		t.Fatalf("sessions table missing (err=%v)", err)
	}
	for _, column := range []string{"password_hash", "password_updated_at"} {
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'users' AND column_name = $1)`, column).Scan(&exists); err != nil || !exists {
			t.Fatalf("users.%s missing (err=%v)", column, err)
		}
	}
}

func TestPGStoreUserLifecycle(t *testing.T) {
	db, ctx := openIsolated(t)
	store := NewPGStore(db)
	now := time.Now().UTC()
	user := User{ID: "usr_pg_1", Email: "jane@example.com", DisplayName: "Jane", CreatedAt: now}
	if err := store.CreateUser(ctx, user, hashToken("hash")); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, hash, err := store.UserByEmail(ctx, "JANE@example.com")
	if err != nil || got.ID != user.ID || got.DisplayName != "Jane" || hash != hashToken("hash") {
		t.Fatalf("by email = %+v %q %v", got, hash, err)
	}
	if _, err := store.UserByID(ctx, user.ID); err != nil {
		t.Fatalf("by id: %v", err)
	}
	if _, _, err := store.UserByEmail(ctx, "nobody@example.com"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("missing user err = %v, want ErrUserNotFound", err)
	}
	if err := store.CreateUser(ctx, User{ID: "usr_pg_2", Email: "jane@example.com"}, "x"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate email err = %v, want ErrEmailTaken", err)
	}
}

func TestPGStoreSessionLifecycleAndHashAtRest(t *testing.T) {
	db, ctx := openIsolated(t)
	store := NewPGStore(db)
	now := time.Now().UTC()
	if err := store.CreateUser(ctx, User{ID: "usr_pg_1", Email: "jane@example.com", CreatedAt: now}, "hash"); err != nil {
		t.Fatal(err)
	}

	const token = "opaque-session-token-value"
	first := Session{
		ID: "ses_1", UserID: "usr_pg_1", UserAgent: "browser-a", IP: "203.0.113.7",
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(DefaultSessionTTL), CSRFToken: "csrf-1",
	}
	if err := store.CreateSession(ctx, first, hashToken(token)); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// The plaintext token is never persisted: only its SHA-256 hash is stored.
	var storedHash string
	if err := db.QueryRowContext(ctx, `SELECT token_hash FROM sessions WHERE session_id = 'ses_1'`).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == token || storedHash != hashToken(token) {
		t.Fatalf("token_hash = %q, want sha256 hash and not the plaintext", storedHash)
	}

	got, err := store.SessionByTokenHash(ctx, hashToken(token))
	if err != nil || got.ID != first.ID || got.UserAgent != "browser-a" {
		t.Fatalf("by token = %+v %v", got, err)
	}
	if _, err := store.SessionByTokenHash(ctx, "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing token err = %v, want ErrSessionNotFound", err)
	}

	later := now.Add(2 * time.Minute)
	if err := store.TouchSession(ctx, first.ID, later); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got, _ = store.SessionByTokenHash(ctx, hashToken(token))
	if !got.LastSeenAt.Truncate(time.Microsecond).Equal(later.Truncate(time.Microsecond)) {
		t.Fatalf("last_seen = %v, want %v", got.LastSeenAt, later)
	}

	// A second session for the same user, then revoke-one and revoke-others.
	second := first
	second.ID = "ses_2"
	second.CSRFToken = "csrf-2"
	if err := store.CreateSession(ctx, second, hashToken("token-2")); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeSession(ctx, "usr_pg_1", "ses_2", later); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := store.RevokeSession(ctx, "usr_pg_1", "ses_2", later); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("double revoke err = %v, want ErrSessionNotFound", err)
	}
	if err := store.RevokeSession(ctx, "usr_other", "ses_1", later); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("cross-user revoke err = %v, want ErrSessionNotFound", err)
	}

	list, err := store.ListSessions(ctx, "usr_pg_1")
	if err != nil || len(list) != 1 || list[0].ID != "ses_1" {
		t.Fatalf("list = %+v %v", list, err)
	}

	third := first
	third.ID = "ses_3"
	third.CSRFToken = "csrf-3"
	if err := store.CreateSession(ctx, third, hashToken("token-3")); err != nil {
		t.Fatal(err)
	}
	n, err := store.RevokeOtherSessions(ctx, "usr_pg_1", "ses_1", later)
	if err != nil || n != 1 {
		t.Fatalf("revoke others = %d %v, want 1", n, err)
	}

	if err := store.RevokeSessionByToken(ctx, hashToken(token), later); err != nil {
		t.Fatalf("revoke by token: %v", err)
	}
	if err := store.RevokeSessionByToken(ctx, hashToken(token), later); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("double revoke by token err = %v, want ErrSessionNotFound", err)
	}
}

func TestPGStoreSessionsCascadeOnUserDelete(t *testing.T) {
	db, ctx := openIsolated(t)
	store := NewPGStore(db)
	now := time.Now().UTC()
	if err := store.CreateUser(ctx, User{ID: "usr_pg_1", Email: "jane@example.com", CreatedAt: now}, "hash"); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, Session{
		ID: "ses_1", UserID: "usr_pg_1", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), CSRFToken: "c",
	}, "h"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = 'usr_pg_1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SessionByTokenHash(ctx, "h"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("session must cascade on user delete, err = %v", err)
	}
}
