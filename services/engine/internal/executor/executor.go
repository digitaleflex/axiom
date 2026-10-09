package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/build"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/health"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
	"github.com/digitaleflex/axiom/services/engine/internal/policy"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// Error codes recorded on failure (docs/architecture/api-contract.md §18).
const (
	ErrorBuildFailed       = "BUILD_FAILED"
	ErrorRuntimeFailed     = "RUNTIME_FAILED"
	ErrorHealthCheckFailed = "HEALTH_CHECK_FAILED"
	ErrorNotEligible       = "DEPLOYMENT_NOT_ELIGIBLE"
	ErrorPolicyDenied      = "POLICY_DENIED"
)

// Step timeouts. VERIFY is computed from the plan health policy; the rest are
// fixed outer bounds (builds enforce their own timeout inside).
var (
	BuildTimeout         = 20 * time.Minute
	CreateRuntimeTimeout = 3 * time.Minute
	NetworkTimeout       = 3 * time.Minute
	StartTimeout         = 2 * time.Minute
)

// Attempts per step. Builds are not retried (expensive, usually deterministic
// failures); runtime operations tolerate one transient failure; verification
// polls through short executor-level retries while the agent owns the
// probe loop (#85).
var Attempts = map[string]int{
	"BUILD": 1, "CREATE_RUNTIME": 2, "NETWORK": 2, "START": 2, "VERIFY": 3,
}

func New(deployments *deployment.Service, builder BuildRunner, agent RuntimeAgent) *PlanExecutor {
	return &PlanExecutor{
		deployments: deployments, builder: builder, agent: agent,
		Log: slog.Default(), Backoff: ExponentialBackoff, Timeouts: nil,
	}
}

