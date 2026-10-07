package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/audit"
	"github.com/digitaleflex/axiom/services/engine/internal/domains"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

func auditHarness(t *testing.T, buf *bytes.Buffer) *harness {
	t.Helper()
	h := newHarness(t)
	h.handler = New(Deps{Log: slog.New(slog.NewJSONHandler(buf, nil)), Auth: NewTokenAuthenticator(token, Principal{UserID: "usr_1"}),
		Deployments:  h.svc,
		Applications: &fakeApps{items: map[string]application.Record{"app_1": {ID: "app_1", OwnerID: "usr_1"}}},
		Servers:      &fakeServers{}, Logs: &fakeLogs{}})
	return h
}

func TestAuditTrailForPrivilegedOperations(t *testing.T) {
	var buf bytes.Buffer
	h := auditHarness(t, &buf)
	id := createDeployment(h, h)
	r := h.do("POST", "/api/v1/deployments/"+id+"/cancel", nil, nil)
	expect(t, r, 200, "")
	r = h.do("POST", "/api/v1/applications/app_1/deployment-plans", map[string]any{"serverId": "srv_1"}, nil)
	if r.code == 201 {
		t.Fatal("plan without collaborators must not succeed here")
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	actions := map[string]string{}
	for _, line := range lines {
		if !strings.Contains(line, `"msg":"audit"`) {
			continue
		}
		for _, a := range []string{"deployment.create", "deployment.cancel", "plan.create"} {
			if strings.Contains(line, `"action":"`+a+`"`) {
				actions[a] = line
			}
		}
	}
	if !strings.Contains(actions["deployment.create"], `"actor":"usr_1"`) || !strings.Contains(actions["deployment.create"], `"result":"ok"`) {
		t.Fatalf("create audit = %s", actions["deployment.create"])
	}
	if !strings.Contains(actions["deployment.cancel"], id) {
		t.Fatalf("cancel audit = %s", actions["deployment.cancel"])
	}
	// Failed plan creation is audited as an error with the target application.
	if !strings.Contains(actions["plan.create"], `"result":"error"`) || !strings.Contains(actions["plan.create"], "app_1") {
		t.Fatalf("plan audit = %s", actions["plan.create"])
	}
	for _, line := range lines {
		if strings.Contains(line, "s3cret") {
			t.Fatalf("secret leaked in audit log: %s", line)
		}
	}
}

func TestServerErrorLogsAreRedacted(t *testing.T) {
	var buf bytes.Buffer
	api := &API{log: slog.New(slog.NewJSONHandler(&buf, nil))}
	req := httptest.NewRequest("GET", "/api/v1/x", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "req_test"))
	rr := httptest.NewRecorder()
	api.writeError(rr, req, errors.New("dial database: password=hunter2 token=abc"))
	if rr.Code != 500 {
		t.Fatalf("code = %d", rr.Code)
	}
	out := buf.String()
	if strings.Contains(out, "hunter2") || strings.Contains(out, "token=abc") {
		t.Fatalf("secret in 5xx log: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("redaction marker missing: %s", out)
	}
	var body map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if !strings.Contains(body["error"].(map[string]any)["message"].(string), "could not complete") {
		t.Fatalf("client message must be generic: %v", body)
	}
}

// auditServiceHarness wires a fake audit service so tests can assert on
// recorded events instead of log lines.
func auditServiceHarness(t *testing.T) (*harness, *fakeAuditService) {
	t.Helper()
	h := newHarness(t)
	f := &fakeAuditService{}
	domains := &fakeDomains{items: map[string]domains.Record{}}
	appConfig := &fakeAppConfig{values: map[string]string{}}
	h.handler = New(Deps{
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:         NewTokenAuthenticator(token, Principal{UserID: "usr_1", Name: "Jane"}),
		Deployments:  h.svc,
		Applications: &fakeApps{items: map[string]application.Record{"app_1": {ID: "app_1", OwnerID: "usr_1"}}},
		Servers:      &fakeServers{items: []server.Record{}}, Logs: &fakeLogs{}, Audit: f,
		Domains: domains, AppConfig: appConfig,
	})
	return h, f
}

func TestAuditServiceRecordsPrivilegedOperations(t *testing.T) {
	h, f := auditServiceHarness(t)
	id := createDeployment(h, h)
	r := h.do("POST", "/api/v1/deployments/"+id+"/cancel", nil, nil)
	expect(t, r, 200, "")

	events := f.action("deployment.cancel")
	if len(events) != 1 {
		t.Fatalf("deployment.cancel events = %d, want 1", len(events))
	}
	e := events[0]
	if e.ActorID != "usr_1" || e.ActorName != "Jane" {
		t.Fatalf("actor = %q %q", e.ActorID, e.ActorName)
	}
	if e.TargetID != id || e.TargetType != "deployment" {
		t.Fatalf("target = %q %q", e.TargetID, e.TargetType)
	}
	if e.Result != audit.ResultOK {
		t.Fatalf("result = %q", e.Result)
	}
	if e.RequestID == "" {
		t.Fatal("requestId must be set")
	}
	if e.OwnerID != "usr_1" {
		t.Fatalf("ownerId = %q", e.OwnerID)
	}
}

func TestAuditServiceRecordsErrors(t *testing.T) {
	h, f := auditServiceHarness(t)
	// Failed deployment creation (unknown plan) is audited as an error.
	r := h.do("POST", "/api/v1/applications/app_1/deployments", map[string]any{"planId": "plan_missing"}, nil)
	expect(t, r, 404, CodeNotFound)
	events := f.action("deployment.create")
	if len(events) != 1 {
		t.Fatalf("deployment.create events = %d, want 1", len(events))
	}
	if events[0].Result != audit.ResultError {
		t.Fatalf("result = %q, want error", events[0].Result)
	}
}

// TestAllMutatingRoutesEmitAuditEvent is the table-driven guarantee that
// every privileged mutation leaves a traceable event.
func TestAllMutatingRoutesEmitAuditEvent(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   any
		hdr    map[string]string
		action string
	}{
		{"deployment.create", "POST", "/api/v1/applications/app_1/deployments", map[string]any{"planId": "plan_1"}, nil, "deployment.create"},
		{"deployment.cancel", "POST", "/api/v1/deployments/{{id}}/cancel", nil, nil, "deployment.cancel"},
		{"plan.create", "POST", "/api/v1/applications/app_1/deployment-plans", map[string]any{"serverId": "srv_1", "environment": "production", "domain": "app.example.com"}, nil, "plan.create"},
		{"domain.add", "POST", "/api/v1/applications/app_1/domains", map[string]any{"hostname": "app.example.com", "environment": "production"}, nil, "domain.add"},
		{"domain.remove", "DELETE", "/api/v1/domains/{{id}}", nil, nil, "domain.remove"},
		{"domain.set_primary", "POST", "/api/v1/domains/{{id}}/primary", nil, nil, "domain.set_primary"},
		{"server.register", "POST", "/api/v1/servers", map[string]any{"name": "srv-eu-9", "address": "203.0.113.99"}, nil, "server.register"},
		{"server.remove", "DELETE", "/api/v1/servers/{{id}}", nil, nil, "server.remove"},
		{"appconfig.set", "PUT", "/api/v1/applications/app_1/configuration/DATABASE_URL", map[string]any{"value": "x"}, nil, "appconfig.set"},
		{"appconfig.delete", "DELETE", "/api/v1/applications/app_1/configuration/DATABASE_URL", nil, nil, "appconfig.delete"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, f := auditServiceHarness(t)
			path := tc.path
			// Create a deployment for cancel, a domain for domain routes.
			if strings.Contains(path, "{{id}}") {
				if tc.name == "deployment.cancel" {
					id := createDeployment(h, h)
					path = strings.ReplaceAll(path, "{{id}}", id)
				} else if strings.HasPrefix(tc.name, "domain.") {
					r := h.do("POST", "/api/v1/applications/app_1/domains", map[string]any{"hostname": "app.example.com", "environment": "production"}, nil)
					expect(t, r, 201, "")
					// Extract domain id from Location header.
					loc := r.hdr.Get("Location")
					path = strings.ReplaceAll(path, "{{id}}", strings.TrimPrefix(loc, "/api/v1/domains/"))
				} else if tc.name == "server.remove" {
					r := h.do("POST", "/api/v1/servers", map[string]any{"name": "srv-eu-9", "address": "203.0.113.99"}, nil)
					expect(t, r, 201, "")
					path = strings.ReplaceAll(path, "{{id}}", "srv_new")
				}
			}
			// appconfig.delete needs the value to exist first.
			if tc.name == "appconfig.delete" {
				h.do("PUT", "/api/v1/applications/app_1/configuration/DATABASE_URL", map[string]any{"value": "x"}, nil)
			}
			h.do(tc.method, path, tc.body, tc.hdr)
			events := f.action(tc.action)
			if len(events) == 0 {
				t.Fatalf("no audit event emitted for %s", tc.action)
			}
			for _, e := range events {
				if e.ActorID != "usr_1" {
					t.Fatalf("%s: actor = %q", tc.action, e.ActorID)
				}
				if e.RequestID == "" {
					t.Fatalf("%s: requestId missing", tc.action)
				}
			}
		})
	}
}

