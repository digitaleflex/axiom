package diagnostics

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/executor"
	"github.com/digitaleflex/axiom/services/engine/internal/health"
	"github.com/digitaleflex/axiom/services/engine/internal/logs"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// --- fakes -----------------------------------------------------------------

type fakeSteps struct {
	steps []deployment.Step
	err   error
	calls int
}

func (f *fakeSteps) Steps(context.Context, string) ([]deployment.Step, error) {
	f.calls++
	return f.steps, f.err
}

type fakeServers struct {
	rec  server.Record
	err  error
	root bool // false → not found
}

func (f *fakeServers) Get(context.Context, string) (server.Record, error) {
	if f.root {
		return f.rec, f.err
	}
	return server.Record{}, server.ErrNotFound
}

type fakeLogs struct {
	entries []logs.Entry
	next    string
	err     error
	filters []logs.Filter
}

func (f *fakeLogs) List(_ context.Context, _ string, fl logs.Filter) ([]logs.Entry, string, error) {
	f.filters = append(f.filters, fl)
	if f.err != nil {
		return nil, "", f.err
	}
	return f.entries, f.next, nil
}

func at(d time.Duration) *time.Time {
	t := time.Now().UTC().Add(-d)
	return &t
}

// --- fixtures ---------------------------------------------------------------

func failedBuild() deployment.Record {
	return deployment.Record{
		ID: "dep_1", Number: 3, ApplicationID: "app_1", ServerID: "srv_1",
		PlanID: "plan_1", Environment: "production", Status: deployment.StateFailed,
		ErrorCode: executor.ErrorBuildFailed, CorrelationID: "req_abc123",
		CreatedAt: time.Now().UTC().Add(-10 * time.Minute),
		StartedAt: at(10 * time.Minute), CompletedAt: at(5 * time.Minute),
	}
}

func buildStepsFailed() []deployment.Step {
	exit := 1
	return []deployment.Step{
		{Name: "BUILD", Position: 1, Status: deployment.StepFailed,
			ErrorCode: executor.ErrorBuildFailed, ExitCode: &exit,
			StartedAt: at(10 * time.Minute), CompletedAt: at(9 * time.Minute)},
		{Name: "CREATE_RUNTIME", Position: 2, Status: deployment.StepQueued},
		{Name: "VERIFY", Position: 5, Status: deployment.StepQueued},
	}
}

// --- report: why did it fail -------------------------------------------------

func TestReportExplainsFailedStep(t *testing.T) {
	svc := New(&fakeSteps{steps: buildStepsFailed()}, &fakeServers{root: true, rec: server.Record{
		ID: "srv_1", Name: "srv-eu-1", Status: server.StatusReady,
		AgentVersion: "0.1.3", Capabilities: []server.Capability{server.CapabilityDocker},
	}}, &fakeLogs{}, nil)

	rep, err := svc.Report(context.Background(), failedBuild())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Conclusion.Code != ConclusionStepFailed {
		t.Fatalf("conclusion code = %q", rep.Conclusion.Code)
	}
	if rep.Conclusion.Severity != logs.LevelError {
		t.Fatalf("severity = %q", rep.Conclusion.Severity)
	}
	if !strings.Contains(rep.Conclusion.Summary, "BUILD") {
		t.Fatalf("summary must name the failing step: %q", rep.Conclusion.Summary)
	}
	// The failed step, its stable error code and its exit code must all be
	// present: this is the whole point of the diagnostic.
	found := map[string]string{}
	for _, f := range rep.Conclusion.Findings {
		found[f.Code] = f.Message
	}
	for _, want := range []string{"failed_step", "step_exit_code", "step_duration"} {
		if _, ok := found[want]; !ok {
			t.Fatalf("missing finding %q in %v", want, found)
		}
	}
	if !strings.Contains(found["step_exit_code"], "exited with code 1") {
		t.Fatalf("exit code finding = %q", found["step_exit_code"])
	}
	// Correlation must be surfaced: it is how an operator escalates.
	if rep.Correlation.CorrelationID != "req_abc123" {
		t.Fatalf("correlationId = %q", rep.Correlation.CorrelationID)
	}
	if found["correlation"] == "" {
		t.Fatal("conclusion must reference the correlation id")
	}
}