// Execute runs a plan for an existing PENDING deployment. Steps and state
// transitions are persisted through the deployment service; a failed step
// stops all downstream steps; LIVE is recorded only after VERIFY succeeds.
// Architecture choice (ADR-0008, NAT vs loopback):
//   - In NAT deployment (agent initiates all communication over public HTTPS),
//     the executor pushes operations to agentpoll.Manager (file d'attente par
//     agentID) instead of calling agentclient directly. The agent then pulls
//     via POST /api/v1/agent/poll (long-poll, 25s max) and pushes results
//     via POST /api/v1/agent/result.
//   - In loopback mode (agent co-localisée avec l'Engine), agentclient
//     reste intact : il parle directement au endpoint agent local.
//   - Le mécanisme de redirection conditionnel (NAT vs loopback) est documenté
//     ici : si AgentPoll est non-nil dans le Deps, le mode NAT est activé ;
//     sinon, agentclient traite directement la requête.
func (e *PlanExecutor) Execute(ctx context.Context, req Request) (Result, error) {
	if e.deployments == nil || e.builder == nil || e.agent == nil {
		return Result{}, errors.New("deployment service, build runner and runtime agent are required")
	}
	if req.DeploymentID == "" || req.Plan.ID == "" || req.Plan.ServerID == "" || req.Source == nil {
		return Result{}, errors.New("deploymentID, plan ID, server ID and source are required")
	}
	id := req.DeploymentID
	corr := req.CorrelationID
	if corr == "" {
		corr = deployment.NewID("req")
	}
	log := e.log().With("deploymentId", id, "correlationId", corr)

	for _, s := range []deployment.State{deployment.StateAnalyzing, deployment.StatePlanning} {
		if _, err := e.deployments.Transition(ctx, id, s); err != nil {
			return Result{}, err
		}
	}

	// Eligibility pre-flight: the server may have changed since plan review.
	if e.Servers != nil {
		if err := e.checkServer(ctx, req.Plan); err != nil {
			_, _ = e.deployments.Transition(ctx, id, deployment.StateBuilding)
			return e.fail(ctx, id, ErrorNotEligible, err)
		}
	}

	// Policy gate: no operation is dispatched unless the plan is authorized.
	// The check runs after eligibility so denials name the operative reason.
	if decision := policy.Evaluate(req.Plan, req.Plan.ServerID); !decision.Allow {
		_, _ = e.deployments.Transition(ctx, id, deployment.StateBuilding)
		log.Warn("deployment denied by policy", "reasons", decision.Reasons)
		return e.fail(ctx, id, ErrorPolicyDenied, errors.New("deployment denied by policy: "+strings.Join(decision.Reasons, "; ")))
	}

	if _, err := e.deployments.Transition(ctx, id, deployment.StateBuilding); err != nil {
		return Result{}, err
	}

	var built build.Result
	if err := e.step(ctx, log, id, "BUILD", ErrorBuildFailed, e.timeout("BUILD", req.Plan), func(ctx context.Context, attempt int) error {
		var err error
		built, err = e.builder.Build(ctx, build.Input{
			DeploymentID: id, ApplicationID: req.Plan.ApplicationID, AppSlug: req.AppSlug,
			Commit: req.Plan.Source.Commit, Plan: req.Plan, Source: req.Source,
		})
		return err
	}); err != nil {
		return e.fail(ctx, id, ErrorBuildFailed, err)
	}

	if _, err := e.deployments.Transition(ctx, id, deployment.StateDeploying); err != nil {
		return Result{}, err
	}
	runtimeSteps := []struct {
		name string
		code string
		run  func(ctx context.Context, op Operation) error
	}{
		{"CREATE_RUNTIME", ErrorRuntimeFailed, func(ctx context.Context, op Operation) error {
			return e.agent.CreateRuntime(ctx, CreateRuntimeRequest{Operation: op,
				ImageRef: built.ImageRef, Container: req.Container, Port: req.Plan.Runtime.Port})
		}},
		{"NETWORK", ErrorRuntimeFailed, func(ctx context.Context, op Operation) error {
			return e.agent.ConfigureNetwork(ctx, NetworkRequest{Operation: op,
				Container: req.Container, Proxy: req.Plan.Network.Proxy, Domain: req.Plan.Network.Domain,
				TLS: req.Plan.Network.TLS, Port: req.Plan.Network.ExposedPort})
		}},
		{"START", ErrorRuntimeFailed, func(ctx context.Context, op Operation) error {
			return e.agent.StartRuntime(ctx, StartRequest{Operation: op, Container: req.Container})
		}},
	}
	for _, s := range runtimeSteps {
		s := s
		if err := e.step(ctx, log, id, s.name, s.code, e.timeout(s.name, req.Plan), func(ctx context.Context, attempt int) error {
			return s.run(ctx, newOperation(corr, id, req.Plan.ServerID, s.name, attempt))
		}); err != nil {
			return e.fail(ctx, id, s.code, err)
		}
	}

	if _, err := e.deployments.Transition(ctx, id, deployment.StateVerifying); err != nil {
		return Result{}, err
	}
	policy := health.DefaultPolicy(
		req.Plan.Health.Type, req.Plan.Health.Path, req.Plan.Health.ExpectedStatus,
		time.Duration(req.Plan.Health.TimeoutSeconds)*time.Second,
		req.Plan.Health.Retries, time.Duration(req.Plan.Health.IntervalSeconds)*time.Second)
	var lastReport health.ProbeReport
	verifyErr := e.step(ctx, log, id, "VERIFY", ErrorHealthCheckFailed, e.timeout("VERIFY", req.Plan), func(ctx context.Context, attempt int) error {
		report, err := e.agent.HealthCheck(ctx, HealthCheckRequest{
			Operation: newOperation(corr, id, req.Plan.ServerID, "VERIFY", attempt),
			Domain:    req.Plan.Network.Domain, Path: req.Plan.Health.Path, TimeoutSeconds: req.Plan.Health.TimeoutSeconds})
		if err != nil {
			return err
		}
		lastReport = report
		if status, reason := policy.Evaluate(report); status != health.StatusHealthy {
			return fmt.Errorf("health check failed: %s", reason)
		}
		return e.deployments.RecordHealth(ctx, id, policy, report)
	})
	if verifyErr != nil {
		// Persist the last probe result as health.failed so the failure is
		// queryable even when the agent never produced a report (#65).
		if err := e.deployments.RecordHealth(ctx, id, policy, lastReport); err != nil {
			log.Warn("record health failure", "deploymentId", id, "error", err.Error())
		}
		return e.fail(ctx, id, ErrorHealthCheckFailed, verifyErr)
	}

	url := ""
	if req.Plan.Network.Domain != "" {
		scheme := "http"
		if req.Plan.Network.TLS {
			scheme = "https"
		}
		url = scheme + "://" + req.Plan.Network.Domain
	}
	record, err := e.deployments.MarkLive(ctx, id, url)
	if err != nil {
		return Result{}, err
	}
	return Result{Deployment: record, ImageRef: built.ImageRef, ArtifactID: built.ArtifactID, Artifact: built.Artifact}, nil
}

