package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/auth"
)

const testPassword = "correct horse battery staple"

type sessionHarness struct {
	t       *testing.T
	handler http.Handler
	svc     *auth.Service
	clock   *time.Time
}

func newSessionHarness(t *testing.T, secure bool) *sessionHarness {
	t.Helper()
	clock := new(time.Time)
	*clock = time.Now().UTC()
	svc := auth.NewService(auth.NewMemoryStore(),
		auth.WithClock(func() time.Time { return *clock }),
		auth.WithPasswordIterations(1000),
		auth.WithTouchInterval(0),
	)
	h := New(Deps{
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:          NewSessionAuthenticator(svc, "", Principal{}),
		Sessions:      svc,
		SecureCookies: secure,
	})
	return &sessionHarness{t: t, handler: h, svc: svc, clock: clock}
}

func (sh *sessionHarness) do(method, path, body string, hdr map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	sh.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	rr := httptest.NewRecorder()
	sh.handler.ServeHTTP(rr, req)
	out := map[string]any{}
	if rr.Body.Len() > 0 {
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
	}
	return rr, out
}

func sessionCookie(rr *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rr.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			return c
		}
	}
	return nil
}

func cookieHeader(value string) map[string]string {
	return map[string]string{"Cookie": auth.SessionCookieName + "=" + value}
}

func errorCodeOf(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func (sh *sessionHarness) register() (*httptest.ResponseRecorder, map[string]any, *http.Cookie, string) {
	sh.t.Helper()
	rr, body := sh.do("POST", "/api/v1/auth/register",
		`{"email":"jane@example.com","password":"`+testPassword+`","name":"Jane"}`, nil)
	if rr.Code != http.StatusCreated {
		sh.t.Fatalf("register = %d %v", rr.Code, body)
	}
	cookie := sessionCookie(rr)
	if cookie == nil {
		sh.t.Fatal("register did not set a session cookie")
	}
	csrf, _ := body["csrfToken"].(string)
	if csrf == "" {
		sh.t.Fatal("register did not return a CSRF token")
	}
	return rr, body, cookie, csrf
}

func TestRegisterLoginAndCookieAttributes(t *testing.T) {
	sh := newSessionHarness(t, false)
	rr, body, cookie, csrf := sh.register()

	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("cookie attributes = %+v", cookie)
	}
	if cookie.Secure {
		t.Fatal("Secure must be off outside production")
	}
	if cookie.MaxAge <= 0 {
		t.Fatalf("cookie MaxAge = %d", cookie.MaxAge)
	}
	user, _ := body["user"].(map[string]any)
	id, _ := user["id"].(string)
	if !strings.HasPrefix(id, "usr_") || user["email"] != "jane@example.com" {
		t.Fatalf("user = %v", user)
	}

	// Session cookie authenticates /auth/me and echoes the CSRF token.
	rr, me := sh.do("GET", "/api/v1/auth/me", "", cookieHeader(cookie.Value))
	if rr.Code != http.StatusOK || me["id"] != id || me["csrfToken"] != csrf {
		t.Fatalf("me = %d %v", rr.Code, me)
	}

	// Login issues a fresh session.
	rr, login := sh.do("POST", "/api/v1/auth/login",
		`{"email":"jane@example.com","password":"`+testPassword+`"}`, nil)
	if rr.Code != http.StatusOK || sessionCookie(rr) == nil || login["csrfToken"] == "" {
		t.Fatalf("login = %d %v", rr.Code, login)
	}

	// Wrong password and unknown email both yield the same generic 401.
	for _, creds := range []string{
		`{"email":"jane@example.com","password":"wrong password here"}`,
		`{"email":"nobody@example.com","password":"` + testPassword + `"}`,
	} {
		rr, out := sh.do("POST", "/api/v1/auth/login", creds, nil)
		if rr.Code != http.StatusUnauthorized || errorCodeOf(out) != CodeUnauthorized {
			t.Fatalf("login failure = %d %v", rr.Code, out)
		}
		if out["error"].(map[string]any)["message"] != "email or password is incorrect" {
			t.Fatalf("login failure must be generic: %v", out)
		}
	}

	// Registration validation.
	if rr, _ := sh.do("POST", "/api/v1/auth/register", `{"email":"bad","password":"`+testPassword+`"}`, nil); rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid email = %d", rr.Code)
	}
	if rr, _ := sh.do("POST", "/api/v1/auth/register", `{"email":"x@example.com","password":"short"}`, nil); rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("weak password = %d", rr.Code)
	}
	if rr, _ := sh.do("POST", "/api/v1/auth/register", `{"email":"jane@example.com","password":"`+testPassword+`"}`, nil); rr.Code != http.StatusConflict {
		t.Fatalf("duplicate email = %d", rr.Code)
	}
}

