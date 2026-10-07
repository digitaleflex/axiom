package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/capabilities"
	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/security/auth"
)

// fakeTransport records every heartbeat and replays a scripted error
// sequence before succeeding.
type fakeTransport struct {
	mu       sync.Mutex
	sent     []protocol.Heartbeat
	errs     []error
	revoked  bool
	resp     HeartbeatResponse
	failures int
}

func (f *fakeTransport) Send(_ context.Context, hb protocol.Heartbeat) (HeartbeatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, hb)
	if f.revoked {
		return HeartbeatResponse{}, fmt.Errorf("%w: engine returned 401", ErrRevoked)
	}
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			f.failures++
			return HeartbeatResponse{}, err
		}
	}
	return f.resp, nil
}

func (f *fakeTransport) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// fixedBackoff is a deterministic tiny backoff for tests.
type fixedBackoff struct{ delay time.Duration }

func (b fixedBackoff) Next() time.Duration { return b.delay }
func (b fixedBackoff) Reset()              {}

var testIdentity = protocol.AgentIdentity{
	AgentID:  "agent_0123456789abcdef01234567",
	ServerID: "srv_0123456789abcdef012345",
}

func testResources() func(ctx context.Context) (capabilities.Report, error) {
	return func(context.Context) (capabilities.Report, error) {
		return capabilities.Report{
			CollectedAt: time.Now().UTC(),
			Docker:      capabilities.DockerInfo{Available: true, Version: "27.0.1"},
			Traefik:     capabilities.TraefikInfo{Available: true, Version: "v3.1.0"},
			CPUCount:    4,
			MemoryMB:    8192,
			DiskFreeMB:  50000,
			Arch:        "amd64",
			OS:          "linux",
		}, nil
	}
}

func runLoop(t *testing.T, loop *Loop) (chan error, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()
	return done, cancel
}

func awaitStop(t *testing.T, done chan error, cancel context.CancelFunc, want error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if want == nil {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Run() = %v, want context.Canceled", err)
			}
			return
		}
		if !errors.Is(err, want) {
			t.Fatalf("Run() = %v, want %v", err, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("loop did not stop")
	}
}

func TestLoopSendsWellFormedHeartbeats(t *testing.T) {
	transport := &fakeTransport{resp: HeartbeatResponse{OK: true, ServerStatus: "READY"}}
	loop := NewLoop(testIdentity, 5*time.Millisecond, transport, slog.Default())
	loop.Resources = testResources()
	var idMu sync.Mutex
	var idSeq int
	loop.NewID = func() string {
		idMu.Lock()
		defer idMu.Unlock()
		idSeq++
		return fmt.Sprintf("hb_%06d", idSeq)
	}

	done, cancel := runLoop(t, loop)
	time.Sleep(35 * time.Millisecond)
	awaitStop(t, done, cancel, nil)

	if got := transport.calls(); got < 3 {
		t.Fatalf("sent %d heartbeats, want >= 3", got)
	}
	seen := map[string]bool{}
	for _, hb := range transport.sent {
		now := time.Now()
		if err := hb.Validate(now); err != nil {
			t.Fatalf("heartbeat does not validate: %v", err)
		}
		if hb.Protocol != protocol.Version {
			t.Errorf("protocol = %d, want %d", hb.Protocol, protocol.Version)
		}
		if hb.AgentID != testIdentity.AgentID || hb.ServerID != testIdentity.ServerID {
			t.Errorf("identity = %+v, want %+v", hb.AgentIdentity, testIdentity)
		}
		if hb.Status != "READY" {
			t.Errorf("status = %q, want READY", hb.Status)
		}
		if seen[hb.MessageID] {
			t.Errorf("duplicate message ID %q", hb.MessageID)
		}
		seen[hb.MessageID] = true
		if now.Sub(hb.SentAt) > time.Minute || hb.SentAt.After(now) {
			t.Errorf("sentAt %v is not fresh (now %v)", hb.SentAt, now)
		}
		if len(hb.Capabilities) == 0 {
			t.Error("capabilities are empty, want the discovered subset")
		}
		if hb.CPUCount != 4 || hb.MemoryMB != 8192 || hb.DiskFreeMB != 50000 {
			t.Errorf("resources = %d/%d/%d, want 4/8192/50000", hb.CPUCount, hb.MemoryMB, hb.DiskFreeMB)
		}
	}
}

func TestLoopStopsOnRevoked(t *testing.T) {
	transport := &fakeTransport{revoked: true}
	loop := NewLoop(testIdentity, time.Minute, transport, slog.Default())
	loop.Resources = testResources()

	done, cancel := runLoop(t, loop)
	defer cancel()
	awaitStop(t, done, cancel, ErrRevoked)

	if got := transport.calls(); got != 1 {
		t.Fatalf("sent %d heartbeats after 401, want exactly 1 (no retry loop)", got)
	}
}