// checkServer re-verifies that the plan target can receive this deployment.
func (e *PlanExecutor) checkServer(ctx context.Context, plan planner.Plan) error {
	rec, err := e.Servers.Get(ctx, plan.ServerID)
	if err != nil {
		return fmt.Errorf("server %s unavailable: %w", plan.ServerID, err)
	}
	if st := server.EffectiveStatusAt(rec, time.Now().UTC()); st != server.StatusReady && st != server.StatusDegraded {
		return fmt.Errorf("server %s is %s", rec.ID, st)
	}
	var missing []string
	for _, c := range server.RequiredCapabilities(plan.Strategy, plan.Network.Domain != "") {
		if !hasServerCapability(rec.Capabilities, c) {
			missing = append(missing, string(c))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("server %s lacks capabilities: %s", rec.ID, strings.Join(missing, ", "))
	}
	return nil
}

func hasServerCapability(caps []server.Capability, want server.Capability) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}

// newOperation builds the envelope for one attempt of a step. The operation
// ID is deterministic (deployment + step + attempt) so retried attempts are
// idempotent at the agent.
func newOperation(corr, deploymentID, serverID, step string, attempt int) Operation {
	return Operation{
		OperationID:   fmt.Sprintf("op_%s_%s_%d", deploymentID, step, attempt),
		CorrelationID: corr,
		DeploymentID:  deploymentID,
		ServerID:      serverID,
	}
}

// step records RUNNING, runs fn with per-attempt timeout and retries, then
// records COMPLETED or FAILED (with exit and error codes for diagnostics).
func (e *PlanExecutor) step(ctx context.Context, log *slog.Logger, id, name, code string, timeout time.Duration, fn func(ctx context.Context, attempt int) error) error {
	if err := e.deployments.RecordStep(ctx, id, deployment.StepChange{Name: name, Status: deployment.StepRunning}); err != nil {
		return fmt.Errorf("%s: record start: %w", name, err)
	}
	attempts := Attempts[name]
	if attempts < 1 {
		attempts = 1
	}
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		err = e.attempt(ctx, timeout, func(ctx context.Context) error { return fn(ctx, attempt) })
		if err == nil {
			return e.deployments.RecordStep(ctx, id, deployment.StepChange{Name: name, Status: deployment.StepCompleted})
		}
		if errors.Is(err, context.Canceled) || attempt == attempts || !retryable(err) {
			break
		}
		log.Info("step attempt failed, retrying", "step", name, "attempt", attempt, "error", err.Error())
		if e.Backoff != nil {
			if wait := e.Backoff(attempt); wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-ctx.Done():
					timer.Stop()
					err = ctx.Err()
				case <-timer.C:
				}
				if ctx.Err() != nil {
					err = ctx.Err()
					break
				}
			}
		}
	}
	change := deployment.StepChange{Name: name, Status: deployment.StepFailed, ErrorCode: code}
	if exit := exitCodeOf(err); exit != nil {
		change.ExitCode = exit
	}
	_ = e.deployments.RecordStep(ctx, id, change)
	return fmt.Errorf("%s: %w", name, err)
}

