package metrics

import "net/http"

// Handler returns an http.HandlerFunc that serves the registry in Prometheus
// text exposition format (text/plain; version=0.0.4). It is mounted by the
// Engine's GET /metrics route in internal/httpserver.
func Handler(registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", ContentType)
		_ = registry.WriteText(w)
	}
}
