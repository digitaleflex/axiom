package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/digitaleflex/axiom/services/engine/internal/authz"
	"github.com/digitaleflex/axiom/services/engine/internal/diagnostics"
	"github.com/digitaleflex/axiom/services/engine/internal/executor"
	"github.com/digitaleflex/axiom/services/engine/internal/health"
)

// Diagnostics exposes read-only deployment diagnostics (#102): why a
// deployment failed, without SSH. Two routes, one page each:
//
//	GET /api/v1/deployments/{deploymentID}/diagnostics        the full report
//	GET /api/v1/deployments/{deploymentID}/diagnostics/logs   one page of logs
//
// Both load the deployment through loadDeployment, so a deployment owned by
// someone else answers 404 (no existence leak) exactly like the other
// deployment routes, and both are strictly read-only: no route here changes
// a deployment, a server or a configuration.

// deploymentDiagnostic returns the full diagnostic report for a deployment.
//
// Read-only by construction: it loads the authorized deployment and asks the
// diagnostics service for a projection of already-persisted state. It cannot
// advance, retry, cancel or repair a stuck deployment — by design.
func (a *API) deploymentDiagnostic(w http.ResponseWriter, r *http.Request) error {
	rec, err := a.loadDeployment(r)
	if err != nil {
		return err
	}
	if a.diagnostics == nil {
		return errUnavailable
	}
	// authz defense-in-depth: loadDeployment already hides foreign
	// deployments behind a 404, so this can only fail closed.
	if ok, _ := a.authorize(r, authz.ActionDeploymentRead, authz.Resource{
		Type: "application", ID: rec.ApplicationID, OwnerID: a.appOwnerID(r, rec.ApplicationID),
	}); !ok {
		return errNotFound("deployment", rec.ID)
	}
	report, err := a.diagnostics.Report(r.Context(), rec)
	if err != nil {
		return fromDiagnostics(err)
	}
	writeJSON(w, http.StatusOK, report)
	return nil
}

// deploymentDiagnosticLogs serves one bounded page of the deployment
// journal, for operators who need to page further than the embedded
// excerpt. It reuses the same paginated store as
// GET /deployments/{id}/logs, so the diagnostic never loads the whole
// journal into memory, and re-applies redaction on the way out.
func (a *API) deploymentDiagnosticLogs(w http.ResponseWriter, r *http.Request) error {
	rec, err := a.loadDeployment(r)
	if err != nil {
		return err
	}
	if a.diagnostics == nil {
		return errUnavailable
	}
	q := r.URL.Query()
	limit := diagnostics.DefaultPageSize
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > diagnostics.MaxPageSize {
			return errInvalid("limit must be an integer between 1 and 100")
		}
		limit = n
	}
	page, err := a.diagnostics.Logs(r.Context(), rec.ID, diagnostics.LogQuery{
		Level: q.Get("level"), Step: q.Get("step"), Source: q.Get("source"),
		Search: q.Get("q"), Cursor: q.Get("cursor"), Limit: limit,
	})
	if err != nil {
		return fromDiagnostics(err)
	}
	writeJSON(w, http.StatusOK, page)
	return nil
}

// fromDiagnostics maps diagnostics errors onto the stable error envelope.
// The messages are operator-facing and already free of internal detail.
func fromDiagnostics(err error) error {
	switch {
	case errors.Is(err, diagnostics.ErrNotFound):
		return newError(http.StatusNotFound, CodeNotFound, "deployment not found", nil)
	case errors.Is(err, diagnostics.ErrLogsUnavailable):
		return errUnavailable
	case errors.Is(err, diagnostics.ErrInvalidQuery):
		return errInvalid(strings.TrimPrefix(err.Error(), diagnostics.ErrInvalidQuery.Error()+": "))
	default:
		return err
	}
}

// probeLookupFor wires the diagnostics service to the persisted health
// probe. It reuses the executor's own reader so the diagnostic and
// GET /deployments/{id}/health can never disagree on the last probe result.
func (a *API) probeLookupFor() diagnostics.ProbeLookup {
	if a.deployments == nil {
		return nil
	}
	store := a.deployments.Store()
	return func(ctx context.Context, deploymentID string) (health.ProbeReport, bool, error) {
		return executor.LastHealthResult(ctx, store, deploymentID)
	}
}
