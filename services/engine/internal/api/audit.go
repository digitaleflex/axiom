package api

import (
	"net/http"
	"strconv"

	"github.com/digitaleflex/axiom/services/engine/internal/audit"
)

// listAuditEvents returns the caller's audit trail (#128). The trail is
// owner-scoped: only events for resources the caller may read (events they
// own, denormalized onto each event at record time) are returned. Results
// never contain secrets — the audit service redacts every field before
// persistence.
//
// Query parameters:
//   - target: filter by target ID (e.g. deployment or domain ID)
//   - actor: filter by actor ID
//   - limit: max events to return (default 100, max 1000)
func (a *API) listAuditEvents(w http.ResponseWriter, r *http.Request) error {
	if a.auditSvc == nil {
		return errUnavailable
	}
	q := r.URL.Query()
	f := audit.Filter{
		ActorID:  q.Get("actor"),
		TargetID: q.Get("target"),
		OwnerID:  principal(r.Context()).UserID,
	}
	limit := 100
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			return errInvalid("limit must be an integer between 1 and 1000")
		}
		limit = n
	}
	f.Limit = limit
	items, err := a.auditSvc.List(r.Context(), f)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}
