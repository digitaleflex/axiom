// Package repos discovers repositories and refs through a bounded GitHub
// adapter (issue #92). It returns deterministic, provider-scoped DTOs; the
// analyzer stays GitHub-agnostic and only receives snapshots (#93).
package repos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Normalized errors.
var (
	ErrNotFound         = errors.New("github: repository or ref not found")
	ErrRefNotFound      = errors.New("github: ref not found")
	ErrInvalidRef       = errors.New("github: invalid ref name")
	ErrForbidden        = errors.New("github: permission denied")
	ErrRateLimited      = errors.New("github: rate limited")
	ErrUnavailable      = errors.New("github: unavailable or timed out")
	ErrReconnectNeeded  = errors.New("github: connection must be reconnected")
	ErrConnectionAbsent = errors.New("github: connection not found")
)

// Repository is the normalized repository DTO (API contract §5).
type Repository struct {
	ID            string     `json:"id"`
	ConnectionID  string     `json:"connectionId"`
	ExternalID    string     `json:"externalId"`
	FullName      string     `json:"fullName"`
	CloneURL      string     `json:"cloneUrl"`
	HTMLURL       string     `json:"htmlUrl"`
	DefaultBranch string     `json:"defaultBranch"`
	Private       bool       `json:"private"`
	Language      string     `json:"language"`
	PushedAt      *time.Time `json:"pushedAt,omitempty"`
}

// Ref is a selectable branch or tag.
type Ref struct {
	Name      string `json:"name"`
	Type      string `json:"type"` // branch | tag
	CommitSHA string `json:"commitSha"`
	Default   bool   `json:"default"`
}

