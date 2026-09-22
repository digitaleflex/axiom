package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type deploymentRepo struct{ db *sql.DB }

func (r deploymentRepo) Create(ctx context.Context, id, applicationID, serverID, environment string) error {
	_, err := r.db.ExecContext(ctx, "INSERT INTO deployments (id, application_id, server_id, environment) VALUES ($1, $2, $3, $4)", id, applicationID, serverID, environment)
	return err
}
func (r deploymentRepo) SetStatus(ctx context.Context, id, status string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE deployments SET status = $1 WHERE id = $2", status, id)
	return err
}
func (r deploymentRepo) GetDomainRecord(ctx context.Context, id string) (deployment.Record, error) {
	var v deployment.Record
	err := r.db.QueryRowContext(ctx, "SELECT id, application_id, server_id, environment, status FROM deployments WHERE id = $1", id).
		Scan(&v.ID, &v.ApplicationID, &v.ServerID, &v.Environment, &v.Status)
	return v, err
}

type API struct {
	db *sql.DB
	deployments *deployment.Service
}

func New(db *sql.DB) http.Handler {
	a := &API{db: db}\n\tif db != nil { a.deployments = deployment.NewService(deploymentRepo{db: db}, deployment.NewEventBus()) }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/auth/me", a.me)
	mux.HandleFunc("GET /api/v1/servers", a.servers)
	mux.HandleFunc("GET /api/v1/deployments/{deploymentID}", a.deployment)
	mux.HandleFunc("GET /api/v1/deployments/{deploymentID}/health", a.deploymentHealth)
	mux.HandleFunc("GET /api/v1/deployments/{deploymentID}/events/stream", a.eventStream)
	mux.HandleFunc("GET /api/v1/deployments/{deploymentID}/logs", a.logs)
	mux.HandleFunc("POST /api/v1/applications/{applicationID}/deployments", a.createDeployment)
	return mux
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "requestId": r.Header.Get("X-Request-ID")})
}

func (a *API) servers(w http.ResponseWriter, _ *http.Request) {
	if a.db == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "database is unavailable", nil)
		return
	}
	rows, err := a.db.Query("SELECT id, name, address, status FROM servers ORDER BY name")
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list servers", nil)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, address, status string
		if err := rows.Scan(&id, &name, &address, &status); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read server", nil)
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "address": address, "status": status})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": 1, "limit": len(items), "total": len(items)})
}

func (a *API) deployment(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "database is unavailable", nil)
		return
	}
	id := r.PathValue("deploymentID")
	var appID, serverID, environment, status string
	err := a.db.QueryRowContext(r.Context(),
		"SELECT application_id, server_id, environment, status FROM deployments WHERE id = $1", id).
		Scan(&appID, &serverID, &environment, &status)
	if err == sql.ErrNoRows {
		writeAPIError(w, http.StatusNotFound, "NOT_FOUND", "deployment not found", map[string]any{"deploymentId": id})
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read deployment", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "applicationId": appID, "serverId": serverID, "environment": environment, "status": status})
}

func (a *API) createDeployment(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "database is unavailable", nil)
		return
	}
	var input struct { PlanID string `json:"planId"`; ServerID string `json:"serverId"`; Environment string `json:"environment"` }
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.PlanID) == "" {
		if strings.TrimSpace(input.ServerID) == "" || strings.TrimSpace(input.Environment) == "" {\n\t\twriteAPIError(w, http.StatusBadRequest, "INVALID_REQUEST", "serverId and environment are required", nil)\n\t\treturn\n\t}\n\tif a.deployments == nil {\n\t\twriteAPIError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "deployment service is unavailable", nil)\n\t\treturn\n\t}\n\tkey := r.Header.Get("Idempotency-Key")\n\trecord, err := a.deployments.CreateIdempotent(r.Context(), key, r.PathValue("applicationID"), input.ServerID, input.Environment, input.PlanID)\n\tif err != nil {\n\t\twriteAPIError(w, http.StatusInternalServerError, "DEPLOYMENT_CREATE_FAILED", err.Error(), nil)\n\t\treturn\n\t}\n\twriteJSON(w, http.StatusAccepted, map[string]any{"id": record.ID, "status": record.Status, "accepted": true, "planId": record.PlanID})\n\treturn\n\n\t/* legacy boundary */\n\t/*
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id": input.PlanID, "status": "PENDING", "accepted": true,
		"message": "deployment execution boundary is registered",
	})
}

func (a *API) deploymentHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "UNKNOWN", "message": "health verification is owned by the Deployment Engine"})
}

func (a *API) logs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "nextCursor": nil})
}

func (a *API) eventStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "streaming is unsupported", nil)
		return
	}
	event := map[string]any{
		"id": "evt-bootstrap", "type": "deployment.stream.connected", "version": 1,
		"deploymentId": r.PathValue("deploymentID"), "occurredAt": time.Now().UTC(),
		"data": map[string]any{"status": "CONNECTED"},
	}
	payload, _ := json.Marshal(event)
	_, _ = w.Write([]byte("event: deployment.stream.connected\ndata: " + string(payload) + "\n\n"))
	flusher.Flush()
	<-r.Context().Done()
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAPIError(w http.ResponseWriter, status int, code, message string, details any) {
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"code": code, "message": message, "requestId": w.Header().Get("X-Request-ID"), "details": details,
	}})
}