func TestLoopRetriesAfterTransportFailure(t *testing.T) {
	transport := &fakeTransport{
		errs: []error{errors.New("connection refused"), errors.New("timeout")},
		resp: HeartbeatResponse{OK: true, ServerStatus: "READY"},
	}
	loop := NewLoop(testIdentity, time.Hour, transport, slog.Default())
	loop.Resources = testResources()
	loop.Backoff = fixedBackoff{delay: time.Millisecond}

	done, cancel := runLoop(t, loop)
	// Two failures with 1ms backoff, then success; the loop keeps running.
	deadline := time.Now().Add(2 * time.Second)
	for transport.calls() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	awaitStop(t, done, cancel, nil)

	if got := transport.calls(); got != 3 {
		t.Fatalf("sent %d heartbeats, want 3 (2 failures + 1 success)", got)
	}
	if transport.failures != 2 {
		t.Fatalf("transport failures = %d, want 2", transport.failures)
	}
}

func TestLoopReportsDegradedWhenProbeFails(t *testing.T) {
	transport := &fakeTransport{resp: HeartbeatResponse{OK: true, ServerStatus: "DEGRADED"}}
	loop := NewLoop(testIdentity, 5*time.Millisecond, transport, slog.Default())
	loop.Resources = func(context.Context) (capabilities.Report, error) {
		return capabilities.Report{}, errors.New("probe failed")
	}

	done, cancel := runLoop(t, loop)
	time.Sleep(15 * time.Millisecond)
	awaitStop(t, done, cancel, nil)

	if transport.calls() == 0 {
		t.Fatal("no heartbeats sent")
	}
	for _, hb := range transport.sent {
		if hb.Status != "DEGRADED" {
			t.Errorf("status = %q, want DEGRADED after probe failure", hb.Status)
		}
	}
}

func TestLoopDefaultInterval(t *testing.T) {
	loop := NewLoop(testIdentity, 0, &fakeTransport{}, nil)
	if loop.Interval != 0 {
		t.Fatalf("Interval = %v, want 0 before Run", loop.Interval)
	}
	// Run applies the default; verify via a cancelled context so it exits.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loop.Run(ctx) }()
	time.Sleep(5 * time.Millisecond)
	cancel()
	<-done
	if loop.Interval != DefaultInterval {
		t.Fatalf("Interval = %v, want default %v", loop.Interval, DefaultInterval)
	}
	if DefaultInterval != 30*time.Second {
		t.Fatalf("DefaultInterval = %v, want 30s", DefaultInterval)
	}
}

func TestLoopRequiresTransport(t *testing.T) {
	loop := NewLoop(testIdentity, time.Minute, nil, nil)
	if err := loop.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "transport") {
		t.Fatalf("Run() = %v, want transport-required error", err)
	}
}

func TestHTTPTransportMapsEngineResponses(t *testing.T) {
	store := auth.NewStore(filepath.Join(t.TempDir(), "credential.json"))
	if err := store.Save(auth.Credential{
		Token: "ac_test", Version: 1, AgentID: testIdentity.AgentID, ServerID: testIdentity.ServerID,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	t.Run("401 is ErrRevoked", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()
		transport := NewHTTPTransport(srv.URL, auth.NewClient(srv.URL, store))
		_, err := transport.Send(context.Background(), protocol.Heartbeat{})
		if !errors.Is(err, ErrRevoked) {
			t.Fatalf("Send() = %v, want ErrRevoked", err)
		}
	})

	t.Run("200 parses the response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer ac_test" {
				t.Errorf("Authorization header = %q", r.Header.Get("Authorization"))
			}
			if r.Header.Get("X-Agent-ID") != testIdentity.AgentID {
				t.Errorf("X-Agent-ID header = %q", r.Header.Get("X-Agent-ID"))
			}
			if r.Header.Get("X-Nonce") == "" || r.Header.Get("X-Timestamp") == "" {
				t.Error("signed headers X-Nonce/X-Timestamp are required")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true,"serverStatus":"READY"}`)
		}))
		defer srv.Close()
		transport := NewHTTPTransport(srv.URL, auth.NewClient(srv.URL, store))
		resp, err := transport.Send(context.Background(), protocol.Heartbeat{})
		if err != nil {
			t.Fatalf("Send() = %v", err)
		}
		if !resp.OK || resp.ServerStatus != "READY" {
			t.Fatalf("response = %+v, want ok/READY", resp)
		}
	})

	t.Run("500 is retryable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		transport := NewHTTPTransport(srv.URL, auth.NewClient(srv.URL, store))
		_, err := transport.Send(context.Background(), protocol.Heartbeat{})
		if err == nil || errors.Is(err, ErrRevoked) {
			t.Fatalf("Send() = %v, want retryable non-ErrRevoked error", err)
		}
	})
}
