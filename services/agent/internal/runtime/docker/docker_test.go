package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/security/ownership"
)

// stubRunner is a scripted Runner: every call records argv and dispatches to
// the test-supplied function.
type stubRunner struct {
	fn    func(ctx context.Context, argv []string) (string, int, error)
	calls [][]string
}

func (s *stubRunner) Run(ctx context.Context, argv ...string) (string, int, error) {
	s.calls = append(s.calls, append([]string(nil), argv...))
	return s.fn(ctx, argv)
}

func (s *stubRunner) callsWith(sub string) [][]string {
	var out [][]string
	for _, c := range s.calls {
		if len(c) > 0 && c[0] == sub {
			out = append(out, c)
		}
	}
	return out
}

func containsSeq(t *testing.T, call []string, seq ...string) {
	t.Helper()
	for i := 0; i+len(seq) <= len(call); i++ {
		match := true
		for j, s := range seq {
			if call[i+j] != s {
				match = false
				break
			}
		}
		if match {
			return
		}
	}
	t.Fatalf("argv %v missing sequence %q", call, seq)
}

func containsArgv(t *testing.T, call []string, want ...string) {
	t.Helper()
	for _, w := range want {
		found := false
		for _, a := range call {
			if a == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("argv %v missing %q", call, w)
		}
	}
}

const (
	testDep = "dep_0123456789abcdef01234567"
	testApp = "app_0123456789abcdef01234567"
	// testOtherApp is a second application on the same server: a container of
	// that application must never be touched by a testApp/testDep operation.
	testOtherApp = "app_fedcba9876543210fedcba98"
)

// inspectJSON builds a docker inspect array document. portsJSON is the raw
// NetworkSettings.Ports object (or "" for none).
func inspectJSON(name, image string, managed, running bool, portsJSON string) string {
	return inspectJSONForApp(name, image, managed, running, portsJSON, testApp)
}

// inspectJSONForApp is inspectJSON with an explicit application label, so a
// test can build a container owned by another application (#145).
func inspectJSONForApp(name, image string, managed, running bool, portsJSON, applicationID string) string {
	labels := `"axiom.deployment": "` + testDep + `", "axiom.application": "` + applicationID + `"`
	if managed {
		labels = `"axiom.managed": "true", ` + labels
	}
	status := "exited"
	if running {
		status = "running"
	}
	ports := portsJSON
	if ports == "" {
		ports = "{}"
	}
	return fmt.Sprintf(`[{"Id":"abc123","Name":"/%s","Image":"sha256:deadbeef","Config":{"Image":%q,"Labels":{%s}},`+
		`"State":{"Status":%q,"Running":%t},"NetworkSettings":{"Ports":%s}}]`,
		name, image, labels, status, running, ports)
}

func notFoundResponse() (string, int, error) {
	return "Error: No such container\n", 1, errors.New("exit status 1")
}

func okResponse(out string) (string, int, error) { return out, 0, nil }

// createFlowStub answers image inspect with success, the first container
// inspect with not-found (so Create proceeds past the idempotency pre-check)
// and later inspects with the given managed document.
func createFlowStub(managedJSON string) *stubRunner {
	var inspects int
	return &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		switch argv[0] {
		case "image":
			return okResponse(`[{"Id":"sha256:deadbeef"}]`)
		case "inspect":
			inspects++
			if inspects == 1 {
				return notFoundResponse()
			}
			return okResponse(managedJSON)
		case "create":
			return okResponse("")
		}
		return "", 0, nil
	}}
}

// imagePresentStub answers "image inspect" with success.
func imagePresentStub(ctx context.Context, argv []string) (string, int, error) {
	if argv[0] == "image" {
		return okResponse(`[{"Id":"sha256:deadbeef"}]`)
	}
	return "", 0, nil
}

