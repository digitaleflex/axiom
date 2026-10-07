package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/digitaleflex/axiom/services/engine/internal/domains"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
	"github.com/digitaleflex/axiom/services/engine/internal/planner/validation"
)

// Plans generates and reads deployment plans (#60/#97).
type Plans interface {
	Create(ctx context.Context, in planner.CreateInput) (planner.Plan, error)
	Get(ctx context.Context, id string) (planner.PlanView, error)
}

func (a *API) createPlan(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("applicationID")
	defer func() { a.audit(r, "plan.create", target, err) }()
	if a.plans == nil {
		return errUnavailable
	}
	app, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	var in struct {
		ServerID    string `json:"serverId"`
		Environment string `json:"environment"`
		Domain      string `json:"domain"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	fields := map[string]any{}
	if in.ServerID == "" {
		fields["serverId"] = "required"
	}
	if in.Environment == "" {
		fields["environment"] = "required"
	}
	if in.Domain == "" {
		fields["domain"] = "required"
	}
	if len(fields) > 0 {
		return errValidation("invalid plan request", map[string]any{"fields": fields})
	}
	plan, err := a.plans.Create(r.Context(), planner.CreateInput{ApplicationID: app.ID, ServerID: in.ServerID, Environment: in.Environment, Domain: in.Domain})
	if err != nil {
		return err
	}
	w.Header().Set("Location", "/api/v1/deployment-plans/"+plan.ID)
	target = plan.ID
	writeJSON(w, http.StatusCreated, planner.PlanView{Plan: plan})
	return nil
}

func (a *API) getPlan(w http.ResponseWriter, r *http.Request) error {
	if a.plans == nil {
		return errUnavailable
	}
	id := r.PathValue("planID")
	v, err := a.plans.Get(r.Context(), id)
	if err != nil {
		return err
	}
	if _, err := a.ownedApplication(r, v.ApplicationID); err != nil {
		return errNotFound("deploymentPlan", id)
	}
	writeJSON(w, http.StatusOK, v)
	return nil
}

func planError(err error) (*Error, bool) {
	var verrs validation.Errors
	var elig *planner.EligibilityError
	switch {
	case errors.As(err, &verrs):
		fields := map[string]any{}
		for _, e := range verrs {
			fields[e.Field] = e.Message
		}
		return errValidation("the deployment plan is invalid", map[string]any{"fields": fields}), true
	case errors.As(err, &elig):
		return newError(http.StatusUnprocessableEntity, CodeDeploymentNotEligible, "the selected server is not eligible for this deployment", map[string]any{"reasons": elig.Reasons}), true
	case errors.Is(err, planner.ErrProfileNotReady):
		return newError(http.StatusConflict, CodeConflict, "the application profile must be ready before planning", map[string]any{"reason": "profile_not_ready"}), true
	case errors.Is(err, planner.ErrPlanNotFound):
		return newError(http.StatusNotFound, CodeNotFound, "deployment plan not found", nil), true
	case errors.Is(err, domains.ErrNotFound):
		return newError(http.StatusNotFound, CodeNotFound, "domain not found", nil), true
	case errors.Is(err, domains.ErrTaken):
		return newError(http.StatusConflict, CodeConflict, "hostname is already in use", map[string]any{"reason": "hostname_taken"}), true
	case errors.Is(err, domains.ErrPrimary):
		return newError(http.StatusConflict, CodeConflict, "set another primary domain before removing this one", map[string]any{"reason": "primary_domain"}), true
	case errors.Is(err, domains.ErrInvalidHostname):
		return errValidation("invalid domain", map[string]any{"fields": map[string]any{"hostname": "must be a hostname such as app.example.com"}}), true
	case errors.Is(err, domains.ErrNotRegistered):
		return errValidation("the hostname is not registered for this application and environment", map[string]any{"fields": map[string]any{"domain": "register it first or use a registered hostname"}}), true
	}
	return nil, false
}
