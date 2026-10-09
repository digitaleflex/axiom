package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/security/operationkey"
)

func sampleCredential() Credential {
	return Credential{
		Token:     "ac_" + "deadbeef",
		Version:   1,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		AgentID:   "agent_0123456789abcdef01234567",
		ServerID:  "srv_1",
	}
}

func TestStoreSaveLoadAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")
	s := NewStore(path)
	c := sampleCredential()
	if err := s.Save(c); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("credential file perm = %o, want 600", perm)
	}
	got, err := NewStore(path).Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Token != c.Token || got.AgentID != c.AgentID || got.Version != 1 {
		t.Fatalf("loaded = %+v", got)
	}
}

func TestLoadMissingIsErrNoCredential(t *testing.T) {
	_, err := NewStore(filepath.Join(t.TempDir(), "none.json")).Load()
	if err != ErrNoCredential {
		t.Fatalf("err = %v, want ErrNoCredential", err)
	}
}

func TestRotateSwapsFileAndKeepsGrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")
	s := NewStore(path)
	old := sampleCredential()
	if err := s.Save(old); err != nil {
		t.Fatalf("save: %v", err)
	}

	now := time.Now().UTC()
	next := old
	next.Token = "ac_" + "cafebabe"
	next.Version = 2
	graceEnd := now.Add(RotationGrace)
	if _, err := s.Rotate(next, graceEnd); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// File holds the new credential immediately (no reinstall).
	reloaded, err := NewStore(path).Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Token != next.Token || reloaded.Version != 2 {
		t.Fatalf("reloaded after rotate = %+v", reloaded)
	}
	// Current is new; previous is still usable inside the grace window.
	if cur, ok := s.Current(); !ok || cur.Token != next.Token {
		t.Fatalf("current = %+v ok=%v", cur, ok)
	}
	if prev, ok := s.Previous(now.Add(time.Minute)); !ok || prev.Token != old.Token {
		t.Fatalf("previous inside grace = %+v ok=%v", prev, ok)
	}
	if _, ok := s.Previous(graceEnd.Add(time.Second)); ok {
		t.Fatal("previous must be gone after the grace window")
	}
}

func TestVerifyCredential(t *testing.T) {
	token := "ac_secret"
	hash := HashCredential(token)
	now := time.Now().UTC()
	expires := now.Add(time.Hour)

	if err := VerifyCredential(token, hash, expires, false, now); err != nil {
		t.Fatalf("valid credential rejected: %v", err)
	}
	if err := VerifyCredential("ac_wrong", hash, expires, false, now); err != ErrCredentialInvalid {
		t.Fatalf("wrong credential err = %v, want ErrCredentialInvalid", err)
	}
	if err := VerifyCredential(token, hash, now.Add(-time.Second), false, now); err != ErrCredentialExpired {
		t.Fatalf("expired err = %v, want ErrCredentialExpired", err)
	}
	if err := VerifyCredential(token, hash, expires, true, now); err != ErrCredentialRevoked {
		t.Fatalf("revoked err = %v, want ErrCredentialRevoked", err)
	}
}

func TestReplayGuardRejectsReusedNonce(t *testing.T) {
	g := NewReplayGuard(10 * time.Minute)
	now := time.Now().UTC()
	if !g.Accept("nonce-1", now) {
		t.Fatal("first nonce must be accepted")
	}
	if g.Accept("nonce-1", now.Add(time.Second)) {
		t.Fatal("replayed nonce must be rejected")
	}
	if !g.Accept("nonce-2", now.Add(time.Second)) {
		t.Fatal("fresh nonce must be accepted")
	}
	if !g.Accept("nonce-1", now.Add(11*time.Minute)) {
		t.Fatal("nonce must be accepted again after the window")
	}
}

func TestClientSignsRequests(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "credential.json")
	s := NewStore(path)
	c := sampleCredential()
	if err := s.Save(c); err != nil {
		t.Fatal(err)
	}
	client := NewClient(srv.URL, s)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(req); err != nil {
		t.Fatalf("do: %v", err)
	}
	if got.Get("Authorization") != "Bearer "+c.Token {
		t.Fatalf("Authorization = %q", got.Get("Authorization"))
	}
	if got.Get("X-Agent-ID") != c.AgentID || got.Get("X-Timestamp") == "" || got.Get("X-Nonce") == "" {
		t.Fatalf("missing auth headers: %v", got)
	}
	if _, err := time.Parse(time.RFC3339, got.Get("X-Timestamp")); err != nil {
		t.Fatalf("timestamp not RFC3339: %v", err)
	}
}

