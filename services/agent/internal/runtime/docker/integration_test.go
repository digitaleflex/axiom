package docker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/security/ownership"
)

// TestIntegrationLifecycle exercises the adapter against a real Docker daemon
// (issue #83 acceptance): ensure image, create, inspect, start, tail, stop,
// remove — with managed labels and ownership enforcement end to end.
// Skipped unless AXIOM_TEST_DOCKER=1.
func TestIntegrationLifecycle(t *testing.T) {
	if os.Getenv("AXIOM_TEST_DOCKER") == "" {
		t.Skip("AXIOM_TEST_DOCKER is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	a := &Adapter{Timeout: 2 * time.Minute}
	dep := "dep_0123456789abcdef01234567"
	name := fmt.Sprintf("axiom-it-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		// Best-effort force removal; ignore errors (container may be gone).
		_ = a.Remove(context.Background(), name)
	})

	// 1. EnsureImage (pulls busybox when missing).
	if err := a.EnsureImage(ctx, "busybox:latest"); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}

	// 2. Create with managed labels, env, limits and a keep-alive command.
	info, err := a.Create(ctx, CreateSpec{
		DeploymentID:  dep,
		ApplicationID: "it-app",
		ServerID:      "srv_it",
		Container:     name,
		ImageRef:      "busybox:latest",
		Port:          18080,
		Env:           map[string]string{"AXIOM_IT": "1"},
		Limits:        ResourceLimits{MemoryMB: 64, NanoCPUs: 500000000},
		Command:       []string{"sleep", "300"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if info.Running {
		t.Fatal("created container must not be running yet")
	}
	if len(info.Ports) != 1 || info.Ports[0].ContainerPort != 18080 || info.Ports[0].HostIP != "127.0.0.1" {
		t.Fatalf("ports = %+v, want one 127.0.0.1 ephemeral mapping for 18080", info.Ports)
	}
	// Docker allocates the ephemeral host port at start; before that the
	// configured binding reports HostPort 0.
	if !ownership.IsManaged(info.Labels) || info.Labels[ownership.LabelDeployment] != dep || info.Labels[ownership.LabelServer] != "srv_it" {
		t.Fatalf("labels = %v", info.Labels)
	}

	// 3. Idempotent create returns the same container.
	again, err := a.Create(ctx, CreateSpec{
		DeploymentID: dep, ServerID: "srv_it", Container: name,
		ImageRef: "busybox:latest", Port: 18080, Command: []string{"sleep", "300"},
	})
	if err != nil {
		t.Fatalf("idempotent Create: %v", err)
	}
	if again.Name != name {
		t.Fatalf("idempotent Create returned %+v", again)
	}

	// 4. Start and verify running status via inspect.
	if err := a.Start(ctx, name); err != nil {
		t.Fatalf("Start: %v", err)
	}
	running, err := a.Inspect(ctx, name)
	if err != nil {
		t.Fatalf("Inspect after start: %v", err)
	}
	if !running.Running || running.Status != "running" {
		t.Fatalf("running = %v status = %q", running.Running, running.Status)
	}
	if len(running.Ports) != 1 || running.Ports[0].HostPort <= 0 {
		t.Fatalf("ports after start = %+v, want an allocated ephemeral host port", running.Ports)
	}

	// 5. Tail works (output may be empty for sleep).
	if _, err := a.Tail(ctx, name, 10); err != nil {
		t.Fatalf("Tail: %v", err)
	}

	// 6. Stop and verify.
	if err := a.Stop(ctx, name); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	stopped, err := a.Inspect(ctx, name)
	if err != nil {
		t.Fatalf("Inspect after stop: %v", err)
	}
	if stopped.Running {
		t.Fatal("container must be stopped")
	}

	// 7. Remove and verify gone.
	if err := a.Remove(ctx, name); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := a.Inspect(ctx, name); err == nil || !isNotFound(err) {
		t.Fatalf("Inspect after remove: %v, want not-found", err)
	}
	// Idempotent remove.
	if err := a.Remove(ctx, name); err != nil {
		t.Fatalf("idempotent Remove: %v", err)
	}
}

// TestIntegrationCleanup verifies Cleanup removes only managed containers in
// the deployment scope and leaves foreign containers untouched.
func TestIntegrationCleanup(t *testing.T) {
	if os.Getenv("AXIOM_TEST_DOCKER") == "" {
		t.Skip("AXIOM_TEST_DOCKER is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	a := &Adapter{Timeout: 2 * time.Minute}
	dep := "dep_0123456789abcdef01234567"
	managed := fmt.Sprintf("axiom-it-clean-%d", time.Now().UnixNano())
	foreign := fmt.Sprintf("axiom-it-foreign-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = a.Remove(context.Background(), managed)
		_ = a.Remove(context.Background(), foreign)
	})

	for _, spec := range []CreateSpec{
		{DeploymentID: dep, Container: managed, ImageRef: "busybox:latest", Port: 18081, Command: []string{"sleep", "300"}},
		{DeploymentID: "dep_ffffffffffffffffffffffff", Container: foreign, ImageRef: "busybox:latest", Port: 18082, Command: []string{"sleep", "300"}},
	} {
		if _, err := a.Create(ctx, spec); err != nil {
			t.Fatalf("Create %s: %v", spec.Container, err)
		}
	}
	if err := a.Cleanup(ctx, dep); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := a.Inspect(ctx, managed); !isNotFound(err) {
		t.Fatalf("managed container must be removed: %v", err)
	}
	if info, err := a.Inspect(ctx, foreign); err != nil || info.Name != foreign {
		t.Fatalf("foreign container must survive: %+v err=%v", info, err)
	}
}

// TestIntegrationUnmanagedRefused proves the ownership boundary against a
// real daemon: a container without the managed label is never started.
func TestIntegrationUnmanagedRefused(t *testing.T) {
	if os.Getenv("AXIOM_TEST_DOCKER") == "" {
		t.Skip("AXIOM_TEST_DOCKER is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	a := &Adapter{Timeout: 2 * time.Minute}
	name := fmt.Sprintf("axiom-it-foreign-%d", time.Now().UnixNano())
	// The container is deliberately unmanaged, so Remove would (correctly)
	// refuse it; force-remove through the raw runner in cleanup.
	t.Cleanup(func() { _ = a.runDocker(context.Background(), "rm", "-f", name) })

	// Create a container WITHOUT managed labels via the raw runner.
	if err := a.runDocker(ctx, "create", "--name", name, "busybox:latest", "sleep", "300"); err != nil {
		t.Fatalf("raw create: %v", err)
	}
	if err := a.Start(ctx, name); !IsNotManaged(err) {
		t.Fatalf("Start foreign: %v, want ErrNotManaged", err)
	}
	if err := a.Stop(ctx, name); !IsNotManaged(err) {
		t.Fatalf("Stop foreign: %v, want ErrNotManaged", err)
	}
	if err := a.Remove(ctx, name); !IsNotManaged(err) {
		t.Fatalf("Remove foreign: %v, want ErrNotManaged", err)
	}
	// The foreign container must still exist and be untouched.
	info, err := a.Inspect(ctx, name)
	if err != nil || info.Name != name {
		t.Fatalf("foreign container must be untouched: %+v err=%v", info, err)
	}
	if strings.Contains(info.Status, "running") {
		t.Fatalf("foreign container must not have been started: %q", info.Status)
	}
}
