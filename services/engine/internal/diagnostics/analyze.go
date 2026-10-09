package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/executor"
	"github.com/digitaleflex/axiom/services/engine/internal/health"
	"github.com/digitaleflex/axiom/services/engine/internal/logs"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

var (
	// ErrNotFound is returned when the deployment does not exist. The API
	// maps it to 404; ownership hiding is applied by the caller
	// (loadDeployment), which already returns 404 for a deployment the
	// caller does not own.
	ErrNotFound = errors.New("deployment not found")
	// ErrLogsUnavailable is returned when no log store is wired.
	ErrLogsUnavailable = errors.New("deployment logs are unavailable")
	// ErrInvalidQuery is returned for an unknown or out-of-range query
	// parameter. The API maps it to 400 INVALID_REQUEST.
	ErrInvalidQuery = errors.New("invalid diagnostic query")
)

func upper(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Report builds the full diagnostic for one deployment. The deployment
// record must already have been loaded and authorized by the caller; this
// method is read-only and issues no write to any store.
func (s *Service) Report(ctx context.Context, rec deployment.Record) (*Report, error) {
	now := time.Now().UTC()
	rep := &Report{
		DeploymentID:  rec.ID,
		Number:        rec.Number,
		ApplicationID: rec.ApplicationID,
		PlanID:        rec.PlanID,
		Environment:   rec.Environment,
		ServerID:      rec.ServerID,
		Status:        rec.Status,
		ErrorCode:     rec.ErrorCode,
		Correlation:   Correlation{CorrelationID: rec.CorrelationID},
		Timeline: Timeline{
			CreatedAt: rec.CreatedAt, StartedAt: rec.StartedAt,
			CompletedAt: rec.CompletedAt, UpdatedAt: rec.UpdatedAt,
		},
		Steps:    []StepReport{},
		Excerpt:  []LogEntry{},
		Redacted: redactionNotice(),
	}

	steps, err := s.loadSteps(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	rep.Steps = s.stepReports(steps, now)
	rep.Timeline.DurationMs = s.durationMs(rec, now)
	rep.Timeline.LastEventAt = s.lastEventAt(steps)

	rep.Server = s.serverView(ctx, rec.ServerID)
	rep.Health = s.healthView(ctx, rec)

	s.fillExcerpt(ctx, rep, steps)
	rep.Conclusion = s.conclude(rec, rep, steps)
	return rep, nil
}

// loadSteps reads the deployment steps, bounded and order-normalised. A
// missing store yields no steps rather than failing the diagnostic.
func (s *Service) loadSteps(ctx context.Context, id string) ([]deployment.Step, error) {
	if s.steps == nil {
		return nil, nil
	}
	steps, err := s.steps.Steps(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(steps) > maxSteps {
		steps = steps[:maxSteps]
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Position < steps[j].Position })
	return steps, nil
}

// stepReports projects persisted steps into their diagnostic form, adding
// the computed duration of each step.
func (s *Service) stepReports(steps []deployment.Step, now time.Time) []StepReport {
	out := make([]StepReport, 0, len(steps))
	for _, st := range steps {
		r := StepReport{
			Name: st.Name, Position: st.Position, Status: st.Status,
			ErrorCode: st.ErrorCode, ExitCode: st.ExitCode,
			StartedAt: st.StartedAt, FinishedAt: st.CompletedAt,
			Running: st.Status == deployment.StepRunning,
		}
		r.DurationMs = spanMs(st.StartedAt, st.CompletedAt, now, r.Running)
		out = append(out, r)
	}
	return out
}

// durationMs is the deployment wall-clock duration: start to completion
// when finished, start to now while running, nil when never started.
func (s *Service) durationMs(rec deployment.Record, now time.Time) *int64 {
	return spanMs(rec.StartedAt, rec.CompletedAt, now, !rec.Status.Terminal())
}

// spanMs computes completed-started, or now-started for an in-flight span.
func spanMs(start, end *time.Time, now time.Time, inFlight bool) *int64 {
	if start == nil {
		return nil
	}
	finish := now
	if end != nil {
		finish = *end
	} else if !inFlight {
		// Neither finished nor in flight (e.g. a QUEUED step): report no
		// duration rather than an invented one.
		return nil
	}
	if finish.Before(*start) {
		return nil
	}
	ms := finish.Sub(*start).Milliseconds()
	return &ms
}

// lastEventAt resolves the most recent timestamp observable in the step
// journal. It is the cheap bound: the step journal is always present,
// whereas the event store would cost an extra read.
func (s *Service) lastEventAt(steps []deployment.Step) *time.Time {
	var newest time.Time
	for _, st := range steps {
		for _, t := range []*time.Time{st.CompletedAt, st.StartedAt} {
			if t != nil && t.After(newest) {
				newest = *t
			}
		}
	}
	if newest.IsZero() {
		return nil
	}
	return utcPtr(newest)
}

// serverView projects the target server's observed state. An unknown or
// unavailable server yields nil rather than a fake-healthy placeholder.
func (s *Service) serverView(ctx context.Context, id string) *ServerView {
	if s.server == nil || id == "" {
		return nil
	}
	rec, err := s.server.Get(ctx, id)
	if err != nil {
		return nil
	}
	caps := rec.Capabilities
	if caps == nil {
		caps = []server.Capability{}
	}
	return &ServerView{
		ID: rec.ID, Name: rec.Name, Status: rec.Status,
		AgentVersion: rec.AgentVersion, Capabilities: caps,
		CPUCount: rec.CPUCount, MemoryMB: rec.MemoryMB, DiskFreeMB: rec.DiskFreeMB,
		LastSeenAt: rec.LastSeenAt,
		Reachable:  rec.Status == server.StatusReady,
	}
}

// healthView projects the last persisted verification probe, applying the
// same rule as GET /deployments/{id}/health: LIVE implies a passed
// verification, an explicit HEALTH_CHECK_FAILED failure implies unhealthy,
// and anything else without probe data is UNKNOWN — never an implied pass.
func (s *Service) healthView(ctx context.Context, rec deployment.Record) *HealthView {
	v := &HealthView{Status: "UNKNOWN", Deployment: string(rec.Status)}
	if s.probe != nil {
		if report, found, err := s.probe(ctx, rec.ID); err == nil && found {
			v.CheckedAt = utcPtr(report.CheckedAt)
			v.Attempt = report.Attempt
			v.HTTP = &HTTPProbe{StatusCode: report.StatusCode, LatencyMs: report.LatencyMs}
			v.Body = safeProbeBody(report.Body)
		}
	}
	switch {
	case rec.Status == deployment.StateLive:
		v.Status = health.StatusHealthy
	case rec.Status == deployment.StateFailed && rec.ErrorCode == executor.ErrorHealthCheckFailed:
		v.Status = health.StatusUnhealthy
	case v.CheckedAt != nil && v.HTTP != nil && v.HTTP.StatusCode > 0 && v.HTTP.StatusCode < 200:
		v.Status = health.StatusUnhealthy
	}
	return v
}

func utcPtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// fillExcerpt attaches a bounded, redacted window of journal lines relevant
// to a failure. On success (LIVE) it stays empty: there is nothing to
// diagnose. It uses the same paginated log API as the logs route and never
// loads the whole journal.
func (s *Service) fillExcerpt(ctx context.Context, rep *Report, steps []deployment.Step) {
	if s.logs == nil || rep.Status == deployment.StateLive {
		return
	}
	limit := DefaultExcerptSize
	step := failedStep(steps)
	if step != nil {
		// Narrow the window to the failing step when it is log-correlated;
		// the canonical step names match logs.Step values.
		if canonical, ok := canonicalLogStep(step.Name); ok {
			rep.Excerpt, rep.ExcerptNext = s.readLogs(ctx, rep.DeploymentID, logs.Filter{Step: canonical, Limit: limit})
			if len(rep.Excerpt) > 0 {
				return
			}
		}
	}
	rep.Excerpt, rep.ExcerptNext = s.readLogs(ctx, rep.DeploymentID, logs.Filter{MinLevel: logs.LevelWarn, Limit: limit})
}

// canonicalLogStep maps a plan step name onto a journal Step value when
// they coincide (the canonical plan uses the same names).
func canonicalLogStep(name string) (logs.Step, bool) {
	switch logs.Step(strings.ToUpper(name)) {
	case logs.StepBuild, logs.StepCreateRuntime, logs.StepNetwork, logs.StepStart, logs.StepVerify:
		return logs.Step(strings.ToUpper(name)), true
	default:
		return "", false
	}
}

// readLogs runs one bounded, redacted page of the journal.
func (s *Service) readLogs(ctx context.Context, id string, f logs.Filter) ([]LogEntry, string) {
	if s.logs == nil {
		return []LogEntry{}, ""
	}
	entries, next, err := s.logs.List(ctx, id, f)
	if err != nil {
		return []LogEntry{}, ""
	}
	out := make([]LogEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, LogEntry{
			ID: e.ID, OccurredAt: e.OccurredAt, Level: e.Level,
			Step: e.Step, Source: e.Source, Message: safeMessage(e.Message),
		})
	}
	return out, next
}

// failedStep returns the first FAILED step, or the CANCELLED step when the
// deployment was cancelled.
func failedStep(steps []deployment.Step) *deployment.Step {
	for i := range steps {
		if steps[i].Status == deployment.StepFailed {
			return &steps[i]
		}
	}
	for i := range steps {
		if steps[i].Status == deployment.StepCancelled {
			return &steps[i]
		}
	}
	return nil
}

// conclude derives the operator-facing verdict from the deployment, its
// steps and the observed server/probe state. It is deterministic and
// read-only: the same inputs always yield the same conclusion.
func (s *Service) conclude(rec deployment.Record, rep *Report, steps []deployment.Step) Conclusion {
	var c Conclusion
	switch {
	case rec.Status == deployment.StateLive:
		c.Severity = logs.LevelInfo
		c.Code = ConclusionLive
		c.Summary = "The deployment succeeded and is LIVE. There is no failure to diagnose."
		c.Findings = []Finding{{Code: "deployment_live", Message: "Verification passed and the deployment reached LIVE."}}
		return c

	case rec.Status == deployment.StateCancelled:
		c.Severity = logs.LevelWarn
		c.Code = ConclusionCancelled
		c.Summary = "The deployment was cancelled by an operator. It did not fail on its own."
		c.Findings = append(c.Findings, Finding{Code: "deployment_cancelled", Message: "A cancel request moved this deployment to CANCELLED; downstream steps were not executed."})
		if st := failedStep(steps); st != nil {
			c.Findings = append(c.Findings, stepFinding(*st))
		}
		return c

	case rec.Status == deployment.StateFailed:
		return s.concludeFailure(rec, rep, steps)

	default:
		c.Severity = logs.LevelInfo
		c.Code = ConclusionInProgress
		c.Summary = fmt.Sprintf("The deployment is still %s; no failure has been recorded, and this diagnostic is read-only.", rec.Status)
		c.Findings = append(c.Findings, Finding{
			Code:    "in_progress",
			Message: fmt.Sprintf("Current state is %s. Diagnostics are read-only and cannot advance, retry or cancel it.", rec.Status),
		})
		if st := runningStep(steps); st != nil {
			c.Findings = append(c.Findings, Finding{
				Code:    "step_running",
				Message: fmt.Sprintf("Step %s is still RUNNING; the deployment is stuck inside it.", st.Name),
			})
		}
		return c
	}
}

// concludeFailure derives the verdict for a FAILED deployment. It always
// leads with the failed step (or the deployment-level error code when no
// step failed), then adds corroborating evidence from the server and the
// health probe.
func (s *Service) concludeFailure(rec deployment.Record, rep *Report, steps []deployment.Step) Conclusion {
	c := Conclusion{Severity: logs.LevelError}

	st := failedStep(steps)
	switch {
	case st != nil && st.Status == deployment.StepFailed:
		c.Code = ConclusionStepFailed
		c.Summary = fmt.Sprintf("The deployment failed at step %s (%s).", st.Name, stepLabel(st))
		c.Findings = append(c.Findings, stepFinding(*st))
		if st.ExitCode != nil {
			c.Findings = append(c.Findings, Finding{
				Code:    "step_exit_code",
				Message: fmt.Sprintf("Step %s exited with code %d.", st.Name, *st.ExitCode),
			})
		}
		if d := spanMs(st.StartedAt, st.CompletedAt, time.Now().UTC(), false); d != nil {
			c.Findings = append(c.Findings, Finding{
				Code:    "step_duration",
				Message: fmt.Sprintf("Step %s ran for %d ms before failing.", st.Name, *d),
			})
		}
	case rec.ErrorCode != "":
		c.Code = ConclusionErrorCode
		c.Summary = fmt.Sprintf("The deployment failed with error code %s.", rec.ErrorCode)
		c.Findings = append(c.Findings, Finding{
			Code:    "deployment_error_code",
			Message: fmt.Sprintf("No step is marked FAILED; the failure is recorded at deployment level as %s.", rec.ErrorCode),
		})
	default:
		c.Code = ConclusionIncomplete
		c.Summary = "The deployment is FAILED but records neither a failed step nor an error code."
		c.Findings = append(c.Findings, Finding{
			Code:    "incomplete_record",
			Message: "The failure is not attributable to a specific step. Correlate the deployment correlationId with the Engine logs.",
		})
	}

	if rec.ErrorCode == executor.ErrorHealthCheckFailed && rep.Health != nil {
		c.Findings = append(c.Findings, healthFinding(*rep.Health))
	}
	if rep.Server != nil && !rep.Server.Reachable {
		c.Findings = append(c.Findings, Finding{
			Code:    "server_not_ready",
			Message: fmt.Sprintf("The target server %s is %s; it was not reachable at report time.", rep.Server.Name, rep.Server.Status),
		})
	}
	if rec.CorrelationID != "" {
		c.Findings = append(c.Findings, Finding{
			Code:    "correlation",
			Message: fmt.Sprintf("Trace this deployment with correlationId %s.", rec.CorrelationID),
		})
	}
	if len(c.Findings) == 0 {
		c.Findings = []Finding{{Code: "no_evidence", Message: "No step, error code or probe data was recorded for this failure."}}
	}
	return c
}

// stepLabel renders a step for a human sentence.
func stepLabel(st *deployment.Step) string {
	if st.ErrorCode != "" {
		return st.ErrorCode
	}
	return string(st.Status)
}

// stepFinding renders a step as an evidence finding.
func stepFinding(st deployment.Step) Finding {
	msg := fmt.Sprintf("Step %d %s is %s", st.Position, st.Name, st.Status)
	if st.ErrorCode != "" {
		msg += " with error code " + st.ErrorCode
	}
	return Finding{Code: "failed_step", Message: msg}
}

// healthFinding renders the last probe as evidence.
func healthFinding(v HealthView) Finding {
	if v.HTTP != nil {
		return Finding{
			Code:    "health_probe_failed",
			Message: fmt.Sprintf("The verification probe returned HTTP %d in %d ms on attempt %d.", v.HTTP.StatusCode, v.HTTP.LatencyMs, v.Attempt),
		}
	}
	return Finding{Code: "health_probe_failed", Message: "The verification step failed and no HTTP probe result was persisted."}
}

// runningStep returns the step currently RUNNING, if any.
func runningStep(steps []deployment.Step) *deployment.Step {
	for i := range steps {
		if steps[i].Status == deployment.StepRunning {
			return &steps[i]
		}
	}
	return nil
}