func TestSecureCookieInProduction(t *testing.T) {
	sh := newSessionHarness(t, true)
	_, _, cookie, _ := sh.register()
	if !cookie.Secure {
		t.Fatal("Secure must be set in production")
	}
}

func TestCSRFEnforcementForCookieAuth(t *testing.T) {
	sh := newSessionHarness(t, false)
	_, _, cookie, csrf := sh.register()

	// Missing CSRF token: 403.
	rr, out := sh.do("POST", "/api/v1/auth/logout", "", cookieHeader(cookie.Value))
	if rr.Code != http.StatusForbidden || errorCodeOf(out) != CodeForbidden {
		t.Fatalf("missing csrf = %d %v", rr.Code, out)
	}
	// Wrong CSRF token: 403.
	hdr := cookieHeader(cookie.Value)
	hdr[csrfHeader] = "wrong-token"
	rr, _ = sh.do("POST", "/api/v1/auth/logout", "", hdr)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("wrong csrf = %d", rr.Code)
	}
	// Safe methods are not CSRF-checked.
	if rr, _ := sh.do("GET", "/api/v1/auth/me", "", cookieHeader(cookie.Value)); rr.Code != http.StatusOK {
		t.Fatalf("GET with cookie = %d", rr.Code)
	}
	// Correct CSRF token: logout succeeds and clears the cookie.
	hdr = cookieHeader(cookie.Value)
	hdr[csrfHeader] = csrf
	rr, _ = sh.do("POST", "/api/v1/auth/logout", "", hdr)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", rr.Code)
	}
	if c := sessionCookie(rr); c == nil || c.MaxAge >= 0 {
		t.Fatalf("logout must clear the cookie: %+v", c)
	}
	if rr, _ := sh.do("GET", "/api/v1/auth/me", "", cookieHeader(cookie.Value)); rr.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d", rr.Code)
	}
}

func TestBearerSessionTokenExemptFromCSRF(t *testing.T) {
	sh := newSessionHarness(t, false)
	_, _, cookie, _ := sh.register()
	rr, _ := sh.do("POST", "/api/v1/auth/logout", "", map[string]string{"Authorization": "Bearer " + cookie.Value})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("bearer logout = %d", rr.Code)
	}
	// The token is now revoked: bearer auth is rejected.
	if rr, _ := sh.do("GET", "/api/v1/auth/me", "", map[string]string{"Authorization": "Bearer " + cookie.Value}); rr.Code != http.StatusUnauthorized {
		t.Fatalf("revoked bearer = %d", rr.Code)
	}
}

func TestSessionExpiryRejects(t *testing.T) {
	sh := newSessionHarness(t, false)
	_, _, cookie, _ := sh.register()
	*sh.clock = sh.clock.Add(auth.DefaultSessionTTL + time.Minute)
	if rr, _ := sh.do("GET", "/api/v1/auth/me", "", cookieHeader(cookie.Value)); rr.Code != http.StatusUnauthorized {
		t.Fatalf("expired session = %d, want 401", rr.Code)
	}
}

func TestSlidingActivityRecorded(t *testing.T) {
	sh := newSessionHarness(t, false)
	_, body, cookie, _ := sh.register()
	id := body["user"].(map[string]any)["id"].(string)

	*sh.clock = sh.clock.Add(2 * time.Minute)
	if rr, _ := sh.do("GET", "/api/v1/auth/me", "", cookieHeader(cookie.Value)); rr.Code != http.StatusOK {
		t.Fatalf("me = %d", rr.Code)
	}
	sessions, err := sh.svc.ListSessions(context.Background(), id)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("list = %+v %v", sessions, err)
	}
	if !sessions[0].LastSeenAt.Equal(*sh.clock) {
		t.Fatalf("last_seen = %v, want %v", sessions[0].LastSeenAt, *sh.clock)
	}
}