func TestCreateHappyPath(t *testing.T) {
	// The fixture must carry every label the adapter sets at create time.
	fullLabels := inspectJSON("axiom-test", "busybox:latest", true, false,
		`{"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"32768"}]}`)
	fullLabels = strings.Replace(fullLabels,
		`"axiom.managed": "true", "axiom.deployment": "`+testDep+`", "axiom.application": "`+testApp+`"`,
		`"axiom.managed": "true", "axiom.deployment": "`+testDep+`", "axiom.application": "`+testApp+`", "axiom.server": "srv_test", "axiom.created": "2026-10-10T10:00:00Z", "axiom.app": "demo"`, 1)
	stub := createFlowStub(fullLabels)
	a := &Adapter{Runner: stub}
	info, err := a.Create(context.Background(), CreateSpec{
		DeploymentID:  testDep,
		ApplicationID: testApp,
		ServerID:      "srv_test",
		Container:     "axiom-test",
		ImageRef:      "busybox:latest",
		Port:          8080,
		Env:           map[string]string{"FOO": "bar"},
		Labels:        map[string]string{"axiom.app": "demo"},
		Limits:        ResourceLimits{MemoryMB: 512, NanoCPUs: 500000000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "axiom-test" || info.Image != "busybox:latest" || info.ImageID != "sha256:deadbeef" {
		t.Fatalf("info = %+v", info)
	}
	if info.Running {
		t.Fatal("freshly created container must not be running")
	}
	if len(info.Ports) != 1 || info.Ports[0].HostPort != 32768 || info.Ports[0].ContainerPort != 8080 || info.Ports[0].HostIP != "127.0.0.1" {
		t.Fatalf("ports = %+v", info.Ports)
	}
	if !ownership.IsManaged(info.Labels) || info.Labels[ownership.LabelDeployment] != testDep ||
		info.Labels[ownership.LabelApplication] != testApp || info.Labels[ownership.LabelServer] != "srv_test" ||
		info.Labels[ownership.LabelCreated] == "" || info.Labels["axiom.app"] != "demo" {
		t.Fatalf("labels = %v", info.Labels)
	}
	creates := stub.callsWith("create")
	if len(creates) != 1 {
		t.Fatalf("create calls = %v", stub.calls)
	}
	c := creates[0]
	containsSeq(t, c, "create", "--name", "axiom-test")
	containsSeq(t, c, "-p", "127.0.0.1::8080")
	containsSeq(t, c, "--memory", "512m")
	containsSeq(t, c, "--cpus", "0.5")
	containsSeq(t, c, "-e", "FOO=bar")
	containsSeq(t, c, "--label", ownership.LabelManaged+"=true")
	containsSeq(t, c, "--label", ownership.LabelDeployment+"="+testDep)
	containsSeq(t, c, "--label", ownership.LabelApplication+"="+testApp)
	containsSeq(t, c, "--label", ownership.LabelServer+"=srv_test")
	containsSeq(t, c, "--label", "axiom.app=demo")
	containsArgv(t, c, "busybox:latest")
}

func TestCreateIdempotent(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		if argv[0] == "image" {
			return okResponse(`[{"Id":"sha256:deadbeef"}]`)
		}
		return okResponse(inspectJSON("axiom-test", "busybox:latest", true, false,
			`{"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"32768"}]}`))
	}}
	a := &Adapter{Runner: stub}
	spec := CreateSpec{DeploymentID: testDep, ApplicationID: testApp, Container: "axiom-test", ImageRef: "busybox:latest", Port: 8080}
	info, err := a.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "axiom-test" || len(info.Ports) != 1 {
		t.Fatalf("info = %+v", info)
	}
	if creates := stub.callsWith("create"); len(creates) != 0 {
		t.Fatalf("existing managed container must not be recreated: %v", creates)
	}
}

func TestCreateUnmanagedExistingRefused(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		if argv[0] == "image" {
			return okResponse(`[{"Id":"sha256:deadbeef"}]`)
		}
		// Exists but NOT axiom-managed.
		return okResponse(inspectJSON("axiom-test", "busybox:latest", false, false, "{}"))
	}}
	a := &Adapter{Runner: stub}
	_, err := a.Create(context.Background(), CreateSpec{DeploymentID: testDep, ApplicationID: testApp, Container: "axiom-test", ImageRef: "busybox:latest", Port: 8080})
	if !IsNotManaged(err) {
		t.Fatalf("err = %v, want ErrNotManaged", err)
	}
	if creates := stub.callsWith("create"); len(creates) != 0 {
		t.Fatalf("unmanaged container must never be touched: %v", creates)
	}
}

func TestCreateWrongImageRefused(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		if argv[0] == "image" {
			return okResponse(`[{"Id":"sha256:deadbeef"}]`)
		}
		return okResponse(inspectJSON("axiom-test", "alpine:latest", true, false, "{}"))
	}}
	a := &Adapter{Runner: stub}
	_, err := a.Create(context.Background(), CreateSpec{DeploymentID: testDep, ApplicationID: testApp, Container: "axiom-test", ImageRef: "busybox:latest", Port: 8080})
	var de *Error
	if !errors.As(err, &de) || de.Code != CodeInvalidInput {
		t.Fatalf("err = %v, want CodeInvalidInput", err)
	}
	if creates := stub.callsWith("create"); len(creates) != 0 {
		t.Fatalf("conflicting container must not be recreated: %v", creates)
	}
}

