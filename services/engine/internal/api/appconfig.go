package api

import (
	"context"
	"errors"
	"net/http"

	appconfig "github.com/digitaleflex/axiom/services/engine/internal/secrets"
	secsecrets "github.com/digitaleflex/axiom/services/engine/internal/security/secrets"
)

// AppConfig manages application configuration values (#126): write-only
// secret storage, metadata-only listing, per-application scope.
type AppConfig interface {
	Set(ctx context.Context, appID, name, value string, secret bool) error
	List(ctx context.Context, appID string) ([]appconfig.Entry, error)
	Delete(ctx context.Context, appID, name string) error
}

func (a *API) listAppConfig(w http.ResponseWriter, r *http.Request) error {
	if a.appConfig == nil {
		return errUnavailable
	}
	app, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	items, err := a.appConfig.List(r.Context(), app.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

// setAppConfig upserts one value. The value is accepted on write only and is
// never returned by any endpoint.
func (a *API) setAppConfig(w http.ResponseWriter, r *http.Request) error {
	if a.appConfig == nil {
		return errUnavailable
	}
	app, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	name := r.PathValue("name")
	var in struct {
		Value  string `json:"value"`
		Secret *bool  `json:"secret"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	secret := true
	if in.Secret != nil {
		secret = *in.Secret
	}
	if err := a.appConfig.Set(r.Context(), app.ID, name, in.Value, secret); err != nil {
		if !appconfig.ValidName(name) {
			return errValidation("invalid configuration name", map[string]any{"fields": map[string]any{"name": "must be UPPER_SNAKE_CASE"}})
		}
		return err
	}
	a.audit(r, "appconfig.set", app.ID+"/"+name, nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) deleteAppConfig(w http.ResponseWriter, r *http.Request) error {
	if a.appConfig == nil {
		return errUnavailable
	}
	app, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	name := r.PathValue("name")
	if err := a.appConfig.Delete(r.Context(), app.ID, name); err != nil {
		if errors.Is(err, secsecrets.ErrNotFound) {
			return errNotFound("configuration value", name)
		}
		return err
	}
	a.audit(r, "appconfig.delete", app.ID+"/"+name, nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
