package api

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/digitaleflex/axiom/services/engine/internal/agentauth"
	"github.com/digitaleflex/axiom/services/engine/internal/database"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
	"github.com/digitaleflex/axiom/services/engine/migrations"
)

// --- harness ------------------------------------------------------------------

type heartbeatHarness struct {
	*agentHarness
	healthStore *healthServers
}

func newHeartbeatHarness(t *testing.T) *heartbeatHarness {
	t.Helper()
	buf := &bytes.Buffer{}
	statuses := map[string]string{"srv_pending": "pending", "srv_ready": "ready", "srv_revoked": "revoked"}
	agents := agentauth.NewService(
		agentauth.NewMemoryStore(),
		agentauth.WithServerLookup(agentauth.StaticServers(statuses)),
	)
	hs := &healthServers{
		fakeServers: &fakeServers{items: []server.Record{{ID: "srv_pending", Status: server.StatusPending}}},
		health:      map[string]server.Health{},
	}
	handler := New(Deps{
		Log:     slog.New(slog.NewJSONHandler(buf, nil)),
		Auth:    NewTokenAuthenticator(token, Principal{UserID: "usr_1", Name: "Jane"}),
		Servers: hs,
		Agents:  agents,
	})
	return &heartbeatHarness{
		agentHarness: &agentHarness{
			harness: &harness{t: t, handler: handler},
			agents:  agents,
			servers: statuses,
			logBuf:  buf,
		},
		healthStore: hs,
	}
}

// healthServers wraps fakeServers with UpdateHealth support and merges the
// latest health into Get, mirroring the real repository (#78).
type healthServers struct {
	*fakeServers
	mu     sync.Mutex
	health map[string]server.Health
}

func (h *healthServers) UpdateHealth(_ context.Context, id string, health server.Health) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.health[id] = health
	return nil
}

