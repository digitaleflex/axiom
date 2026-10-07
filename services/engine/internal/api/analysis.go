package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/digitaleflex/axiom/services/engine/internal/analysis"
	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/authz"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
)

// Analyses is the repository understanding pipeline (#58/#59).
type Analyses interface {
	Analyze(ctx context.Context, userID string, app application.Record, ref string) (analysis.Record, error)
	SetRoot(ctx context.Context, app application.Record, root string) error
	UpdateOverrides(ctx context.Context, app application.Record, h profile.Hints) (profile.Profile, error)
	Get(ctx context.Context, applicationID, id string) (analysis.Record, error)
	CurrentProfile(ctx context.Context, applicationID string) (profile.Profile, error)
}

func (a *API) startAnalysis(w http.ResponseWriter, r *http.Request) error {
	if a.analyses == nil {
		return errUnavailable
	}
	app, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	// authz defense-in-depth: the caller must own the application.
	if ok, _ := a.authorize(r, authz.ActionApplicationWrite, authz.Resource{
		Type: "application", ID: app.ID, OwnerID: app.OwnerID,
	}); !ok {
		return errNotFound("application", app.ID)
	}
	var in struct {
		Ref  string  `json:"ref"`
		Root *string `json:"root"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Ref) == "" {
		return errValidation("invalid analysis request", map[string]any{"fields": map[string]any{"ref": "required"}})
	}
	if in.Root != nil {
		if err := a.analyses.SetRoot(r.Context(), app, strings.Trim(strings.TrimSpace(*in.Root), "/")); err != nil {
			return err
		}
	}
	rec, err := a.analyses.Analyze(r.Context(), principal(r.Context()).UserID, app, strings.TrimSpace(in.Ref))
	if err != nil {
		return err
	}
	w.Header().Set("Location", "/api/v1/applications/"+app.ID+"/analysis/"+rec.ID)
	writeJSON(w, http.StatusCreated, rec)
	return nil
}

func (a *API) getAnalysis(w http.ResponseWriter, r *http.Request) error {
	if a.analyses == nil {
		return errUnavailable
	}
	app, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	rec, err := a.analyses.Get(r.Context(), app.ID, r.PathValue("analysisID"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rec)
	return nil
}

func (a *API) getProfile(w http.ResponseWriter, r *http.Request) error {
	if a.analyses == nil {
		return errUnavailable
	}
	app, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	p, err := a.analyses.CurrentProfile(r.Context(), app.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, p)
	return nil
}

// putOverrides replaces the explicit values; omitted fields revert to detection.
func (a *API) putOverrides(w http.ResponseWriter, r *http.Request) error {
	if a.analyses == nil {
		return errUnavailable
	}
	app, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	// authz defense-in-depth: the caller must own the application.
	if ok, _ := a.authorize(r, authz.ActionApplicationWrite, authz.Resource{
		Type: "application", ID: app.ID, OwnerID: app.OwnerID,
	}); !ok {
		return errNotFound("application", app.ID)
	}
	var h profile.Hints
	if err := decodeJSON(w, r, &h); err != nil {
		return err
	}
	p, err := a.analyses.UpdateOverrides(r.Context(), app, h)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, p)
	return nil
}

func analysisError(err error) (*Error, bool) {
	switch {
	case errors.Is(err, analysis.ErrNotFound):
		return newError(http.StatusNotFound, CodeNotFound, "analysis not found", nil), true
	case errors.Is(err, analysis.ErrNoProfile):
		return newError(http.StatusNotFound, CodeNotFound, "this application has no profile yet; analyze the repository first", nil), true
	case errors.Is(err, analysis.ErrInvalidRoot):
		return errValidation("invalid application root", map[string]any{"fields": map[string]any{"root": "must be a relative directory"}}), true
	}
	return nil, false
}
