package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/diagnostics"
	"github.com/digitaleflex/axiom/services/engine/internal/logs"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// diagServers is the server store used by the diagnostics routes: srv_1 is
// ready, which is the healthy baseline for a failure diagnostic.
var diagServers = &fakeServers{items: []server.Record{
	{ID: "srv_1", Name: "srv-eu-1", Address: "203.0.113.10", OwnerID: "usr_1",
		Status: server.StatusReady, AgentVersion: "0.1.3",
		Capabilities: []server.Capability{server.CapabilityDocker}},
}}

// otherToken authenticates usr_2, who owns no application in the harness.
const otherToken = "other-token-0123456789abcdef01234567"

// withDiagnostics rebuilds the harness handler with a diagnostics service
// wired from the harness stores, mirroring the composition root. It returns
// the handler together with the bearer token that authenticates userID, so a
// test can act as an owner or as a stranger.
func withDiagnostics(h *harness, logStore LogStore) http.Handler {
	h.handler = withDiagnosticsAs(h, logStore, token, "usr_1")
	return h.handler
}

func withDiagnosticsAs(h *harness, logStore LogStore, tok, userID string) http.Handler {
	d := diagnostics.New(h.svc.Store(), diagServers, logStore, nil)
	return New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:         NewTokenAuthenticator(tok, Principal{UserID: userID, Name: userID}),
		Deployments:  h.svc,
		Applications: &fakeApps{items: map[string]application.Record{"app_1": {ID: "app_1", OwnerID: "usr_1"}}},
		Servers:      diagServers, Logs: logStore, Diagnostics: d})
}

// failBuildDeployment drives a deployment to a FAILED build so the
// diagnostic has a real failed step to explain.
func failBuildDeployment(h *harness) string {
	h.t.Helper()
	id := createDeployment(h, h)
	ctx := context.Background()
	if err := h.svc.RecordStep(ctx, id, deployment.StepChange{Name: "BUILD", Status: deployment.StepRunning}); err != nil {
		h.t.Fatal(err)
	}
	if err := h.svc.RecordStep(ctx, id, deployment.StepChange{Name: "BUILD", Status: deployment.StepFailed, ErrorCode: "BUILD_FAILED"}); err != nil {
		h.t.Fatal(err)
	}
	if _, err := h.svc.Transition(ctx, id, deployment.StateAnalyzing); err != nil {
		h.t.Fatal(err)
	}
	if _, err := h.svc.Fail(ctx, id, "BUILD_FAILED"); err != nil {
		h.t.Fatal(err)
	}
	return id
}

// rawDoAs returns the status and the raw response body for a request
// authenticated with tok, so a test can assert on the exact bytes served
// (used to prove no secret escapes).
func rawDoAs(handler http.Handler, method, path, tok string) (int, string) {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr.Code, rr.Body.String()
}

// rawDo returns the status and the raw response body for the owner.
func (h *harness) rawDo(method, path string) (int, string) {
	h.t.Helper()
	return rawDoAs(h.handler, method, path, token)
}

func TestDeploymentDiagnosticReport(t *testing.T) {
	h := newHarness(t)
	h.handler = withDiagnostics(h, &fakeLogs{})
	id := failBuildDeployment(h)

	r := h.do("GET", "/api/v1/deployments/"+id+"/diagnostics", nil, nil)
	expect(t, r, 200, "")
	if r.body["status"] != "FAILED" {
		t.Fatalf("status = %v", r.body["status"])
	}
	if r.body["errorCode"] != "BUILD_FAILED" {
		t.Fatalf("errorCode = %v", r.body["errorCode"])
	}
	concl, ok := r.body["conclusion"].(map[string]any)
	if !ok || concl["code"] != diagnostics.ConclusionStepFailed {
		t.Fatalf("conclusion = %v", r.body["conclusion"])
	}
	if !strings.Contains(concl["summary"].(string), "BUILD") {
		t.Fatalf("summary = %v", concl["summary"])
	}
	// The stable error code must be reachable from the findings, so a
	// console can act on the code rather than parse prose.
	sawCode := false
	for _, f := range concl["findings"].([]any) {
		if strings.Contains(f.(map[string]any)["message"].(string), "BUILD_FAILED") {
			sawCode = true
		}
	}
	if !sawCode {
		t.Fatalf("findings must carry the stable error code: %v", concl["findings"])
	}
	// The canonical plan's five steps, each with a status.
	steps, _ := r.body["steps"].([]any)
	if len(steps) != 5 {
		t.Fatalf("expected the 5 canonical steps, got %d", len(steps))
	}
	// The masking guarantee is part of the contract and travels in the body.
	red, ok := r.body["redacted"].(map[string]any)
	if !ok || red["applied"] != true || len(red["fields"].([]any)) == 0 {
		t.Fatalf("redaction notice = %v", r.body["redacted"])
	}
	// Correlation is the escalation anchor and must always be a section.
	if _, ok := r.body["correlation"].(map[string]any); !ok {
		t.Fatalf("correlation section missing: %v", r.body)
	}
	// The target server's observed state must be exposed.
	srv, ok := r.body["server"].(map[string]any)
	if !ok || srv["status"] != "ready" {
		t.Fatalf("server = %v", r.body["server"])
	}
}