func (h *healthServers) Get(ctx context.Context, id string) (server.Record, error) {
	rec, err := h.fakeServers.Get(ctx, id)
	if err != nil {
		return rec, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if health, ok := h.health[id]; ok {
		rec.Status = health.Status
		rec.AgentVersion = health.AgentVersion
		rec.Capabilities = health.Capabilities
		rec.CPUCount = health.CPUCount
		rec.MemoryMB = health.MemoryMB
		rec.DiskFreeMB = health.DiskFreeMB
		rec.LastSeenAt = health.LastSeenAt
	}
	return rec, nil
}

func (h *heartbeatHarness) registerAgent(t *testing.T) (agentID, credential string) {
	t.Helper()
	r := h.register("srv_pending", h.bootstrap("srv_pending"))
	expect(t, r, 201, "")
	agentID, _ = r.body["agentId"].(string)
	credential, _ = r.body["credential"].(string)
	if agentID == "" || credential == "" {
		t.Fatalf("register = %v", r.body)
	}
	return agentID, credential
}

func (h *heartbeatHarness) heartbeat(agentID, credential, nonce string, body map[string]any) resp {
	t := h.t
	t.Helper()
	return h.do("POST", "/api/v1/agent/heartbeat", body, map[string]string{
		"Authorization": "Bearer " + credential,
		"X-Agent-ID":    agentID,
		"X-Nonce":       nonce,
		"X-Timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
}

func heartbeatBody(agentID, serverID, status string) map[string]any {
	return map[string]any{
		"protocol":     1,
		"messageId":    "msg_hb_0123456789abcdef0123456789abcdef",
		"sentAt":       time.Now().UTC().Format(time.RFC3339),
		"agentId":      agentID,
		"serverId":     serverID,
		"status":       status,
		"capabilities": []string{"docker", "traefik"},
		"cpuCount":     4,
		"memoryMb":     8192,
		"diskFreeMb":   50000,
	}
}

// --- tests --------------------------------------------------------------------

func TestAgentHeartbeatPersistsAndReturns(t *testing.T) {
	h := newHeartbeatHarness(t)
	agentID, credential := h.registerAgent(t)

	r := h.heartbeat(agentID, credential, "nonce-hb-1", heartbeatBody(agentID, "srv_pending", "READY"))
	expect(t, r, 200, "")
	if r.body["ok"] != true {
		t.Fatalf("ok = %v, want true", r.body["ok"])
	}
	if r.body["serverStatus"] != "READY" {
		t.Fatalf("serverStatus = %v, want READY", r.body["serverStatus"])
	}

	health := h.healthStore.health["srv_pending"]
	if health.Status != server.StatusReady {
		t.Fatalf("stored status = %q, want ready", health.Status)
	}
	if health.AgentVersion != "0.1.0" {
		t.Fatalf("agentVersion = %q, want 0.1.0", health.AgentVersion)
	}
	if len(health.Capabilities) != 2 || health.Capabilities[0] != server.CapabilityDocker || health.Capabilities[1] != server.CapabilityTraefik {
		t.Fatalf("capabilities = %v, want [docker traefik]", health.Capabilities)
	}
	if health.CPUCount != 4 || health.MemoryMB != 8192 || health.DiskFreeMB != 50000 {
		t.Fatalf("resources = %d/%d/%d, want 4/8192/50000", health.CPUCount, health.MemoryMB, health.DiskFreeMB)
	}
	if health.LastSeenAt == "" {
		t.Fatal("lastSeenAt is empty")
	}
	if _, err := time.Parse(time.RFC3339, health.LastSeenAt); err != nil {
		t.Fatalf("lastSeenAt %q is not RFC3339: %v", health.LastSeenAt, err)
	}

	// The read model reflects the heartbeat immediately.
	rec, err := h.healthStore.Get(context.Background(), "srv_pending")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != server.StatusReady || rec.LastSeenAt == "" {
		t.Fatalf("read model = %+v, want ready with lastSeenAt", rec)
	}
}

func TestAgentHeartbeatDegradedAndOfflineStatuses(t *testing.T) {
	h := newHeartbeatHarness(t)
	agentID, credential := h.registerAgent(t)

	r := h.heartbeat(agentID, credential, "nonce-hb-d", heartbeatBody(agentID, "srv_pending", "DEGRADED"))
	expect(t, r, 200, "")
	if r.body["serverStatus"] != "DEGRADED" {
		t.Fatalf("serverStatus = %v, want DEGRADED", r.body["serverStatus"])
	}
	if h.healthStore.health["srv_pending"].Status != server.StatusDegraded {
		t.Fatalf("stored status = %q, want degraded", h.healthStore.health["srv_pending"].Status)
	}
}

func TestAgentHeartbeatUnknownAgent(t *testing.T) {
	h := newHeartbeatHarness(t)
	// A well-formed request for an agent that does not exist is 404.
	r := h.heartbeat("agent_ffffffffffffffffffffffff", "ac_unknown", "nonce-hb-u",
		heartbeatBody("agent_ffffffffffffffffffffffff", "srv_pending", "READY"))
	expect(t, r, 404, CodeNotFound)
}

func TestAgentHeartbeatRevoked(t *testing.T) {
	h := newHeartbeatHarness(t)
	agentID, credential := h.registerAgent(t)
	// Revocation is observed through the server-status lookup (agentauth
	// checks the server's revocation state on every authenticated request).
	h.servers["srv_pending"] = "revoked"

	r := h.heartbeat(agentID, credential, "nonce-hb-r", heartbeatBody(agentID, "srv_pending", "READY"))
	expect(t, r, 401, CodeUnauthorized)
	if reason := errorReason(r); reason != "revoked" {
		t.Fatalf("reason = %q, want revoked", reason)
	}
}

func TestAgentHeartbeatReplayAndClockSkew(t *testing.T) {
	h := newHeartbeatHarness(t)
	agentID, credential := h.registerAgent(t)

	// Replayed nonce: the verifier rejects the second use.
	h.heartbeat(agentID, credential, "nonce-hb-x", heartbeatBody(agentID, "srv_pending", "READY"))
	r := h.heartbeat(agentID, credential, "nonce-hb-x", heartbeatBody(agentID, "srv_pending", "READY"))
	expect(t, r, 401, CodeUnauthorized)
	if reason := errorReason(r); reason != "replay" {
		t.Fatalf("replay reason = %q", reason)
	}

	// Clock skew: a timestamp 10 minutes in the past is rejected.
	r = h.do("POST", "/api/v1/agent/heartbeat", heartbeatBody(agentID, "srv_pending", "READY"), map[string]string{
		"Authorization": "Bearer " + credential,
		"X-Agent-ID":    agentID,
		"X-Nonce":       "nonce-hb-skew",
		"X-Timestamp":   time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339),
	})
	expect(t, r, 401, CodeUnauthorized)
	if reason := errorReason(r); reason != "clock_skew" {
		t.Fatalf("clock skew reason = %q", reason)
	}
}

func TestAgentHeartbeatMissingCredential(t *testing.T) {
	h := newHeartbeatHarness(t)
	// No Authorization header: the user token in the harness must NOT be
	// accepted for the agent endpoint.
	expect(t, h.do("POST", "/api/v1/agent/heartbeat", heartbeatBody("agent_x", "srv_pending", "READY"), nil),
		401, CodeUnauthorized)
}

func TestAgentHeartbeatInvalidPayloads(t *testing.T) {
	h := newHeartbeatHarness(t)
	agentID, credential := h.registerAgent(t)

	// Unknown status.
	r := h.heartbeat(agentID, credential, "nonce-hb-s1", heartbeatBody(agentID, "srv_pending", "FOO"))
	expect(t, r, 400, CodeInvalidRequest)

	// Identity mismatch: the body names another server than the credential's.
	body := heartbeatBody(agentID, "srv_other", "READY")
	r = h.heartbeat(agentID, credential, "nonce-hb-s2", body)
	expect(t, r, 400, CodeInvalidRequest)

	// Stale sentAt (older than 24h).
	body = heartbeatBody(agentID, "srv_pending", "READY")
	body["sentAt"] = time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
	r = h.heartbeat(agentID, credential, "nonce-hb-s3", body)
	expect(t, r, 400, CodeInvalidRequest)

	// Future sentAt beyond the skew window.
	body = heartbeatBody(agentID, "srv_pending", "READY")
	body["sentAt"] = time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)
	r = h.heartbeat(agentID, credential, "nonce-hb-s4", body)
	expect(t, r, 400, CodeInvalidRequest)

	// Unsupported protocol version.
	body = heartbeatBody(agentID, "srv_pending", "READY")
	body["protocol"] = 2
	r = h.heartbeat(agentID, credential, "nonce-hb-s5", body)
	expect(t, r, 400, CodeInvalidRequest)

	// Malformed message ID.
	body = heartbeatBody(agentID, "srv_pending", "READY")
	body["messageId"] = "has spaces"
	r = h.heartbeat(agentID, credential, "nonce-hb-s6", body)
	expect(t, r, 400, CodeInvalidRequest)
}

func TestAgentHeartbeatRapidDoubleSendLastWriteWins(t *testing.T) {
	h := newHeartbeatHarness(t)
	agentID, credential := h.registerAgent(t)

	// Two heartbeats in rapid succession (duplicate delivery): both succeed
	// and the last write wins — no dedupe or ordering state is kept.
	r1 := h.heartbeat(agentID, credential, "nonce-hb-d1", heartbeatBody(agentID, "srv_pending", "READY"))
	expect(t, r1, 200, "")
	r2 := h.heartbeat(agentID, credential, "nonce-hb-d2", heartbeatBody(agentID, "srv_pending", "DEGRADED"))
	expect(t, r2, 200, "")
	if r2.body["serverStatus"] != "DEGRADED" {
		t.Fatalf("serverStatus = %v, want DEGRADED (last write wins)", r2.body["serverStatus"])
	}
	if h.healthStore.health["srv_pending"].Status != server.StatusDegraded {
		t.Fatalf("stored status = %q, want degraded (last write wins)", h.healthStore.health["srv_pending"].Status)
	}
}

func TestAgentHeartbeatCredentialNeverLogged(t *testing.T) {
	h := newHeartbeatHarness(t)
	agentID, credential := h.registerAgent(t)

	h.heartbeat(agentID, credential, "nonce-hb-log", heartbeatBody(agentID, "srv_pending", "READY"))
	if strings.Contains(h.logBuf.String(), credential) {
		t.Fatalf("credential leaked into logs: %s", h.logBuf.String())
	}
}

// --- database-backed test ------------------------------------------------------

// openHeartbeatDB opens an isolated schema with migrations applied, mirroring
// the database package's test harness.
func openHeartbeatDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	schema := "hb_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`) })
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	db, err := sql.Open("pgx", url+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestAgentHeartbeatPersistsToDatabase exercises the endpoint against the real
// PostgreSQL repository: the heartbeat row is written by UpdateHealth and read
// back through the same schema.
func TestAgentHeartbeatPersistsToDatabase(t *testing.T) {
	db := openHeartbeatDB(t)
	ctx := context.Background()
	repos := database.NewRepositories(db)
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id) VALUES ('usr_1')`); err != nil {
		t.Fatal(err)
	}
	servers := server.NewService(repos.Servers)
	agents := agentauth.NewService(
		agentauth.NewPGStore(db),
		agentauth.WithServerLookup(agentauth.ServerLookupFunc(func(ctx context.Context, serverID string) (string, error) {
			rec, err := servers.Get(ctx, serverID)
			if errors.Is(err, server.ErrNotFound) {
				return "", agentauth.ErrServerNotFound
			}
			if err != nil {
				return "", err
			}
			return string(rec.Status), nil
		})),
	)
	if err := repos.Servers.Create(ctx, server.Record{
		ID: "srv_db", Name: "srv-db-test", Address: "203.0.113.10", OwnerID: "usr_1", Status: server.StatusPending,
	}); err != nil {
		t.Fatal(err)
	}
	handler := New(Deps{
		Log:     slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Auth:    NewTokenAuthenticator(token, Principal{UserID: "usr_1", Name: "Jane"}),
		Servers: servers,
		Agents:  agents,
	})
	h := &harness{t: t, handler: handler}

	tok, _, err := agents.Issue(ctx, "srv_db")
	if err != nil {
		t.Fatal(err)
	}
	reg, err := agents.Redeem(ctx, tok, "srv_db", "0.2.0", []string{"docker"})
	if err != nil {
		t.Fatal(err)
	}

	r := h.do("POST", "/api/v1/agent/heartbeat", heartbeatBody(reg.AgentID, "srv_db", "READY"), map[string]string{
		"Authorization": "Bearer " + reg.Credential,
		"X-Agent-ID":    reg.AgentID,
		"X-Nonce":       "nonce-db-1",
		"X-Timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
	expect(t, r, 200, "")
	if r.body["ok"] != true || r.body["serverStatus"] != "READY" {
		t.Fatalf("response = %v, want ok/READY", r.body)
	}

	rec, err := repos.Servers.Get(ctx, "srv_db")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != server.StatusReady {
		t.Fatalf("stored status = %q, want ready", rec.Status)
	}
	if rec.AgentVersion != "0.2.0" || rec.CPUCount != 4 || rec.MemoryMB != 8192 || rec.DiskFreeMB != 50000 {
		t.Fatalf("stored health = %+v", rec)
	}
	if rec.LastSeenAt == "" {
		t.Fatal("lastSeenAt was not persisted")
	}
}