// attempt runs fn with the step timeout.
func (e *PlanExecutor) attempt(ctx context.Context, timeout time.Duration, fn func(context.Context) error) error {
	if timeout <= 0 {
		return fn(ctx)
	}
	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return fn(stepCtx)
}

// retryable reports whether a step error deserves another attempt.
// Cancellations and deterministic build failures (non-zero exit) are permanent.
func retryable(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	var be *build.Error
	if errors.As(err, &be) {
		return be.ExitCode == 0 && be.Code != build.CodeInterrupted
	}
	return true
}

func exitCodeOf(err error) *int {
	var be *build.Error
	if errors.As(err, &be) && be.ExitCode != 0 {
		v := be.ExitCode
		return &v
	}
	return nil
}

func (e *PlanExecutor) fail(ctx context.Context, id, code string, cause error) (Result, error) {
	if _, err := e.deployments.Fail(ctx, id, code); err != nil {
		return Result{}, fmt.Errorf("%w; marking deployment failed: %v", cause, err)
	}
	return Result{}, cause
}

// timeout returns the outer bound for one attempt of a step. VERIFY is
// derived from the plan health policy (retries × (timeout + interval) with a
// 60s margin, minimum 2m); overrides in Timeouts win for tests and tuning.
func (e *PlanExecutor) timeout(name string, plan planner.Plan) time.Duration {
	if e.Timeouts != nil {
		if d, ok := e.Timeouts[name]; ok {
			return d
		}
	}
	switch name {
	case "BUILD":
		return BuildTimeout
	case "CREATE_RUNTIME":
		return CreateRuntimeTimeout
	case "NETWORK":
		return NetworkTimeout
	case "START":
		return StartTimeout
	case "VERIFY":
		total := time.Duration(plan.Health.Retries)*time.Duration(plan.Health.TimeoutSeconds+plan.Health.IntervalSeconds)*time.Second + time.Minute
		if total < 2*time.Minute {
			total = 2 * time.Minute
		}
		return total
	default:
		return 0
	}
}

func (e *PlanExecutor) log() *slog.Logger {
	if e.Log != nil {
		return e.Log
	}
	return slog.Default()
}

// ExponentialBackoff waits 2s, 4s, 8s… capped at 30s.
func ExponentialBackoff(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

// LastHealthResult returns the newest persisted health probe result for a
// deployment by scanning deployment events (health.passed / health.failed)
// newest-first (#65). found is false when no health event exists. The event
// data is the source for GET /deployments/{id}/health (API contract §16).
func LastHealthResult(ctx context.Context, store deployment.Store, deploymentID string) (health.ProbeReport, bool, error) {
	events, err := store.Events(ctx, deploymentID, 0, 1000)
	if err != nil {
		return health.ProbeReport{}, false, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Type != health.EventPassed && ev.Type != health.EventFailed {
			continue
		}
		data := ev.Data
		return health.ProbeReport{
			StatusCode: dataInt(data, "statusCode"),
			LatencyMs:  dataInt64(data, "latencyMs"),
			Body:       dataString(data, "body"),
			CheckedAt:  dataTime(data, "checkedAt"),
			Attempt:    dataInt(data, "attempt"),
		}, true, nil
	}
	return health.ProbeReport{}, false, nil
}

// dataInt reads an integer event value, tolerating the float64 that JSON
// round-tripping produces as well as native ints.
func dataInt(data map[string]any, key string) int {
	switch v := data[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

func dataInt64(data map[string]any, key string) int64 {
	switch v := data[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func dataString(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return s
}

func dataTime(data map[string]any, key string) time.Time {
	s, _ := data[key].(string)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
