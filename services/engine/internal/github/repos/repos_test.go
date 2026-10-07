package repos

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/migrations"
)

type tokens struct {
	mu     sync.Mutex
	marked []string
	err    error
}

func (t *tokens) AccessToken(_ context.Context, userID, conn string) (string, error) {
	if t.err != nil {
		return "", t.err
	}
	if userID != "usr_1" || conn != "ghc_1" {
		return "", ErrConnectionAbsent
	}
	return "gho_test", nil
}
func (t *tokens) MarkNeedsAttention(_ context.Context, conn string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.marked = append(t.marked, conn)
	return nil
}

type memStore struct {
	mu    sync.Mutex
	byExt map[string]Repository
}

func (m *memStore) Upsert(_ context.Context, conn string, items []Repository) ([]Repository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Repository{}
	for _, r := range items {
		if existing, ok := m.byExt[r.ExternalID]; ok {
			r.ID = existing.ID
		} else {
			r.ID = "repo_" + r.ExternalID
		}
		r.ConnectionID = conn
		m.byExt[r.ExternalID] = r
		out = append(out, r)
	}
	return out, nil
}
func (m *memStore) Get(_ context.Context, userID, id string) (Repository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.byExt {
		if r.ID == id && userID == "usr_1" {
			return r, nil
		}
	}
	return Repository{}, ErrNotFound
}

// fakeAPI serves 250 repositories (3 pages), branches/tags, commits, tarballs.
type fakeAPI struct {
	reposStatus int
	rateLimited bool
	slow        time.Duration
	authHeaders []string
	codeloadURL string
	mu          sync.Mutex
}

func (f *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()
	page := func(r *http.Request) int { p, _ := strconv.Atoi(r.URL.Query().Get("page")); return p }
	link := func(w http.ResponseWriter, r *http.Request, last int) {
		if p := page(r); p < last {
			w.Header().Set("Link", fmt.Sprintf(`<%s?page=%d>; rel="next"`, r.URL.Path, p+1))
		}
	}
	mux.HandleFunc("GET /user/repos", func(w http.ResponseWriter, r *http.Request) {
		if f.slow > 0 {
			time.Sleep(f.slow)
		}
		if f.rateLimited {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(403)
			return
		}
		if f.reposStatus != 0 {
			w.WriteHeader(f.reposStatus)
			return
		}
		p := page(r)
		var out []map[string]any
		for i := (p-1)*100 + 1; i <= p*100 && i <= 250; i++ {
			out = append(out, map[string]any{"id": i, "full_name": fmt.Sprintf("acme/repo-%03d", i), "clone_url": "https://github.com/acme/x.git",
				"default_branch": "main", "private": i%2 == 0, "language": "Go"})
		}
		link(w, r, 3)
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /repositories/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "full_name": "acme/repo-007", "default_branch": "develop", "private": true})
	})
	mux.HandleFunc("GET /repos/acme/repo-007/branches", func(w http.ResponseWriter, r *http.Request) {
		if page(r) == 1 {
			link(w, r, 2)
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "zeta", "commit": map[string]string{"sha": "z1"}}, {"name": "main", "commit": map[string]string{"sha": "m1"}}})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "develop", "commit": map[string]string{"sha": "d1"}}})
	})
	mux.HandleFunc("GET /repos/acme/repo-007/tags", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "v1.0.0", "commit": map[string]string{"sha": "t1"}}})
	})
	mux.HandleFunc("GET /repos/acme/repo-007/commits/{ref}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("ref") != "main" {
			w.WriteHeader(422)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": strings.Repeat("a", 40), "commit": map[string]any{"message": "fix: header\n\nbody", "author": map[string]any{"name": "Jane", "date": "2026-10-07T10:00:00Z"}}})
	})
	mux.HandleFunc("GET /repos/acme/repo-007/tarball/{sha}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, f.codeloadURL+"/codeload/"+r.PathValue("sha"), http.StatusFound)
	})
	mux.HandleFunc("GET /codeload/{sha}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.authHeaders = append(f.authHeaders, r.Header.Get("Authorization"))
		f.mu.Unlock()
		_, _ = w.Write([]byte("TARBALL"))
	})
	return mux
}

