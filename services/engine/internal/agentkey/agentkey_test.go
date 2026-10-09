package agentkey

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/digitaleflex/axiom/services/engine/internal/security/secrets"
	"github.com/digitaleflex/axiom/services/engine/migrations"
)

// Golden vector of the ADR-0008 operation signature, shared byte-for-byte
// with the agent side (services/agent/internal/security/operationkey) and
// with agentclient. If any of these constants change on either side, the two
// implementations stop interoperating — that is exactly what this pins.
const (
	goldenKey       = "axiom-test-key" // raw ASCII bytes, not a production KeySize key
	goldenMethod    = "POST"
	goldenPath      = "/api/v1/agent/operations"
	goldenAgentID   = "agt_0123456789abcdef01234567"
	goldenVersion   = 2
	goldenTimestamp = "2026-10-09T12:00:00Z"
	goldenNonce     = "00112233445566778899aabbccddeeff"
	goldenBody      = "{}"
	goldenBodySHA   = "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"
	goldenSignature = "f6adba7b8e7e84811dc101769c40914d5b2ab7a9411ebed636b45925b909b6db"
)

// TestSignatureGoldenVector is the wire-contract pin: the exact canonical
// form and the exact HMAC-SHA256 it produces for fixed inputs.
func TestSignatureGoldenVector(t *testing.T) {
	body := []byte(goldenBody)

	if got := BodyDigest(body); got != goldenBodySHA {
		t.Fatalf("body digest = %s, want %s", got, goldenBodySHA)
	}

	canonical := Canonical(goldenMethod, goldenPath, goldenAgentID, goldenVersion, goldenTimestamp, goldenNonce, body)
	if strings.HasSuffix(canonical, "\n") {
		t.Fatal("the canonical form has no trailing newline")
	}
	wantCanonical := strings.Join([]string{
		CanonicalDomain,
		goldenMethod,
		goldenPath,
		goldenAgentID,
		"2",
		goldenTimestamp,
		goldenNonce,
		goldenBodySHA,
	}, "\n")
	if canonical != wantCanonical {
		t.Fatalf("canonical form = %q, want %q", canonical, wantCanonical)
	}

	sig := Key([]byte(goldenKey)).Sign(canonical)
	if sig != goldenSignature {
		t.Fatalf("signature = %s, want %s", sig, goldenSignature)
	}
	if strings.ToLower(sig) != sig {
		t.Fatal("the signature is lowercase hex")
	}
	if SignaturePrefix+sig != "v1="+goldenSignature {
		t.Fatalf("header value = %q", SignaturePrefix+sig)
	}
}

// TestCanonicalIsDomainSeparated proves the domain separator is the first
// line: an operation signature can never collide with another HMAC scheme.
func TestCanonicalIsDomainSeparated(t *testing.T) {
	canonical := Canonical("POST", "/x", "agt_1", 2, "t", "n", nil)
	if !strings.HasPrefix(canonical, "AXIOM-HMAC-V1\n") {
		t.Fatalf("canonical = %q", canonical)
	}
	other := Canonical("POST", "/x", "agt_1", 2, "t", "n", []byte("different"))
	if Key([]byte("k")).Sign(canonical) == Key([]byte("k")).Sign(other) {
		t.Fatal("signatures must bind the body")
	}
}

// newTestService connects to the test database inside an isolated schema and
// runs the full migration set, so each test exercises a fresh secrets table.
func newTestService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	dsn := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	schema := "agentkey_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
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
	return &Service{Store: &secrets.EncryptedStore{DB: db, Box: box}}, ctx
}

// TestIssueAndSigningKeyRoundTrip proves the key lifecycle: one fresh 32-byte
// key per issue, readable only through SigningKey, rotated on every issue and
// scoped per agent.
func TestIssueAndSigningKeyRoundTrip(t *testing.T) {
	svc, ctx := newTestService(t)

	first, err := svc.Issue(ctx, "agent_aaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if len(first) != KeySize {
		t.Fatalf("key length = %d, want %d", len(first), KeySize)
	}

	got, err := svc.SigningKey(ctx, "agent_aaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("SigningKey: %v", err)
	}
	if !bytes.Equal(got, first) {
		t.Fatal("SigningKey must return the issued key")
	}

	// Rotation: every Issue reissues, exactly like agentauth reissues a
	// credential on rotate.
	second, err := svc.Issue(ctx, "agent_aaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("Issue (rotate): %v", err)
	}
	if bytes.Equal(second, first) {
		t.Fatal("rotation must issue a fresh key")
	}
	got, err = svc.SigningKey(ctx, "agent_aaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || !bytes.Equal(got, second) {
		t.Fatalf("SigningKey after rotation = %v, %v", got, err)
	}

	// Scopes are per agent: one agent's key is not another's.
	other, err := svc.Issue(ctx, "agent_bbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatalf("Issue (other agent): %v", err)
	}
	if bytes.Equal(other, second) {
		t.Fatal("two agents must not share a key")
	}
	if got, err := svc.SigningKey(ctx, "agent_bbbbbbbbbbbbbbbbbbbbbbbb"); err != nil || !bytes.Equal(got, other) {
		t.Fatalf("SigningKey (other agent) = %v, %v", got, err)
	}
}

// TestSigningKeyUnknownAgentFailsClosed proves an agent without an issued key
// yields ErrNoKey: the caller must refuse to dispatch, never sign with
// substitute material.
func TestSigningKeyUnknownAgentFailsClosed(t *testing.T) {
	svc, ctx := newTestService(t)

	if _, err := svc.SigningKey(ctx, "agent_ffffffffffffffffffffffff"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("SigningKey = %v, want ErrNoKey", err)
	}
	if _, err := svc.Issue(ctx, ""); err == nil {
		t.Fatal("Issue must require an agent id")
	}
	if _, err := svc.SigningKey(ctx, ""); err == nil {
		t.Fatal("SigningKey must require an agent id")
	}
}

// TestIssueNeverStoresPlaintext proves the sealed column holds ciphertext,
// not the key: the plaintext never reaches the database (AES-256-GCM via
// security/secrets.Box).
func TestIssueNeverStoresPlaintext(t *testing.T) {
	svc, ctx := newTestService(t)

	key, err := svc.Issue(ctx, "agent_cccccccccccccccccccccccc")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	var sealed []byte
	if err := svc.Store.DB.QueryRowContext(ctx,
		`SELECT value_sealed FROM secrets WHERE scope = $1 AND name = $2`,
		scopeFor("agent_cccccccccccccccccccccccc"), KeyName).Scan(&sealed); err != nil {
		t.Fatalf("read sealed column: %v", err)
	}
	if bytes.Contains(sealed, key) || bytes.Contains(sealed, []byte(hex.EncodeToString(key))) {
		t.Fatal("the sealed column must not contain the plaintext key")
	}
}
