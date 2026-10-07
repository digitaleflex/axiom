package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Tokens returned by GitHub. ExpiresAt is zero for non-expiring tokens.
type Tokens struct {
	Access    string
	Refresh   string
	ExpiresAt time.Time
	Scopes    string
}

// Account is the GitHub identity of a connection.
type Account struct {
	ID    string
	Login string
	Type  string // "User" or "Organization"
}

// Provider is the GitHub OAuth boundary.
type Provider interface {
	AuthorizeURL(state, codeChallenge string) string
	Exchange(ctx context.Context, code, codeVerifier string) (Tokens, error)
	Refresh(ctx context.Context, refreshToken string) (Tokens, error)
	Account(ctx context.Context, accessToken string) (Account, error)
	Revoke(ctx context.Context, accessToken string) error
}

// OAuthProvider talks to GitHub's OAuth web flow and REST API.
// It works for OAuth Apps and for GitHub App user-to-server tokens.
type OAuthProvider struct {
	ClientID, ClientSecret, RedirectURL, OAuthURL, APIURL, Scopes string
	HTTP                                                          *http.Client
}

func (p *OAuthProvider) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (p *OAuthProvider) AuthorizeURL(state, challenge string) string {
	q := url.Values{
		"client_id":             {p.ClientID},
		"redirect_uri":          {p.RedirectURL},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"allow_signup":          {"true"},
	}
	if p.Scopes != "" {
		q.Set("scope", p.Scopes)
	}
	return p.OAuthURL + "/login/oauth/authorize?" + q.Encode()
}

type tokenResponse struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresIn             int64  `json:"expires_in"`
	Scope                 string `json:"scope"`
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
}

func (p *OAuthProvider) Exchange(ctx context.Context, code, verifier string) (Tokens, error) {
	return p.token(ctx, url.Values{"code": {code}, "code_verifier": {verifier}, "redirect_uri": {p.RedirectURL}})
}

func (p *OAuthProvider) Refresh(ctx context.Context, refresh string) (Tokens, error) {
	return p.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
}

func (p *OAuthProvider) token(ctx context.Context, form url.Values) (Tokens, error) {
	form.Set("client_id", p.ClientID)
	form.Set("client_secret", p.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.OAuthURL+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client().Do(req)
	if err != nil {
		return Tokens{}, fmt.Errorf("%w: token endpoint unreachable", ErrProvider)
	}
	defer resp.Body.Close()
	var tr tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&tr); err != nil || resp.StatusCode >= 500 {
		return Tokens{}, fmt.Errorf("%w: invalid token response (HTTP %d)", ErrProvider, resp.StatusCode)
	}
	switch tr.Error {
	case "":
	case "bad_verification_code", "incorrect_client_credentials", "redirect_uri_mismatch", "bad_refresh_token", "unverified_user_email":
		// Error codes are safe to surface; descriptions/tokens are not logged.
		return Tokens{}, fmt.Errorf("%w: %s", ErrCodeRejected, tr.Error)
	default:
		return Tokens{}, fmt.Errorf("%w: %s", ErrProvider, tr.Error)
	}
	if tr.AccessToken == "" {
		return Tokens{}, fmt.Errorf("%w: empty access token", ErrProvider)
	}
	t := Tokens{Access: tr.AccessToken, Refresh: tr.RefreshToken, Scopes: tr.Scope}
	if tr.ExpiresIn > 0 {
		t.ExpiresAt = time.Now().UTC().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return t, nil
}

func (p *OAuthProvider) Account(ctx context.Context, token string) (Account, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.APIURL+"/user", nil)
	if err != nil {
		return Account{}, err
	}
	setAPIHeaders(req, token)
	resp, err := p.client().Do(req)
	if err != nil {
		return Account{}, fmt.Errorf("%w: GitHub API unreachable", ErrProvider)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return Account{}, ErrTokenInvalid
	}
	if resp.StatusCode != http.StatusOK {
		return Account{}, fmt.Errorf("%w: GET /user returned HTTP %d", ErrProvider, resp.StatusCode)
	}
	var u struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Type  string `json:"type"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&u); err != nil || u.ID == 0 || u.Login == "" {
		return Account{}, fmt.Errorf("%w: invalid /user response", ErrProvider)
	}
	if u.Type != "Organization" {
		u.Type = "User"
	}
	return Account{ID: strconv.FormatInt(u.ID, 10), Login: u.Login, Type: u.Type}, nil
}

// Revoke deletes the user's authorization grant (best effort).
func (p *OAuthProvider) Revoke(ctx context.Context, token string) error {
	body, _ := json.Marshal(map[string]string{"access_token": token})
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.APIURL+"/applications/"+url.PathEscape(p.ClientID)+"/grant", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.SetBasicAuth(p.ClientID, p.ClientSecret)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client().Do(req)
	if err != nil {
		return fmt.Errorf("%w: revoke unreachable", ErrProvider)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusUnprocessableEntity {
		return fmt.Errorf("%w: revoke returned HTTP %d", ErrProvider, resp.StatusCode)
	}
	return nil
}

func setAPIHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "axiom-engine")
}