func TestClientRotateSwapsCredential(t *testing.T) {
	next := sampleCredential()
	next.Token = "ac_" + "0123abcd"
	next.Version = 2
	next.ExpiresAt = time.Now().UTC().Add(24 * time.Hour)

	var sawAuth, sawNonce string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/rotate" {
			t.Errorf("path = %s", r.URL.Path)
		}
		sawAuth = r.Header.Get("Authorization")
		sawNonce = r.Header.Get("X-Nonce")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"agentId":             next.AgentID,
			"serverId":            next.ServerID,
			"credential":          next.Token,
			"credentialVersion":   next.Version,
			"credentialExpiresAt": next.ExpiresAt.Format(time.RFC3339),
		})
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "credential.json")
	s := NewStore(path)
	old := sampleCredential()
	if err := s.Save(old); err != nil {
		t.Fatal(err)
	}
	client := NewClient(srv.URL, s)
	rotated, err := client.Rotate(context.Background())
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if sawAuth != "Bearer "+old.Token || sawNonce == "" {
		t.Fatalf("rotate request not signed: auth=%q nonce=%q", sawAuth, sawNonce)
	}
	if rotated.Token != next.Token || rotated.Version != 2 {
		t.Fatalf("rotated = %+v", rotated)
	}
	if cur, _ := s.Current(); cur.Token != next.Token {
		t.Fatalf("store current after rotate = %+v", cur)
	}
	if prev, ok := s.Previous(time.Now().UTC()); !ok || prev.Token != old.Token {
		t.Fatalf("old credential must remain in grace: %+v ok=%v", prev, ok)
	}
}

// TestClientRotatePersistsOperationSigningKey covers ADR-0008 C6 on the
// rotation leg: a rotate response carrying "operationSigningKey" (lowercase
// hex, 32 bytes) has it persisted 0600 via operationkey before the rotated
// credential is swapped in.
func TestClientRotatePersistsOperationSigningKey(t *testing.T) {
	newKeyHex := strings.Repeat("ab", 32)
	next := sampleCredential()
	next.Token = "ac_" + "0123abcd"
	next.Version = 2

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"agentId":             next.AgentID,
			"serverId":            next.ServerID,
			"credential":          next.Token,
			"credentialVersion":   next.Version,
			"credentialExpiresAt": next.ExpiresAt.Format(time.RFC3339),
			"operationSigningKey": newKeyHex,
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "credential.json"))
	if err := s.Save(sampleCredential()); err != nil {
		t.Fatal(err)
	}
	opKeys := operationkey.NewStore(filepath.Join(dir, "operation-key.json"))
	client := NewClient(srv.URL, s)
	client.OperationKeys = opKeys

	if _, err := client.Rotate(context.Background()); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	got, ok := opKeys.Current()
	if !ok || got.Hex() != newKeyHex {
		t.Fatalf("persisted operation key = %x ok=%v, want %s", []byte(got), ok, newKeyHex)
	}
	info, err := os.Stat(opKeys.Path())
	if err != nil {
		t.Fatalf("key file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("operation key file perm = %o, want 600", perm)
	}

	// A rotate response without the field leaves the stored key untouched.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"agentId": next.AgentID, "serverId": next.ServerID,
			"credential": next.Token, "credentialVersion": 3,
			"credentialExpiresAt": next.ExpiresAt.Format(time.RFC3339),
		})
	}))
	defer srv2.Close()
	client2 := NewClient(srv2.URL, s)
	client2.OperationKeys = opKeys
	if _, err := client2.Rotate(context.Background()); err != nil {
		t.Fatalf("rotate without key: %v", err)
	}
	if still, ok := opKeys.Current(); !ok || still.Hex() != newKeyHex {
		t.Fatalf("stored key after key-less rotate = %x ok=%v, want %s", []byte(still), ok, newKeyHex)
	}

	// Fail closed: a key with no store wired is never dropped silently.
	client3 := NewClient(srv.URL, s)
	if _, err := client3.Rotate(context.Background()); err == nil {
		t.Fatal("rotate must fail closed when the operation key has nowhere to go")
	}

	// Fail closed: a malformed key is refused, never normalized.
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"agentId": next.AgentID, "serverId": next.ServerID,
			"credential": next.Token, "credentialVersion": 4,
			"credentialExpiresAt": next.ExpiresAt.Format(time.RFC3339),
			"operationSigningKey": "NOT-HEX",
		})
	}))
	defer srv3.Close()
	client4 := NewClient(srv3.URL, s)
	client4.OperationKeys = opKeys
	if _, err := client4.Rotate(context.Background()); err == nil {
		t.Fatal("rotate must refuse a malformed operation signing key")
	}
}