func TestReportExposesStepDurations(t *testing.T) {
	svc := New(&fakeSteps{steps: buildStepsFailed()}, nil, nil, nil)
	rep, err := svc.Report(context.Background(), failedBuild())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Steps) != 3 {
		t.Fatalf("steps = %d", len(rep.Steps))
	}
	failed := rep.Steps[0]
	if failed.DurationMs == nil {
		t.Fatal("a finished step must carry its duration")
	}
	// BUILD ran from -10m to -9m: roughly one minute.
	if *failed.DurationMs < 55_000 || *failed.DurationMs > 65_000 {
		t.Fatalf("BUILD duration = %d ms", *failed.DurationMs)
	}
	if rep.Steps[1].DurationMs != nil {
		t.Fatalf("a QUEUED step has no duration, got %v", *rep.Steps[1].DurationMs)
	}
	if rep.Timeline.DurationMs == nil || *rep.Timeline.DurationMs < 290_000 {
		t.Fatalf("timeline duration = %v", rep.Timeline.DurationMs)
	}
}

func TestReportStepsAreSortedAndBounded(t *testing.T) {
	unsorted := []deployment.Step{
		{Name: "VERIFY", Position: 5, Status: deployment.StepQueued},
		{Name: "BUILD", Position: 1, Status: deployment.StepCompleted},
		{Name: "START", Position: 4, Status: deployment.StepQueued},
	}
	svc := New(&fakeSteps{steps: unsorted}, nil, nil, nil)
	rep, _ := svc.Report(context.Background(), failedBuild())
	if rep.Steps[0].Name != "BUILD" || rep.Steps[2].Name != "VERIFY" {
		t.Fatalf("steps not sorted by position: %+v", rep.Steps)
	}

	// A corrupt or hand-edited plan must not be able to blow up a response.
	many := make([]deployment.Step, 500)
	for i := range many {
		many[i] = deployment.Step{Name: "S", Position: i, Status: deployment.StepQueued}
	}
	svc2 := New(&fakeSteps{steps: many}, nil, nil, nil)
	rep2, _ := svc2.Report(context.Background(), failedBuild())
	if len(rep2.Steps) != maxSteps {
		t.Fatalf("step count not bounded: %d", len(rep2.Steps))
	}
}

