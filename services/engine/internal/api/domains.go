package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/digitaleflex/axiom/services/engine/internal/authz"
	"github.com/digitaleflex/axiom/services/engine/internal/domains"
	"github.com/digitaleflex/axiom/services/engine/internal/logs"
)

// LogStore serves persisted deployment logs (#66).
type LogStore interface {
	List(ctx context.Context, deploymentID string, f logs.Filter) ([]logs.Entry, string, error)
}

// Domains manages application hostnames (#64).
type Domains interface {
	Get(ctx context.Context, id string) (domains.Record, error)
	List(ctx context.Context, applicationID, environment string) ([]domains.Record, error)
	Create(ctx context.Context, applicationID, environment, hostname string) (domains.Record, error)
	SetPrimary(ctx context.Context, applicationID, id string) (domains.Record, error)
	Remove(ctx context.Context, applicationID, id string) error
	CheckDNS(ctx context.Context, applicationID, id string) (domains.Record, error)
	RoutingTarget(ctx context.Context, applicationID, environment string) (domains.Target, bool, error)
}

type domainDTO struct {
	domains.Record
	Target *domains.Target `json:"target,omitempty"`
}

func (a *API) withTarget(ctx context.Context, r domains.Record) domainDTO {
	out := domainDTO{Record: r}
	if t, ok, err := a.domains.RoutingTarget(ctx, r.ApplicationID, r.Environment); err == nil && ok {
		out.Target = &t
	}
	return out
}

// ownedDomain resolves a domain ID to its record after verifying the caller
// owns the application. Foreign domains report as not found (no leak).
// Ownership is resolved through authz (#127).
func (a *API) ownedDomain(r *http.Request) (domains.Record, error) {
	if a.domains == nil {
		return domains.Record{}, errUnavailable
	}
	id := r.PathValue("domainID")
	rec, err := a.domains.Get(r.Context(), id)
	if errors.Is(err, domains.ErrNotFound) {
		return domains.Record{}, errNotFound("domain", id)
	}
	if err != nil {
		return domains.Record{}, err
	}
	rel, err := a.authz.Resolve(r.Context(), a.actorOf(r), authz.Resource{
		Type: "application", ID: rec.ApplicationID, OwnerID: a.appOwnerID(r, rec.ApplicationID),
	})
	if err != nil || rel != authz.RelationshipOwner {
		return domains.Record{}, errNotFound("domain", id)
	}
	return rec, nil
}

func (a *API) listDomains(w http.ResponseWriter, r *http.Request) error {
	if a.domains == nil {
		return errUnavailable
	}
	app, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	env := r.URL.Query().Get("environment")
	switch env {
	case "", "production", "staging", "preview":
	default:
		return errInvalid("environment must be production, staging or preview")
	}
	items, err := a.domains.List(r.Context(), app.ID, env)
	if err != nil {
		return err
	}
	out := make([]domainDTO, 0, len(items))
	for _, d := range items {
		out = append(out, a.withTarget(r.Context(), d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
	return nil
}

func (a *API) addDomain(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("applicationID")
	defer func() { a.audit(r, "domain.add", target, principal(r.Context()).UserID, err) }()
	if a.domains == nil {
		return errUnavailable
	}
	app, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	// authz defense-in-depth: the caller must own the application.
	if ok, _ := a.authorize(r, authz.ActionDomainWrite, authz.Resource{
		Type: "application", ID: app.ID, OwnerID: app.OwnerID,
	}); !ok {
		return errNotFound("application", app.ID)
	}
	var in struct {
		Hostname    string `json:"hostname"`
		Environment string `json:"environment"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if in.Environment == "" {
		in.Environment = "production"
	}
	switch in.Environment {
	case "production", "staging", "preview":
	default:
		return errValidation("invalid domain", map[string]any{"fields": map[string]any{"environment": "must be production, staging or preview"}})
	}
	rec, err := a.domains.Create(r.Context(), app.ID, in.Environment, in.Hostname)
	if err != nil {
		return err
	}
	w.Header().Set("Location", "/api/v1/domains/"+rec.ID)
	target = rec.ID
	writeJSON(w, http.StatusCreated, a.withTarget(r.Context(), rec))
	return nil
}

func (a *API) removeDomain(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("domainID")
	defer func() { a.audit(r, "domain.remove", target, principal(r.Context()).UserID, err) }()
	rec, err := a.ownedDomain(r)
	if err != nil {
		return err
	}
	// authz defense-in-depth: the caller must own the domain's application.
	if ok, _ := a.authorize(r, authz.ActionDomainWrite, authz.Resource{
		Type: "application", ID: rec.ApplicationID, OwnerID: a.appOwnerID(r, rec.ApplicationID),
	}); !ok {
		return errNotFound("domain", rec.ID)
	}
	if err := a.domains.Remove(r.Context(), rec.ApplicationID, rec.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) setPrimaryDomain(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("domainID")
	defer func() { a.audit(r, "domain.set_primary", target, principal(r.Context()).UserID, err) }()
	rec, err := a.ownedDomain(r)
	if err != nil {
		return err
	}
	// authz defense-in-depth: the caller must own the domain's application.
	if ok, _ := a.authorize(r, authz.ActionDomainWrite, authz.Resource{
		Type: "application", ID: rec.ApplicationID, OwnerID: a.appOwnerID(r, rec.ApplicationID),
	}); !ok {
		return errNotFound("domain", rec.ID)
	}
	updated, err := a.domains.SetPrimary(r.Context(), rec.ApplicationID, rec.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, a.withTarget(r.Context(), updated))
	return nil
}

func (a *API) checkDomain(w http.ResponseWriter, r *http.Request) error {
	rec, err := a.ownedDomain(r)
	if err != nil {
		return err
	}
	// authz defense-in-depth: the caller must own the domain's application.
	if ok, _ := a.authorize(r, authz.ActionDomainWrite, authz.Resource{
		Type: "application", ID: rec.ApplicationID, OwnerID: a.appOwnerID(r, rec.ApplicationID),
	}); !ok {
		return errNotFound("domain", rec.ID)
	}
	checked, err := a.domains.CheckDNS(r.Context(), rec.ApplicationID, rec.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, a.withTarget(r.Context(), checked))
	return nil
}
