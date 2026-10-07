package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/agent/internal/dispatcher"
	"github.com/digitaleflex/axiom/services/agent/internal/protocol"
	"github.com/digitaleflex/axiom/services/agent/internal/runtime/docker"
	"github.com/digitaleflex/axiom/services/agent/internal/runtime/traefik"
)

// TestScenario_OperationSuccess drives all six operation types through the real
// dispatcher into the real Docker/Traefik/health adapters, with only the
// OS/process boundary faked.
func TestScenario_OperationSuccess(t *testing.T) {
	h := newHarness(t)
	healthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthSrv.Close()
	h.br.HealthURL = healthSrv.URL

	d := h.dispatcher()
	image := "sha256:" + strings.Repeat("a", 64)

	cases := []struct {
		name    string
		typ     string
		payload protocol.Payload
	}{
		{"CREATE_RUNTIME", protocol.OpCreateRuntime, protocol.Payload{ImageRef: image, Container: testContainer, Port: 3000}},
		{"NETWORK", protocol.OpConfigureNetwork, protocol.Payload{Container: testContainer, Proxy: "traefik", Domain: "app.example.com", TLS: true, Port: 443}},
		{"START", protocol.OpStartRuntime, protocol.Payload{Container: testContainer}},
		{"VERIFY", protocol.OpVerifyHealth, protocol.Payload{Domain: "app.example.com", Path: "/health", TimeoutSeconds: 30}},
		{"STOP", protocol.OpStopRuntime, protocol.Payload{Container: testContainer}},
		{"REMOVE", protocol.OpRemoveRuntime, protocol.Payload{Container: testContainer}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := newOperation(opID(tc.name, i+1), tc.typ, tc.payload, testServer)
			ack, result := d.Dispatch(context.Background(), op, testIdentity)
			if !ack.Accepted {
				t.Fatalf("ack rejected: %s", ack.Reason)
			}
			if !result.Success {
				t.Fatalf("result failed: code=%s message=%s", result.ErrorCode, result.Message)
			}
			if tc.typ == protocol.OpVerifyHealth {
				if result.Health == nil || result.Health.StatusCode != http.StatusOK {
					t.Fatalf("VERIFY result health = %+v, want 200", result.Health)
				}
			}
		})
	}

	// The Traefik adapter wrote exactly the Axiom-owned dynamic file.
	file := filepath.Join(h.dir, "dynamic", "axiom-"+testDeployment+".yml")
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("traefik dynamic config not written: %v", err)
	}
}

// TestScenario_DuplicateOperation dispatches the same operation ID twice: the
// second returns the cached result and the adapter is called exactly once.
func TestScenario_DuplicateOperation(t *testing.T) {
	h := newHarness(t)
	d := h.dispatcher()
	image := "sha256:" + strings.Repeat("a", 64)
	op := newOperation(opID("CREATE_RUNTIME", 1), protocol.OpCreateRuntime,
		protocol.Payload{ImageRef: image, Container: testContainer, Port: 3000}, testServer)

	ack1, res1 := d.Dispatch(context.Background(), op, testIdentity)
	ack2, res2 := d.Dispatch(context.Background(), op, testIdentity)

	if !ack1.Accepted || !res1.Success {
		t.Fatalf("first dispatch failed: ack=%v result=%+v", ack1.Accepted, res1)
	}
	if !ack2.Accepted {
		t.Fatalf("replay ack rejected: %s", ack2.Reason)
	}
	if !res2.Success || res2.ErrorCode != "" {
		t.Fatalf("cached result = %+v, want success", res2)
	}
	if res1.OperationID != res2.OperationID || !res1.FinishedAt.Equal(res2.FinishedAt) {
		t.Fatalf("cached result differs from original: %+v vs %+v", res1, res2)
	}
	if got := len(h.sim.callsWith("create")); got != 1 {
		t.Fatalf("docker create calls = %d, want 1 (adapter must run once)", got)
	}
}

// TestScenario_InvalidOperation rejects unknown operation types before any
// adapter runs.
func TestScenario_InvalidOperation(t *testing.T) {
	h := newHarness(t)
	d := h.dispatcher()

	for i, typ := range []string{"PREPARE", "RUN_SHELL", "EXEC"} {
		op := newOperation(opID("EXEC", i+1), typ, protocol.Payload{Container: testContainer}, testServer)
		ack, result := d.Dispatch(context.Background(), op, testIdentity)
		if ack.Accepted {
			t.Fatalf("%s: ack accepted, want rejected", typ)
		}
		if result.Success || result.ErrorCode != dispatcher.CodeUnknownOperation {
			t.Fatalf("%s: result = %+v, want UNKNOWN_OPERATION", typ, result)
		}
	}
	if calls := h.sim.callsWith("create"); len(calls) != 0 {
		t.Fatalf("invalid operations reached the adapter: %v", calls)
	}
}