func TestReportRunningStepIsFlagged(t *testing.T) {
	steps := []deployment.Step{
		{Name: "BUILD", Position: 1, Status: deployment.StepRunning, StartedAt: at(3 * time.Minute)},
	}
	rec := failedBuild()
	rec.Status = deployment.StateDeploying
	rec.CompletedAt = nil
	rec.ErrorCode = ""

	rep, err := New(&fakeSteps{steps: steps}, nil, nil, nil).Report(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Conclusion.Code != ConclusionInProgress {
		t.Fatalf("code = %q", rep.Conclusion.Code)
	}
	if !rep.Steps[0].Running || rep.Steps[0].DurationMs == nil {
		t.Fatalf("running step must be flagged with an elapsed duration: %+v", rep.Steps[0])
	}
	found := false
	for _, f := range rep.Conclusion.Findings {
		if f.Code == "step_running" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a stuck deployment must be reported as stuck: %v", rep.Conclusion.Findings)
	}
	// Read-only guarantee, stated to the operator.
	if !strings.Contains(rep.Conclusion.Summary, "read-only") {
		t.Fatalf("summary must state the diagnostic cannot act: %q", rep.Conclusion.Summary)
	}
}

func TestReportHealthCheckFailure(t *testing.T) {
	steps := []deployment.Step{
		{Name: "VERIFY", Position: 5, Status: deployment.StepFailed, ErrorCode: executor.ErrorHealthCheckFailed,
			StartedAt: at(2 * time.Minute), CompletedAt: at(90 * time.Second)},
	}
	rec := failedBuild()
	rec.Status = deployment.StateFailed
	rec.ErrorCode = executor.ErrorHealthCheckFailed
	rec.CompletedAt = at(90 * time.Second)

	probe := func(context.Context, string) (health.ProbeReport, bool, error) {
		return health.NewProbeReport(503, 1200, "service unavailable", 3), true, nil
	}
	rep, err := New(&fakeSteps{steps: steps}, nil, nil, probe).Report(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Health.Status != health.StatusUnhealthy {
		t.Fatalf("health status = %q", rep.Health.Status)
	}
	if rep.Health.HTTP == nil || rep.Health.HTTP.StatusCode != 503 || rep.Health.Attempt != 3 {
		t.Fatalf("probe details = %+v", rep.Health)
	}
	found := false
	for _, f := range rep.Conclusion.Findings {
		if f.Code == "health_probe_failed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a health failure must cite the probe: %v", rep.Conclusion.Findings)
	}
}

func TestReportLiveIsNotAFailure(t *testing.T) {
	rec := failedBuild()
	rec.Status = deployment.StateLive
	rec.ErrorCode = ""
	steps := []deployment.Step{{Name: "VERIFY", Position: 5, Status: deployment.StepCompleted,
		StartedAt: at(5 * time.Minute), CompletedAt: at(4 * time.Minute)}}

	rep, err := New(&fakeSteps{steps: steps}, nil, nil, nil).Report(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Conclusion.Code != ConclusionLive || rep.Conclusion.Severity != logs.LevelInfo {
		t.Fatalf("conclusion = %+v", rep.Conclusion)
	}
	// No failure means no log noise in the report.
	if len(rep.Excerpt) != 0 {
		t.Fatalf("a LIVE deployment must not carry a log excerpt: %v", rep.Excerpt)
	}
}

func TestReportCancelled(t *testing.T) {
	rec := failedBuild()
	rec.Status = deployment.StateCancelled
	rec.ErrorCode = ""
	rep, err := New(&fakeSteps{}, nil, nil, nil).Report(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Conclusion.Code != ConclusionCancelled || rep.Conclusion.Severity != logs.LevelWarn {
		t.Fatalf("conclusion = %+v", rep.Conclusion)
	}
}

func TestReportUnattributableFailureIsCalledOut(t *testing.T) {
	// FAILED with neither a failed step nor an error code: the record itself
	// is incomplete and the diagnostic must say so rather than inventing a
	// cause.
	rec := failedBuild()
	rec.ErrorCode = ""
	rep, err := New(&fakeSteps{}, nil, nil, nil).Report(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Conclusion.Code != ConclusionIncomplete {
		t.Fatalf("code = %q", rep.Conclusion.Code)
	}
	if !strings.Contains(rep.Conclusion.Findings[0].Message, "correlationId") {
		t.Fatalf("must point at the correlation id: %q", rep.Conclusion.Findings[0].Message)
	}
}

func TestReportUnreachableServerIsEvidence(t *testing.T) {
	svc := New(&fakeSteps{steps: buildStepsFailed()}, &fakeServers{root: true, rec: server.Record{
		ID: "srv_1", Name: "srv-eu-1", Status: server.StatusOffline,
	}}, &fakeLogs{}, nil)
	rep, err := svc.Report(context.Background(), failedBuild())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Server.Reachable {
		t.Fatal("an offline server must not be reported as reachable")
	}
	found := false
	for _, f := range rep.Conclusion.Findings {
		if f.Code == "server_not_ready" {
			found = true
		}
	}
	if !found {
		t.Fatalf("an unreachable server must be evidence: %v", rep.Conclusion.Findings)
	}
}

func TestReportUnknownServerYieldsNoServerSection(t *testing.T) {
	svc := New(&fakeSteps{}, &fakeServers{root: false}, nil, nil)
	rep, err := svc.Report(context.Background(), failedBuild())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Server != nil {
		t.Fatalf("an unknown server must yield no section, got %+v", rep.Server)
	}
}

func TestReportUnknownHealthIsNeverAnImpliedPass(t *testing.T) {
	// FAILED for a non-health reason with no probe data: UNKNOWN, not HEALTHY.
	rep, err := New(&fakeSteps{}, nil, nil, nil).Report(context.Background(), failedBuild())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Health.Status != "UNKNOWN" {
		t.Fatalf("health status = %q", rep.Health.Status)
	}
	if rep.Health.CheckedAt != nil || rep.Health.HTTP != nil {
		t.Fatalf("no probe data must not be invented: %+v", rep.Health)
	}
}

// --- secrets -----------------------------------------------------------------

// TestReportNeverLeaksSecrets is the guard for the strictest constraint of
// this issue: no secret may ever leave through a diagnostic, whatever the
// path it took to get into a stored field.
func TestReportNeverLeaksSecrets(t *testing.T) {
	const secret = "sup3rs3cr3t-token-value"
	steps := []deployment.Step{{Name: "VERIFY", Position: 5, Status: deployment.StepFailed,
		StartedAt: at(time.Minute), CompletedAt: at(30 * time.Second)}}
	rec := failedBuild()
	rec.ErrorCode = executor.ErrorHealthCheckFailed

	// A probe body captured from an upstream response, and a journal line
	// carrying a bearer token and a password. Both bypassed the persisting
	// redactor: this is defense in depth.
	probe := func(context.Context, string) (health.ProbeReport, bool, error) {
		return health.ProbeReport{
			StatusCode: 500, CheckedAt: time.Now().UTC(), Attempt: 1,
			Body: "upstream said: Authorization: Bearer " + secret + " password=" + secret,
		}, true, nil
	}
	logStore := &fakeLogs{entries: []logs.Entry{{
		ID: "log_1", Level: logs.LevelError, Step: logs.StepVerify, Source: logs.SourceRuntime,
		Message: "connect failed with token=" + secret,
	}}}
	svc := New(&fakeSteps{steps: steps}, &fakeServers{root: true, rec: server.Record{
		ID: "srv_1", Name: "srv-eu-1", Status: server.StatusReady, Address: "203.0.113.10",
	}}, logStore, probe)

	rep, err := svc.Report(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, secret) {
		t.Fatalf("SECRET LEAKED in the diagnostic report: %s", body)
	}
	// And the guarantee must be documented in the response itself.
	if !rep.Redacted.Applied || len(rep.Redacted.Fields) == 0 {
		t.Fatalf("the response must document what was masked: %+v", rep.Redacted)
	}
	if len(rep.Excerpt) == 0 || !strings.Contains(rep.Excerpt[0].Message, logs.RedactionMarker) {
		t.Fatalf("the excerpt must be redacted, not dropped: %+v", rep.Excerpt)
	}
	if !strings.Contains(rep.Health.Body, logs.RedactionMarker) {
		t.Fatalf("the probe body must be redacted: %q", rep.Health.Body)
	}
}

func TestServerAddressIsNeverServed(t *testing.T) {
	svc := New(&fakeSteps{}, &fakeServers{root: true, rec: server.Record{
		ID: "srv_1", Name: "srv-eu-1", Status: server.StatusReady,
		Address: "203.0.113.10",
	}}, nil, nil)
	rep, err := svc.Report(context.Background(), failedBuild())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rep)
	if strings.Contains(string(raw), "203.0.113.10") {
		t.Fatalf("the server address must not be served by diagnostics: %s", raw)
	}
}

func TestTruncateBoundsFreeText(t *testing.T) {
	long := strings.Repeat("é", maxMessageRunes+500)
	got := truncate(long)
	if n := utf8.RuneCountInString(got); n > maxMessageRunes+1 { // +1 for the ellipsis rune
		t.Fatalf("truncated length = %d", n)
	}
	if !utf8.ValidString(got) {
		t.Fatal("truncation must not split a rune")
	}
	if truncate("short") != "short" {
		t.Fatal("short strings must pass through unchanged")
	}
}

// --- paginated log route -----------------------------------------------------

func TestLogsPaginatesAndRedacts(t *testing.T) {
	const secret = "another-secret-value"
	store := &fakeLogs{entries: []logs.Entry{{
		ID: "log_1", Level: logs.LevelError, Message: "failed token=" + secret,
	}}, next: "log_1"}
	page, err := New(nil, nil, store, nil).Logs(context.Background(), "dep_1", LogQuery{Level: "error"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor != "log_1" {
		t.Fatalf("page = %+v", page)
	}
	if strings.Contains(page.Items[0].Message, secret) {
		t.Fatalf("SECRET LEAKED in the log page: %q", page.Items[0].Message)
	}
	if len(store.filters) != 1 || store.filters[0].MinLevel != logs.LevelError {
		t.Fatalf("filter = %+v", store.filters)
	}
	// The journal must never be loaded whole.
	if store.filters[0].Limit != DefaultPageSize {
		t.Fatalf("limit = %d", store.filters[0].Limit)
	}
}

func TestLogsRejectsInvalidQuery(t *testing.T) {
	svc := New(nil, nil, &fakeLogs{}, nil)
	for name, q := range map[string]LogQuery{
		"level":  {Level: "trace"},
		"step":   {Step: "PREPARE"},
		"source": {Source: "agent"},
		"limit":  {Limit: 5000},
		"zero":   {Limit: -1},
	} {
		if _, err := svc.Logs(context.Background(), "dep_1", q); err == nil {
			t.Fatalf("%s: expected an invalid-query error", name)
		} else if !strings.Contains(err.Error(), "invalid diagnostic query") {
			t.Fatalf("%s: error = %v", name, err)
		}
	}
}

func TestLogsAcceptsCanonicalStepsCaseInsensitively(t *testing.T) {
	store := &fakeLogs{}
	page, err := New(nil, nil, store, nil).Logs(context.Background(), "dep_1", LogQuery{Step: "build", Source: "RUNTIME"})
	if err != nil {
		t.Fatal(err)
	}
	if store.filters[0].Step != logs.StepBuild || store.filters[0].Source != logs.SourceRuntime {
		t.Fatalf("filter = %+v", store.filters[0])
	}
	_ = page
}

func TestLogsUnavailableWithoutStore(t *testing.T) {
	if _, err := New(nil, nil, nil, nil).Logs(context.Background(), "dep_1", LogQuery{}); err != ErrLogsUnavailable {
		t.Fatalf("err = %v", err)
	}
}

func TestDefaultLimitIsApplied(t *testing.T) {
	f, err := LogQuery{}.filter()
	if err != nil || f.Limit != DefaultPageSize {
		t.Fatalf("filter = %+v err = %v", f, err)
	}
}

// --- resilience --------------------------------------------------------------

func TestReportSurvivesFailingDependencies(t *testing.T) {
	// A log store that errors must degrade the excerpt, not the report: an
	// operator still gets the failed step and the conclusion.
	svc := New(&fakeSteps{steps: buildStepsFailed()}, nil, &fakeLogs{err: context.DeadlineExceeded}, nil)
	rep, err := svc.Report(context.Background(), failedBuild())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Excerpt) != 0 {
		t.Fatalf("excerpt = %v", rep.Excerpt)
	}
	if rep.Conclusion.Code != ConclusionStepFailed {
		t.Fatalf("conclusion = %q", rep.Conclusion.Code)
	}
}

func TestReportPropagatesStepStoreFailure(t *testing.T) {
	// A hard read failure is surfaced (500), not silently answered as an
	// empty step list that would look like "no steps ran".
	svc := New(&fakeSteps{err: context.DeadlineExceeded}, nil, nil, nil)
	if _, err := svc.Report(context.Background(), failedBuild()); err == nil {
		t.Fatal("expected the step store error to propagate")
	}
}
