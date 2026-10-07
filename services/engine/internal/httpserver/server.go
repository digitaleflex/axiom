// Package httpserver exposes liveness/readiness endpoints and mounts the API.
package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/config"
)

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

// Handler serves /health, /ready and delegates everything else to api.
type Handler struct {
	mux      *http.ServeMux
	draining atomic.Bool
}

// NewHandler builds the root handler. db may be nil when the database is optional.
func NewHandler(cfg config.Config, db *sql.DB, api http.Handler) *Handler {
	h := &Handler{mux: http.NewServeMux()}

	// Liveness only: the process is up.
	h.mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Service: "axiom-engine", Version: cfg.Version})
	})

	// Readiness: required dependencies are reachable and the server is not draining.
	h.mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if h.draining.Load() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "shutting_down"})
			return
		}
		if cfg.Database.Required && db == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "database_unavailable"})
			return
		}
		if db != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "database_unavailable"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	if api != nil {
		h.mux.Handle("/", api)
	}
	return h
}

// SetDraining marks the server as shutting down so load balancers stop routing to it.
func (h *Handler) SetDraining() { h.draining.Store(true) }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
