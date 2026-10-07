package agentauth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type testClock struct{ t time.Time }

func (c *testClock) now() time.Time          { return c.t }
func (c *testClock) advance(d time.Duration) { c.t = c.t.Add(d) }
func (c *testClock) stamp() string           { return c.t.Format(time.RFC3339) }

func newTestService(servers map[string]string) (*Service, *MemoryStore, *testClock) {
	clk := &testClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store := NewMemoryStore()
	svc := NewService(store, WithClock(clk.now), WithServerLookup(StaticServers(servers)))
	return svc, store, clk
}

func register(t *testing.T, svc *Service, serverID string) Registration {
	t.Helper()
	ctx := context.Background()
	token, _, err := svc.Issue(ctx, serverID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	reg, err := svc.Redeem(ctx, token, serverID, "0.1.0", []string{"docker", "traefik"})
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	return reg
}

func TestIssueRequiresPendingServer(t *testing.T) {
	svc, _, clk := newTestService(map[string]string{"srv_ready": "ready", "srv_revoked": "revoked", "srv_pending": "pending"})
	ctx := context.Background()

	if _, _, err := svc.Issue(ctx, "srv_ready"); !errors.Is(err, ErrServerNotPending) {
		t.Fatalf("ready server err = %v, want ErrServerNotPending", err)
	}
	if _, _, err := svc.Issue(ctx, "srv_revoked"); !errors.Is(err, ErrServerNotPending) {
		t.Fatalf("revoked server err = %v, want ErrServerNotPending", err)
	}
	if _, _, err := svc.Issue(ctx, "srv_missing"); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("missing server err = %v, want ErrServerNotFound", err)
	}
	token, expires, err := svc.Issue(ctx, "srv_pending")
	if err != nil || token == "" || !expires.After(clk.now()) {
		t.Fatalf("issue pending = %q %v %v", token, expires, err)
	}
}

func TestRedeemCreatesIdentityAndCredential(t *testing.T) {
	svc, _, clk := newTestService(map[string]string{"srv_1": "pending"})
	reg := register(t, svc, "srv_1")

	if !strings.HasPrefix(reg.AgentID, "agent_") || len(reg.AgentID) != len("agent_")+24 {
		t.Fatalf("agent id = %q", reg.AgentID)
	}
	if reg.ServerID != "srv_1" || reg.CredentialVersion != 1 || reg.Credential == "" {
		t.Fatalf("registration = %+v", reg)
	}
	if reg.Negotiated != ProtocolVersion || reg.HeartbeatIntervalSeconds != HeartbeatIntervalSeconds {
		t.Fatalf("negotiation = %+v", reg)
	}
	if _, err := svc.Verify(context.Background(), reg.AgentID, reg.Credential, clk.now()); err != nil {
		t.Fatalf("verify fresh credential: %v", err)
	}
}

func TestCredentialsNeverStoredInPlaintext(t *testing.T) {
	svc, store, _ := newTestService(map[string]string{"srv_1": "pending"})
	reg := register(t, svc, "srv_1")

	id, err := store.IdentityByAgent(context.Background(), reg.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if id.credentialHash == reg.Credential {
		t.Fatal("credential stored in plaintext")
	}
	if id.credentialHash != hashToken(reg.Credential) {
		t.Fatal("credential hash mismatch")
	}
	if strings.Contains(id.credentialHash, reg.Credential) {
		t.Fatal("plaintext leaked into stored hash")
	}
}

func TestDoubleRedeemRejected(t *testing.T) {
	svc, _, _ := newTestService(map[string]string{"srv_1": "pending"})
	ctx := context.Background()
	token, _, err := svc.Issue(ctx, "srv_1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Redeem(ctx, token, "srv_1", "0.1.0", []string{"docker"}); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	if _, err := svc.Redeem(ctx, token, "srv_1", "0.1.0", []string{"docker"}); !errors.Is(err, ErrTokenUsed) {
		t.Fatalf("second redeem err = %v, want ErrTokenUsed", err)
	}
}

func TestBootstrapTokenExpiry(t *testing.T) {
	svc, _, clk := newTestService(map[string]string{"srv_1": "pending"})
	ctx := context.Background()
	token, _, err := svc.Issue(ctx, "srv_1")
	if err != nil {
		t.Fatal(err)
	}
	clk.advance(BootstrapTTL + time.Minute)
	if _, err := svc.Redeem(ctx, token, "srv_1", "0.1.0", []string{"docker"}); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expired token err = %v, want ErrTokenExpired", err)
	}
}

