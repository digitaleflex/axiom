package tests

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/heartbeat"
	"github.com/digitaleflex/axiom/services/agent/internal/identity"
	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/security/auth"
)

// fixedBackoff is a deterministic, tiny backoff for heartbeat retries.
type fixedBackoff struct{ d time.Duration }

func (b fixedBackoff) Next() time.Duration { return b.d }
func (b fixedBackoff) Reset()              {}

// waitFor polls cond with a tiny interval up to a short deadline.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func newHeartbeatLoop(engine *fakeEngine, credStore *auth.Store, id protocol.AgentIdentity, interval time.Duration) *heartbeat.Loop {
	client := auth.NewClient(engine.URL(), credStore)
	tr := heartbeat.NewHTTPTransport(engine.URL(), client)
	loop := heartbeat.NewLoop(id, interval, tr, discardLogger())
	loop.Backoff = fixedBackoff{d: time.Millisecond}
	return loop
}

// TestScenario_Registration covers registration → credential store → signed
// heartbeat with an Engine 200. The real identity, auth, protocol and
// heartbeat packages run end to end; only the Engine is faked.
func TestScenario_Registration(t *testing.T) {
	dir := t.TempDir()
	idStore := identity.NewStore(filepath.Join(dir, "identity.json"))
	credStore := auth.NewStore(filepath.Join(dir, "credential.json"))
	engine := newFakeEngine(t, testServer)

	id, cred := registerAgent(t, engine, idStore, credStore, []string{"docker", "traefik"})

	// 1. Registration request was signed and well-formed.
	regReq, ok := engine.requestFor("/api/v1/agent/register")
	if !ok {
		t.Fatal("engine never received a registration request")
	}
	if got := regReq.header.Get("Authorization"); got != "Bearer ac_bootstrap_test" {
		t.Fatalf("register Authorization = %q", got)
	}
	for _, h := range []string{"X-Agent-ID", "X-Timestamp", "X-Nonce"} {
		if regReq.header.Get(h) == "" {
			t.Fatalf("register request missing signed header %s", h)
		}
	}

	// 2. Issued identity and credential are persisted.
	if id.AgentID != testAgentID || id.ServerID != testServer {
		t.Fatalf("issued identity = %+v", id)
	}
	cur, ok := credStore.Current()
	if !ok || cur.Token != cred.Token || cur.AgentID != testAgentID {
		t.Fatalf("credential store = %+v (ok=%v)", cur, ok)
	}
	persisted, err := idStore.Load()
	if err != nil || !persisted.Registered || persisted.AgentID != testAgentID {
		t.Fatalf("persisted identity = %+v, err=%v", persisted, err)
	}

	// 3. A signed heartbeat is accepted (Engine 200).
	loop := newHeartbeatLoop(engine, credStore, id, 2*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()
	waitFor(t, "first heartbeat", func() bool { return engine.count("/api/v1/agent/heartbeat") >= 1 })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("heartbeat Run() = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat loop did not stop")
	}

	hbReq, ok := engine.requestFor("/api/v1/agent/heartbeat")
	if !ok {
		t.Fatal("engine never received a heartbeat")
	}
	if got := hbReq.header.Get("Authorization"); got != "Bearer "+cred.Token {
		t.Fatalf("heartbeat Authorization = %q, want issued credential", got)
	}
	if got := hbReq.header.Get("X-Agent-ID"); got != testAgentID {
		t.Fatalf("heartbeat X-Agent-ID = %q", got)
	}
	for _, h := range []string{"X-Timestamp", "X-Nonce"} {
		if hbReq.header.Get(h) == "" {
			t.Fatalf("heartbeat missing signed header %s", h)
		}
	}
}

// TestScenario_AuthenticationFailure covers the Engine returning 401: the
// heartbeat loop stops with ErrRevoked and never retries.
func TestScenario_AuthenticationFailure(t *testing.T) {
	dir := t.TempDir()
	idStore := identity.NewStore(filepath.Join(dir, "identity.json"))
	credStore := auth.NewStore(filepath.Join(dir, "credential.json"))
	engine := newFakeEngine(t, testServer)
	engine.failAfter = 0 // reject the very first heartbeat

	id, _ := registerAgent(t, engine, idStore, credStore, []string{"docker"})

	loop := newHeartbeatLoop(engine, credStore, id, 2*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := loop.Run(ctx)
	if !errors.Is(err, heartbeat.ErrRevoked) {
		t.Fatalf("Run() = %v, want heartbeat.ErrRevoked", err)
	}
	if n := engine.count("/api/v1/agent/heartbeat"); n != 1 {
		t.Fatalf("heartbeat attempts = %d, want 1 (no retry after 401)", n)
	}
}

// TestScenario_HeartbeatTimeout covers the agent-side semantics of heartbeat
// timeout: staleness is derived by the Engine (server.EffectiveStatusAt), and
// the agent keeps sending on schedule until the credential is rejected. The
// agent must not stop or change cadence on its own.
func TestScenario_HeartbeatTimeout(t *testing.T) {
	dir := t.TempDir()
	idStore := identity.NewStore(filepath.Join(dir, "identity.json"))
	credStore := auth.NewStore(filepath.Join(dir, "credential.json"))
	engine := newFakeEngine(t, testServer)
	engine.failAfter = 2 // two accepted heartbeats, then 401

	id, _ := registerAgent(t, engine, idStore, credStore, []string{"docker"})

	loop := newHeartbeatLoop(engine, credStore, id, 2*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := loop.Run(ctx)
	if !errors.Is(err, heartbeat.ErrRevoked) {
		t.Fatalf("Run() = %v, want heartbeat.ErrRevoked", err)
	}
	// The agent kept sending (3 attempts) despite the Engine's staleness clock.
	if n := engine.count("/api/v1/agent/heartbeat"); n != 3 {
		t.Fatalf("heartbeat attempts = %d, want 3 (keep sending until 401)", n)
	}
}
