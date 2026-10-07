package api

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
	"github.com/digitaleflex/axiom/services/engine/internal/planner/validation"
)

type fakePlans struct{ err error }

func (f fakePlans) Create(_ context.Context, in planner.CreateInput) (planner.Plan, error) {
	if f.err != nil {
		return planner.Plan{}, f.err
	}
	return planner.Plan{ID: "plan_1", ApplicationID: in.ApplicationID, Status: "READY", Steps: []string{"BUILD"}}, nil
}
func (f fakePlans) Get(_ context.Context, id string) (planner.PlanView, error) {
	if id == "plan_other" {
		return planner.PlanView{Plan: planner.Plan{ID: id, ApplicationID: "app_other"}}, nil
	}
	if id != "plan_1" {
		return planner.PlanView{}, planner.ErrPlanNotFound
	}
	return planner.PlanView{Plan: planner.Plan{ID: id, ApplicationID: "app_1"}, Stale: true}, nil
}

func TestPlanRoutes(t *testing.T) {
	newH := func(p Plans) *harness {
		h := newHarness(t)
		h.handler = New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: NewTokenAuthenticator(token, Principal{UserID: "usr_1"}),
			Applications: &fakeApps{items: map[string]application.Record{"app_1": {ID: "app_1", OwnerID: "usr_1"}, "app_other": {ID: "app_other", OwnerID: "usr_2"}}}, Plans: p})
		return h
	}
	h := newH(fakePlans{})
	body := map[string]any{"serverId": "srv_1", "environment": "production", "domain": "app.acme.dev"}
	r := h.do("POST", "/api/v1/applications/app_1/deployment-plans", body, nil)
	expect(t, r, 201, "")
	if r.body["id"] != "plan_1" || r.hdr.Get("Location") != "/api/v1/deployment-plans/plan_1" || r.body["stale"] != false {
		t.Fatalf("create = %v", r.body)
	}
	expect(t, h.do("POST", "/api/v1/applications/app_1/deployment-plans", map[string]any{}, nil), 422, CodeValidationFailed)
	if r = h.do("GET", "/api/v1/deployment-plans/plan_1", nil, nil); r.code != 200 || r.body["stale"] != true {
		t.Fatalf("get = %d %v", r.code, r.body)
	}
	expect(t, h.do("GET", "/api/v1/deployment-plans/plan_other", nil, nil), 404, CodeNotFound)
	expect(t, h.do("GET", "/api/v1/deployment-plans/plan_x", nil, nil), 404, CodeNotFound)

	cases := []struct {
		err  error
		code int
		api  string
	}{
		{&planner.EligibilityError{Reasons: []string{"server is OFFLINE"}}, 422, CodeDeploymentNotEligible},
		{validation.Errors{{Field: "network.domain", Message: "bad"}}, 422, CodeValidationFailed},
		{planner.ErrProfileNotReady, 409, CodeConflict},
	}
	for _, c := range cases {
		expect(t, newH(fakePlans{err: c.err}).do("POST", "/api/v1/applications/app_1/deployment-plans", body, nil), c.code, c.api)
	}
	r = newH(fakePlans{err: validation.Errors{{Field: "network.domain", Message: "bad"}}}).do("POST", "/api/v1/applications/app_1/deployment-plans", body, nil)
	if f := r.body["error"].(map[string]any)["details"].(map[string]any)["fields"].(map[string]any); f["network.domain"] != "bad" {
		t.Fatalf("field-addressable errors expected: %v", f)
	}
}
