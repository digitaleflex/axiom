package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/internal/security/secrets"
	"github.com/digitaleflex/axiom/services/engine/migrations"
)

// fakeGitHub emulates the OAuth and REST endpoints and verifies PKCE.
type fakeGitHub struct {
	mu          sync.Mutex
	challenges  map[string]string // code -> challenge
	tokenSeq    int
	expiresIn   int64
	refreshFail bool
	tokenStatus int
	tokenError  string
	revoked     []string
	userID      int64
}

func newFakeGitHub() *fakeGitHub {
	return &fakeGitHub{challenges: map[string]string{}, userID: 4242}
}

func (f *fakeGitHub) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.tokenStatus != 0 {
			w.WriteHeader(f.tokenStatus)
			return
		}
		if r.Form.Get("client_secret") != "secret" {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "incorrect_client_credentials"})
			return
		}
		if f.tokenError != "" {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": f.tokenError, "error_description": "nope"})
			return
		}
		if r.Form.Get("grant_type") == "refresh_token" {
			if f.refreshFail {
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_refresh_token"})
				return
			}
		} else {
			want := f.challenges[r.Form.Get("code")]
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if want == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != want {
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_verification_code"})
				return
			}
		}
		f.tokenSeq++
		resp := map[string]any{"access_token": "gho_access_" + string(rune('A'+f.tokenSeq)), "refresh_token": "ghr_refresh", "scope": "read:user"}
		if f.expiresIn > 0 {
			resp["expires_in"] = f.expiresIn
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer gho_") {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": f.userID, "login": "octocat", "type": "User"})
	})
	mux.HandleFunc("DELETE /applications/{client}/grant", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.revoked = append(f.revoked, body["access_token"])
		f.mu.Unlock()
		w.WriteHeader(204)
	})
	return mux
}