func setup(t *testing.T, f *fakeAPI) (*Service, *tokens) {
	// The archive redirect goes to a different host name (localhost vs 127.0.0.1).
	codeload := httptest.NewServer(f.handler())
	t.Cleanup(codeload.Close)
	f.codeloadURL = strings.Replace(codeload.URL, "127.0.0.1", "localhost", 1)
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	tok := &tokens{}
	return &Service{Tokens: tok, Store: &memStore{byExt: map[string]Repository{}}, APIURL: srv.URL, HTTP: &http.Client{Timeout: 2 * time.Second}}, tok
}

func TestListRepositoriesPaginatesDeterministically(t *testing.T) {
	svc, _ := setup(t, &fakeAPI{})
	ctx := context.Background()
	res, err := svc.ListRepositories(ctx, "usr_1", "ghc_1", 3, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 250 || len(res.Items) != 50 || res.Items[0].FullName != "acme/repo-201" || res.Truncated {
		t.Fatalf("page 3 = total %d len %d first %s", res.Total, len(res.Items), res.Items[0].FullName)
	}
	again, _ := svc.ListRepositories(ctx, "usr_1", "ghc_1", 3, 100, "")
	if again.Items[0].ID != res.Items[0].ID || !strings.HasPrefix(res.Items[0].ID, "repo_") {
		t.Fatal("repository IDs must be stable and provider-scoped")
	}
	res, _ = svc.ListRepositories(ctx, "usr_1", "ghc_1", 1, 20, "REPO-00")
	if res.Total != 9 || res.Items[0].FullName != "acme/repo-001" {
		t.Fatalf("search = %d %v", res.Total, res.Items)
	}
	if !res.Items[1].Private || res.Items[0].Private {
		t.Fatal("private flag must be preserved")
	}
	res, _ = svc.ListRepositories(ctx, "usr_1", "ghc_1", 99, 20, "")
	if len(res.Items) != 0 || res.Total != 250 {
		t.Fatal("out-of-range page must be empty, not an error")
	}
}

func TestRefsAndResolution(t *testing.T) {
	svc, _ := setup(t, &fakeAPI{})
	ctx := context.Background()
	if _, err := svc.ListRepositories(ctx, "usr_1", "ghc_1", 1, 10, ""); err != nil {
		t.Fatal(err)
	}
	repo, err := svc.GetRepository(ctx, "usr_1", "repo_7")
	if err != nil || repo.DefaultBranch != "develop" {
		t.Fatalf("get = %+v %v", repo, err)
	}
	refs, err := svc.ListRefs(ctx, "usr_1", "repo_7")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range refs {
		names = append(names, r.Type+":"+r.Name)
	}
	if got := strings.Join(names, ","); got != "branch:develop,branch:main,branch:zeta,tag:v1.0.0" || !refs[0].Default {
		t.Fatalf("refs = %s", got)
	}
	c, err := svc.ResolveRef(ctx, "usr_1", "repo_7", "main")
	if err != nil || len(c.SHA) != 40 || c.Message != "fix: header" || c.Author != "Jane" {
		t.Fatalf("resolve = %+v %v", c, err)
	}
	if _, err := svc.ResolveRef(ctx, "usr_1", "repo_7", "missing"); !errors.Is(err, ErrRefNotFound) {
		t.Fatalf("missing ref: %v", err)
	}
	for _, bad := range []string{"../etc", "a b", "", "x..y", "-/", "refs/heads/x.lock", strings.Repeat("a", 256)} {
		if _, err := svc.ResolveRef(ctx, "usr_1", "repo_7", bad); !errors.Is(err, ErrInvalidRef) {
			t.Errorf("ref %q must be rejected, got %v", bad, err)
		}
	}
	if _, err := svc.ListRefs(ctx, "usr_2", "repo_7"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user: %v", err)
	}
}

func TestArchiveDoesNotLeakTokenToRedirectHost(t *testing.T) {
	f := &fakeAPI{}
	svc, _ := setup(t, f)
	ctx := context.Background()
	_, _ = svc.ListRepositories(ctx, "usr_1", "ghc_1", 1, 10, "")
	rc, err := svc.Archive(ctx, "usr_1", "repo_7", strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	rc.Close()
	if string(body) != "TARBALL" {
		t.Fatalf("archive = %q", body)
	}
	if len(f.authHeaders) != 1 || f.authHeaders[0] != "" {
		t.Fatalf("token must not be forwarded to the redirect host, got %q", f.authHeaders)
	}
	if _, err := svc.Archive(ctx, "usr_1", "repo_7", "main"); !errors.Is(err, ErrInvalidRef) {
		t.Fatalf("archive by branch must be rejected: %v", err)
	}
}

func TestProviderErrors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		api  *fakeAPI
		want error
	}{
		{&fakeAPI{rateLimited: true}, ErrRateLimited},
		{&fakeAPI{reposStatus: 403}, ErrForbidden},
		{&fakeAPI{reposStatus: 502}, ErrUnavailable},
		{&fakeAPI{slow: 3 * time.Second}, ErrUnavailable},
	}
	for _, c := range cases {
		svc, _ := setup(t, c.api)
		if _, err := svc.ListRepositories(ctx, "usr_1", "ghc_1", 1, 10, ""); !errors.Is(err, c.want) {
			t.Errorf("want %v, got %v", c.want, err)
		}
	}
	svc, tok := setup(t, &fakeAPI{reposStatus: 401})
	if _, err := svc.ListRepositories(ctx, "usr_1", "ghc_1", 1, 10, ""); !errors.Is(err, ErrReconnectNeeded) || len(tok.marked) != 1 {
		t.Fatalf("401 must flag the connection: %v marked=%v", err, tok.marked)
	}
	svc, tok = setup(t, &fakeAPI{})
	tok.err = errors.New("disconnected")
	svc.MapTokenError = func(error) error { return ErrReconnectNeeded }
	if _, err := svc.ListRepositories(ctx, "usr_1", "ghc_1", 1, 10, ""); !errors.Is(err, ErrReconnectNeeded) {
		t.Fatalf("token errors must be mapped: %v", err)
	}
}

