package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/build/workspace"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
)

var commit = strings.Repeat("a", 40)

func archive(files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "repo/", Typeflag: tar.TypeDir, Mode: 0o755})
	for p, c := range files {
		_ = tw.WriteHeader(&tar.Header{Name: "repo/" + p, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(c))})
		_, _ = tw.Write([]byte(c))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

type fakeSource struct {
	data []byte
	err  error
}

func (f fakeSource) Archive(context.Context) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(bytes.NewReader(f.data)), nil
}

type fakeBuilder struct {
	specs []BuildSpec
	err   error
}

func (f *fakeBuilder) Build(_ context.Context, spec BuildSpec) (ImageResult, error) {
	f.specs = append(f.specs, spec)
	if f.err != nil {
		return ImageResult{}, f.err
	}
	content, _ := os.ReadFile(filepath.Join(spec.ContextDir, spec.Dockerfile))
	return ImageResult{ImageRef: spec.Tags[0], ImageID: "sha256:" + strings.Repeat("d", 64), DurationMs: 42, Log: string(content[:min(64, len(content))])}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type logSink struct{ msgs []string }

func (l *logSink) Log(_ context.Context, e LogEvent) { l.msgs = append(l.msgs, e.Level+":"+e.Message) }

func testEngine(t *testing.T, builder ImageBuilder) (*Engine, *logSink, string) {
	t.Helper()
	root := t.TempDir()
	logs := &logSink{}
	return &Engine{Workspaces: &workspace.Manager{Root: root}, Builder: builder, Log: logs}, logs, root
}

func nextPlan() planner.Plan {
	return planner.Plan{
		ID: "plan_1", ApplicationID: "app_1", Strategy: "nextjs",
		Source:   profile.Source{RepositoryID: "repo_1", Ref: "main", Commit: commit},
		Build:    planner.BuildPlan{Strategy: "source", PackageManager: "pnpm", Command: "pnpm run build"},
		Runtime:  planner.RuntimePlan{Type: "node", StartCommand: "pnpm start", Port: 3000},
		Network:  planner.NetworkPlan{Proxy: "traefik", Domain: "app.example.com", TLS: true, ExposedPort: 3000},
		Health:   planner.HealthPlan{Type: "http", Path: "/", TimeoutSeconds: 5},
		Rollback: planner.RollbackPlan{Strategy: "keep_previous_until_verified"},
		Steps:    []string{"BUILD", "CREATE_RUNTIME", "NETWORK", "START", "VERIFY"},
	}
}

func TestBuildSourcePresetGeneratesDockerfile(t *testing.T) {
	eng, logs, root := testEngine(t, &fakeBuilder{})
	files := map[string]string{"package.json": `{"scripts":{"build":"x"}}`}
	res, err := eng.Build(context.Background(), Input{
		DeploymentID: "dep_12345678901234567890", AppSlug: "Acme Web!", Commit: commit,
		Plan: nextPlan(), Source: fakeSource{data: archive(files)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ImageRef != "axiom-local/acme-web:"+commit[:7]+"-34567890" {
		t.Fatalf("image = %s", res.ImageRef)
	}
	a := res.Artifact
	if a.Digest != "sha256:"+strings.Repeat("d", 64) || a.Commit != commit || a.DeploymentID != "dep_12345678901234567890" ||
		a.PlanID != "plan_1" || a.Strategy != "source" || a.DurationMs != 42 || !strings.HasPrefix(a.DockerfileHash, "sha256:") {
		t.Fatalf("artifact = %+v", a)
	}
	if res.ArtifactID != "artifact:"+res.ImageRef {
		t.Fatalf("artifact id = %s", res.ArtifactID)
	}
	if len(logs.msgs) != 3 {
		t.Fatalf("logs = %v", logs.msgs)
	}
	// Workspace cleaned up.
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatalf("workspace leaked %d entries", len(entries))
	}
}

func TestBuildDockerfileStrategyUsesRepositoryDockerfile(t *testing.T) {
	fb := &fakeBuilder{}
	eng, _, _ := testEngine(t, fb)
	plan := nextPlan()
	plan.Strategy = "go"
	plan.Build.Strategy = "dockerfile"
	files := map[string]string{"Dockerfile": "FROM alpine\n", "main.go": "package main"}
	if _, err := eng.Build(context.Background(), Input{DeploymentID: "dep_1", Commit: commit, Plan: plan, Source: fakeSource{data: archive(files)}}); err != nil {
		t.Fatal(err)
	}
	if fb.specs[0].Dockerfile != "Dockerfile" {
		t.Fatalf("dockerfile = %s", fb.specs[0].Dockerfile)
	}
	if fb.specs[0].Labels["axiom.deployment"] != "dep_1" || fb.specs[0].Labels["axiom.commit"] != commit {
		t.Fatalf("labels = %v", fb.specs[0].Labels)
	}
}

func TestBuildFailuresAreStructured(t *testing.T) {
	eng, _, _ := testEngine(t, &fakeBuilder{})
	if _, err := eng.Build(context.Background(), Input{}); !isCode(err, CodeInvalidInput) {
		t.Fatalf("empty input: %v", err)
	}
	bad := nextPlan()
	bad.Build.Strategy = "dockerfile"
	if _, err := eng.Build(context.Background(), Input{DeploymentID: "dep_1", Commit: commit, Plan: bad, Source: fakeSource{data: archive(map[string]string{"app.js": "x"})}}); !isCode(err, CodeNoDockerfile) {
		t.Fatalf("missing Dockerfile: %v", err)
	}
	compose := nextPlan()
	compose.Build.Strategy = "compose"
	if _, err := eng.Build(context.Background(), Input{DeploymentID: "dep_1", Commit: commit, Plan: compose,
		Source: fakeSource{data: archive(map[string]string{"compose.yaml": "services:\n  web:\n    image: nginx\n"})}}); !isCode(err, CodeComposeNoBuild) {
		t.Fatalf("compose: %v", err)
	}
	eng, _, root := testEngine(t, &fakeBuilder{err: &Error{Code: CodeBuildFailed, Message: "boom", ExitCode: 2}})
	if _, err := eng.Build(context.Background(), Input{DeploymentID: "dep_1", Commit: commit, Plan: nextPlan(), Source: fakeSource{data: archive(map[string]string{"p": "x"})}}); !isCode(err, CodeBuildFailed) {
		t.Fatalf("builder failure: %v", err)
	} else if !strings.Contains(err.Error(), "exit 2") {
		t.Fatalf("exit code missing: %v", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("failed builds must clean up the workspace")
	}
	eng, _, _ = testEngine(t, &fakeBuilder{})
	srcErr := errors.New("connection reset")
	if _, err := eng.Build(context.Background(), Input{DeploymentID: "dep_1", Commit: commit, Plan: nextPlan(), Source: fakeSource{err: srcErr}}); !isCode(err, CodeSourceFailed) {
		t.Fatalf("source failure: %v", err)
	}
}

func TestBuildInterruptionCleansUp(t *testing.T) {
	block := make(chan struct{})
	fb := &fakeBuilder{}
	_ = fb
	eng, _, root := testEngine(t, ImageBuilderFunc(func(ctx context.Context, _ BuildSpec) (ImageResult, error) {
		select {
		case <-ctx.Done():
			return ImageResult{}, ctx.Err()
		case <-block:
			return ImageResult{}, nil
		}
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := eng.Build(ctx, Input{DeploymentID: "dep_1", Commit: commit, Plan: nextPlan(), Source: fakeSource{data: archive(map[string]string{"p": "x"})}})
		done <- err
	}()
	cancel()
	if err := <-done; !isCode(err, CodeInterrupted) && !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted: %v", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("interrupted builds must clean up the workspace")
	}
}

func isCode(err error, code string) bool {
	var be *Error
	return errors.As(err, &be) && be.Code == code
}

// ImageBuilderFunc adapts a function to ImageBuilder.
type ImageBuilderFunc func(context.Context, BuildSpec) (ImageResult, error)

func (f ImageBuilderFunc) Build(ctx context.Context, spec BuildSpec) (ImageResult, error) {
	return f(ctx, spec)
}
