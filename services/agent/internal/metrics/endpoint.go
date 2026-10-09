package metrics

import "net/http"

// Endpoint exposes the registry at /metrics (minimal endpoint branch).
func Endpoint(r *Registry) http.HandlerFunc {
	return Handler(r)
}
