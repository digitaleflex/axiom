package build

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/planner"
)

func composePlan(services []string, public string) planner.Plan {
	p := nextPlan()
	p.Strategy = "compose"
	p.Build.Strategy = "compose"
	p.Runtime.Type = "compose"
	p.Runtime.Services = services
	p.Network.PublicService = public
	return p
}

func TestBuildComposeSingleServiceEmitsLabeledImage(t *testing.T) {
	fb := &fakeBuilder{}
	eng, logs, root := testEngine(t, fb)
	files := map[string]string{
		"compose.yaml": "services:\n  web:\n    build: .\n    ports:\n      - \"3000\"\n",
		"Dockerfile":   "FROM alpine\n",
	}
	res, err := eng.Build(context.Background(), Input{
		DeploymentID: "dep_1", ApplicationID: "app_1", AppSlug: "Acme", Commit: commit,
		Plan: composePlan(nil, ""), Source: fakeSource{data: archive(files)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ImageRef != "axiom-acme-web:"+commit {
		t.Fatalf("image = %q", res.ImageRef)
	}
	if res.Artifact.Strategy != "compose" || res.Artifact.Image != res.ImageRef {
		t.Fatalf("artifact = %+v", res.Artifact)
	}
	if len(fb.specs) != 1 {
		t.Fatalf("builds = %d", len(fb.specs))
	}
	spec := fb.specs[0]
	if spec.Tags[0] != res.ImageRef {
		t.Fatalf("tag = %v", spec.Tags)
	}
	for k, want := range map[string]string{
		"axiom.deployment":  "dep_1",
		"axiom.commit":      commit,
		"axiom.strategy":    "compose",
		"axiom.service":     "web",
		"axiom.application": "app_1",
	} {
		if spec.Labels[k] != want {
			t.Fatalf("label %s = %q, want %q", k, spec.Labels[k], want)
		}
	}
	// Source checkout, build message, built message.
	if len(logs.msgs) != 3 {
		t.Fatalf("logs = %v", logs.msgs)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatal("workspace leaked")
	}
}

func TestBuildComposeMultiServiceStaysPending(t *testing.T) {
	fb := &fakeBuilder{}
	eng, _, _ := testEngine(t, fb)
	files := map[string]string{
		"compose.yaml": "services:\n  web:\n    build: .\n    ports:\n      - \"3000\"\n  worker:\n    build: ./worker\n",
		"Dockerfile":   "FROM alpine\n",
	}
	_, err := eng.Build(context.Background(), Input{
		DeploymentID: "dep_1", AppSlug: "acme", Commit: commit,
		Plan: composePlan(nil, ""), Source: fakeSource{data: archive(files)},
	})
	if !isCode(err, CodeComposePending) {
		t.Fatalf("multi-service compose must stay pending: %v", err)
	}
	// The refusal must happen before any image is built: no half-built compose.
	if len(fb.specs) != 0 {
		t.Fatalf("no image may be built for a multi-service compose, got %d", len(fb.specs))
	}
	if !strings.Contains(err.Error(), "axiom-acme-web:"+commit) {
		t.Fatalf("pending error must carry the planned images: %v", err)
	}
}

func TestBuildComposePlanSelectionDrivesBuild(t *testing.T) {
	fb := &fakeBuilder{}
	eng, _, _ := testEngine(t, fb)
	files := map[string]string{
		"compose.yaml": "services:\n  web:\n    build: .\n    ports:\n      - \"3000\"\n    depends_on:\n      - db\n  db:\n    image: postgres:16\n",
		"Dockerfile":   "FROM alpine\n",
	}
	// The plan selects web; web depends_on db, so selection closure pulls db in
	// and the compose becomes multi-service.
	_, err := eng.Build(context.Background(), Input{
		DeploymentID: "dep_1", AppSlug: "acme", Commit: commit,
		Plan: composePlan([]string{"web"}, "web"), Source: fakeSource{data: archive(files)},
	})
	if !isCode(err, CodeComposePending) {
		t.Fatalf("web+db compose must stay pending: %v", err)
	}
	if len(fb.specs) != 0 {
		t.Fatalf("no image may be built, got %d", len(fb.specs))
	}
}

func TestBuildComposeMissingFile(t *testing.T) {
	eng, _, _ := testEngine(t, &fakeBuilder{})
	_, err := eng.Build(context.Background(), Input{
		DeploymentID: "dep_1", AppSlug: "acme", Commit: commit,
		Plan: composePlan(nil, ""), Source: fakeSource{data: archive(map[string]string{"main.go": "package main"})},
	})
	if !isCode(err, CodeComposeFileMissing) {
		t.Fatalf("missing compose file: %v", err)
	}
}

func TestBuildComposeRejectsPolicyViolation(t *testing.T) {
	eng, _, _ := testEngine(t, &fakeBuilder{})
	files := map[string]string{
		"compose.yaml": "services:\n  web:\n    build: .\n    privileged: true\n",
		"Dockerfile":   "FROM alpine\n",
	}
	_, err := eng.Build(context.Background(), Input{
		DeploymentID: "dep_1", AppSlug: "acme", Commit: commit,
		Plan: composePlan(nil, ""), Source: fakeSource{data: archive(files)},
	})
	if !isCode(err, CodeComposeInvalid) {
		t.Fatalf("privileged service must be rejected: %v", err)
	}
	if !strings.Contains(err.Error(), "COMPOSE_PRIVILEGED") {
		t.Fatalf("error must carry the rule code: %v", err)
	}
}

func TestBuildComposeContextEscapeRejected(t *testing.T) {
	eng, _, _ := testEngine(t, &fakeBuilder{})
	files := map[string]string{
		"compose.yaml": "services:\n  web:\n    build: ../outside\n",
		"Dockerfile":   "FROM alpine\n",
	}
	_, err := eng.Build(context.Background(), Input{
		DeploymentID: "dep_1", AppSlug: "acme", Commit: commit,
		Plan: composePlan(nil, ""), Source: fakeSource{data: archive(files)},
	})
	if !isCode(err, CodeComposeInvalid) {
		t.Fatalf("escaping build context must be rejected: %v", err)
	}
}

func TestBuildComposeSubdirectoryContext(t *testing.T) {
	fb := &fakeBuilder{}
	eng, _, _ := testEngine(t, fb)
	files := map[string]string{
		"compose.yaml":   "services:\n  web:\n    build: ./web\n    ports:\n      - \"3000\"\n",
		"web/Dockerfile": "FROM alpine\n",
	}
	res, err := eng.Build(context.Background(), Input{
		DeploymentID: "dep_1", AppSlug: "acme", Commit: commit,
		Plan: composePlan(nil, ""), Source: fakeSource{data: archive(files)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ImageRef != "axiom-acme-web:"+commit {
		t.Fatalf("image = %q", res.ImageRef)
	}
	if !strings.HasSuffix(fb.specs[0].ContextDir, "/web") || fb.specs[0].Dockerfile != "Dockerfile" {
		t.Fatalf("spec = %+v", fb.specs[0])
	}
}

func TestBuildComposeMissingServiceDockerfile(t *testing.T) {
	eng, _, _ := testEngine(t, &fakeBuilder{})
	files := map[string]string{
		"compose.yaml": "services:\n  web:\n    build: .\n    ports:\n      - \"3000\"\n",
	}
	_, err := eng.Build(context.Background(), Input{
		DeploymentID: "dep_1", AppSlug: "acme", Commit: commit,
		Plan: composePlan(nil, ""), Source: fakeSource{data: archive(files)},
	})
	if !isCode(err, CodeNoDockerfile) {
		t.Fatalf("missing service Dockerfile: %v", err)
	}
}
