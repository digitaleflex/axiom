package build

import (
	"context"
	"testing"
)

type fakeSource struct{ checkedOut bool }
func (f *fakeSource) Checkout(context.Context, string, string, string) error { f.checkedOut = true; return nil }

type fakeRunner struct{ called bool }
func (f *fakeRunner) BuildImage(context.Context, string, string) (int, error) { f.called = true; return 0, nil }

func TestBuildEngine(t *testing.T) {
	source := &fakeSource{}
	runner := &fakeRunner{}
	engine := New(source, runner, nil)

	result, err := engine.Build(context.Background(), Request{
		DeploymentID: "dep_1", Repository: "owner/app", Ref: "main",
		WorkDir: "/tmp/build", Image: "axiom/app:dep_1",
	})
	if err != nil { t.Fatal(err) }
	if !source.checkedOut || !runner.called || result.ExitCode != 0 {
		t.Fatalf("unexpected build result: %+v", result)
	}
}

func TestBuildEngineRejectsMissingDependencies(t *testing.T) {
	_, err := New(nil, nil, nil).Build(context.Background(), Request{
		DeploymentID: "dep_1", Repository: "owner/app", Ref: "main",
		WorkDir: "/tmp/build", Image: "axiom/app:dep_1",
	})
	if err == nil { t.Fatal("expected dependency error") }
}