func TestDeploymentDiagnosticUnknownDeployment(t *testing.T) {
	h := newHarness(t)
	h.handler = withDiagnostics(h, &fakeLogs{})
	// Aligned with getDeployment: a missing deployment is 404 NOT_FOUND.
	expect(t, h.do("GET", "/api/v1/deployments/dep_nope/diagnostics", nil, nil), 404, CodeNotFound)
	expect(t, h.do("GET", "/api/v1/deployments/dep_nope/diagnostics/logs", nil, nil), 404, CodeNotFound)
}

func TestDeploymentDiagnosticHidesForeignDeployment(t *testing.T) {
	h := newHarness(t)
	id := failBuildDeployment(h)
	// A caller that does not own the application must get 404, never 403:
	// the API hides resource existence from non-owners, on reads and writes.
	// usr_2 authenticates successfully but owns nothing in this harness.
	foreign := withDiagnosticsAs(h, &fakeLogs{}, otherToken, "usr_2")
	for _, p := range []string{"/diagnostics", "/diagnostics/logs"} {
		code, body := rawDoAs(foreign, "GET", "/api/v1/deployments/"+id+p, otherToken)
		if code != http.StatusNotFound {
			t.Fatalf("%s as a non-owner = %d %s (want 404)", p, code, body)
		}
		if !strings.Contains(body, CodeNotFound) {
			t.Fatalf("%s must use the stable envelope: %s", p, body)
		}
	}
}

func TestDeploymentDiagnosticRequiresAuthentication(t *testing.T) {
	h := newHarness(t)
	h.handler = withDiagnostics(h, &fakeLogs{})
	id := failBuildDeployment(h)
	r := h.do("GET", "/api/v1/deployments/"+id+"/diagnostics", nil, map[string]string{"Authorization": ""})
	if r.code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated diagnostic = %d %v", r.code, r.body)
	}
}

func TestDeploymentDiagnosticIsReadOnly(t *testing.T) {
	h := newHarness(t)
	h.handler = withDiagnostics(h, &fakeLogs{})
	id := failBuildDeployment(h)
	before, err := h.svc.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	// Only GET is routed on the diagnostic paths. Any other method falls
	// through to the catch-all /api/v1/ route, which answers the stable
	// NOT_FOUND envelope: there is no mutating diagnostic route to reach.
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		for _, p := range []string{"/diagnostics", "/diagnostics/logs"} {
			r := h.do(m, "/api/v1/deployments/"+id+p, nil, nil)
			if r.code != http.StatusNotFound || errCode(r) != CodeNotFound {
				t.Fatalf("%s %s = %d %s (want 404 NOT_FOUND)", m, p, r.code, errCode(r))
			}
		}
	}
	after, err := h.svc.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if before.Status != after.Status || !before.UpdatedAt.Equal(after.UpdatedAt) || before.ErrorCode != after.ErrorCode {
		t.Fatalf("diagnostic routes must not mutate the deployment: %+v -> %+v", before, after)
	}
}

