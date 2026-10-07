package executor

import (
	"context"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/health"
)

// reportAgent returns a fixed probe report (or error) for VERIFY.
type reportAgent struct {
	fakeAgent
	report health.ProbeReport
	err    error
}

func (a *reportAgent) HealthCheck(_ context.Context, _ HealthCheckRequest) (health.ProbeReport, error) {
	return a.report, a.err
}

func healthEvent(t *testing.T, svc *deployment.Service, id, typ string) *deployment.Event {
	t.Helper()
	events, err := svc.Store().Events(context.Background(), id, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for i := range events {
		if events[i].Type == typ {
			return &events[i]
		}
	}
	return nil
}

func TestVerifySuccessPersistsHealthPassed(t *testing.T) {
	svc, rec, plan := setup(t)
	agent := &fakeAgent{}
	res, err := run(t, svc, fakeBuilder{}, agent, context.Background(), Request{
		DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deployment.Status != deployment.StateLive {
		t.Fatalf("status = %s, want LIVE", res.Deployment.Status)
	}
	ev := healthEvent(t, svc, rec.ID, health.EventPassed)
	if ev == nil {
		t.Fatal("no health.passed event persisted")
	}
	if ev.Data["status"] != health.StatusHealthy {
		t.Fatalf("status = %v, want HEALTHY", ev.Data["status"])
	}
	if got := ev.Data["statusCode"]; got != float64(200) {
		t.Fatalf("statusCode = %v, want 200", got)
	}
	if got := ev.Data["latencyMs"]; got != float64(84) {
		t.Fatalf("latencyMs = %v, want 84", got)
	}
	if got := ev.Data["path"]; got != "/" {
		t.Fatalf("path = %v, want /", got)
	}
	if got := ev.Data["attempt"]; got != float64(1) {
		t.Fatalf("attempt = %v, want 1", got)
	}
	if _, ok := ev.Data["checkedAt"]; !ok {
		t.Fatalf("checkedAt missing from event data: %v", ev.Data)
	}
	// The query API reads the same persisted event.
	report, found, err := LastHealthResult(context.Background(), svc.Store(), rec.ID)
	if err != nil || !found {
		t.Fatalf("LastHealthResult: found=%v err=%v", found, err)
	}
	if report.StatusCode != 200 || report.LatencyMs != 84 || report.Attempt != 1 {
		t.Fatalf("LastHealthResult report = %+v", report)
	}
}

func TestVerifyFailurePersistsHealthFailed(t *testing.T) {
	svc, rec, plan := setup(t)
	_, err := run(t, svc, fakeBuilder{}, &failAgent{}, context.Background(), Request{
		DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan,
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	got, _ := svc.Get(context.Background(), rec.ID)
	if got.Status != deployment.StateFailed || got.ErrorCode != ErrorHealthCheckFailed {
		t.Fatalf("status=%s code=%s", got.Status, got.ErrorCode)
	}
	ev := healthEvent(t, svc, rec.ID, health.EventFailed)
	if ev == nil {
		t.Fatal("no health.failed event persisted")
	}
	if ev.Data["status"] != health.StatusUnhealthy {
		t.Fatalf("status = %v, want UNHEALTHY", ev.Data["status"])
	}
	if _, ok := ev.Data["checkedAt"]; !ok {
		t.Fatalf("checkedAt missing from event data: %v", ev.Data)
	}
}

func TestVerifyPolicyFailurePersistsProbeReport(t *testing.T) {
	svc, rec, plan := setup(t)
	// The agent returns a 503 response: a real probe report that fails the
	// policy. The last probe report must be in the health.failed event.
	agent := &reportAgent{report: health.ProbeReport{StatusCode: 503, LatencyMs: 12, CheckedAt: time.Now().UTC(), Attempt: 1}}
	_, err := run(t, svc, fakeBuilder{}, agent, context.Background(), Request{
		DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan,
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	got, _ := svc.Get(context.Background(), rec.ID)
	if got.Status != deployment.StateFailed || got.ErrorCode != ErrorHealthCheckFailed {
		t.Fatalf("status=%s code=%s", got.Status, got.ErrorCode)
	}
	ev := healthEvent(t, svc, rec.ID, health.EventFailed)
	if ev == nil {
		t.Fatal("no health.failed event persisted")
	}
	if ev.Data["status"] != health.StatusUnhealthy {
		t.Fatalf("status = %v, want UNHEALTHY", ev.Data["status"])
	}
	if got := ev.Data["statusCode"]; got != float64(503) {
		t.Fatalf("statusCode = %v, want 503", got)
	}
	if got := ev.Data["latencyMs"]; got != float64(12) {
		t.Fatalf("latencyMs = %v, want 12", got)
	}
	if got := ev.Data["path"]; got != "/" {
		t.Fatalf("path = %v, want /", got)
	}
	if _, ok := ev.Data["reason"]; !ok {
		t.Fatalf("reason missing from event data: %v", ev.Data)
	}
}

func TestLastHealthResultReturnsNewest(t *testing.T) {
	svc, rec, plan := setup(t)
	_, err := run(t, svc, fakeBuilder{}, &fakeAgent{}, context.Background(), Request{
		DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A later, failing probe overwrites the healthy result as the newest.
	policy := health.DefaultPolicy(health.TypeHTTP, "/", "200-399", time.Second, 1, time.Second)
	failing := health.ProbeReport{StatusCode: 503, LatencyMs: 99, CheckedAt: time.Now().UTC(), Attempt: 2}
	if err := svc.RecordHealth(context.Background(), rec.ID, policy, failing); err != nil {
		t.Fatal(err)
	}
	report, found, err := LastHealthResult(context.Background(), svc.Store(), rec.ID)
	if err != nil || !found {
		t.Fatalf("LastHealthResult: found=%v err=%v", found, err)
	}
	if report.StatusCode != 503 || report.LatencyMs != 99 || report.Attempt != 2 {
		t.Fatalf("LastHealthResult report = %+v, want the newest (503/99/2)", report)
	}
}

func TestLastHealthResultNotFound(t *testing.T) {
	svc, rec, _ := setup(t)
	if _, found, err := LastHealthResult(context.Background(), svc.Store(), rec.ID); err != nil || found {
		t.Fatalf("expected not found, got found=%v err=%v", found, err)
	}
}