func TestCreateManagedLabelsNotOverridable(t *testing.T) {
	stub := createFlowStub(inspectJSON("axiom-test", "busybox:latest", true, false, "{}"))
	a := &Adapter{Runner: stub}
	_, err := a.Create(context.Background(), CreateSpec{
		DeploymentID:  testDep,
		ApplicationID: testApp,
		Container:     "axiom-test",
		ImageRef:      "busybox:latest",
		Port:          8080,
		Labels:        map[string]string{ownership.LabelManaged: "false", "axiom.app": "demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	creates := stub.callsWith("create")
	if len(creates) != 1 {
		t.Fatalf("create calls = %v", stub.calls)
	}
	// The caller's attempt to forge axiom.managed=false must be dropped.
	for i := 0; i+1 < len(creates[0]); i++ {
		if creates[0][i] == "--label" && creates[0][i+1] == ownership.LabelManaged+"=false" {
			t.Fatalf("managed label must not be overridable: %v", creates[0])
		}
	}
	containsSeq(t, creates[0], "--label", "axiom.managed=true")
}

func TestStartStopRemoveHappyPath(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		if argv[0] == "image" {
			return okResponse(`[{"Id":"sha256:deadbeef"}]`)
		}
		return okResponse(inspectJSON("axiom-test", "busybox:latest", true, true, "{}"))
	}}
	a := &Adapter{Runner: stub}
	ctx := context.Background()
	if err := a.Start(ctx, "axiom-test"); err != nil {
		t.Fatal(err)
	}
	if err := a.Stop(ctx, "axiom-test"); err != nil {
		t.Fatal(err)
	}
	if err := a.Remove(ctx, "axiom-test"); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"start", "stop", "rm"} {
		calls := stub.callsWith(sub)
		if len(calls) != 1 {
			t.Fatalf("%s calls = %v", sub, stub.calls)
		}
		containsSeq(t, calls[0], sub, "axiom-test")
	}
	// Ownership verification (inspect) must precede every mutation.
	firstStart := -1
	firstInspect := -1
	for i, c := range stub.calls {
		if firstStart < 0 && len(c) > 0 && c[0] == "start" {
			firstStart = i
		}
		if firstInspect < 0 && len(c) > 0 && c[0] == "inspect" {
			firstInspect = i
		}
	}
	if firstInspect < 0 || firstStart < 0 || firstInspect > firstStart {
		t.Fatalf("inspect must precede start: %v", stub.calls)
	}
}

func TestMutationsRefuseUnmanaged(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		if argv[0] == "image" {
			return okResponse(`[{"Id":"sha256:deadbeef"}]`)
		}
		return okResponse(inspectJSON("axiom-test", "busybox:latest", false, false, "{}"))
	}}
	a := &Adapter{Runner: stub}
	ctx := context.Background()
	for _, op := range []struct {
		name string
		fn   func() error
	}{
		{"start", func() error { return a.Start(ctx, "axiom-test") }},
		{"stop", func() error { return a.Stop(ctx, "axiom-test") }},
		{"remove", func() error { return a.Remove(ctx, "axiom-test") }},
	} {
		if err := op.fn(); !IsNotManaged(err) {
			t.Fatalf("%s: err = %v, want ErrNotManaged", op.name, err)
		}
	}
	for _, sub := range []string{"start", "stop", "rm"} {
		if calls := stub.callsWith(sub); len(calls) != 0 {
			t.Fatalf("%s must never run on unmanaged container: %v", sub, calls)
		}
	}
}

func TestMutationsRefuseUnknown(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		if argv[0] == "image" {
			return okResponse(`[{"Id":"sha256:deadbeef"}]`)
		}
		return notFoundResponse()
	}}
	a := &Adapter{Runner: stub}
	ctx := context.Background()
	for _, op := range []struct {
		name string
		fn   func() error
	}{
		{"start", func() error { return a.Start(ctx, "axiom-test") }},
		{"stop", func() error { return a.Stop(ctx, "axiom-test") }},
	} {
		if err := op.fn(); err == nil || IsNotManaged(err) {
			t.Fatalf("%s: err = %v, want not-found", op.name, err)
		}
	}
	// Remove is idempotent: unknown container is already gone.
	if err := a.Remove(ctx, "axiom-test"); err != nil {
		t.Fatalf("remove unknown: %v", err)
	}
	if calls := stub.callsWith("rm"); len(calls) != 0 {
		t.Fatalf("rm must not run for unknown container: %v", calls)
	}
}