// TestScenario_RuntimeFailure verifies Docker adapter error codes surface
// verbatim through the dispatcher.
func TestScenario_RuntimeFailure(t *testing.T) {
	t.Run("image missing", func(t *testing.T) {
		h := newHarness(t)
		h.sim.failPull = true
		d := h.dispatcher()
		op := newOperation(opID("CREATE_RUNTIME", 1), protocol.OpCreateRuntime,
			protocol.Payload{ImageRef: "sha256:" + strings.Repeat("a", 64), Container: testContainer, Port: 3000}, testServer)
		_, result := d.Dispatch(context.Background(), op, testIdentity)
		if result.Success || result.ErrorCode != docker.CodeImageMissing {
			t.Fatalf("result = %+v, want %s", result, docker.CodeImageMissing)
		}
	})

	t.Run("docker start failure", func(t *testing.T) {
		h := newHarness(t)
		d := h.dispatcher()
		image := "sha256:" + strings.Repeat("a", 64)
		create := newOperation(opID("CREATE_RUNTIME", 1), protocol.OpCreateRuntime,
			protocol.Payload{ImageRef: image, Container: testContainer, Port: 3000}, testServer)
		if _, res := d.Dispatch(context.Background(), create, testIdentity); !res.Success {
			t.Fatalf("setup create failed: %+v", res)
		}
		h.sim.failStart[testContainer] = true
		start := newOperation(opID("START", 1), protocol.OpStartRuntime,
			protocol.Payload{Container: testContainer}, testServer)
		_, result := d.Dispatch(context.Background(), start, testIdentity)
		if result.Success || result.ErrorCode != docker.CodeDockerFailed {
			t.Fatalf("result = %+v, want %s", result, docker.CodeDockerFailed)
		}
	})
}

// TestScenario_NetworkFailure verifies a Traefik write/render failure surfaces
// through the dispatcher and never leaves a partial file behind.
func TestScenario_NetworkFailure(t *testing.T) {
	h := newHarness(t)
	d := h.dispatcher()
	image := "sha256:" + strings.Repeat("a", 64)
	create := newOperation(opID("CREATE_RUNTIME", 1), protocol.OpCreateRuntime,
		protocol.Payload{ImageRef: image, Container: testContainer, Port: 3000}, testServer)
	if _, res := d.Dispatch(context.Background(), create, testIdentity); !res.Success {
		t.Fatalf("setup create failed: %+v", res)
	}
	network := func(attempt int) protocol.Operation {
		return newOperation(opID("NETWORK", attempt), protocol.OpConfigureNetwork,
			protocol.Payload{Container: testContainer, Proxy: "traefik", Domain: "app.example.com", TLS: true, Port: 443}, testServer)
	}

	t.Run("write error", func(t *testing.T) {
		// A dynamic directory path that is an existing regular file makes the
		// write path fail without creating anything.
		blocked := filepath.Join(h.dir, "blocked")
		if err := os.WriteFile(blocked, []byte("keep"), 0o600); err != nil {
			t.Fatalf("seed blocked file: %v", err)
		}
		h.trf.DynamicDir = blocked
		_, result := d.Dispatch(context.Background(), network(1), testIdentity)
		if result.Success || result.ErrorCode != codeNetworkFailed {
			t.Fatalf("result = %+v, want %s", result, codeNetworkFailed)
		}
		info, err := os.Stat(blocked)
		if err != nil || info.IsDir() {
			t.Fatalf("blocked path changed: info=%v err=%v", info, err)
		}
		if got, _ := os.ReadFile(blocked); string(got) != "keep" {
			t.Fatalf("blocked file mutated: %q", got)
		}
	})

	t.Run("render error leaves no partial file", func(t *testing.T) {
		dir := filepath.Join(h.dir, "dynamic2")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		existing := filepath.Join(dir, "axiom-"+testDeployment+".yml")
		if err := os.WriteFile(existing, []byte("original\n"), 0o644); err != nil {
			t.Fatalf("seed existing: %v", err)
		}
		h.trf.DynamicDir = dir
		h.trf.Marshal = func(traefik.Request) ([]byte, error) {
			return nil, errors.New("render boom")
		}
		defer func() { h.trf.Marshal = nil }()

		_, result := d.Dispatch(context.Background(), network(2), testIdentity)
		if result.Success {
			t.Fatal("dispatch succeeded despite render failure")
		}
		if got, _ := os.ReadFile(existing); string(got) != "original\n" {
			t.Fatalf("existing config mutated: %q", got)
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.Contains(e.Name(), ".tmp-") {
				t.Fatalf("partial temp file left behind: %s", e.Name())
			}
		}
	})
}

// TestScenario_AuthorizationViolation covers two authorization boundaries: the
// dispatcher rejects an operation bound to a different server, and the Docker
// adapter refuses to touch an unmanaged container.
func TestScenario_AuthorizationViolation(t *testing.T) {
	t.Run("server identity mismatch", func(t *testing.T) {
		h := newHarness(t)
		d := h.dispatcher()
		op := newOperation(opID("START", 1), protocol.OpStartRuntime,
			protocol.Payload{Container: testContainer}, "srv_other")
		ack, result := d.Dispatch(context.Background(), op, testIdentity)
		if ack.Accepted {
			t.Fatal("ack accepted, want rejected")
		}
		if result.ErrorCode != dispatcher.CodeForbidden {
			t.Fatalf("result = %+v, want FORBIDDEN", result)
		}
		if len(h.sim.calls) != 0 {
			t.Fatalf("adapter ran for a foreign-server operation: %v", h.sim.calls)
		}
	})

	t.Run("docker refuses unmanaged container", func(t *testing.T) {
		h := newHarness(t)
		h.sim.seed("foreign", map[string]string{}, false)

		// Direct adapter call.
		err := h.docker.Start(context.Background(), "foreign")
		if err == nil || !docker.IsNotManaged(err) {
			t.Fatalf("docker.Start(foreign) = %v, want ErrNotManaged", err)
		}

		// Through the dispatcher: the code surfaces.
		d := h.dispatcher()
		op := newOperation(opID("START", 2), protocol.OpStartRuntime,
			protocol.Payload{Container: "foreign"}, testServer)
		_, result := d.Dispatch(context.Background(), op, testIdentity)
		if result.Success || result.ErrorCode != docker.CodeNotManaged {
			t.Fatalf("result = %+v, want %s", result, docker.CodeNotManaged)
		}
	})
}