// Commit is a resolved ref.
type Commit struct {
	SHA     string    `json:"sha"`
	Message string    `json:"message"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
}

// TokenSource yields a valid GitHub token for a user's connection (#91).
type TokenSource interface {
	AccessToken(ctx context.Context, userID, connectionID string) (string, error)
	MarkNeedsAttention(ctx context.Context, connectionID string) error
}

// Store persists repository identity (stable internal IDs).
type Store interface {
	Upsert(ctx context.Context, connectionID string, items []Repository) ([]Repository, error)
	// Get returns a repository whose connection belongs to userID.
	Get(ctx context.Context, userID, repoID string) (Repository, error)
}

// Limits bound provider calls.
const (
	perPage  = 100
	maxPages = 10 // at most 1000 repositories / refs per listing
)

// Service implements discovery.
type Service struct {
	Tokens TokenSource
	Store  Store
	APIURL string
	HTTP   *http.Client
	// MapTokenError translates TokenSource errors (connection missing, needs attention...).
	MapTokenError func(error) error
}

func (s *Service) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// ListResult is a page of repositories.
type ListResult struct {
	Items     []Repository
	Total     int
	Truncated bool // the account has more repositories than the listing cap
}

// ListRepositories lists repositories accessible through a connection,
// sorted by full name, filtered by case-insensitive search, paginated.
func (s *Service) ListRepositories(ctx context.Context, userID, connectionID string, page, limit int, search string) (ListResult, error) {
	token, err := s.token(ctx, userID, connectionID)
	if err != nil {
		return ListResult{}, err
	}
	var all []Repository
	truncated := false
	for p := 1; ; p++ {
		var batch []ghRepo
		hasNext, err := s.get(ctx, connectionID, token, fmt.Sprintf("/user/repos?per_page=%d&page=%d&sort=full_name&affiliation=owner,collaborator,organization_member", perPage, p), &batch)
		if err != nil {
			return ListResult{}, err
		}
		for _, r := range batch {
			all = append(all, r.normalize())
		}
		if !hasNext {
			break
		}
		if p == maxPages {
			truncated = true
			break
		}
	}
	all, err = s.Store.Upsert(ctx, connectionID, all)
	if err != nil {
		return ListResult{}, fmt.Errorf("persist repositories: %w", err)
	}
	sort.Slice(all, func(i, j int) bool { return strings.ToLower(all[i].FullName) < strings.ToLower(all[j].FullName) })
	if search = strings.ToLower(strings.TrimSpace(search)); search != "" {
		filtered := all[:0:0]
		for _, r := range all {
			if strings.Contains(strings.ToLower(r.FullName), search) {
				filtered = append(filtered, r)
			}
		}
		all = filtered
	}
	total := len(all)
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	return ListResult{Items: all[start:end], Total: total, Truncated: truncated}, nil
}

// GetRepository returns a repository and refreshes its metadata from GitHub.
func (s *Service) GetRepository(ctx context.Context, userID, repoID string) (Repository, error) {
	repo, err := s.Store.Get(ctx, userID, repoID)
	if err != nil {
		return Repository{}, err
	}
	token, err := s.token(ctx, userID, repo.ConnectionID)
	if err != nil {
		return Repository{}, err
	}
	var gr ghRepo
	if _, err := s.get(ctx, repo.ConnectionID, token, "/repositories/"+url.PathEscape(repo.ExternalID), &gr); err != nil {
		return Repository{}, err
	}
	out, err := s.Store.Upsert(ctx, repo.ConnectionID, []Repository{gr.normalize()})
	if err != nil {
		return Repository{}, err
	}
	return out[0], nil
}

// ListRefs returns branches (default first, then by name) followed by tags.
func (s *Service) ListRefs(ctx context.Context, userID, repoID string) ([]Ref, error) {
	repo, token, err := s.repoAndToken(ctx, userID, repoID)
	if err != nil {
		return nil, err
	}
	base, err := repoPath(repo.FullName)
	if err != nil {
		return nil, err
	}
	var branches, tags []Ref
	for _, kind := range []string{"branches", "tags"} {
		for p := 1; p <= maxPages; p++ {
			var batch []struct {
				Name   string `json:"name"`
				Commit struct {
					SHA string `json:"sha"`
				} `json:"commit"`
			}
			hasNext, err := s.get(ctx, repo.ConnectionID, token, fmt.Sprintf("%s/%s?per_page=%d&page=%d", base, kind, perPage, p), &batch)
			if err != nil {
				return nil, err
			}
			for _, b := range batch {
				ref := Ref{Name: b.Name, CommitSHA: b.Commit.SHA}
				if kind == "branches" {
					ref.Type, ref.Default = "branch", b.Name == repo.DefaultBranch
					branches = append(branches, ref)
				} else {
					ref.Type = "tag"
					tags = append(tags, ref)
				}
			}
			if !hasNext {
				break
			}
		}
	}
	sort.SliceStable(branches, func(i, j int) bool {
		if branches[i].Default != branches[j].Default {
			return branches[i].Default
		}
		return branches[i].Name < branches[j].Name
	})
	sort.SliceStable(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })
	return append(branches, tags...), nil
}

var refRe = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,255}$`)

// ValidRef reports whether ref is a safe git ref name or SHA.
func ValidRef(ref string) bool {
	return refRe.MatchString(ref) && !strings.Contains(ref, "..") && !strings.HasPrefix(ref, "/") &&
		!strings.HasSuffix(ref, "/") && !strings.HasSuffix(ref, ".lock") && !strings.Contains(ref, "//")
}

// ResolveRef resolves a branch, tag or SHA to an exact commit.
func (s *Service) ResolveRef(ctx context.Context, userID, repoID, ref string) (Commit, error) {
	if !ValidRef(ref) {
		return Commit{}, ErrInvalidRef
	}
	repo, token, err := s.repoAndToken(ctx, userID, repoID)
	if err != nil {
		return Commit{}, err
	}
	base, err := repoPath(repo.FullName)
	if err != nil {
		return Commit{}, err
	}
	var c struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name string    `json:"name"`
				Date time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if _, err := s.get(ctx, repo.ConnectionID, token, base+"/commits/"+url.PathEscape(ref), &c); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Commit{}, ErrRefNotFound
		}
		return Commit{}, err
	}
	msg, _, _ := strings.Cut(c.Commit.Message, "\n")
	return Commit{SHA: c.SHA, Message: msg, Author: c.Commit.Author.Name, Date: c.Commit.Author.Date.UTC()}, nil
}