func TestInvalidNamesRejected(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		t.Fatalf("runner must not be called for invalid input: %v", argv)
		return "", 0, nil
	}}
	a := &Adapter{Runner: stub}
	ctx := context.Background()
	bad := []string{"", "-bad", "a b", "../escape", "UPPER CASE", strings.Repeat("a", 129), "axiom;rm"}
	for _, name := range bad {
		if _, err := a.Inspect(ctx, name); err == nil {
			t.Fatalf("inspect %q: expected error", name)
		}
		if _, err := a.Create(ctx, CreateSpec{DeploymentID: testDep, ApplicationID: testApp, Container: name, ImageRef: "busybox:latest", Port: 80}); err == nil {
			t.Fatalf("create %q: expected error", name)
		}
		if err := a.Start(ctx, name); err == nil {
			t.Fatalf("start %q: expected error", name)
		}
		if err := a.Stop(ctx, name); err == nil {
			t.Fatalf("stop %q: expected error", name)
		}
		if err := a.Remove(ctx, name); err == nil {
			t.Fatalf("remove %q: expected error", name)
		}
		if _, err := a.Tail(ctx, name, 10); err == nil {
			t.Fatalf("tail %q: expected error", name)
		}
	}
	if len(stub.calls) != 0 {
		t.Fatalf("no docker call may happen for invalid names: %v", stub.calls)
	}
}

func TestInvalidImageRefRejected(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		t.Fatalf("runner must not be called for invalid input: %v", argv)
		return "", 0, nil
	}}
	a := &Adapter{Runner: stub}
	for _, ref := range []string{"", "\tleading", "trailing\n", strings.Repeat("x", 513), "uni€ode"} {
		if err := a.EnsureImage(context.Background(), ref); err == nil {
			t.Fatalf("EnsureImage %q: expected error", ref)
		}
	}
	if len(stub.calls) != 0 {
		t.Fatalf("no docker call may happen for invalid image refs: %v", stub.calls)
	}
}

func TestEnsureImageSkipsPullWhenPresent(t *testing.T) {
	stub := &stubRunner{fn: imagePresentStub}
	a := &Adapter{Runner: stub}
	if err := a.EnsureImage(context.Background(), "busybox:latest"); err != nil {
		t.Fatal(err)
	}
	if pulls := stub.callsWith("pull"); len(pulls) != 0 {
		t.Fatalf("pull must be skipped when image exists: %v", pulls)
	}
}

func TestEnsureImagePullFallback(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		if argv[0] == "image" {
			return "Error: No such image\n", 1, errors.New("exit status 1")
		}
		if argv[0] == "pull" {
			return okResponse("Pulled busybox:latest\n")
		}
		return "", 0, nil
	}}
	a := &Adapter{Runner: stub}
	if err := a.EnsureImage(context.Background(), "busybox:latest"); err != nil {
		t.Fatal(err)
	}
	pulls := stub.callsWith("pull")
	if len(pulls) != 1 || len(pulls[0]) != 2 || pulls[0][1] != "busybox:latest" {
		t.Fatalf("pull calls = %v", pulls)
	}
}

func TestEnsureImagePullFailure(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		if argv[0] == "image" {
			return "Error: No such image\n", 1, errors.New("exit status 1")
		}
		return "pull access denied\n", 1, errors.New("exit status 1")
	}}
	a := &Adapter{Runner: stub}
	err := a.EnsureImage(context.Background(), "busybox:latest")
	var de *Error
	if !errors.As(err, &de) || de.Code != CodeImageMissing || de.ExitCode != 1 {
		t.Fatalf("err = %v, want CodeImageMissing with exit 1", err)
	}
}

