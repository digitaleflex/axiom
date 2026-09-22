package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeploymentStream(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/deployments/dep_1/events/stream", nil)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		New(nil).ServeHTTP(rr, req)
		close(done)
	}()
	<-time.After(10 * time.Millisecond)
	cancel()
	<-done
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "deployment.stream.connected") {
		t.Fatalf("expected bootstrap event")
	}
}
