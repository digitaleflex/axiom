package bootstrap

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/agent/internal/dispatcher"
	"github.com/digitaleflex/axiom/services/agent/internal/health"
	"github.com/digitaleflex/axiom/services/agent/internal/runtime/docker"
	"github.com/digitaleflex/axiom/services/agent/internal/runtime/traefik"
	"github.com/digitaleflex/axiom/services/agent/internal/security/ownership"
)

const (
	bridgeDeploymentID  = "dep_0123456789abcdef01234567"
	bridgeApplicationID = "app_0123456789abcdef01234567"
)

// stubRunner records the argv of every docker invocation and answers the
// minimum an image inspect + create needs.
type stubRunner struct {
	calls    [][]string
	inspects int
}

func (r *stubRunner) Run(_ context.Context, argv ...string) (string, int, error) {
	r.calls = append(r.calls, argv)
	switch argv[0] {
	case "image":
		return `[{"Id":"sha256:deadbeef"}]`, 0, nil
	case "create":
		return "containerid", 0, nil
	case "inspect":
		// The pre-create existence check must miss; every inspection after the
		// create succeeds and reports the labels the bridge stamped.
		r.inspects++
		if r.inspects == 1 {
			return `[]`, 1, errors.New("no such container")
		}
		return `[{"Id":"containerid","Name":"/axiom-app-1","Image":"sha256:deadbeef","Config":{"Image":"busybox:latest","Labels":{` +
			r.labelsJSON() + `}},"State":{"Status":"created","Running":false},"NetworkSettings":{"Ports":{}}}]`, 0, nil
	}
	return "", 0, nil
}

// labelsOf returns the value the bridge stamped for one canonical label.
func (r *stubRunner) labelsOf(key string) string {
	for _, call := range r.calls {
		if len(call) == 0 || call[0] != "create" {
			continue
		}
		for i, arg := range call {
			if arg == "--label" && i+1 < len(call) && strings.HasPrefix(call[i+1], key+"=") {
				return strings.TrimPrefix(call[i+1], key+"=")
			}
		}
	}
	return ""
}

// labelsJSON renders the labels the bridge stamped on the create call as a JSON
// object, so the post-create inspection reflects them.
func (r *stubRunner) labelsJSON() string {
	var parts []string
	for _, call := range r.calls {
		if len(call) == 0 || call[0] != "create" {
			continue
		}
		for i, arg := range call {
			if arg == "--label" && i+1 < len(call) {
				key, value, _ := strings.Cut(call[i+1], "=")
				parts = append(parts, strconv.Quote(key)+":"+strconv.Quote(value))
			}
		}
	}
	return strings.Join(parts, ",")
}

func newStubBridge() (*runtimeBridge, *stubRunner) {
	runner := &stubRunner{}
	return &runtimeBridge{
		docker:  &docker.Adapter{Runner: runner},
		traefik: &traefik.Adapter{},
		health:  health.NewChecker(),
	}, runner
}

// TestBridgeStampsDistinctApplicationAndDeployment is the #145 regression: the
// bridge must stamp the operation's application ID verbatim, never the
// deployment ID it used to conflate with it.
func TestBridgeStampsDistinctApplicationAndDeployment(t *testing.T) {
	b, runner := newStubBridge()
	scoped, ok := b.WithScope(bridgeApplicationID, bridgeDeploymentID, "srv_test").(*runtimeBridge)
	if !ok {
		t.Fatal("WithScope must return a *runtimeBridge")
	}
	if err := scoped.CreateRuntime(context.Background(), dispatcher.CreateParams{
		Container: "axiom-app-1", ImageRef: "busybox:latest", Port: 3000,
	}); err != nil {
		t.Fatalf("CreateRuntime: %v", err)
	}
	app := runner.labelsOf(ownership.LabelApplication)
	dep := runner.labelsOf(ownership.LabelDeployment)
	if app != bridgeApplicationID {
		t.Fatalf("axiom.application = %q, want %q", app, bridgeApplicationID)
	}
	if dep != bridgeDeploymentID {
		t.Fatalf("axiom.deployment = %q, want %q", dep, bridgeDeploymentID)
	}
	if app == dep {
		t.Fatal("the application and the deployment labels must not share a value")
	}
}

// TestBridgeRefusesUnscopedCreate is the defense-in-depth backstop: a bridge
// that was never scoped refuses CREATE_RUNTIME with the stable
// INCOMPLETE_SCOPE code instead of stamping an un-attributable label.
func TestBridgeRefusesUnscopedCreate(t *testing.T) {
	b, runner := newStubBridge()
	// No WithScope call: the application scope was never resolved.
	err := b.CreateRuntime(context.Background(), dispatcher.CreateParams{
		Container: "axiom-app-1", ImageRef: "busybox:latest", Port: 3000,
	})
	if !errors.Is(err, errUnscopedBridge) {
		t.Fatalf("err = %v, want errUnscopedBridge", err)
	}
	var coder dispatcher.ErrorCoder
	if !errors.As(err, &coder) || coder.ErrorCode() != dispatcher.CodeIncompleteScope {
		t.Fatalf("error code = %v, want %s", coder, dispatcher.CodeIncompleteScope)
	}
	for _, call := range runner.calls {
		if len(call) > 0 && call[0] == "create" {
			t.Fatalf("docker create must not be reached: %v", call)
		}
	}
}

// TestBridgeRefusesUnscopedNetwork applies the same backstop to NETWORK.
func TestBridgeRefusesUnscopedNetwork(t *testing.T) {
	b, _ := newStubBridge()
	err := b.ConfigureNetwork(context.Background(), dispatcher.NetworkParams{
		Container: "axiom-app-1", Domain: "app.example.com", Proxy: "traefik", Port: 3000,
	})
	if !errors.Is(err, errUnscopedBridge) {
		t.Fatalf("err = %v, want errUnscopedBridge", err)
	}
}

// TestWithScopeDoesNotMutateReceiver proves concurrent operations never share
// an ownership scope.
func TestWithScopeDoesNotMutateReceiver(t *testing.T) {
	b, _ := newStubBridge()
	first := b.WithScope(bridgeApplicationID, bridgeDeploymentID, "srv_test").(*runtimeBridge)
	second := b.WithScope("app_ffffffffffffffffffffffff", "dep_ffffffffffffffffffffffff", "srv_test").(*runtimeBridge)
	if first == second {
		t.Fatal("WithScope must return a distinct copy")
	}
	if b.applicationID != "" || b.deploymentID != "" || b.serverID != "" {
		t.Fatalf("the receiver was mutated: %+v", b)
	}
	if first.applicationID != bridgeApplicationID || second.applicationID == first.applicationID {
		t.Fatalf("scopes leaked between copies: %q / %q", first.applicationID, second.applicationID)
	}
}

// TestScopeAccessor mirrors the scope into the ownership package.
func TestScopeAccessor(t *testing.T) {
	b := &runtimeBridge{applicationID: bridgeApplicationID, deploymentID: bridgeDeploymentID}
	got := b.scope()
	if got.ApplicationID != bridgeApplicationID || got.DeploymentID != bridgeDeploymentID {
		t.Fatalf("scope = %+v", got)
	}
}
