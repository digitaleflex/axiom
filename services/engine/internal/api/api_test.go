package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

const token = "test-token-0123456789abcdef0123456789"

// --- fakes --------------------------------------------------------------------

type fakeApps struct {
	mu    sync.Mutex
	items map[string]application.Record
}

func (f *fakeApps) Create(_ context.Context, r application.Record) (application.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.RepositoryID == "repo_missing" {
		return application.Record{}, application.ErrRepositoryNotFound
	}
	r.CreatedAt, r.UpdatedAt = time.Now().UTC(), time.Now().UTC()
	f.items[r.ID] = r
	return r, nil
}
func (f *fakeApps) Get(_ context.Context, id string) (application.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.items[id]
	if !ok {
		return application.Record{}, application.ErrNotFound
	}
	return r, nil
}
func (f *fakeApps) List(_ context.Context, owner string, limit, offset int) ([]application.Record, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []application.Record
	for _, r := range f.items {
		if r.OwnerID == owner {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	total := len(out)
	if offset > len(out) {
		offset = len(out)
	}
	out = out[offset:]
	if len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

type fakeServers struct{ items []server.Record }

func (f fakeServers) Get(_ context.Context, id string) (server.Record, error) {
	for _, s := range f.items {
		if s.ID == id {
			return s, nil
		}
	}
	return server.Record{}, server.ErrNotFound
}
func (f *fakeServers) Register(_ context.Context, owner, name, address string) (server.Record, error) {
	if !server.ValidName(name) {
		return server.Record{}, server.ErrInvalidName
	}
	if !server.ValidAddress(address) {
		return server.Record{}, server.ErrInvalidAddress
	}
	s := server.Record{ID: "srv_new", Name: name, Address: address, OwnerID: owner, Status: server.StatusPending}
	f.items = append(f.items, s)
	return s, nil
}
func (f *fakeServers) Rename(_ context.Context, id, name string) (server.Record, error) {
	if !server.ValidName(name) {
		return server.Record{}, server.ErrInvalidName
	}
	for i, s := range f.items {
		if s.ID == id {
			f.items[i].Name = name
			return f.items[i], nil
		}
	}
	return server.Record{}, server.ErrNotFound
}
func (f *fakeServers) Remove(_ context.Context, id string) error {
	for i, s := range f.items {
		if s.ID == id {
			if id == "srv_1" {
				return server.ErrInUse
			}
			f.items = append(f.items[:i], f.items[i+1:]...)
			return nil
		}
	}
	return server.ErrNotFound
}
func (f *fakeServers) UpdateHealth(_ context.Context, id string, health server.Health) error {
	for i, s := range f.items {
		if s.ID == id {
			f.items[i].Status = health.Status
			f.items[i].AgentVersion = health.AgentVersion
			f.items[i].Capabilities = health.Capabilities
			f.items[i].CPUCount = health.CPUCount
			f.items[i].MemoryMB = health.MemoryMB
			f.items[i].DiskFreeMB = health.DiskFreeMB
			f.items[i].LastSeenAt = health.LastSeenAt
			return nil
		}
	}
	return server.ErrNotFound
}

func (f *fakeServers) ListFiltered(_ context.Context, status string, limit, offset int) ([]server.Record, int, error) {
	var items []server.Record
	for _, s := range f.items {
		if status == "" || string(s.Status) == status {
			items = append(items, s)
		}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	if offset > end {
		offset = end
	}
	return items[offset:end], len(items), nil
}

// --- harness ------------------------------------------------------------------

type harness struct {
	t       *testing.T
	handler http.Handler
	store   *deployment.MemoryStore
	svc     *deployment.Service
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store := deployment.NewMemoryStore()
	for _, p := range []string{"plan_1", "plan_2", "plan_3"} {
		store.AddPlan(deployment.MemoryPlan{ID: p, ApplicationID: "app_1", ServerID: "srv_1", Environment: "production", Steps: []string{"BUILD", "CREATE_RUNTIME", "NETWORK", "START", "VERIFY"}})
	}
	store.AddPlan(deployment.MemoryPlan{ID: "plan_other", ApplicationID: "app_other", ServerID: "srv_1", Environment: "production", Steps: []string{"BUILD"}})
	svc := deployment.NewService(store, nil)
	apps := &fakeApps{items: map[string]application.Record{
		"app_1":     {ID: "app_1", Name: "acme-web", RepositoryID: "repo_1", OwnerID: "usr_1"},
		"app_other": {ID: "app_other", Name: "other", RepositoryID: "repo_2", OwnerID: "usr_2"},
	}}
	servers := &fakeServers{items: []server.Record{
		{ID: "srv_1", Name: "srv-eu-1", Address: "203.0.113.10", Status: server.StatusReady, AgentVersion: "0.1.3", Capabilities: []server.Capability{server.CapabilityDocker}, CPUCount: 4, MemoryMB: 8192, DiskFreeMB: 50000},
		{ID: "srv_2", Name: "srv-eu-2", Status: server.StatusOffline},
	}}
	h := New(Deps{
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:         NewTokenAuthenticator(token, Principal{UserID: "usr_1", Name: "Jane"}),
		Deployments:  svc,
		Applications: apps,
		Servers:      servers,
		Logs:         &fakeLogs{},
	})
	return &harness{t: t, handler: h, store: store, svc: svc}
}

type resp struct {
	code int
	hdr  http.Header
	body map[string]any
}

func (h *harness) do(method, path string, body any, hdr map[string]string) resp {
	h.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, req)
	out := resp{code: rr.Code, hdr: rr.Header()}
	if rr.Body.Len() > 0 {
		_ = json.Unmarshal(rr.Body.Bytes(), &out.body)
	}
	return out
}

func errCode(r resp) string {
	e, _ := r.body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func expect(t *testing.T, r resp, code int, errorCode string) {
	t.Helper()
	if r.code != code || (errorCode != "" && errCode(r) != errorCode) {
		t.Fatalf("got %d %s %v, want %d %s", r.code, errCode(r), r.body, code, errorCode)
	}
}

// --- tests --------------------------------------------------------------------

func TestAuthenticationBoundary(t *testing.T) {
	h := newHarness(t)
	r := h.do("GET", "/api/v1/auth/me", nil, map[string]string{"Authorization": ""})
	expect(t, r, 401, CodeUnauthorized)
	if r.hdr.Get("WWW-Authenticate") == "" {
		t.Fatal("missing WWW-Authenticate")
	}
	expect(t, h.do("GET", "/api/v1/auth/me", nil, map[string]string{"Authorization": "Bearer wrong"}), 401, CodeUnauthorized)
	r = h.do("GET", "/api/v1/auth/me", nil, nil)
	expect(t, r, 200, "")
	if r.body["id"] != "usr_1" {
		t.Fatalf("me = %v", r.body)
	}
	expect(t, h.do("POST", "/api/v1/auth/logout", nil, nil), 204, "")

	// No authenticator configured: fail closed.
	closed := New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	rr := httptest.NewRecorder()
	closed.ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/auth/me", nil))
	if rr.Code != 401 {
		t.Fatalf("unconfigured auth must reject, got %d", rr.Code)
	}
}

func TestErrorEnvelopeAndRequestID(t *testing.T) {
	h := newHarness(t)
	r := h.do("GET", "/api/v1/nope", nil, map[string]string{"X-Request-ID": "req_client-1"})
	expect(t, r, 404, CodeNotFound)
	e := r.body["error"].(map[string]any)
	if e["requestId"] != "req_client-1" || r.hdr.Get("X-Request-ID") != "req_client-1" || e["message"] == "" {
		t.Fatalf("envelope = %v", e)
	}
	if _, ok := e["details"].(map[string]any); !ok {
		t.Fatal("details must be an object")
	}
	r = h.do("GET", "/api/v1/nope", nil, map[string]string{"X-Request-ID": "bad id with spaces"})
	if id := r.hdr.Get("X-Request-ID"); !strings.HasPrefix(id, "req_") {
		t.Fatalf("invalid client request id must be replaced, got %q", id)
	}
}

func TestApplications(t *testing.T) {
	h := newHarness(t)
	expect(t, h.do("POST", "/api/v1/applications", map[string]any{"repositoryId": "repo_1", "name": "Bad Name"}, nil), 422, CodeValidationFailed)
	expect(t, h.do("POST", "/api/v1/applications", map[string]any{"repositoryId": "repo_1", "name": "ok", "extra": 1}, nil), 400, CodeInvalidRequest)
	expect(t, h.do("POST", "/api/v1/applications", `{"repositoryId":"repo_1","name":"ok"}`, map[string]string{"Content-Type": "text/plain"}), 415, CodeInvalidRequest)
	expect(t, h.do("POST", "/api/v1/applications", `{"repositoryId":`, nil), 400, CodeInvalidRequest)
	expect(t, h.do("POST", "/api/v1/applications", map[string]any{"repositoryId": "repo_missing", "name": "ok"}, nil), 404, CodeNotFound)

	r := h.do("POST", "/api/v1/applications", map[string]any{"repositoryId": "repo_1", "name": "shop-web"}, nil)
	expect(t, r, 201, "")
	id, _ := r.body["id"].(string)
	if !strings.HasPrefix(id, "app_") || r.hdr.Get("Location") != "/api/v1/applications/"+id {
		t.Fatalf("created = %v location=%s", r.body, r.hdr.Get("Location"))
	}
	expect(t, h.do("GET", "/api/v1/applications/"+id, nil, nil), 200, "")
	expect(t, h.do("GET", "/api/v1/applications/app_other", nil, nil), 404, CodeNotFound) // not owned: no leak

	r = h.do("GET", "/api/v1/applications?limit=1&page=2", nil, nil)
	expect(t, r, 200, "")
	if r.body["total"].(float64) != 2 || r.body["page"].(float64) != 2 || len(r.body["items"].([]any)) != 1 {
		t.Fatalf("pagination = %v", r.body)
	}
	expect(t, h.do("GET", "/api/v1/applications?limit=1000", nil, nil), 400, CodeInvalidRequest)
	expect(t, h.do("GET", "/api/v1/applications?page=0", nil, nil), 400, CodeInvalidRequest)
}

func TestServers(t *testing.T) {
	h := newHarness(t)
	r := h.do("GET", "/api/v1/servers", nil, nil)
	expect(t, r, 200, "")
	items := r.body["items"].([]any)
	first := items[0].(map[string]any)
	if len(items) != 2 || first["status"] != "READY" || first["resources"].(map[string]any)["memoryMb"].(float64) != 8192 {
		t.Fatalf("servers = %v", r.body)
	}
	expect(t, h.do("GET", "/api/v1/servers/srv_2/health", nil, nil), 200, "")
	expect(t, h.do("GET", "/api/v1/servers/srv_x", nil, nil), 404, CodeNotFound)
	expect(t, h.do("GET", "/api/v1/servers?status=ready", nil, nil), 200, "")
	expect(t, h.do("GET", "/api/v1/servers?status=bogus", nil, nil), 400, CodeInvalidRequest)

	r = h.do("POST", "/api/v1/servers", map[string]any{"name": "srv-eu-3", "address": "203.0.113.12"}, nil)
	expect(t, r, 201, "")
	if r.body["id"] != "srv_new" || r.body["status"] != "PENDING" || r.hdr.Get("Location") != "/api/v1/servers/srv_new" {
		t.Fatalf("register = %v", r.body)
	}
	expect(t, h.do("POST", "/api/v1/servers", map[string]any{"name": "Bad Name", "address": "h"}, nil), 422, CodeValidationFailed)
	expect(t, h.do("POST", "/api/v1/servers", map[string]any{"name": "ok", "address": "http://h/"}, nil), 422, CodeValidationFailed)
	expect(t, h.do("POST", "/api/v1/servers", map[string]any{"name": "ok"}, nil), 422, CodeValidationFailed)

	r = h.do("PATCH", "/api/v1/servers/srv_new", map[string]any{"name": "srv-eu-4"}, nil)
	expect(t, r, 200, "")
	if r.body["name"] != "srv-eu-4" {
		t.Fatalf("rename = %v", r.body)
	}
	expect(t, h.do("PATCH", "/api/v1/servers/srv_new", map[string]any{"name": "bad name"}, nil), 422, CodeValidationFailed)
	expect(t, h.do("PATCH", "/api/v1/servers/srv_x", map[string]any{"name": "srv-9"}, nil), 404, CodeNotFound)

	expect(t, h.do("DELETE", "/api/v1/servers/srv_1", nil, nil), 409, CodeConflict) // in use (fake)
	expect(t, h.do("DELETE", "/api/v1/servers/srv_new", nil, nil), 204, "")
	expect(t, h.do("DELETE", "/api/v1/servers/srv_x", nil, nil), 404, CodeNotFound)
}

func TestDeploymentLifecycle(t *testing.T) {
	h := newHarness(t)
	path := "/api/v1/applications/app_1/deployments"
	expect(t, h.do("POST", path, map[string]any{}, nil), 422, CodeValidationFailed)
	expect(t, h.do("POST", path, map[string]any{"planId": "plan_missing"}, nil), 404, CodeNotFound)
	expect(t, h.do("POST", path, map[string]any{"planId": "plan_1", "serverId": "srv_9"}, nil), 400, CodeInvalidRequest) // server comes from plan only
	expect(t, h.do("POST", "/api/v1/applications/app_other/deployments", map[string]any{"planId": "plan_other"}, nil), 404, CodeNotFound)

	key := map[string]string{"Idempotency-Key": "deploy:plan_1"}
	r := h.do("POST", path, map[string]any{"planId": "plan_1"}, key)
	expect(t, r, 202, "")
	id := r.body["id"].(string)
	if r.body["status"] != "PENDING" || r.body["environment"] != "production" || r.hdr.Get("Location") != "/api/v1/deployments/"+id {
		t.Fatalf("created = %v", r.body)
	}
	replay := h.do("POST", path, map[string]any{"planId": "plan_1"}, key)
	expect(t, replay, 202, "")
	if replay.body["id"] != id || replay.hdr.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay must return the original deployment: %v", replay.body)
	}
	expect(t, h.do("POST", path, map[string]any{"planId": "plan_2"}, key), 409, CodeConflict) // key reuse, different request
	expect(t, h.do("POST", path, map[string]any{"planId": "plan_1"}, nil), 409, CodeConflict) // plan single-use
	expect(t, h.do("POST", path, map[string]any{"planId": "plan_3"}, map[string]string{"Idempotency-Key": strings.Repeat("k", 256)}), 400, CodeInvalidRequest)

	r = h.do("GET", "/api/v1/deployments/"+id, nil, nil)
	expect(t, r, 200, "")
	if r.body["number"].(float64) != 1 {
		t.Fatalf("deployment = %v", r.body)
	}
	expect(t, h.do("GET", "/api/v1/deployments/dep_missing", nil, nil), 404, CodeNotFound)

	r = h.do("GET", "/api/v1/deployments/"+id+"/steps", nil, nil)
	expect(t, r, 200, "")
	if len(r.body["items"].([]any)) != 5 {
		t.Fatalf("steps = %v", r.body)
	}

	r = h.do("GET", path+"?environment=production&status=PENDING", nil, nil)
	expect(t, r, 200, "")
	if r.body["total"].(float64) != 1 {
		t.Fatalf("list = %v", r.body)
	}
	expect(t, h.do("GET", path+"?status=RUNNING", nil, nil), 400, CodeInvalidRequest)
	expect(t, h.do("GET", path+"?environment=dev", nil, nil), 400, CodeInvalidRequest)

	r = h.do("POST", "/api/v1/deployments/"+id+"/cancel", nil, nil)
	expect(t, r, 200, "")
	if r.body["status"] != "CANCELLED" {
		t.Fatalf("cancel = %v", r.body)
	}
	expect(t, h.do("POST", "/api/v1/deployments/"+id+"/cancel", nil, nil), 409, CodeDeploymentInvalidState)

	r = h.do("GET", "/api/v1/deployments/"+id+"/events", nil, nil)
	expect(t, r, 200, "")
	events := r.body["items"].([]any)
	if len(events) != 2 || events[0].(map[string]any)["type"] != deployment.EventCreated || events[1].(map[string]any)["seq"].(float64) != 2 {
		t.Fatalf("events = %v", r.body)
	}
	r = h.do("GET", "/api/v1/deployments/"+id+"/events?after=1", nil, nil)
	if len(r.body["items"].([]any)) != 1 {
		t.Fatalf("events after 1 = %v", r.body)
	}
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/events?after=-1", nil, nil), 400, CodeInvalidRequest)

	r = h.do("GET", "/api/v1/deployments/"+id+"/logs", nil, nil)
	expect(t, r, 200, "")
	expect(t, h.do("GET", "/api/v1/deployments/"+id+"/logs?level=trace", nil, nil), 400, CodeInvalidRequest)
	r = h.do("GET", "/api/v1/deployments/"+id+"/health", nil, nil)
	if r.body["status"] != "UNKNOWN" {
		t.Fatalf("health = %v", r.body)
	}
}

func TestForeignDeploymentIsHidden(t *testing.T) {
	h := newHarness(t)
	rec, _, err := h.svc.Create(context.Background(), deployment.CreateInput{ApplicationID: "app_other", PlanID: "plan_other"})
	if err != nil {
		t.Fatal(err)
	}
	expect(t, h.do("GET", "/api/v1/deployments/"+rec.ID, nil, nil), 404, CodeNotFound)
	expect(t, h.do("POST", "/api/v1/deployments/"+rec.ID+"/cancel", nil, nil), 404, CodeNotFound)
	expect(t, h.do("GET", "/api/v1/deployments/"+rec.ID+"/events/stream", nil, nil), 503, CodeServiceUnavailable)
}

func TestMissingDependenciesAnswer503(t *testing.T) {
	h := New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: DevAuthenticator{Principal: Principal{UserID: "u"}}})
	for _, path := range []string{"/api/v1/applications", "/api/v1/servers", "/api/v1/deployments/dep_1"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != 503 || !strings.Contains(rr.Body.String(), CodeServiceUnavailable) {
			t.Fatalf("%s = %d %s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestLargeBodyRejected(t *testing.T) {
	h := newHarness(t)
	big := `{"repositoryId":"repo_1","name":"` + strings.Repeat("a", maxBodyBytes) + `"}`
	expect(t, h.do("POST", "/api/v1/applications", big, nil), 413, CodeInvalidRequest)
}
