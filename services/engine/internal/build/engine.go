// Package build turns a validated deployment plan into a traceable container
// image artifact (issues #61, #99). Source is fetched as the archive of an
// exact commit into an isolated workspace (#98); images are built through the
// ImageBuilder boundary; every success returns immutable artifact metadata and
// every failure returns a structured error that blocks deployment.
package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/build/workspace"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
)

// Structured error codes. The executor maps them to the API BUILD_FAILED class.
const (
	CodeInvalidInput   = "BUILD_INVALID_INPUT"
	CodeSourceFailed   = "BUILD_SOURCE_FAILED"
	CodeNoDockerfile   = "BUILD_NO_DOCKERFILE"
	CodeComposePending = "BUILD_COMPOSE_PENDING"
	CodeBuildFailed    = "BUILD_FAILED"
	CodeInterrupted    = "BUILD_INTERRUPTED"
	CodeInternal       = "BUILD_INTERNAL"
)

// Error is a structured build failure.
type Error struct {
	Code     string
	Message  string
	ExitCode int
	Log      string
	Cause    error
}

func (e *Error) Error() string {
	if e.ExitCode != 0 {
		return fmt.Sprintf("%s (exit %d): %s", e.Code, e.ExitCode, e.Message)
	}
	return e.Code + ": " + e.Message
}
func (e *Error) Unwrap() error { return e.Cause }

// Source fetches the source archive of the exact commit being built.
type Source interface {
	Archive(ctx context.Context) (io.ReadCloser, error)
}

// Logger receives build lifecycle events.
type Logger interface {
	Log(ctx context.Context, event LogEvent)
}

// LogEvent is one build log entry.
type LogEvent struct {
	DeploymentID string
	Level        string // INFO | ERROR
	Step         string // BUILD
	Message      string
}

// Input describes one image build.
type Input struct {
	DeploymentID  string
	ApplicationID string
	AppSlug       string // used for image naming; sanitized
	Commit        string // exact 40-char SHA
	Plan          planner.Plan
	Registry      string // default "axiom-local"
	Source        Source
}

// Artifact is the immutable, traceable output of a successful build.
type Artifact struct {
	Image          string `json:"image"`
	Digest         string `json:"digest"` // image ID sha256:…
	Commit         string `json:"commit"`
	DeploymentID   string `json:"deploymentId"`
	PlanID         string `json:"planId"`
	Strategy       string `json:"strategy"`
	DockerfileHash string `json:"dockerfileHash"`
	DurationMs     int64  `json:"durationMs"`
}

// Result is a completed build.
type Result struct {
	ImageRef   string
	ArtifactID string
	Artifact   Artifact
	ExitCode   int
}

// Engine orchestrates workspace → source → Dockerfile → image.
type Engine struct {
	Workspaces *workspace.Manager
	Builder    ImageBuilder
	Log        Logger
	Now        func() time.Time
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)
var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now().UTC()
}

// Build runs the image build pipeline. The workspace is always cleaned up,
// on success and on failure.
func (e *Engine) Build(ctx context.Context, in Input) (Result, error) {
	if e.Workspaces == nil || e.Builder == nil || in.Source == nil {
		return Result{}, &Error{Code: CodeInvalidInput, Message: "build engine dependencies are not configured"}
	}
	if in.DeploymentID == "" || !shaRe.MatchString(in.Commit) {
		return Result{}, &Error{Code: CodeInvalidInput, Message: "deployment ID and an exact 40-character commit SHA are required"}
	}
	if in.Plan.Build.Strategy == "compose" {
		return Result{}, &Error{Code: CodeComposePending,
			Message: "Compose service builds are resolved by the Compose preset (#123); single-image builds cannot represent them"}
	}
	ws, err := e.Workspaces.Create(ctx)
	if err != nil {
		return Result{}, &Error{Code: CodeInternal, Message: "create build workspace", Cause: err}
	}
	defer ws.Cleanup() // deterministic cleanup in every path

	e.log(ctx, in.DeploymentID, "INFO", "fetching source at "+in.Commit[:7])
	rc, err := in.Source.Archive(ctx)
	if err != nil {
		return Result{}, &Error{Code: CodeSourceFailed, Message: "fetch source archive", Cause: err}
	}
	_, err = ws.ExtractTarGz(ctx, rc, workspace.DefaultLimits)
	_ = rc.Close()
	if err != nil {
		return Result{}, wrapSourceError(err)
	}

	dockerfile, generated, err := e.resolveDockerfile(ws.Dir, in.Plan)
	if err != nil {
		return Result{}, err
	}
	e.log(ctx, in.DeploymentID, "INFO", describeDockerfile(dockerfile, generated))

	tag := imageTag(in.Registry, in.AppSlug, in.Commit, in.DeploymentID)
	start := e.now()
	res, err := e.Builder.Build(ctx, BuildSpec{
		ContextDir: ws.Dir,
		Dockerfile: dockerfile,
		Tags:       []string{tag},
		Labels: map[string]string{
			"axiom.deployment": in.DeploymentID,
			"axiom.commit":     in.Commit,
			"axiom.strategy":   in.Plan.Build.Strategy,
		},
	})
	if err != nil {
		var be *Error
		if errors.As(err, &be) {
			return Result{}, be
		}
		return Result{}, &Error{Code: CodeBuildFailed, Message: "image build failed", Cause: err}
	}
	duration := res.DurationMs
	if duration == 0 {
		duration = e.now().Sub(start).Milliseconds()
	}
	dockerfileContent, _ := os.ReadFile(filepath.Join(ws.Dir, filepath.FromSlash(dockerfile)))
	sum := sha256.Sum256(dockerfileContent)
	artifact := Artifact{
		Image: tag, Digest: res.ImageID, Commit: in.Commit, DeploymentID: in.DeploymentID,
		PlanID: in.Plan.ID, Strategy: in.Plan.Build.Strategy,
		DockerfileHash: "sha256:" + hex.EncodeToString(sum[:]), DurationMs: duration,
	}
	e.log(ctx, in.DeploymentID, "INFO", "built "+tag+" ("+shortDigest(res.ImageID)+")")
	return Result{ImageRef: tag, ArtifactID: "artifact:" + tag, Artifact: artifact}, nil
}