func TestDeploymentDiagnosticUnavailableWithoutService(t *testing.T) {
	h := newHarness(t)
	// Explicitly no Diagnostics service and no deployments-backed fallback:
	// the endpoints must answer 503 rather than serve a half-built report.
	h.handler = New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:        NewTokenAuthenticator(token, Principal{UserID: "usr_1"}),
		Deployments: nil,
		Servers:     diagServers})
	id := "dep_1"
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/diagnostics", nil, nil), 503, CodeServiceUnavailable)
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/diagnostics/logs", nil, nil), 503, CodeServiceUnavailable)
}

func TestDeploymentDiagnosticLogsPagination(t *testing.T) {
	h := newHarness(t)
	fl := &fakeLogs{entries: []logs.Entry{{
		ID: "log_1", Level: logs.LevelError, Step: logs.StepVerify,
		Source: logs.SourceRuntime, Message: "probe returned 503",
	}}, next: "log_1"}
	h.handler = withDiagnostics(h, fl)
	id := failBuildDeployment(h)

	r := h.do("GET", "/api/v1/deployments/"+id+"/diagnostics/logs?level=error&step=VERIFY&limit=5", nil, nil)
	expect(t, r, 200, "")
	if r.body["nextCursor"] != "log_1" {
		t.Fatalf("nextCursor = %v", r.body["nextCursor"])
	}
	items, _ := r.body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", r.body["items"])
	}
	// The filters must be forwarded to the journal store, bounded: the
	// diagnostic never loads the whole journal into memory.
	if fl.lastFilter.MinLevel != logs.LevelError || fl.lastFilter.Step != logs.StepVerify {
		t.Fatalf("filter = %+v", fl.lastFilter)
	}
	if fl.lastFilter.Limit != 5 {
		t.Fatalf("limit = %d", fl.lastFilter.Limit)
	}
	// Unknown enum values are rejected rather than silently dropped, so an
	// operator is never handed a page that is not what they asked for.
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/diagnostics/logs?level=trace", nil, nil), 400, CodeInvalidRequest)
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/diagnostics/logs?step=PREPARE", nil, nil), 400, CodeInvalidRequest)
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/diagnostics/logs?source=agent", nil, nil), 400, CodeInvalidRequest)
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/diagnostics/logs?limit=5000", nil, nil), 400, CodeInvalidRequest)

	// The last page must omit the cursor so clients know they are done.
	fl.next = ""
	r = h.do("GET", "/api/v1/deployments/"+id+"/diagnostics/logs", nil, nil)
	expect(t, r, 200, "")
	if _, has := r.body["nextCursor"]; has {
		t.Fatalf("the last page must omit nextCursor: %v", r.body)
	}
	if fl.lastFilter.Limit != diagnostics.DefaultPageSize {
		t.Fatalf("default limit = %d", fl.lastFilter.Limit)
	}
}

// TestDeploymentDiagnosticNeverLeaksSecrets is the HTTP-level guard: whatever
// the report claims, no secret may appear in the exact bytes served.
func TestDeploymentDiagnosticNeverLeaksSecrets(t *testing.T) {
	const secret = "leaky-secret-do-not-serve"
	h := newHarness(t)
	fl := &fakeLogs{entries: []logs.Entry{{
		ID: "log_1", Level: logs.LevelError, Step: logs.StepVerify,
		Source: logs.SourceRuntime, Message: "connection refused for token=" + secret,
	}}}
	h.handler = withDiagnostics(h, fl)
	id := failBuildDeployment(h)

	code, body := h.rawDo("GET", "/api/v1/deployments/"+id+"/diagnostics")
	if code != 200 {
		t.Fatalf("status = %d %s", code, body)
	}
	if strings.Contains(body, secret) {
		t.Fatalf("SECRET LEAKED in the report: %s", body)
	}
	if !strings.Contains(body, logs.RedactionMarker) {
		t.Fatalf("the redaction marker must be present: %s", body)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("the report must stay valid JSON after redaction: %v", err)
	}
	// The same guarantee on the paginated log route.
	code, body = h.rawDo("GET", "/api/v1/deployments/"+id+"/diagnostics/logs")
	if code != 200 {
		t.Fatalf("status = %d %s", code, body)
	}
	if strings.Contains(body, secret) {
		t.Fatalf("SECRET LEAKED in the log page: %s", body)
	}
}
