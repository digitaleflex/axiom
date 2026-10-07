package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ghauth "github.com/digitaleflex/axiom/services/engine/internal/github/auth"
)

type fakeGitHub struct {
	lastSecret  string
	userID      string
	callbackErr error
}

func (f *fakeGitHub) Start(_ context.Context, userID string) (string, string, error) {
	f.userID = userID
	return "https://github.com/login/oauth/authorize?state=s", "browser-secret", nil
}
func (f *fakeGitHub) Callback(_ context.Context, state, code, providerErr, secret string) (ghauth.Connection, error) {
	f.lastSecret = secret
	if providerErr == "access_denied" {
		return ghauth.Connection{}, ghauth.ErrDenied
	}
	if f.callbackErr != nil {
		return ghauth.Connection{}, f.callbackErr
	}
	return ghauth.Connection{ID: "ghc_1"}, nil
}
func (f *fakeGitHub) List(context.Context, string) ([]ghauth.Connection, error) {
	return []ghauth.Connection{{ID: "ghc_1", AccountLogin: "octocat", Status: "active"}}, nil
}
func (f *fakeGitHub) Get(context.Context, string, string) (ghauth.Connection, error) {
	return ghauth.Connection{}, nil
}
func (f *fakeGitHub) Disconnect(_ context.Context, _ string, id string) error {
	if id != "ghc_1" {
		return ghauth.ErrNotFound
	}
	return nil
}

func TestGitHubConnectionRoutes(t *testing.T) {
	gh := &fakeGitHub{}
	h := New(Deps{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: NewTokenAuthenticator(token, Principal{UserID: "usr_1"}),
		GitHub: gh, ConsoleURL: "https://console.test", SecureCookies: true,
	})
	do := func(req *http.Request) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	req := httptest.NewRequest("POST", "/api/v1/github/connections", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := do(req)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "authorizeUrl") || gh.userID != "usr_1" {
		t.Fatalf("start = %d %s", rr.Code, rr.Body.String())
	}
	cookie := rr.Result().Cookies()[0]
	if cookie.Name != githubCookie || !cookie.HttpOnly || !cookie.Secure || cookie.Path != githubCallbackPath || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %+v", cookie)
	}

	// Callback is public (no bearer) and redirects to the Console.
	req = httptest.NewRequest("GET", "/api/v1/github/callback?state=s&code=c", nil)
	req.AddCookie(&http.Cookie{Name: githubCookie, Value: "browser-secret"})
	rr = do(req)
	if rr.Code != 302 || rr.Header().Get("Location") != "https://console.test/github?result=connected" || gh.lastSecret != "browser-secret" {
		t.Fatalf("callback = %d %s secret=%q", rr.Code, rr.Header().Get("Location"), gh.lastSecret)
	}
	req = httptest.NewRequest("GET", "/api/v1/github/callback?state=s&error=access_denied", nil)
	if loc := do(req).Header().Get("Location"); !strings.HasSuffix(loc, "result=denied") {
		t.Fatalf("denied redirect = %s", loc)
	}
	gh.callbackErr = ghauth.ErrStateInvalid
	req = httptest.NewRequest("GET", "/api/v1/github/callback?state=forged&code=c", nil)
	if loc := do(req).Header().Get("Location"); !strings.HasSuffix(loc, "result=error") {
		t.Fatalf("error redirect = %s", loc)
	}

	req = httptest.NewRequest("GET", "/api/v1/github/connections", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if rr := do(req); rr.Code != 200 || strings.Contains(strings.ToLower(rr.Body.String()), "token") {
		t.Fatalf("list = %d %s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest("DELETE", "/api/v1/github/connections/ghc_1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if rr := do(req); rr.Code != 204 {
		t.Fatalf("disconnect = %d", rr.Code)
	}
	req = httptest.NewRequest("DELETE", "/api/v1/github/connections/ghc_x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if rr := do(req); rr.Code != 404 {
		t.Fatalf("disconnect unknown = %d", rr.Code)
	}
	// Other GitHub endpoints still require authentication.
	if rr := do(httptest.NewRequest("GET", "/api/v1/github/connections", nil)); rr.Code != 401 {
		t.Fatalf("unauthenticated list = %d", rr.Code)
	}
}

func TestGitHubNotConfigured(t *testing.T) {
	h := New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: DevAuthenticator{Principal: Principal{UserID: "u"}}, ConsoleURL: "https://c"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", "/api/v1/github/connections", nil))
	if rr.Code != 503 {
		t.Fatalf("start without GitHub = %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/github/callback?state=x", nil))
	if rr.Code != 302 || !strings.HasSuffix(rr.Header().Get("Location"), "result=error") {
		t.Fatalf("callback without GitHub = %d %s", rr.Code, rr.Header().Get("Location"))
	}
}
