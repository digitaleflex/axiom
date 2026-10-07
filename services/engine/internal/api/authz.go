package api

import (
	"net/http"

	"github.com/digitaleflex/axiom/services/engine/internal/authz"
)

// actorOf mirrors the request principal into the authz actor.
func (a *API) actorOf(r *http.Request) authz.Actor {
	p := principal(r.Context())
	return authz.Actor{UserID: p.UserID, Name: p.Name}
}

// authorize checks the actor's relationship to a resource and reports
// whether the action is permitted. It is defense-in-depth beneath the
// API's 404-hiding rule: handlers call it before acting, and the resource
// loaders independently hide foreign resources. See
// docs/architecture/authorization.md for the deliberate divergence.
func (a *API) authorize(r *http.Request, action authz.Action, res authz.Resource) (bool, string) {
	return a.authz.Authorize(r.Context(), a.actorOf(r), action, res)
}

// requireOwner loads an application and verifies the caller owns it.
// Foreign applications report as not found (no existence leak), matching
// the V0.1 read/deny rule: 404 everywhere for non-owned IDs.
func (a *API) requireOwner(r *http.Request, id string) (ownerID string, ok bool) {
	if a.applications == nil {
		return "", false
	}
	rec, err := a.applications.Get(r.Context(), id)
	if err != nil {
		return "", false
	}
	rel, err := a.authz.Resolve(r.Context(), a.actorOf(r), authz.Resource{
		Type: "application", ID: rec.ID, OwnerID: rec.OwnerID,
	})
	if err != nil || rel != authz.RelationshipOwner {
		return "", false
	}
	return rec.OwnerID, true
}
