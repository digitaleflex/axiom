package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeploymentStreamRequiresService(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/deployments/dep_1/events/stream", nil)
	rr := httptest.NewRecorder()
	New(nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}
