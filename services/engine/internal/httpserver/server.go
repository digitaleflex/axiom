package httpserver

import (
	"encoding/json"
	"net/http"

	"github.com/digitaleflex/axiom/services/engine/internal/config"
)

type Server struct {
	httpServer *http.Server
}

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

func New(cfg config.Config) *Server {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{
			Status:  "ok",
			Service: "axiom-engine",
			Version: cfg.Version,
		})
	})

	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	return &Server{httpServer: &http.Server{
		Addr:              cfg.Host + ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * 1e9,
	}}
}

func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown() error {
	return s.httpServer.Close()
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