func TestEnvPassedAsArgvItems(t *testing.T) {
	stub := createFlowStub(inspectJSON("axiom-test", "busybox:latest", true, false, "{}"))
	a := &Adapter{Runner: stub}
	_, err := a.Create(context.Background(), CreateSpec{
		DeploymentID:  testDep,
		ApplicationID: testApp,
		Container:     "axiom-test",
		ImageRef:      "busybox:latest",
		Port:          8080,
		Env: map[string]string{
			"PLAIN":   "value",
			"SPACED":  "hello world",
			"META":    "a;rm -rf /",
			"QUOTED":  `he said "hi"`,
			"NEWLINE": "line1\nline2",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	creates := stub.callsWith("create")
	if len(creates) != 1 {
		t.Fatalf("create calls = %v", stub.calls)
	}
	c := creates[0]
	// Each env entry is exactly one -e KEY=VALUE argv item — never concatenated
	// into a shell string, so metacharacters stay inert data.
	containsSeq(t, c, "-e", "PLAIN=value")
	containsSeq(t, c, "-e", "SPACED=hello world")
	containsSeq(t, c, "-e", "META=a;rm -rf /")
	containsSeq(t, c, "-e", `QUOTED=he said "hi"`)
	containsSeq(t, c, "-e", "NEWLINE=line1\nline2")
	// No shell metacharacter ever appears as its own argv item.
	for _, item := range c {
		if item == ";" || item == "&&" || item == "|" || item == "`" {
			t.Fatalf("shell metacharacter leaked into argv: %v", c)
		}
	}
}

func TestResourceLimitsZeroUnset(t *testing.T) {
	stub := createFlowStub(inspectJSON("axiom-test", "busybox:latest", true, false, "{}"))
	a := &Adapter{Runner: stub}
	_, err := a.Create(context.Background(), CreateSpec{
		DeploymentID:  testDep,
		ApplicationID: testApp, Container: "axiom-test",
		ImageRef: "busybox:latest", Port: 8080,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := stub.callsWith("create")[0]
	for _, flag := range []string{"--memory", "--cpus"} {
		for _, item := range c {
			if item == flag {
				t.Fatalf("zero limits must not set %s: %v", flag, c)
			}
		}
	}
}

func TestPortMappingParsed(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		if argv[0] == "image" {
			return okResponse(`[{"Id":"sha256:deadbeef"}]`)
		}
		return okResponse(inspectJSON("axiom-test", "busybox:latest", true, true, `{
			"8080/tcp": [{"HostIp":"127.0.0.1","HostPort":"32768"}],
			"9090/tcp": [{"HostIp":"127.0.0.1","HostPort":"32769"}]
		}`))
	}}
	a := &Adapter{Runner: stub}
	info, err := a.Inspect(context.Background(), "axiom-test")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Running || info.Status != "running" {
		t.Fatalf("running = %v status = %q", info.Running, info.Status)
	}
	if len(info.Ports) != 2 {
		t.Fatalf("ports = %+v", info.Ports)
	}
	if info.Ports[0].ContainerPort != 8080 || info.Ports[0].HostPort != 32768 {
		t.Fatalf("ports[0] = %+v", info.Ports[0])
	}
	if info.Ports[1].ContainerPort != 9090 || info.Ports[1].HostPort != 32769 {
		t.Fatalf("ports[1] = %+v", info.Ports[1])
	}
}

func TestCreatedContainerPortBindingReported(t *testing.T) {
	// A created (not started) container has the binding in HostConfig with an
	// empty HostPort; NetworkSettings.Ports is empty until start.
	jsonDoc := `[{"Id":"abc","Name":"/axiom-test","Image":"sha256:deadbeef","Config":{"Image":"busybox:latest","Labels":{"axiom.managed":"true"}},` +
		`"HostConfig":{"PortBindings":{"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":""}]}},` +
		`"State":{"Status":"created","Running":false},"NetworkSettings":{"Ports":{}}}]`
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		if argv[0] == "image" {
			return okResponse(`[{"Id":"sha256:deadbeef"}]`)
		}
		return okResponse(jsonDoc)
	}}
	a := &Adapter{Runner: stub}
	info, err := a.Inspect(context.Background(), "axiom-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Ports) != 1 || info.Ports[0].ContainerPort != 8080 || info.Ports[0].HostIP != "127.0.0.1" || info.Ports[0].HostPort != 0 {
		t.Fatalf("ports = %+v, want configured ephemeral binding with HostPort 0", info.Ports)
	}
}

func TestTail(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		if argv[0] == "logs" {
			return okResponse("line1\nline2\n")
		}
		return "", 0, nil
	}}
	a := &Adapter{Runner: stub}
	out, err := a.Tail(context.Background(), "axiom-test", 50)
	if err != nil {
		t.Fatal(err)
	}
	if out != "line1\nline2\n" {
		t.Fatalf("out = %q", out)
	}
	calls := stub.callsWith("logs")
	if len(calls) != 1 {
		t.Fatalf("logs calls = %v", stub.calls)
	}
	containsSeq(t, calls[0], "logs", "--tail", "50", "axiom-test")
}

