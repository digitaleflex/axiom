package api

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/analysis"
	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
)

type fakeAnalyses struct{ root string }

func (f *fakeAnalyses) Analyze(_ context.Context, _ string, app application.Record, ref string) (analysis.Record, error) {
	return analysis.Record{ID: "analysis_1", ApplicationID: app.ID, Ref: ref, Status: analysis.StatusCompleted}, nil
}
func (f *fakeAnalyses) SetRoot(_ context.Context, _ application.Record, root string) error {
	if strings.Contains(root, "..") {
		return analysis.ErrInvalidRoot
	}
	f.root = root
	return nil
}
func (f *fakeAnalyses) UpdateOverrides(context.Context, application.Record, profile.Hints) (profile.Profile, error) {
	return profile.Profile{Version: 2, Status: profile.StatusReady}, nil
}
func (f *fakeAnalyses) Get(_ context.Context, _, id string) (analysis.Record, error) {
	if id != "analysis_1" {
		return analysis.Record{}, analysis.ErrNotFound
	}
	return analysis.Record{ID: id}, nil
}
func (f *fakeAnalyses) CurrentProfile(_ context.Context, appID string) (profile.Profile, error) {
	if appID == "app_1" {
		return profile.Profile{}, analysis.ErrNoProfile
	}
	return profile.Profile{}, nil
}

func TestAnalysisRoutes(t *testing.T) {
	h := newHarness(t)
	fa := &fakeAnalyses{}
	h.handler = New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: NewTokenAuthenticator(token, Principal{UserID: "usr_1"}),
		Applications: &fakeApps{items: map[string]application.Record{"app_1": {ID: "app_1", OwnerID: "usr_1", RepositoryID: "repo_1"}}}, Analyses: fa})

	expect(t, h.do("POST", "/api/v1/applications/app_1/analysis", map[string]any{}, nil), 422, CodeValidationFailed)
	r := h.do("POST", "/api/v1/applications/app_1/analysis", map[string]any{"ref": "main", "root": "/apps/web/"}, nil)
	expect(t, r, 201, "")
	if r.body["analysisId"] != "analysis_1" || r.body["status"] != "COMPLETED" || fa.root != "apps/web" || r.hdr.Get("Location") == "" {
		t.Fatalf("analysis = %v root=%q", r.body, fa.root)
	}
	expect(t, h.do("POST", "/api/v1/applications/app_1/analysis", map[string]any{"ref": "main", "root": "../x"}, nil), 422, CodeValidationFailed)
	expect(t, h.do("GET", "/api/v1/applications/app_1/analysis/analysis_1", nil, nil), 200, "")
	expect(t, h.do("GET", "/api/v1/applications/app_1/analysis/analysis_x", nil, nil), 404, CodeNotFound)
	expect(t, h.do("GET", "/api/v1/applications/app_1/profile", nil, nil), 404, CodeNotFound)
	r = h.do("PUT", "/api/v1/applications/app_1/profile/overrides", map[string]any{"port": 8080}, nil)
	if r.code != 200 || r.body["version"].(float64) != 2 {
		t.Fatalf("overrides = %d %v", r.code, r.body)
	}
	expect(t, h.do("PUT", "/api/v1/applications/app_1/profile/overrides", map[string]any{"replicas": 3}, nil), 400, CodeInvalidRequest)
	_ = httptest.NewRecorder
}