func TestPGStore(t *testing.T) {
	dsn := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, _ := sql.Open("pgx", dsn)
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS repos_pgstore CASCADE; CREATE SCHEMA repos_pgstore`); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS repos_pgstore CASCADE`)
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, _ := sql.Open("pgx", dsn+sep+"search_path=repos_pgstore")
	defer db.Close()
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`INSERT INTO users (id) VALUES ('usr_1'), ('usr_2')`, `INSERT INTO github_connections (id, user_id) VALUES ('ghc_1', 'usr_1')`} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	st := PGStore{DB: db}
	first, err := st.Upsert(ctx, "ghc_1", []Repository{{ExternalID: "1", FullName: "acme/web", DefaultBranch: "main", Private: true}})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := st.Upsert(ctx, "ghc_1", []Repository{{ExternalID: "1", FullName: "acme/web-renamed", DefaultBranch: "main"}})
	if first[0].ID != second[0].ID {
		t.Fatal("upsert must keep a stable ID")
	}
	got, err := st.Get(ctx, "usr_1", first[0].ID)
	if err != nil || got.FullName != "acme/web-renamed" || got.Private {
		t.Fatalf("get = %+v %v", got, err)
	}
	if _, err := st.Get(ctx, "usr_2", first[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user: %v", err)
	}
}
