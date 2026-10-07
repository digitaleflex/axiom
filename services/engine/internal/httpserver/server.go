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
	"github.com/digitaleflex/axiom/services/engine/internal/observability/metrics"
)

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

// Handler serves /health, /ready, /metrics and delegates everything else to api.
type Handler struct {
	mux      *http.ServeMux
	draining atomic.Bool
	metrics  *metrics.Registry
}

// Option customizes the root handler.
type Option func(*Handler)

// WithMetrics makes the handler serve the given registry at GET /metrics.
// Without it, an empty registry is served (valid, just no series).
func WithMetrics(registry *metrics.Registry) Option {
	return func(h *Handler) {
		if registry != nil {
			h.metrics = registry
		}
	}
}

// NewHandler builds the root handler. db may be nil when the database is optional.
func NewHandler(cfg config.Config, db *sql.DB, api http.Handler, opts ...Option) *Handler {
	h := &Handler{mux: http.NewServeMux(), metrics: metrics.NewRegistry()}
	for _, opt := range opts {
		opt(h)
	}

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

	// Metrics: Prometheus text exposition (issue #103, ADR-0006).
	h.mux.Handle("GET /metrics", metrics.Handler(h.metrics))

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