// Archive streams the tarball of an exact commit (consumed by the snapshot boundary, #93).
// The caller must close the reader. The Authorization header is not forwarded
// to the codeload redirect host (net/http drops it across hosts).
func (s *Service) Archive(ctx context.Context, userID, repoID, sha string) (io.ReadCloser, error) {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(sha) {
		return nil, ErrInvalidRef
	}
	repo, token, err := s.repoAndToken(ctx, userID, repoID)
	if err != nil {
		return nil, err
	}
	base, err := repoPath(repo.FullName)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.APIURL+base+"/tarball/"+sha, nil)
	if err != nil {
		return nil, err
	}
	setHeaders(req, token)
	resp, err := (&http.Client{Transport: s.client().Transport}).Do(req) // no global timeout: archives stream; ctx bounds it
	if err != nil {
		return nil, ErrUnavailable
	}
	if err := s.statusError(ctx, repo.ConnectionID, resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp.Body, nil
}

func (s *Service) repoAndToken(ctx context.Context, userID, repoID string) (Repository, string, error) {
	repo, err := s.Store.Get(ctx, userID, repoID)
	if err != nil {
		return Repository{}, "", err
	}
	token, err := s.token(ctx, userID, repo.ConnectionID)
	return repo, token, err
}

func (s *Service) token(ctx context.Context, userID, connectionID string) (string, error) {
	t, err := s.Tokens.AccessToken(ctx, userID, connectionID)
	if err != nil && s.MapTokenError != nil {
		return "", s.MapTokenError(err)
	}
	return t, err
}

// get performs a GET and decodes JSON. hasNext reports a Link rel="next".
func (s *Service) get(ctx context.Context, connectionID, token, path string, out any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.APIURL+path, nil)
	if err != nil {
		return false, err
	}
	setHeaders(req, token)
	resp, err := s.client().Do(req)
	if err != nil {
		return false, ErrUnavailable
	}
	defer resp.Body.Close()
	if err := s.statusError(ctx, connectionID, resp); err != nil {
		return false, err
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out); err != nil {
		return false, fmt.Errorf("%w: invalid response", ErrUnavailable)
	}
	return strings.Contains(resp.Header.Get("Link"), `rel="next"`), nil
}

func (s *Service) statusError(ctx context.Context, connectionID string, resp *http.Response) error {
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		_ = s.Tokens.MarkNeedsAttention(ctx, connectionID)
		return ErrReconnectNeeded
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return ErrRateLimited
	case resp.StatusCode == http.StatusForbidden:
		return ErrForbidden
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusUnprocessableEntity:
		return ErrNotFound
	default:
		return fmt.Errorf("%w: HTTP %d", ErrUnavailable, resp.StatusCode)
	}
}

func setHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "axiom-engine")
}

var fullNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)

func repoPath(fullName string) (string, error) {
	if !fullNameRe.MatchString(fullName) {
		return "", fmt.Errorf("%w: unexpected repository name", ErrNotFound)
	}
	owner, name, _ := strings.Cut(fullName, "/")
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name), nil
}

type ghRepo struct {
	ID            int64      `json:"id"`
	FullName      string     `json:"full_name"`
	CloneURL      string     `json:"clone_url"`
	HTMLURL       string     `json:"html_url"`
	DefaultBranch string     `json:"default_branch"`
	Private       bool       `json:"private"`
	Language      string     `json:"language"`
	PushedAt      *time.Time `json:"pushed_at"`
}

func (g ghRepo) normalize() Repository {
	r := Repository{ExternalID: strconv.FormatInt(g.ID, 10), FullName: g.FullName, CloneURL: g.CloneURL, HTMLURL: g.HTMLURL,
		DefaultBranch: g.DefaultBranch, Private: g.Private, Language: g.Language}
	if g.PushedAt != nil {
		t := g.PushedAt.UTC()
		r.PushedAt = &t
	}
	if r.DefaultBranch == "" {
		r.DefaultBranch = "main"
	}
	return r
}
