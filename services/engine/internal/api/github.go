package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	ghauth "github.com/digitaleflex/axiom/services/engine/internal/github/auth"
	"github.com/digitaleflex/axiom/services/engine/internal/github/repos"
)

const (
	githubCallbackPath = "/api/v1/github/callback"
	githubCookie       = "axiom_github_oauth"
)

// GitHubConnections is the GitHub connection boundary (#91).
type GitHubConnections interface {
	Start(ctx context.Context, userID string) (authorizeURL, browserSecret string, err error)
	Callback(ctx context.Context, state, code, providerError, browserSecret string) (ghauth.Connection, error)
	List(ctx context.Context, userID string) ([]ghauth.Connection, error)
	Get(ctx context.Context, userID, id string) (ghauth.Connection, error)
	Disconnect(ctx context.Context, userID, id string) error
}

// startGitHubConnection returns the GitHub authorization URL and binds the
// flow to this browser with an HttpOnly cookie scoped to the callback path.
func (a *API) startGitHubConnection(w http.ResponseWriter, r *http.Request) error {
	if a.github == nil {
		return newError(http.StatusServiceUnavailable, CodeServiceUnavailable, "GitHub integration is not configured", nil)
	}
	authorizeURL, secret, err := a.github.Start(r.Context(), principal(r.Context()).UserID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: githubCookie, Value: secret, Path: githubCallbackPath, MaxAge: 600,
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"authorizeUrl": authorizeURL})
	return nil
}

// githubCallback completes the flow and redirects the browser to the Console
// (`/github?result=connected|denied|error`). It never renders tokens.
func (a *API) githubCallback(w http.ResponseWriter, r *http.Request) {
	result := "error"
	if a.github != nil {
		q := r.URL.Query()
		secret := ""
		if c, err := r.Cookie(githubCookie); err == nil {
			secret = c.Value
		}
		_, err := a.github.Callback(r.Context(), q.Get("state"), q.Get("code"), q.Get("error"), secret)
		switch {
		case err == nil:
			result = "connected"
		case errors.Is(err, ghauth.ErrDenied):
			result = "denied"
		default:
			a.log.Warn("github callback rejected", "requestId", requestID(r.Context()), "reason", err.Error())
		}
	}
	http.SetCookie(w, &http.Cookie{Name: githubCookie, Value: "", Path: githubCallbackPath, MaxAge: -1, HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, a.consoleURL+"/github?result="+url.QueryEscape(result), http.StatusFound)
}

func (a *API) listGitHubConnections(w http.ResponseWriter, r *http.Request) error {
	if a.github == nil {
		return newError(http.StatusServiceUnavailable, CodeServiceUnavailable, "GitHub integration is not configured", nil)
	}
	items, err := a.github.List(r.Context(), principal(r.Context()).UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

func (a *API) disconnectGitHub(w http.ResponseWriter, r *http.Request) (err error) {
	target := r.PathValue("connectionID")
	defer func() { a.audit(r, "github.disconnect", target, err) }()
	if a.github == nil {
		return newError(http.StatusServiceUnavailable, CodeServiceUnavailable, "GitHub integration is not configured", nil)
	}
	id := r.PathValue("connectionID")
	err = a.github.Disconnect(r.Context(), principal(r.Context()).UserID, id)
	if errors.Is(err, ghauth.ErrNotFound) {
		return errNotFound("connection", id)
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// RepositoryDiscovery is the repository/ref adapter (#92).
type RepositoryDiscovery interface {
	ListRepositories(ctx context.Context, userID, connectionID string, page, limit int, search string) (repos.ListResult, error)
	GetRepository(ctx context.Context, userID, repoID string) (repos.Repository, error)
	ListRefs(ctx context.Context, userID, repoID string) ([]repos.Ref, error)
}

func (a *API) listRepositories(w http.ResponseWriter, r *http.Request) error {
	if a.repos == nil {
		return newError(http.StatusServiceUnavailable, CodeServiceUnavailable, "GitHub integration is not configured", nil)
	}
	p, err := parsePage(r)
	if err != nil {
		return err
	}
	search := r.URL.Query().Get("search")
	if len(search) > 100 {
		return errInvalid("search must be at most 100 characters")
	}
	res, err := a.repos.ListRepositories(r.Context(), principal(r.Context()).UserID, r.PathValue("connectionID"), p.Page, p.Limit, search)
	if err != nil {
		return err
	}
	out := pageResponse(res.Items, p, res.Total)
	out["truncated"] = res.Truncated
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (a *API) getRepository(w http.ResponseWriter, r *http.Request) error {
	if a.repos == nil {
		return newError(http.StatusServiceUnavailable, CodeServiceUnavailable, "GitHub integration is not configured", nil)
	}
	repo, err := a.repos.GetRepository(r.Context(), principal(r.Context()).UserID, r.PathValue("repositoryID"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, repo)
	return nil
}

func (a *API) listRefs(w http.ResponseWriter, r *http.Request) error {
	if a.repos == nil {
		return newError(http.StatusServiceUnavailable, CodeServiceUnavailable, "GitHub integration is not configured", nil)
	}
	refs, err := a.repos.ListRefs(r.Context(), principal(r.Context()).UserID, r.PathValue("repositoryID"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": refs})
	return nil
}