func TestContextCancellation(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		<-ctx.Done() // block until the context is cancelled
		return "", -1, ctx.Err()
	}}
	a := &Adapter{Runner: stub}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := a.Start(ctx, "axiom-test")
	var de *Error
	if !errors.As(err, &de) || de.Code != CodeInterrupted {
		t.Fatalf("err = %v, want CodeInterrupted", err)
	}
}

func TestAdapterTimeoutPropagates(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		<-ctx.Done() // block past the adapter timeout
		return "", -1, ctx.Err()
	}}
	a := &Adapter{Runner: stub, Timeout: 30 * time.Millisecond}
	err := a.Start(context.Background(), "axiom-test")
	var de *Error
	if !errors.As(err, &de) || de.Code != CodeInterrupted {
		t.Fatalf("err = %v, want CodeInterrupted", err)
	}
}

func TestCleanupSkipsUnmanaged(t *testing.T) {
	var inspected []string
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		switch argv[0] {
		case "ps":
			return okResponse("axiom-a\naxiom-b\naxiom-c\n")
		case "inspect":
			inspected = append(inspected, argv[1])
			if argv[1] == "axiom-c" {
				// Listed but no longer managed (label race): must be skipped.
				return okResponse(inspectJSON(argv[1], "busybox:latest", false, false, "{}"))
			}
			return okResponse(inspectJSON(argv[1], "busybox:latest", true, false, "{}"))
		case "rm":
			return okResponse("")
		}
		return "", 0, nil
	}}
	a := &Adapter{Runner: stub}
	// A candidate that fails the scope check is skipped, never force-touched;
	// the rest of the cleanup proceeds.
	if err := a.Cleanup(context.Background(), testApp, testDep); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	rms := stub.callsWith("rm")
	if len(rms) != 2 {
		t.Fatalf("rm calls = %v, want exactly the two managed containers", rms)
	}
	for _, c := range rms {
		containsSeq(t, c, "rm", "-f")
		if c[2] == "axiom-c" {
			t.Fatalf("unmanaged container must never be removed: %v", c)
		}
	}
}

func TestCleanupHappyPath(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		switch argv[0] {
		case "ps":
			return okResponse("axiom-a\naxiom-b\n")
		case "inspect":
			return okResponse(inspectJSON(argv[1], "busybox:latest", true, false, "{}"))
		case "rm":
			return okResponse("")
		}
		return "", 0, nil
	}}
	a := &Adapter{Runner: stub}
	if err := a.Cleanup(context.Background(), testApp, testDep); err != nil {
		t.Fatal(err)
	}
	rms := stub.callsWith("rm")
	if len(rms) != 2 {
		t.Fatalf("rm calls = %v", rms)
	}
	// The listing query must filter on both the managed and deployment labels.
	ps := stub.callsWith("ps")
	if len(ps) != 1 {
		t.Fatalf("ps calls = %v", ps)
	}
	containsSeq(t, ps[0], "--filter", "label="+ownership.LabelManaged+"="+ownership.ManagedTrue)
	containsSeq(t, ps[0], "--filter", "label="+ownership.LabelApplication+"="+testApp)
	containsSeq(t, ps[0], "--filter", "label="+ownership.LabelDeployment+"="+testDep)
	containsSeq(t, ps[0], "--format", "{{.Names}}")
}

func TestCleanupInvalidDeployment(t *testing.T) {
	stub := &stubRunner{fn: func(ctx context.Context, argv []string) (string, int, error) {
		t.Fatalf("runner must not be called: %v", argv)
		return "", 0, nil
	}}
	a := &Adapter{Runner: stub}
	if err := a.Cleanup(context.Background(), testApp, "not-a-deployment"); err == nil {
		t.Fatal("expected error for invalid deploymentId")
	}
}

func TestErrorFormat(t *testing.T) {
	err := &Error{Code: CodeNotManaged, Message: "container \"x\" is not managed by axiom", Cause: ErrNotManaged}
	if !IsNotManaged(err) {
		t.Fatal("IsNotManaged must detect wrapped ErrNotManaged")
	}
	if !errors.Is(err, ErrNotManaged) {
		t.Fatal("errors.Is must reach ErrNotManaged")
	}
	want := `runtime: RUNTIME_NOT_MANAGED: container "x" is not managed by axiom: ownership: resource does not belong to Axiom: not labelled axiom.managed=true`
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}