func TestListAndRevokeSessions(t *testing.T) {
	sh := newSessionHarness(t, false)
	_, body, cookieA, csrfA := sh.register()
	id := body["user"].(map[string]any)["id"].(string)

	// Second concurrent session for the same account.
	rr, _ := sh.do("POST", "/api/v1/auth/login", `{"email":"jane@example.com","password":"`+testPassword+`"}`, nil)
	cookieB := sessionCookie(rr)
	if cookieB == nil {
		t.Fatal("second login did not set a cookie")
	}

	rr, list := sh.do("GET", "/api/v1/auth/sessions", "", cookieHeader(cookieA.Value))
	if rr.Code != http.StatusOK {
		t.Fatalf("list = %d %v", rr.Code, list)
	}
	items, _ := list["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("sessions = %v", list)
	}
	// No secrets in the listing body.
	raw := rr.Body.String()
	if strings.Contains(raw, cookieA.Value) || strings.Contains(raw, cookieB.Value) || strings.Contains(raw, csrfA) {
		t.Fatalf("session listing leaked a secret: %s", raw)
	}
	current := 0
	for _, it := range items {
		if it.(map[string]any)["current"] == true {
			current++
		}
	}
	if current != 1 {
		t.Fatalf("current sessions = %d", current)
	}

	// Revoke other sessions (keep A).
	hdr := cookieHeader(cookieA.Value)
	hdr[csrfHeader] = csrfA
	rr, out := sh.do("DELETE", "/api/v1/auth/sessions", "", hdr)
	if rr.Code != http.StatusOK || out["revoked"] != float64(1) {
		t.Fatalf("revoke others = %d %v", rr.Code, out)
	}
	if rr, _ := sh.do("GET", "/api/v1/auth/me", "", cookieHeader(cookieB.Value)); rr.Code != http.StatusUnauthorized {
		t.Fatalf("revoked other session = %d", rr.Code)
	}
	if rr, _ := sh.do("GET", "/api/v1/auth/me", "", cookieHeader(cookieA.Value)); rr.Code != http.StatusOK {
		t.Fatalf("kept session = %d", rr.Code)
	}

	// Revoke a specific session by id.
	rr, _ = sh.do("POST", "/api/v1/auth/login", `{"email":"jane@example.com","password":"`+testPassword+`"}`, nil)
	cookieC := sessionCookie(rr)
	rr, list = sh.do("GET", "/api/v1/auth/sessions", "", cookieHeader(cookieA.Value))
	var otherID string
	for _, it := range list["items"].([]any) {
		m := it.(map[string]any)
		if m["current"] != true {
			otherID = m["id"].(string)
		}
	}
	if otherID == "" {
		t.Fatalf("no other session to revoke: %v", list)
	}
	hdr = cookieHeader(cookieA.Value)
	hdr[csrfHeader] = csrfA
	if rr, _ := sh.do("DELETE", "/api/v1/auth/sessions/"+otherID, "", hdr); rr.Code != http.StatusNoContent {
		t.Fatalf("revoke session = %d", rr.Code)
	}
	if rr, _ := sh.do("GET", "/api/v1/auth/me", "", cookieHeader(cookieC.Value)); rr.Code != http.StatusUnauthorized {
		t.Fatalf("specifically revoked session = %d", rr.Code)
	}
	if rr, _ := sh.do("DELETE", "/api/v1/auth/sessions/ses_missing", "", hdr); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown session = %d", rr.Code)
	}
	_ = id
}

func TestStaticMachineTokenEscapeHatch(t *testing.T) {
	clock := new(time.Time)
	*clock = time.Now().UTC()
	svc := auth.NewService(auth.NewMemoryStore(), auth.WithClock(func() time.Time { return *clock }))
	const machine = "machine-token-0123456789abcdef01234567"
	h := New(Deps{
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:     NewSessionAuthenticator(svc, machine, Principal{UserID: "usr_machine", Name: "Machine"}),
		Sessions: svc,
	})
	get := func(method, path, authHeader string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", authHeader)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}
	if rr := get("GET", "/api/v1/auth/me", "Bearer "+machine); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "usr_machine") {
		t.Fatalf("machine me = %d %s", rr.Code, rr.Body.String())
	}
	// Machine clients are bearer-based and exempt from CSRF.
	if rr := get("POST", "/api/v1/auth/logout", "Bearer "+machine); rr.Code != http.StatusNoContent {
		t.Fatalf("machine logout = %d", rr.Code)
	}
	// A wrong static token is rejected.
	if rr := get("GET", "/api/v1/auth/me", "Bearer nope"); rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong machine token = %d", rr.Code)
	}
}

func TestSessionSecretsNeverLogged(t *testing.T) {
	var buf bytes.Buffer
	clock := new(time.Time)
	*clock = time.Now().UTC()
	svc := auth.NewService(auth.NewMemoryStore(),
		auth.WithClock(func() time.Time { return *clock }),
		auth.WithPasswordIterations(1000),
	)
	h := New(Deps{
		Log:      slog.New(slog.NewJSONHandler(&buf, nil)),
		Auth:     NewSessionAuthenticator(svc, "", Principal{}),
		Sessions: svc,
	})
	do := func(method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, rd)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}
	rr := do("POST", "/api/v1/auth/register", `{"email":"jane@example.com","password":"`+testPassword+`"}`, nil)
	cookie := sessionCookie(rr)
	if cookie == nil {
		t.Fatal("no cookie")
	}
	var body map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	csrf, _ := body["csrfToken"].(string)
	_ = do("GET", "/api/v1/auth/me", "", cookieHeader(cookie.Value))
	_ = do("GET", "/api/v1/auth/sessions", "", cookieHeader(cookie.Value))

	logs := buf.String()
	for _, secret := range []string{cookie.Value, csrf, testPassword} {
		if secret != "" && strings.Contains(logs, secret) {
			t.Fatalf("secret leaked into logs: %s", logs)
		}
	}
}
