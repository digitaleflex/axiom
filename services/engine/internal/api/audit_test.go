package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/application"
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
