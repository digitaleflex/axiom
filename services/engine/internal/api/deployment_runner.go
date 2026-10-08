package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/digitaleflex/axiom/services/engine/internal/build"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/executor"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
)

// DeploymentRunner starts the background execution of a deployment
// (issue #100). It is satisfied by *executor.Runner; the API depends on this
// narrow interface so a test can inject a recording fake and prove the
// handler really triggers execution.
type DeploymentRunner interface {
	// Start launches the execution in the background. It must not block the
	// caller's request.
	Start(ctx context.Context, req executor.Request) error
}

// SourceFetcher streams the source archive of an exact commit. It is
// satisfied by *githubrepos.Service (repos.Service.Archive).
type SourceFetcher interface {
	Archive(ctx context.Context, userID, repoID, sha string) (io.ReadCloser, error)
}

// Executors turns a persisted deployment into an execution request and hands
// it to the runner. The API keeps this seam narrow so the handler stays a
// translation layer and no package constructs its own dependencies (#114).
type Executors struct {
	Runner  DeploymentRunner
	Sources SourceFetcher
	// Deployments is used only to record a terminal failure when an execution
	// cannot even be started; nil disables that.
	Deployments *deployment.Service
	Log         *slog.Logger
}

// Start builds the executor request for in and starts it in the background.
//
// A nil Runner means the Engine is deliberately execution-less (tests,
// read-only tooling): Start is a no-op so POST deployments still persists
// and answers 202. A nil Sources is a wiring error and is reported as one.
func (e Executors) Start(ctx context.Context, in ExecutionInput) error {
	if e.Runner == nil {
		return nil
	}
	if e.Sources == nil {
		return errors.New("deployment execution is configured without a source fetcher")
	}
	container, err := ContainerName(in.AppSlug, in.Deployment.ID)
	if err != nil {
		return fmt.Errorf("derive container name: %w", err)
	}
	return e.Runner.Start(ctx, executor.Request{
		DeploymentID: in.Deployment.ID,
		// The API request ID, persisted on the deployment record at creation,
		// so every event of this execution traces back to the user's request.
		CorrelationID: in.Deployment.CorrelationID,
		AppSlug:       in.AppSlug,
		Container:     container,
		Source:        &archiveSource{src: e.Sources, userID: in.UserID, repoID: in.Plan.Source.RepositoryID, sha: in.Plan.Source.Commit},
		Plan:          in.Plan,
	})
}

// FailUnstartable marks a deployment that was persisted but cannot execute.
// It exists because a stuck PENDING record is indistinguishable from the
// never-wired bug this seam closes: leaving it pending would hide the
// failure forever.
//
// TODO(#100): the API contract (§18) has no error class for "execution could
// not be started". Until it defines one, INTERNAL_ERROR is emitted rather
// than inventing a code clients would have to special-case.
func (e Executors) FailUnstartable(ctx context.Context, rec deployment.Record, cause error) {
	log := e.Log
	if log == nil {
		log = slog.Default()
	}
	log.Error("starting deployment execution failed",
		"deploymentId", rec.ID, "correlationId", rec.CorrelationID, "error", cause.Error())
	if e.Deployments == nil {
		return
	}
	// Detached from the request: the record outlives the HTTP call.
	if _, err := e.Deployments.Fail(context.WithoutCancel(ctx), rec.ID, CodeInternalError); err != nil {
		log.Error("marking unstartable deployment failed", "deploymentId", rec.ID, "error", err.Error())
	}
}

// ExecutionInput is what the handler resolves for one deployment before
// handing it to the runner.
type ExecutionInput struct {
	Deployment deployment.Record
	Plan       planner.Plan
	AppSlug    string
	UserID     string
}

// archiveSource adapts a repository archive to build.Source (#98). The
// archive is fetched lazily, inside the build step, never before.
type archiveSource struct {
	src    SourceFetcher
	userID string
	repoID string
	sha    string
}

var _ build.Source = (*archiveSource)(nil)

func (a *archiveSource) Archive(ctx context.Context) (io.ReadCloser, error) {
	if a.repoID == "" || a.sha == "" {
		return nil, fmt.Errorf("%w: plan carries no repository and commit to build", deployment.ErrInvalidInput)
	}
	return a.src.Archive(ctx, a.userID, a.repoID, a.sha)
}

// ContainerName returns the DNS-safe runtime container name for an
// application slug within a deployment: axiom-<slug>-<short deployment id>.
//
// It mirrors the agent's own naming (services/agent/internal/security/
// ownership.ContainerName, #89) so both sides agree without sharing a package,
// and stays within the agent's ValidateName rules (lowercase, DNS-label safe,
// at most 63 characters).
func ContainerName(appSlug, deploymentID string) (string, error) {
	slug := strings.Trim(sanitizeSlug(strings.ToLower(strings.TrimSpace(appSlug))), "-")
	if slug == "" {
		return "", errors.New("container name: application slug is empty")
	}
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	suffix := strings.TrimPrefix(deploymentID, "dep_")
	if len(suffix) > 12 {
		suffix = suffix[len(suffix)-12:]
	}
	if suffix == "" {
		return "", errors.New("container name: deployment id is empty")
	}
	name := "axiom-" + slug + "-" + suffix
	if len(name) > 63 {
		return "", fmt.Errorf("container name: %q exceeds 63 characters", name)
	}
	return name, nil
}

func sanitizeSlug(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.', r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}