// authorize simulates the user approving: returns code+state for the callback.
func (f *fakeGitHub) authorize(t *testing.T, authorizeURL string) (code, state string) {
	t.Helper()
	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "client" {
		t.Fatalf("bad authorize URL %s", authorizeURL)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	code = "code-" + q.Get("state")[:8]
	f.challenges[code] = q.Get("code_challenge")
	return code, q.Get("state")
}

type fixture struct {
	svc  *Service
	gh   *fakeGitHub
	db   *sql.DB
	logs *bytes.Buffer
	now  time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	schema := "ghauth_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`) })
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, err := sql.Open("pgx", dsn+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"usr_1", "usr_2"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO users (id) VALUES ($1)`, u); err != nil {
			t.Fatal(err)
		}
	}
	gh := newFakeGitHub()
	srv := httptest.NewServer(gh.handler(t))
	t.Cleanup(srv.Close)
	box, _ := secrets.NewBox(bytes.Repeat([]byte{9}, 32))
	logs := &bytes.Buffer{}
	f := &fixture{gh: gh, db: db, logs: logs, now: time.Now().UTC()}
	f.svc = &Service{
		Store: PGStore{DB: db},
		Provider: &OAuthProvider{ClientID: "client", ClientSecret: "secret", RedirectURL: "https://engine.test/api/v1/github/callback",
			OAuthURL: srv.URL, APIURL: srv.URL},
		Box: box, Log: slog.New(slog.NewJSONHandler(logs, nil)),
		Now: func() time.Time { return f.now },
	}
	return f
}

func (f *fixture) connect(t *testing.T, user string) Connection {
	t.Helper()
	authURL, browser, err := f.svc.Start(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	code, state := f.gh.authorize(t, authURL)
	conn, err := f.svc.Callback(context.Background(), state, code, "", browser)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	return conn
}

func TestConnectFlowStoresEncryptedTokens(t *testing.T) {
	f := setup(t)
	conn := f.connect(t, "usr_1")
	if conn.Status != "active" || conn.AccountLogin != "octocat" || !strings.HasPrefix(conn.ID, "ghc_") {
		t.Fatalf("connection = %+v", conn)
	}
	var raw []byte
	if err := f.db.QueryRow(`SELECT token_ciphertext FROM github_connections WHERE id = $1`, conn.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("gho_")) {
		t.Fatal("token stored in plaintext")
	}
	token, err := f.svc.AccessToken(context.Background(), "usr_1", conn.ID)
	if err != nil || !strings.HasPrefix(token, "gho_access_") {
		t.Fatalf("access token = %q %v", token, err)
	}
	if strings.Contains(f.logs.String(), "gho_") || strings.Contains(f.logs.String(), "ghr_") {
		t.Fatalf("tokens leaked into logs: %s", f.logs.String())
	}
	// Reconnecting the same GitHub account reuses the connection.
	if again := f.connect(t, "usr_1"); again.ID != conn.ID {
		t.Fatalf("reconnect created a new connection: %s vs %s", again.ID, conn.ID)
	}
	list, _ := f.svc.List(context.Background(), "usr_1")
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
	if _, err := f.svc.AccessToken(context.Background(), "usr_2", conn.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another user must not access the connection, got %v", err)
	}
}

func TestInvalidCallbacksAreRejected(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	authURL, browser, _ := f.svc.Start(ctx, "usr_1")
	code, state := f.gh.authorize(t, authURL)
	if _, err := f.svc.Callback(ctx, state, code, "", "other-browser"); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("foreign browser: %v", err)
	}
	if _, err := f.svc.Callback(ctx, state, code, "", browser); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("state must be single-use even after a failed attempt: %v", err)
	}
	if _, err := f.svc.Callback(ctx, "forged", code, "", browser); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("forged state: %v", err)
	}
	if _, err := f.svc.Callback(ctx, "", code, "", browser); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("empty state: %v", err)
	}

	authURL, browser, _ = f.svc.Start(ctx, "usr_1")
	code, state = f.gh.authorize(t, authURL)
	f.now = f.now.Add(11 * time.Minute)
	if _, err := f.svc.Callback(ctx, state, code, "", browser); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("expired state: %v", err)
	}
	f.now = time.Now().UTC()

	authURL, browser, _ = f.svc.Start(ctx, "usr_1")
	_, state = f.gh.authorize(t, authURL)
	if _, err := f.svc.Callback(ctx, state, "", "access_denied", browser); !errors.Is(err, ErrDenied) {
		t.Fatalf("access_denied: %v", err)
	}

	authURL, browser, _ = f.svc.Start(ctx, "usr_1")
	_, state = f.gh.authorize(t, authURL)
	if _, err := f.svc.Callback(ctx, state, "wrong-code", "", browser); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("PKCE/code mismatch: %v", err)
	}
}

func TestProviderErrorsAreNormalized(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.gh.tokenStatus = http.StatusBadGateway
	authURL, browser, _ := f.svc.Start(ctx, "usr_1")
	code, state := f.gh.authorize(t, authURL)
	if _, err := f.svc.Callback(ctx, state, code, "", browser); !errors.Is(err, ErrProvider) {
		t.Fatalf("5xx: %v", err)
	}
	f.gh.tokenStatus = 0
	f.gh.tokenError = "slow_down"
	authURL, browser, _ = f.svc.Start(ctx, "usr_1")
	code, state = f.gh.authorize(t, authURL)
	if _, err := f.svc.Callback(ctx, state, code, "", browser); !errors.Is(err, ErrProvider) || strings.Contains(err.Error(), "nope") {
		t.Fatalf("provider error must be normalized without descriptions: %v", err)
	}
}

func TestRefreshAndNeedsAttention(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.gh.expiresIn = 30 // expires within the refresh window
	conn := f.connect(t, "usr_1")
	first, _ := f.svc.Tokens(ctx, conn.ID)
	token, err := f.svc.AccessToken(ctx, "usr_1", conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := f.svc.Tokens(ctx, conn.ID)
	if bytes.Equal(first.AccessSealed, second.AccessSealed) || !strings.HasPrefix(token, "gho_access_") {
		t.Fatal("expiring token must be refreshed and re-sealed")
	}
	f.gh.refreshFail = true
	if _, err := f.svc.AccessToken(ctx, "usr_1", conn.ID); !errors.Is(err, ErrNeedsAttention) {
		t.Fatalf("rejected refresh: %v", err)
	}
	got, _ := f.svc.Get(ctx, "usr_1", conn.ID)
	if got.Status != "needs_attention" {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestDisconnectPreventsAccess(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	conn := f.connect(t, "usr_1")
	if err := f.svc.Disconnect(ctx, "usr_1", conn.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.gh.revoked) != 1 || !strings.HasPrefix(f.gh.revoked[0], "gho_access_") {
		t.Fatalf("grant must be revoked, got %v", f.gh.revoked)
	}
	if _, err := f.svc.AccessToken(ctx, "usr_1", conn.ID); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("access after disconnect: %v", err)
	}
	var tokenNull bool
	_ = f.db.QueryRow(`SELECT token_ciphertext IS NULL AND refresh_ciphertext IS NULL FROM github_connections WHERE id = $1`, conn.ID).Scan(&tokenNull)
	if !tokenNull {
		t.Fatal("tokens must be wiped on disconnect")
	}
	if err := f.svc.Disconnect(ctx, "usr_2", conn.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user disconnect: %v", err)
	}
	// Reconnecting restores access.
	if again := f.connect(t, "usr_1"); again.ID != conn.ID || again.Status != "active" {
		t.Fatalf("reconnect = %+v", again)
	}
}

// Tokens exposes stored tokens for assertions (test-only helper).
func (s *Service) Tokens(ctx context.Context, id string) (StoredTokens, error) {
	return s.Store.Tokens(ctx, id)
}
