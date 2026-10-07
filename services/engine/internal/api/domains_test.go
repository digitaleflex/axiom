package api

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/domains"
)

type fakeDomains struct {
	items  map[string]domains.Record
	check  domains.Record
	target domains.Target
	hasTg  bool
}

func (f *fakeDomains) Get(_ context.Context, id string) (domains.Record, error) {
	r, ok := f.items[id]
	if !ok {
		return domains.Record{}, domains.ErrNotFound
	}
	return r, nil
}
func (f *fakeDomains) List(_ context.Context, app, env string) ([]domains.Record, error) {
	var out []domains.Record
	for _, r := range f.items {
		if r.ApplicationID == app && (env == "" || r.Environment == env) {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeDomains) Create(_ context.Context, app, env, hostname string) (domains.Record, error) {
	name, err := domains.Normalize(hostname)
	if err != nil {
		return domains.Record{}, err
	}
	for _, r := range f.items {
		if r.Hostname == name {
			return domains.Record{}, domains.ErrTaken
		}
	}
	r := domains.Record{ID: "dom_new", ApplicationID: app, Environment: env, Hostname: name, DNSStatus: "unknown", TLSStatus: "pending", RoutingStatus: "pending"}
	f.items[r.ID] = r
	return r, nil
}
func (f *fakeDomains) SetPrimary(_ context.Context, app, id string) (domains.Record, error) {
	r, ok := f.items[id]
	if !ok || r.ApplicationID != app {
		return domains.Record{}, domains.ErrNotFound
	}
	r.IsPrimary = true
	f.items[id] = r
	return r, nil
}
func (f *fakeDomains) Remove(_ context.Context, app, id string) error {
	r, ok := f.items[id]
	if !ok || r.ApplicationID != app {
		return domains.ErrNotFound
	}
	if r.IsPrimary {
		return domains.ErrPrimary
	}
	delete(f.items, id)
	return nil
}
func (f *fakeDomains) CheckDNS(_ context.Context, app, id string) (domains.Record, error) {
	r, ok := f.items[id]
	if !ok || r.ApplicationID != app {
		return domains.Record{}, domains.ErrNotFound
	}
	r.DNSStatus = f.check.DNSStatus
	r.DNSExpected = f.check.DNSExpected
	r.DNSObserved = f.check.DNSObserved
	return r, nil
}
func (f *fakeDomains) RoutingTarget(_ context.Context, _, _ string) (domains.Target, bool, error) {
	return f.target, f.hasTg, nil
}

func domainsHarness(t *testing.T) (*harness, *fakeDomains) {
	t.Helper()
	h := newHarness(t)
	fd := &fakeDomains{items: map[string]domains.Record{
		"dom_1": {ID: "dom_1", ApplicationID: "app_1", Environment: "production", Hostname: "app.acme.dev", IsPrimary: true, DNSStatus: "ok", TLSStatus: "valid", RoutingStatus: "active"},
		"dom_2": {ID: "dom_2", ApplicationID: "app_other", Environment: "production", Hostname: "other.acme.dev"},
	}}
	h.handler = New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: NewTokenAuthenticator(token, Principal{UserID: "usr_1"}),
		Applications: &fakeApps{items: map[string]application.Record{"app_1": {ID: "app_1", OwnerID: "usr_1"}}}, Domains: fd})
	fd.target = domains.Target{DeploymentID: "dep_1", ServerID: "srv_1"}
	fd.hasTg = true
	return h, fd
}

func TestDomainRoutes(t *testing.T) {
	h, _ := domainsHarness(t)
	r := h.do("GET", "/api/v1/applications/app_1/domains", nil, nil)
	expect(t, r, 200, "")
	items := r.body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["hostname"] != "app.acme.dev" {
		t.Fatalf("list = %v", r.body)
	}
	if items[0].(map[string]any)["target"] == nil {
		t.Fatal("routing target must be included")
	}
	expect(t, h.do("GET", "/api/v1/applications/app_1/domains?environment=staging", nil, nil), 200, "")
	expect(t, h.do("GET", "/api/v1/applications/app_1/domains?environment=dev", nil, nil), 400, CodeInvalidRequest)

	r = h.do("POST", "/api/v1/applications/app_1/domains", map[string]any{"hostname": "WWW.Acme.Dev", "environment": "staging"}, nil)
	expect(t, r, 201, "")
	if r.body["hostname"] != "www.acme.dev" || r.hdr.Get("Location") != "/api/v1/domains/dom_new" {
		t.Fatalf("create normalizes: %v", r.body)
	}
	expect(t, h.do("POST", "/api/v1/applications/app_1/domains", map[string]any{"hostname": "app.acme.dev"}, nil), 409, CodeConflict)
	expect(t, h.do("POST", "/api/v1/applications/app_1/domains", map[string]any{"hostname": "not a host"}, nil), 422, CodeValidationFailed)
	expect(t, h.do("POST", "/api/v1/applications/app_1/domains", map[string]any{"hostname": "x.test", "environment": "dev"}, nil), 422, CodeValidationFailed)

	r = h.do("POST", "/api/v1/domains/dom_1/primary", nil, nil)
	expect(t, r, 200, "")
	expect(t, h.do("POST", "/api/v1/domains/dom_2/primary", nil, nil), 404, CodeNotFound) // foreign: no leak
	expect(t, h.do("POST", "/api/v1/domains/dom_x/primary", nil, nil), 404, CodeNotFound)

	expect(t, h.do("DELETE", "/api/v1/domains/dom_1", nil, nil), 409, CodeConflict) // primary
	expect(t, h.do("DELETE", "/api/v1/domains/dom_2", nil, nil), 404, CodeNotFound)
	expect(t, h.do("DELETE", "/api/v1/domains/dom_x", nil, nil), 404, CodeNotFound)
}

func TestDomainCheckRoute(t *testing.T) {
	h, fd := domainsHarness(t)
	fd.check = domains.Record{DNSStatus: "mismatch", DNSExpected: "203.0.113.10", DNSObserved: "198.51.100.7"}
	r := h.do("POST", "/api/v1/domains/dom_1/check", nil, nil)
	expect(t, r, 200, "")
	if r.body["dnsStatus"] != "mismatch" || r.body["dnsExpected"] != "203.0.113.10" {
		t.Fatalf("check = %v", r.body)
	}
	expect(t, h.do("POST", "/api/v1/domains/dom_2/check", nil, nil), 404, CodeNotFound)
}

func TestPlanRejectsUnregisteredDomain(t *testing.T) {
	h := newHarness(t)
	h.handler = New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: NewTokenAuthenticator(token, Principal{UserID: "usr_1"}),
		Applications: &fakeApps{items: map[string]application.Record{"app_1": {ID: "app_1", OwnerID: "usr_1"}}},
		Plans:        fakePlans{err: domains.ErrNotRegistered}})
	r := h.do("POST", "/api/v1/applications/app_1/deployment-plans", map[string]any{"serverId": "srv_1", "environment": "production", "domain": "ghost.acme.dev"}, nil)
	expect(t, r, 422, CodeValidationFailed)
	if !strings.Contains(r.body["error"].(map[string]any)["details"].(map[string]any)["fields"].(map[string]any)["domain"].(string), "register") {
		t.Fatalf("field error = %v", r.body)
	}
}
