package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/digitaleflex/axiom/services/engine/internal/build/workspace"
	"github.com/digitaleflex/axiom/services/engine/internal/logs"
	"github.com/digitaleflex/axiom/services/engine/internal/runtime/presets/compose"
)

// composeFileNames are the conventional Compose file names, in precedence order.
var composeFileNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// buildCompose implements the compose build path (issue #123).
//
// Decision (documented per #123): Axiom's build artifact contract
// (docs/architecture/artifacts.md §3) is a single image reference + digest, and
// the runtime handoff (executor.CreateRuntimeRequest.ImageRef) is
// single-image. A Compose selection that resolves to exactly one service with
// a `build:` section maps cleanly onto that contract, so Axiom builds it as
// axiom-<app>-<service>:<commit> with ownership labels and returns a normal
// BuildArtifact. A multi-service selection cannot be represented by one image:
// emitting one of them would hand the executor a half-built Compose, so Axiom
// refuses and returns a structured BUILD_COMPOSE_PENDING error carrying the
// full per-service build plan. The multi-image handoff is owned by #100/#83.
func (e *Engine) buildCompose(ctx context.Context, in Input, ws *workspace.Workspace) (Result, error) {
	path, content, ok := findComposeFile(ws.Dir)
	if !ok {
		return Result{}, &Error{Code: CodeComposeFileMissing,
			Message: "compose strategy but the source defines no compose file (" + strings.Join(composeFileNames, ", ") + ")"}
	}
	doc, err := compose.Parse(content)
	if err != nil {
		return Result{}, &Error{Code: CodeComposeInvalid, Message: "compose file " + path + " is not valid: " + err.Error()}
	}
	sel := doc.SelectServices(in.Plan.Runtime.Services, in.Plan.Network.PublicService)
	if sel.HasErrors() {
		return Result{}, &Error{Code: CodeComposeInvalid,
			Message: "compose file " + path + " violates the Axiom compose policy: " + joinIssues(sel.Errors())}
	}
	bp := doc.BuildPlan(sel, in.AppSlug, in.Commit)
	if len(bp.Services) == 0 {
		return Result{}, &Error{Code: CodeComposeNoBuild,
			Message: "compose selection [" + strings.Join(sel.Services, ", ") + "] has no service with a build section"}
	}
	if len(sel.Services) != 1 {
		return Result{}, &Error{Code: CodeComposePending, Message: composePendingMessage(path, sel, bp)}
	}

	sb := bp.Services[0]
	contextDir, err := serviceContextDir(ws.Dir, sb.Context)
	if err != nil {
		return Result{}, &Error{Code: CodeComposeInvalid, Message: err.Error()}
	}
	if _, statErr := os.Stat(filepath.Join(contextDir, filepath.FromSlash(sb.Dockerfile))); statErr != nil {
		return Result{}, &Error{Code: CodeNoDockerfile,
			Message: "compose service " + sb.Service + " build context has no " + sb.Dockerfile}
	}

	e.log(ctx, in.DeploymentID, "INFO", "building compose service "+sb.Service+" ("+sb.Image+")")
	start := e.now()
	res, err := e.Builder.Build(ctx, BuildSpec{
		ContextDir: contextDir,
		Dockerfile: sb.Dockerfile,
		Tags:       []string{sb.Image},
		Labels: map[string]string{
			"axiom.deployment":  in.DeploymentID,
			"axiom.commit":      in.Commit,
			"axiom.strategy":    "compose",
			"axiom.service":     sb.Service,
			"axiom.application": in.ApplicationID,
		},
	})
	if err != nil {
		var be *Error
		if errors.As(err, &be) {
			e.persistOutput(ctx, in.DeploymentID, logs.LevelError, be.Log)
			return Result{}, be
		}
		e.log(ctx, in.DeploymentID, "ERROR", "compose image build failed: "+err.Error())
		return Result{}, &Error{Code: CodeBuildFailed, Message: "compose image build failed", Cause: err}
	}
	e.persistOutput(ctx, in.DeploymentID, logs.LevelInfo, res.Log)
	duration := res.DurationMs
	if duration == 0 {
		duration = e.now().Sub(start).Milliseconds()
	}
	dockerfileContent, _ := os.ReadFile(filepath.Join(contextDir, filepath.FromSlash(sb.Dockerfile)))
	sum := sha256.Sum256(dockerfileContent)
	artifact := Artifact{
		Image: sb.Image, Digest: res.ImageID, Commit: in.Commit, DeploymentID: in.DeploymentID,
		PlanID: in.Plan.ID, Strategy: "compose",
		DockerfileHash: "sha256:" + hex.EncodeToString(sum[:]), DurationMs: duration,
	}
	e.log(ctx, in.DeploymentID, "INFO", "built "+sb.Image+" ("+shortDigest(res.ImageID)+")")
	return Result{ImageRef: sb.Image, ArtifactID: "artifact:" + sb.Image, Artifact: artifact}, nil
}

// findComposeFile returns the first conventional compose file in dir.
func findComposeFile(dir string) (string, []byte, bool) {
	for _, name := range composeFileNames {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil {
			return name, content, true
		}
	}
	return "", nil, false
}

// serviceContextDir resolves a compose build context against the workspace and
// refuses paths that escape the repository.
func serviceContextDir(root, context string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(context))
	if clean == "." || clean == "" {
		return root, nil
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("compose build context %q must be relative to the repository", context)
	}
	dir := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("compose build context %q escapes the repository", context)
	}
	return dir, nil
}

// composePendingMessage explains exactly why a multi-service compose cannot be
// handed off yet and what would be built.
func composePendingMessage(path string, sel compose.ValidationResult, bp compose.BuildPlan) string {
	images := make([]string, 0, len(bp.Services))
	for _, b := range bp.Services {
		images = append(images, b.Service+"="+b.Image)
	}
	return fmt.Sprintf("compose file %s selects %d services [%s]; Axiom's build artifact is a single image, so emitting one would hand the executor a half-built compose. Planned images: [%s]. The multi-image compose handoff is owned by #100/#83.",
		path, len(sel.Services), strings.Join(sel.Services, ", "), strings.Join(images, ", "))
}

// joinIssues renders blocking compose issues as a stable message.
func joinIssues(issues []compose.Issue) string {
	parts := make([]string, 0, len(issues))
	for _, i := range issues {
		if i.Service != "" {
			parts = append(parts, i.Rule+" ("+i.Service+"): "+i.Message)
		} else {
			parts = append(parts, i.Rule+": "+i.Message)
		}
	}
	return strings.Join(parts, "; ")
}