func TestAuditReadEndpointOwnerScoped(t *testing.T) {
	h, f := auditServiceHarness(t)
	// Record events for usr_1 (owner) via a deployment.
	id := createDeployment(h, h)
	h.do("POST", "/api/v1/deployments/"+id+"/cancel", nil, nil)

	// usr_1 sees their own events.
	r := h.do("GET", "/api/v1/audit", nil, nil)
	expect(t, r, 200, "")
	items := r.body["items"].([]any)
	if len(items) == 0 {
		t.Fatal("owner must see their own audit events")
	}
	first := items[0].(map[string]any)
	if first["actorId"] != "usr_1" {
		t.Fatalf("actorId = %v", first["actorId"])
	}
	if first["action"] == "" || first["targetId"] == "" {
		t.Fatalf("event missing action/target: %v", first)
	}

	// Filter by target.
	r = h.do("GET", "/api/v1/audit?target="+id, nil, nil)
	expect(t, r, 200, "")
	items = r.body["items"].([]any)
	if len(items) == 0 {
		t.Fatal("target filter must return events")
	}

	// Filter by actor.
	r = h.do("GET", "/api/v1/audit?actor=usr_1", nil, nil)
	expect(t, r, 200, "")
	items = r.body["items"].([]any)
	if len(items) == 0 {
		t.Fatal("actor filter must return events")
	}

	// A different user (usr_2) sees nothing: owner-scoped.
	h2 := newHarness(t)
	h2.handler = New(Deps{
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:         NewTokenAuthenticator(token, Principal{UserID: "usr_2", Name: "Other"}),
		Deployments:  h2.svc,
		Applications: &fakeApps{items: map[string]application.Record{"app_1": {ID: "app_1", OwnerID: "usr_1"}}},
		Servers:      &fakeServers{}, Logs: &fakeLogs{}, Audit: f,
	})
	r = h2.do("GET", "/api/v1/audit", nil, nil)
	expect(t, r, 200, "")
	items = r.body["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("non-owner must see no events, got %d", len(items))
	}
}

func TestAuditReadEndpointRequiresAuth(t *testing.T) {
	h, _ := auditServiceHarness(t)
	r := h.do("GET", "/api/v1/audit", nil, map[string]string{"Authorization": ""})
	expect(t, r, 401, CodeUnauthorized)
}

func TestAuditReadEndpointInvalidLimit(t *testing.T) {
	h, _ := auditServiceHarness(t)
	expect(t, h.do("GET", "/api/v1/audit?limit=0", nil, nil), 400, CodeInvalidRequest)
	expect(t, h.do("GET", "/api/v1/audit?limit=1001", nil, nil), 400, CodeInvalidRequest)
	expect(t, h.do("GET", "/api/v1/audit?limit=abc", nil, nil), 400, CodeInvalidRequest)
}