func TestRedeemRejectsWrongServer(t *testing.T) {
	svc, _, _ := newTestService(map[string]string{"srv_1": "pending", "srv_2": "pending"})
	ctx := context.Background()
	token, _, err := svc.Issue(ctx, "srv_1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Redeem(ctx, token, "srv_2", "0.1.0", []string{"docker"}); !errors.Is(err, ErrServerMismatch) {
		t.Fatalf("wrong server err = %v, want ErrServerMismatch", err)
	}
}

func TestRedeemRejectsUnknownAndInvalidToken(t *testing.T) {
	svc, _, _ := newTestService(map[string]string{"srv_1": "pending"})
	ctx := context.Background()
	if _, err := svc.Redeem(ctx, "bt_nope", "srv_1", "0.1.0", []string{"docker"}); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("unknown token err = %v, want ErrTokenInvalid", err)
	}
	token, _, _ := svc.Issue(ctx, "srv_1")
	if _, err := svc.Redeem(ctx, token, "srv_1", "", nil); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("missing fields err = %v, want ErrTokenInvalid", err)
	}
}

func TestReregistrationIsIdempotentWithNewCredentialVersion(t *testing.T) {
	svc, _, clk := newTestService(map[string]string{"srv_1": "pending"})
	ctx := context.Background()

	first := register(t, svc, "srv_1")
	token, _, err := svc.Issue(ctx, "srv_1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Redeem(ctx, token, "srv_1", "0.2.0", []string{"docker"})
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if second.AgentID != first.AgentID {
		t.Fatalf("agent id changed on re-registration: %q != %q", second.AgentID, first.AgentID)
	}
	if second.CredentialVersion != 2 || second.Credential == first.Credential {
		t.Fatalf("re-registration must issue a new credential version: %+v", second)
	}
	if _, err := svc.Verify(ctx, first.AgentID, second.Credential, clk.now()); err != nil {
		t.Fatalf("new credential invalid: %v", err)
	}
}

func TestRotateGraceAndExpiry(t *testing.T) {
	svc, _, clk := newTestService(map[string]string{"srv_1": "pending"})
	ctx := context.Background()
	first := register(t, svc, "srv_1")

	second, err := svc.Rotate(ctx, first.AgentID, first.Credential, "nonce-rotate-1", clk.stamp())
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if second.CredentialVersion != 2 || second.Credential == first.Credential {
		t.Fatalf("rotate result = %+v", second)
	}

	// Old credential still valid inside the grace window.
	if _, err := svc.Verify(ctx, first.AgentID, first.Credential, clk.now().Add(time.Minute)); err != nil {
		t.Fatalf("old credential inside grace rejected: %v", err)
	}
	// Old credential dead after the grace window.
	if _, err := svc.Verify(ctx, first.AgentID, first.Credential, clk.now().Add(RotationGrace+time.Second)); !errors.Is(err, ErrCredentialExpired) {
		t.Fatalf("old credential after grace err = %v, want ErrCredentialExpired", err)
	}
	// New credential is valid.
	if _, err := svc.Verify(ctx, first.AgentID, second.Credential, clk.now().Add(RotationGrace+time.Minute)); err != nil {
		t.Fatalf("new credential rejected: %v", err)
	}
}

func TestRotateRejectsReplayedNonce(t *testing.T) {
	svc, _, clk := newTestService(map[string]string{"srv_1": "pending"})
	ctx := context.Background()
	first := register(t, svc, "srv_1")

	if _, err := svc.Rotate(ctx, first.AgentID, first.Credential, "nonce-dup", clk.stamp()); err != nil {
		t.Fatalf("first rotate: %v", err)
	}
	// Same nonce, same (valid) timestamp: replay must be rejected before any
	// credential check.
	if _, err := svc.Rotate(ctx, first.AgentID, first.Credential, "nonce-dup", clk.stamp()); !errors.Is(err, ErrReplay) {
		t.Fatalf("replayed nonce err = %v, want ErrReplay", err)
	}
}

func TestRotateRejectsClockSkew(t *testing.T) {
	svc, _, clk := newTestService(map[string]string{"srv_1": "pending"})
	ctx := context.Background()
	first := register(t, svc, "srv_1")

	future := clk.now().Add(MaxClockSkew + time.Minute).Format(time.RFC3339)
	if _, err := svc.Rotate(ctx, first.AgentID, first.Credential, "nonce-skew", future); !errors.Is(err, ErrClockSkew) {
		t.Fatalf("future timestamp err = %v, want ErrClockSkew", err)
	}
	if _, err := svc.Rotate(ctx, first.AgentID, first.Credential, "nonce-skew", ""); !errors.Is(err, ErrClockSkew) {
		t.Fatalf("malformed timestamp err = %v, want ErrClockSkew", err)
	}
	if _, err := svc.Rotate(ctx, first.AgentID, first.Credential, "", clk.stamp()); !errors.Is(err, ErrReplay) {
		t.Fatalf("missing nonce err = %v, want ErrReplay", err)
	}
}

func TestRotateRejectsInvalidAndExpiredCredential(t *testing.T) {
	svc, _, clk := newTestService(map[string]string{"srv_1": "pending"})
	ctx := context.Background()
	first := register(t, svc, "srv_1")

	if _, err := svc.Rotate(ctx, first.AgentID, "ac_wrong", "nonce-x", clk.stamp()); !errors.Is(err, ErrCredentialInvalid) {
		t.Fatalf("wrong credential err = %v, want ErrCredentialInvalid", err)
	}
	clk.advance(CredentialTTL + time.Minute)
	if _, err := svc.Rotate(ctx, first.AgentID, first.Credential, "nonce-y", clk.stamp()); !errors.Is(err, ErrCredentialExpired) {
		t.Fatalf("expired credential err = %v, want ErrCredentialExpired", err)
	}
}

func TestRevokedServerKillsCredentials(t *testing.T) {
	servers := map[string]string{"srv_1": "pending"}
	svc, _, clk := newTestService(servers)
	ctx := context.Background()
	reg := register(t, svc, "srv_1")

	servers["srv_1"] = "revoked"
	if _, err := svc.Verify(ctx, reg.AgentID, reg.Credential, clk.now()); !errors.Is(err, ErrRevoked) {
		t.Fatalf("verify on revoked server err = %v, want ErrRevoked", err)
	}
	if _, err := svc.Rotate(ctx, reg.AgentID, reg.Credential, "nonce-r", clk.stamp()); !errors.Is(err, ErrRevoked) {
		t.Fatalf("rotate on revoked server err = %v, want ErrRevoked", err)
	}
}

func TestRevokedIdentityRejected(t *testing.T) {
	svc, _, _ := newTestService(map[string]string{"srv_1": "pending"})
	ctx := context.Background()
	reg := register(t, svc, "srv_1")

	if err := svc.Revoke(ctx, reg.AgentID, time.Now()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.Verify(ctx, reg.AgentID, reg.Credential, time.Now()); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked identity err = %v, want ErrRevoked", err)
	}
}

func TestExpiredCredentialRejected(t *testing.T) {
	svc, _, clk := newTestService(map[string]string{"srv_1": "pending"})
	ctx := context.Background()
	reg := register(t, svc, "srv_1")

	clk.advance(CredentialTTL + time.Minute)
	if _, err := svc.Verify(ctx, reg.AgentID, reg.Credential, clk.now()); !errors.Is(err, ErrCredentialExpired) {
		t.Fatalf("expired credential err = %v, want ErrCredentialExpired", err)
	}
}

func TestStatusDistinguishesRegisteredRevokedUnknown(t *testing.T) {
	svc, _, _ := newTestService(map[string]string{"srv_1": "pending"})
	ctx := context.Background()
	reg := register(t, svc, "srv_1")

	res, err := svc.Status(ctx, reg.AgentID, "")
	if err != nil || res.Status != StatusRegistered {
		t.Fatalf("registered status = %+v %v", res, err)
	}
	res, err = svc.Status(ctx, "", reg.ServerID)
	if err != nil || res.Status != StatusRegistered || res.AgentID != reg.AgentID {
		t.Fatalf("by-server status = %+v %v", res, err)
	}
	res, err = svc.Status(ctx, "agent_missing", "")
	if err != nil || res.Status != StatusUnknown {
		t.Fatalf("unknown status = %+v %v", res, err)
	}
	if err := svc.Revoke(ctx, reg.AgentID, time.Now()); err != nil {
		t.Fatal(err)
	}
	res, err = svc.Status(ctx, reg.AgentID, "")
	if err != nil || res.Status != StatusRevoked {
		t.Fatalf("revoked status = %+v %v", res, err)
	}
}

func TestAuthenticateEnforcesFreshnessThenCredential(t *testing.T) {
	svc, _, clk := newTestService(map[string]string{"srv_1": "pending"})
	ctx := context.Background()
	reg := register(t, svc, "srv_1")

	if _, err := svc.Authenticate(ctx, reg.AgentID, reg.Credential, "nonce-a", clk.stamp()); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if _, err := svc.Authenticate(ctx, reg.AgentID, reg.Credential, "nonce-a", clk.stamp()); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay err = %v, want ErrReplay", err)
	}
	if _, err := svc.Authenticate(ctx, reg.AgentID, "ac_wrong", "nonce-b", clk.stamp()); !errors.Is(err, ErrCredentialInvalid) {
		t.Fatalf("bad credential err = %v, want ErrCredentialInvalid", err)
	}
}
