package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/executor"
	"github.com/digitaleflex/axiom/services/engine/internal/logs"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// --- applications -------------------------------------------------------------

func (a *API) listApplications(w http.ResponseWriter, r *http.Request) error {
	if a.applications == nil {
		return errUnavailable
	}
	p, err := parsePage(r)
	if err != nil {
		return err
	}
	items, total, err := a.applications.List(r.Context(), principal(r.Context()).UserID, p.Limit, p.offset())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, pageResponse(items, p, total))
	return nil
}

func (a *API) createApplication(w http.ResponseWriter, r *http.Request) error {
	if a.applications == nil {
		return errUnavailable
	}
	var in struct {
		RepositoryID string `json:"repositoryId"`
		Name         string `json:"name"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	fields := map[string]any{}
	if strings.TrimSpace(in.RepositoryID) == "" {
		fields["repositoryId"] = "required"
	}
	if !application.ValidName(in.Name) {
		fields["name"] = "must be 1-63 lowercase letters, digits or hyphens, starting and ending with a letter or digit"
	}
	if len(fields) > 0 {
		return errValidation("invalid application", map[string]any{"fields": fields})
	}
	rec, err := a.applications.Create(r.Context(), application.Record{
		ID: deployment.NewID("app"), Name: in.Name, RepositoryID: in.RepositoryID, OwnerID: principal(r.Context()).UserID,
	})
	switch {
	case errors.Is(err, application.ErrRepositoryNotFound):
		return errNotFound("repository", in.RepositoryID)
	case errors.Is(err, application.ErrNameTaken):
		return newError(http.StatusConflict, CodeConflict, "application name already in use", nil)
	case err != nil:
		return err
	}
	w.Header().Set("Location", "/api/v1/applications/"+rec.ID)
	writeJSON(w, http.StatusCreated, rec)
	return nil
}

// ownedApplication loads an application visible to the caller. Applications
// owned by someone else are reported as not found (no existence leak).
func (a *API) ownedApplication(r *http.Request, id string) (application.Record, error) {
	if a.applications == nil {
		return application.Record{}, errUnavailable
	}
	rec, err := a.applications.Get(r.Context(), id)
	if errors.Is(err, application.ErrNotFound) || (err == nil && rec.OwnerID != principal(r.Context()).UserID) {
		return application.Record{}, errNotFound("application", id)
	}
	return rec, err
}

func (a *API) getApplication(w http.ResponseWriter, r *http.Request) error {
	rec, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rec)
	return nil
}

// --- servers ------------------------------------------------------------------

// serverDTO exposes server state with the canonical uppercase vocabulary
// (READY/DEGRADED/OFFLINE…) and never includes agent credentials.
type serverDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Address      string   `json:"address"`
	Status       string   `json:"status"`
	AgentVersion string   `json:"agentVersion"`
	Capabilities []string `json:"capabilities"`
	Resources    struct {
		CPUCount   int `json:"cpuCount"`
		MemoryMB   int `json:"memoryMb"`
		DiskFreeMB int `json:"diskFreeMb"`
	} `json:"resources"`
	LastSeenAt string `json:"lastSeenAt,omitempty"`
}

func toServerDTO(s server.Record) serverDTO {
	d := serverDTO{ID: s.ID, Name: s.Name, Address: s.Address, Status: strings.ToUpper(string(server.EffectiveStatusAt(s, time.Now().UTC()))), AgentVersion: s.AgentVersion, LastSeenAt: s.LastSeenAt}
	d.Capabilities = make([]string, 0, len(s.Capabilities))
	for _, c := range s.Capabilities {
		d.Capabilities = append(d.Capabilities, string(c))
	}
	d.Resources.CPUCount, d.Resources.MemoryMB, d.Resources.DiskFreeMB = s.CPUCount, s.MemoryMB, s.DiskFreeMB
	return d
}

func (a *API) listServers(w http.ResponseWriter, r *http.Request) error {
	if a.servers == nil {
		return errUnavailable
	}
	p, err := parsePage(r)
	if err != nil {
		return err
	}
	status := r.URL.Query().Get("status")
	switch status {
	case "", "pending", "ready", "degraded", "offline", "revoked", "unknown":
	default:
		return errInvalid("status must be pending, ready, degraded, offline, revoked or unknown")
	}
	items, total, err := a.servers.ListFiltered(r.Context(), status, p.Limit, p.offset())
	if err != nil {
		return err
	}
	out := make([]serverDTO, 0, len(items))
	for _, s := range items {
		out = append(out, toServerDTO(s))
	}
	writeJSON(w, http.StatusOK, pageResponse(out, p, total))
	return nil
}

func (a *API) loadServer(r *http.Request) (server.Record, error) {
	if a.servers == nil {
		return server.Record{}, errUnavailable
	}
	id := r.PathValue("serverID")
	s, err := a.servers.Get(r.Context(), id)
	if errors.Is(err, server.ErrNotFound) {
		return server.Record{}, errNotFound("server", id)
	}
	return s, err
}

func (a *API) getServer(w http.ResponseWriter, r *http.Request) error {
	s, err := a.loadServer(r)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, toServerDTO(s))
	return nil
}

func (a *API) registerServer(w http.ResponseWriter, r *http.Request) (err error) {
	target := ""
	defer func() { a.audit(r, "server.register", target, err) }()
	if a.servers == nil {
		return errUnavailable
	}
	var in struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	rec, err := a.servers.Register(r.Context(), principal(r.Context()).UserID, in.Name, in.Address)
	if err != nil {
		return err
	}
	w.Header().Set("Location", "/api/v1/servers/"+rec.ID)
	target = rec.ID
	writeJSON(w, http.StatusCreated, toServerDTO(rec))
	return nil
}

func (a *API) renameServer(w http.ResponseWriter, r *http.Request) error {
	if a.servers == nil {
		return errUnavailable
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	rec, err := a.servers.Rename(r.Context(), r.PathValue("serverID"), in.Name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, toServerDTO(rec))
	return nil
}

func (a *API) removeServer(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("serverID")
	defer func() { a.audit(r, "server.remove", target, err) }()
	if a.servers == nil {
		return errUnavailable
	}
	id := r.PathValue("serverID")
	if err := a.servers.Remove(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) serverHealth(w http.ResponseWriter, r *http.Request) error {
	s, err := a.loadServer(r)
	if err != nil {
		return err
	}
	d := toServerDTO(s)
	writeJSON(w, http.StatusOK, map[string]any{"status": d.Status, "lastSeenAt": d.LastSeenAt, "agentVersion": d.AgentVersion, "capabilities": d.Capabilities})
	return nil
}

// --- deployments --------------------------------------------------------------

func (a *API) createDeployment(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("applicationID")
	defer func() { a.audit(r, "deployment.create", target, err) }()
	if a.deployments == nil {
		return errUnavailable
	}
	app, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) > 255 {
		return errInvalid("Idempotency-Key must be at most 255 characters")
	}
	var in struct {
		PlanID string `json:"planId"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.PlanID) == "" {
		return errValidation("invalid deployment request", map[string]any{"fields": map[string]any{"planId": "required"}})
	}
	rec, created, err := a.deployments.Create(r.Context(), deployment.CreateInput{
		ApplicationID: app.ID, PlanID: in.PlanID, CreatedBy: principal(r.Context()).UserID, IdempotencyKey: key,
		CorrelationID: requestID(r.Context()),
	})
	if err != nil {
		return err
	}
	w.Header().Set("Location", "/api/v1/deployments/"+rec.ID)
	if !created {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	target = rec.ID
	writeJSON(w, http.StatusAccepted, rec)
	return nil
}

func (a *API) listDeployments(w http.ResponseWriter, r *http.Request) error {
	if a.deployments == nil {
		return errUnavailable
	}
	app, err := a.ownedApplication(r, r.PathValue("applicationID"))
	if err != nil {
		return err
	}
	p, err := parsePage(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	status, env := q.Get("status"), q.Get("environment")
	if status != "" && !deployment.State(status).Valid() {
		return errInvalid("status must be a canonical deployment status")
	}
	switch env {
	case "", "production", "staging", "preview":
	default:
		return errInvalid("environment must be production, staging or preview")
	}
	items, total, err := a.deployments.Store().List(r.Context(), deployment.ListFilter{
		ApplicationID: app.ID, Environment: env, Status: status, Limit: p.Limit, Offset: p.offset(),
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, pageResponse(items, p, total))
	return nil
}

// loadDeployment returns a deployment the caller may access.
func (a *API) loadDeployment(r *http.Request) (deployment.Record, error) {
	if a.deployments == nil {
		return deployment.Record{}, errUnavailable
	}
	id := r.PathValue("deploymentID")
	rec, err := a.deployments.Get(r.Context(), id)
	if errors.Is(err, deployment.ErrNotFound) {
		return deployment.Record{}, errNotFound("deployment", id)
	}
	if err != nil {
		return deployment.Record{}, err
	}
	if _, err := a.ownedApplication(r, rec.ApplicationID); err != nil {
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return deployment.Record{}, errNotFound("deployment", id)
		}
		return deployment.Record{}, err
	}
	return rec, nil
}

// ownedDeployment guards non-API handlers (SSE) with the same access check.
func (a *API) ownedDeployment(next http.Handler) http.Handler {
	return a.wrap(func(w http.ResponseWriter, r *http.Request) error {
		if _, err := a.loadDeployment(r); err != nil {
			return err
		}
		next.ServeHTTP(w, r)
		return nil
	})
}

func (a *API) getDeployment(w http.ResponseWriter, r *http.Request) error {
	rec, err := a.loadDeployment(r)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rec)
	return nil
}

func (a *API) cancelDeployment(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("deploymentID")
	defer func() { a.audit(r, "deployment.cancel", target, err) }()
	rec, err := a.loadDeployment(r)
	if err != nil {
		return err
	}
	rec, err = a.deployments.Cancel(r.Context(), rec.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rec)
	return nil
}

func (a *API) deploymentSteps(w http.ResponseWriter, r *http.Request) error {
	rec, err := a.loadDeployment(r)
	if err != nil {
		return err
	}
	steps, err := a.deployments.Store().Steps(r.Context(), rec.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": steps})
	return nil
}

// deploymentEvents returns persisted events; ?after=<seq> resumes a timeline.
func (a *API) deploymentEvents(w http.ResponseWriter, r *http.Request) error {
	rec, err := a.loadDeployment(r)
	if err != nil {
		return err
	}
	var after int64
	if v := r.URL.Query().Get("after"); v != "" {
		if after, err = strconv.ParseInt(v, 10, 64); err != nil || after < 0 {
			return errInvalid("after must be a non-negative event sequence number")
		}
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 || limit > 1000 {
			return errInvalid("limit must be an integer between 1 and 1000")
		}
	}
	events, err := a.deployments.Store().Events(r.Context(), rec.ID, after, limit)
	if err != nil {
		return err
	}
	var next any
	if len(events) == limit {
		next = events[len(events)-1].Seq
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": events, "nextAfter": next})
	return nil
}

// deploymentLogs serves durable, redacted logs (#66). Runtime log streaming
// arrives with the agent (#86); stored lines are served here.
func (a *API) deploymentLogs(w http.ResponseWriter, r *http.Request) error {
	rec, err := a.loadDeployment(r)
	if err != nil {
		return err
	}
	if a.logs == nil {
		return errUnavailable
	}
	q := r.URL.Query()
	f := logs.Filter{Limit: 100}
	if v := q.Get("level"); v != "" {
		switch logs.Level(strings.ToUpper(v)) {
		case logs.LevelDebug, logs.LevelInfo, logs.LevelWarn, logs.LevelError:
			f.MinLevel = logs.Level(strings.ToUpper(v))
		default:
			return errInvalid("level must be debug, info, warn or error")
		}
	}
	if v := q.Get("step"); v != "" {
		switch logs.Step(strings.ToUpper(v)) {
		case logs.StepBuild, logs.StepCreateRuntime, logs.StepNetwork, logs.StepStart, logs.StepVerify:
			f.Step = logs.Step(strings.ToUpper(v))
		default:
			return errInvalid("step must be a canonical plan step")
		}
	}
	if v := q.Get("source"); v != "" {
		switch logs.Source(strings.ToLower(v)) {
		case logs.SourceBuild, logs.SourceDeploy, logs.SourceRuntime:
			f.Source = logs.Source(strings.ToLower(v))
		default:
			return errInvalid("source must be build, deploy or runtime")
		}
	}
	f.Search = q.Get("q")
	f.Cursor = q.Get("cursor")
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			return errInvalid("limit must be an integer between 1 and 1000")
		}
		f.Limit = n
	}
	items, next, err := a.logs.List(r.Context(), rec.ID, f)
	if err != nil {
		return err
	}
	out := map[string]any{"items": items}
	if next != "" {
		out["nextCursor"] = next
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// deploymentHealth reports the verification outcome known to the Engine (#65):
// persisted probe details when verification ran, otherwise the authoritative
// state (LIVE implies a passed verification; anything else without probe
// data is UNKNOWN, never an implied pass).
func (a *API) deploymentHealth(w http.ResponseWriter, r *http.Request) error {
	rec, err := a.loadDeployment(r)
	if err != nil {
		return err
	}
	status := "UNKNOWN"
	out := map[string]any{"deploymentStatus": rec.Status}
	if a.deployments != nil {
		if report, found, err := executor.LastHealthResult(r.Context(), a.deployments.Store(), rec.ID); err != nil {
			return err
		} else if found {
			out["http"] = map[string]any{"statusCode": report.StatusCode, "latencyMs": report.LatencyMs}
			out["checkedAt"] = report.CheckedAt.UTC().Format(time.RFC3339)
			out["attempt"] = report.Attempt
			if report.Body != "" {
				out["body"] = report.Body
			}
		}
	}
	switch {
	case rec.Status == deployment.StateLive:
		status = "HEALTHY"
	case rec.Status == deployment.StateFailed && rec.ErrorCode == "HEALTH_CHECK_FAILED":
		status = "UNHEALTHY"
	}
	out["status"] = status
	writeJSON(w, http.StatusOK, out)
	return nil
}
