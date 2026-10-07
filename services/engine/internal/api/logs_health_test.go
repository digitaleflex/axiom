package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/health"
	"github.com/digitaleflex/axiom/services/engine/internal/logs"
)

// withLogs rebuilds the harness handler with a given log store.
func withLogs(h *harness, ls LogStore) http.Handler {
	return New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: NewTokenAuthenticator(token, Principal{UserID: "usr_1"}),
		Deployments:  h.svc,
		Applications: &fakeApps{items: map[string]application.Record{"app_1": {ID: "app_1", OwnerID: "usr_1"}}},
		Servers:      &fakeServers{}, Logs: ls})
}

// createDeployment creates plan_1's deployment for app_1.
func createDeployment(t *harness, h *harness) string {
	t.t.Helper()
	r := h.do("POST", "/api/v1/applications/app_1/deployments", map[string]any{"planId": "plan_1"}, nil)
	if r.code != http.StatusAccepted {
		t.t.Fatalf("setup deployment: %d %v", r.code, r.body)
	}
	return r.body["id"].(string)
}

func nowTime() health.ProbeReport {
	return health.NewProbeReport(200, 84, "", 2)
}

type fakeLogs struct {
	lastFilter logs.Filter
	entries    []logs.Entry
	next       string
	err        error
}

func (f *fakeLogs) List(_ context.Context, _ string, fl logs.Filter) ([]logs.Entry, string, error) {
	f.lastFilter = fl
	if f.err != nil {
		return nil, "", f.err
	}
	return f.entries, f.next, nil
}

func TestDeploymentLogsQuery(t *testing.T) {
	h := newHarness(t)
	fl := &fakeLogs{entries: []logs.Entry{
		{ID: "log_1", DeploymentID: "x", Level: logs.LevelError, Step: logs.StepVerify, Source: logs.SourceRuntime, Message: "boom"},
	}, next: "log_1"}
	h.handler = withLogs(h, fl)

	id := createDeployment(h, h)
	r := h.do("GET", "/api/v1/deployments/"+id+"/logs?level=error&step=VERIFY&source=runtime&q=boom&limit=10", nil, nil)
	expect(t, r, 200, "")
	if len(r.body["items"].([]any)) != 1 || r.body["nextCursor"] != "log_1" {
		t.Fatalf("logs = %v", r.body)
	}
	if fl.lastFilter.MinLevel != logs.LevelError || fl.lastFilter.Step != logs.StepVerify || fl.lastFilter.Source != logs.SourceRuntime ||
		fl.lastFilter.Search != "boom" || fl.lastFilter.Limit != 10 {
		t.Fatalf("filter = %+v", fl.lastFilter)
	}
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/logs?level=trace", nil, nil), 400, CodeInvalidRequest)
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/logs?step=PREPARE", nil, nil), 400, CodeInvalidRequest)
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/logs?source=agent", nil, nil), 400, CodeInvalidRequest)
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/logs?limit=5000", nil, nil), 400, CodeInvalidRequest)
	fl.next = ""
	r = h.do("GET", "/api/v1/deployments/"+id+"/logs", nil, nil)
	if _, hasCursor := r.body["nextCursor"]; hasCursor {
		t.Fatalf("last page must omit nextCursor: %v", r.body)
	}
}

func TestDeploymentLogsUnavailable(t *testing.T) {
	h := newHarness(t)
	h.handler = withLogs(h, nil)
	id := createDeployment(h, h)
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/logs", nil, nil), 503, CodeServiceUnavailable)
}

func TestDeploymentHealthFromProbe(t *testing.T) {
	h := newHarness(t)
	id := createDeployment(h, h)
	r := h.do("GET", "/api/v1/deployments/"+id+"/health", nil, nil)
	expect(t, r, 200, "")
	if r.body["status"] != "UNKNOWN" {
		t.Fatalf("no probe data must be UNKNOWN, got %v", r.body)
	}
	for _, s := range []deployment.State{deployment.StateAnalyzing, deployment.StatePlanning, deployment.StateBuilding, deployment.StateDeploying, deployment.StateVerifying} {
		if _, err := h.svc.Transition(context.Background(), id, s); err != nil {
			t.Fatal(err)
		}
	}
	now := nowTime()
	pol := health.Policy{Type: "http", Path: "/", ExpectedStatus: "200-399"}
	if err := h.svc.RecordHealth(context.Background(), id, pol, now); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.MarkLive(context.Background(), id, "https://app.example.com"); err != nil {
		t.Fatal(err)
	}
	r = h.do("GET", "/api/v1/deployments/"+id+"/health", nil, nil)
	expect(t, r, 200, "")
	if r.body["status"] != "HEALTHY" {
		t.Fatalf("health = %v", r.body)
	}
	httpPart := r.body["http"].(map[string]any)
	if httpPart["statusCode"].(float64) != 200 || httpPart["latencyMs"].(float64) != 84 {
		t.Fatalf("probe details = %v", r.body)
	}
	if !strings.Contains(r.body["checkedAt"].(string), "T") || r.body["attempt"].(float64) != 2 {
		t.Fatalf("meta = %v", r.body)
	}
}