// resolveDockerfile returns the Dockerfile path relative to the workspace.
// Repository Dockerfiles win; source strategies generate one from the preset template.
func (e *Engine) resolveDockerfile(dir string, plan planner.Plan) (string, bool, error) {
	if plan.Build.Strategy == "dockerfile" {
		for _, candidate := range []string{"Dockerfile", "docker/Dockerfile", "deploy/Dockerfile"} {
			if st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(candidate))); err == nil && !st.IsDir() {
				return candidate, false, nil
			}
		}
		return "", false, &Error{Code: CodeNoDockerfile, Message: "strategy is dockerfile but no Dockerfile was found in the repository"}
	}
	content, err := dockerfileForPreset(plan.Strategy, plan.Build.Command, plan.Runtime.StartCommand, plan.Runtime.Port, plan.Build.OutputDir)
	if err != nil {
		return "", false, &Error{Code: CodeInvalidInput, Message: err.Error()}
	}
	const generated = ".axiom/Dockerfile"
	if err := os.MkdirAll(filepath.Join(dir, ".axiom"), 0o755); err != nil {
		return "", false, &Error{Code: CodeInternal, Message: "create generated Dockerfile directory", Cause: err}
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(generated)), []byte(content), 0o644); err != nil {
		return "", false, &Error{Code: CodeInternal, Message: "write generated Dockerfile", Cause: err}
	}
	return generated, true, nil
}

func wrapSourceError(err error) *Error {
	switch {
	case errors.Is(err, workspace.ErrTooLarge), errors.Is(err, workspace.ErrTooMany):
		return &Error{Code: CodeSourceFailed, Message: "source exceeds build limits: " + err.Error(), Cause: err}
	case errors.Is(err, workspace.ErrMalformed), errors.Is(err, workspace.ErrUnsafePath), errors.Is(err, workspace.ErrEmpty):
		return &Error{Code: CodeSourceFailed, Message: "source archive is unusable: " + err.Error(), Cause: err}
	default:
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return &Error{Code: CodeInterrupted, Message: "build interrupted", Cause: err}
		}
		return &Error{Code: CodeSourceFailed, Message: "extract source archive", Cause: err}
	}
}

func (e *Engine) log(ctx context.Context, deploymentID, level, msg string) {
	if e.Log != nil {
		e.Log.Log(ctx, LogEvent{DeploymentID: deploymentID, Level: level, Step: "BUILD", Message: msg})
	}
}

func describeDockerfile(path string, generated bool) string {
	if generated {
		return "using generated Dockerfile (" + path + ")"
	}
	return "using repository " + path
}

// imageTag builds a valid, unique tag: <registry>/<slug>:<commit7>-<dep8>.
func imageTag(registry, slug, commit, deploymentID string) string {
	if registry == "" {
		registry = "axiom-local"
	}
	slug = strings.ToLower(slugRe.ReplaceAllString(strings.ToLower(slug), "-"))
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "app"
	}
	if len(slug) > 64 {
		slug = slug[:64]
	}
	dep := strings.TrimPrefix(deploymentID, "dep_")
	if len(dep) > 8 {
		dep = dep[len(dep)-8:]
	}
	return registry + "/" + slug + ":" + commit[:7] + "-" + dep
}

func shortDigest(d string) string {
	if len(d) > 19 {
		return d[:19]
	}
	return d
}
