package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/digitaleflex/axiom/services/engine/internal/project"
)
// Projects manages the client-facing project resource (#146, M12.1). It is
// satisfied by *project.Service.
type Projects interface {
	Create(ctx context.Context, in project.CreateInput) (project.Record, error)
	Get(ctx context.Context, orgID, id string) (project.Record, error)
	List(ctx context.Context, orgID string, limit, offset int) ([]project.Record, int, error)
	Update(ctx context.Context, orgID, requesterID, id string, in project.UpdateInput) (project.Record, error)
	Delete(ctx context.Context, orgID, requesterID, id string) error
}

// listProjects returns the organization's projects, paginated.
//
// It is org-scoped from the route: the organization comes from the path and the
// caller never supplies it, so a client cannot list another tenant's projects by
// asking for a different organization.
func (a *API) listProjects(w http.ResponseWriter, r *http.Request) error {
	if a.projects == nil {
		return errUnavailable
	}
	orgID := r.PathValue("orgID")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)
	items, total, err := a.projects.List(r.Context(), orgID, limit, offset)
	if err != nil {
		return fromProjectErr(err)
	}
	if items == nil {
		items = []project.Record{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "limit": limit, "offset": offset,
	})
	return nil
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request) (err error) {
	orgID := r.PathValue("orgID")
	defer func() { a.audit(r, "project.create", orgID, principal(r.Context()).UserID, err) }()
	if a.projects == nil {
		return errUnavailable
	}
	var in struct {
		Name          string `json:"name"`
		Slug          string `json:"slug"`
		Description   string `json:"description"`
		Environment   string `json:"environment"`
		PrimaryDomain string `json:"primaryDomain"`
		ApplicationID string `json:"applicationId"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	rec, err := a.projects.Create(r.Context(), project.CreateInput{
		OrgID:         orgID,
		RequesterID:   principal(r.Context()).UserID,
		Name:          in.Name,
		Slug:          in.Slug,
		Description:   in.Description,
		Environment:   project.Environment(in.Environment),
		PrimaryDomain: in.PrimaryDomain,
		ApplicationID: in.ApplicationID,
	})
	if err != nil {
		return fromProjectErr(err)
	}
	w.Header().Set("Location", "/api/v1/orgs/"+orgID+"/projects/"+rec.ID)
	writeJSON(w, http.StatusCreated, rec)
	return nil
}

func (a *API) getProject(w http.ResponseWriter, r *http.Request) error {
	if a.projects == nil {
		return errUnavailable
	}
	rec, err := a.projects.Get(r.Context(), r.PathValue("orgID"), r.PathValue("projectID"))
	if err != nil {
		return fromProjectErr(err)
	}
	writeJSON(w, http.StatusOK, rec)
	return nil
}

func (a *API) updateProject(w http.ResponseWriter, r *http.Request) (err error) {
	orgID, id := r.PathValue("orgID"), r.PathValue("projectID")
	defer func() { a.audit(r, "project.update", orgID+"/"+id, principal(r.Context()).UserID, err) }()
	if a.projects == nil {
		return errUnavailable
	}
	var in struct {
		Name          string  `json:"name"`
		Description   string  `json:"description"`
		Environment   string  `json:"environment"`
		PrimaryDomain *string `json:"primaryDomain"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	rec, err := a.projects.Update(r.Context(), orgID, principal(r.Context()).UserID, id,
		project.UpdateInput{
			Name:          in.Name,
			Description:   in.Description,
			Environment:   project.Environment(in.Environment),
			PrimaryDomain: in.PrimaryDomain,
		})
	if err != nil {
		return fromProjectErr(err)
	}
	writeJSON(w, http.StatusOK, rec)
	return nil
}

func (a *API) deleteProject(w http.ResponseWriter, r *http.Request) (err error) {
	orgID, id := r.PathValue("orgID"), r.PathValue("projectID")
	defer func() { a.audit(r, "project.delete", orgID+"/"+id, principal(r.Context()).UserID, err) }()
	if a.projects == nil {
		return errUnavailable
	}
	if err := a.projects.Delete(r.Context(), orgID, principal(r.Context()).UserID, id); err != nil {
		return fromProjectErr(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// queryInt reads a bounded integer query parameter, falling back when the value
// is absent or unparsable. A malformed value is ignored rather than rejected:
// pagination must never be the reason a list call fails.
func queryInt(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

// fromProjectErr maps project errors onto the API error envelope.
func fromProjectErr(err error) *Error {
	switch {
	case errors.Is(err, project.ErrNotFound):
		return errNotFound("project", "")
	case errors.Is(err, project.ErrAlreadyExists):
		return newError(http.StatusConflict, CodeConflict,
			"a project with this slug already exists in this organization", nil)
	case errors.Is(err, project.ErrInvalidName):
		return errValidation("invalid project", map[string]any{"fields": map[string]any{
			"name": "must be 1-63 lowercase letters, digits or hyphens, starting and ending alphanumeric",
			"slug": "must be 1-63 lowercase letters, digits or hyphens, starting and ending alphanumeric"}})
	case errors.Is(err, project.ErrInvalidEnvironment):
		return errValidation("invalid project", map[string]any{"fields": map[string]any{
			"environment": "must be production, staging or preview"}})
	case errors.Is(err, project.ErrInvalidDescription):
		return errValidation("invalid project", map[string]any{"fields": map[string]any{
			"description": "must be at most 500 characters"}})
	case errors.Is(err, project.ErrInUse):
		return newError(http.StatusConflict, CodeConflict,
			"this project still has deployments; delete them before removing the project",
			map[string]any{"reason": "project_in_use"})
	case errors.Is(err, project.ErrQuotaExceeded):
		return newError(http.StatusConflict, CodeConflict,
			"the plan's allowance for this organization is spent", map[string]any{"reason": "quota_exceeded"})
	case errors.Is(err, project.ErrOrgRequired):
		return errInvalid("organization is required")
	default:
		return fromDomain(err)
	}
}
