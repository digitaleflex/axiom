package bootstrap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/config"
	"github.com/digitaleflex/axiom/services/engine/internal/logger"
)

func testConfig(t *testing.T, m map[string]string) config.Config {
	t.Helper()
	cfg, err := config.LoadFrom(func(k string) (string, bool) { v, ok := m[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestStartServeAndGracefulShutdown(t *testing.T) {
	var logs bytes.Buffer
	cfg := testConfig(t, map[string]string{"AXIOM_ENV": "test", "AXIOM_SHUTDOWN_TIMEOUT": "2s"})
	app, err := New(context.Background(), cfg, logger.NewWriter(&logs, "info"))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, ln) }()

	base := "http://" + ln.Addr().String()
	for _, path := range []string{"/health", "/ready"} {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s = %d, want 200", path, resp.StatusCode)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("engine did not stop")
	}
	if !strings.Contains(logs.String(), "axiom engine stopped") {
		t.Fatalf("missing shutdown log: %s", logs.String())
	}
}

func TestRequiredDatabaseFailsFast(t *testing.T) {
	cfg := testConfig(t, map[string]string{
		"AXIOM_DB_REQUIRED": "true",
		"DATABASE_URL":      "postgres://axiom:axiom@127.0.0.1:1/axiom?sslmode=disable&connect_timeout=1",
	})
	if _, err := New(context.Background(), cfg, logger.NewWriter(&bytes.Buffer{}, "error")); err == nil {
		t.Fatal("expected startup failure when the required database is unreachable")
	}
}

func TestOptionalDatabaseDegrades(t *testing.T) {
	cfg := testConfig(t, map[string]string{
		"DATABASE_URL": "postgres://axiom:axiom@127.0.0.1:1/axiom?sslmode=disable&connect_timeout=1",
	})
	app, err := New(context.Background(), cfg, logger.NewWriter(&bytes.Buffer{}, "error"))
	if err != nil {
		t.Fatalf("optional database must not block startup: %v", err)
	}
	if app.DB() != nil {
		t.Fatal("expected nil database handle")
	}
}

func TestInvalidConfigRejected(t *testing.T) {
	cfg := config.Config{Env: "prod", Port: "x"}
	if _, err := New(context.Background(), cfg, nil); err == nil {
		t.Fatal("expected invalid configuration error")
	}
}

func TestListenErrorSurfaces(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	cfg := testConfig(t, map[string]string{"AXIOM_ENGINE_HOST": "127.0.0.1", "AXIOM_ENGINE_PORT": strconv.Itoa(port)})
	app, err := New(context.Background(), cfg, logger.NewWriter(&bytes.Buffer{}, "error"))
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background()); err == nil {
		t.Fatal("expected bind error on an occupied port")
	}
}

func TestWithDatabase(t *testing.T) {
	url := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	const token = "bootstrap-test-token-0123456789abcdef"
	cfg := testConfig(t, map[string]string{"DATABASE_URL": url, "AXIOM_DB_REQUIRED": "true", "AXIOM_API_TOKEN": token})
	app, err := New(context.Background(), cfg, logger.NewWriter(&bytes.Buffer{}, "error"))
	if err != nil {
		t.Fatal(err)
	}
	defer app.close()
	db := app.DB()
	ctx := context.Background()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	for _, q := range []string{
		`INSERT INTO github_connections (id, user_id) VALUES ('ghc_` + suffix + `', 'usr_local')`,
		`INSERT INTO repositories (id, connection_id, external_id, full_name, clone_url) VALUES ('repo_` + suffix + `', 'ghc_` + suffix + `', '` + suffix + `', 'acme/web', 'https://github.com/acme/web.git')`,
		`INSERT INTO servers (id, name, address, status) VALUES ('srv_` + suffix + `', 'srv', '203.0.113.10', 'ready')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM github_connections WHERE id = 'ghc_`+suffix+`'`)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM servers WHERE id = 'srv_`+suffix+`'`)
	})

	call := func(method, path, body string, hdr map[string]string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rr := httptest.NewRecorder()
		app.Handler().ServeHTTP(rr, req)
		var out map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		return rr.Code, out
	}

	code, created := call("POST", "/api/v1/applications", `{"repositoryId":"repo_`+suffix+`","name":"web-`+suffix+`"}`, nil)
	if code != 201 {
		t.Fatalf("create application = %d %v", code, created)
	}
	appID := created["id"].(string)
	if _, err := db.ExecContext(ctx, `INSERT INTO deployment_plans (id, application_id, server_id, environment, ref, application_profile_version, fingerprint, body)
		VALUES ('plan_`+suffix+`', $1, 'srv_`+suffix+`', 'production', 'main', 1, 'sha256:x', '{"steps":["BUILD","VERIFY"]}')`, appID); err != nil {
		t.Fatal(err)
	}
	key := map[string]string{"Idempotency-Key": "k-" + suffix}
	code, dep := call("POST", "/api/v1/applications/"+appID+"/deployments", `{"planId":"plan_`+suffix+`"}`, key)
	if code != 202 || dep["status"] != "PENDING" {
		t.Fatalf("create deployment = %d %v", code, dep)
	}
	code, again := call("POST", "/api/v1/applications/"+appID+"/deployments", `{"planId":"plan_`+suffix+`"}`, key)
	if code != 202 || again["id"] != dep["id"] {
		t.Fatalf("idempotent replay = %d %v", code, again)
	}
	code, got := call("GET", "/api/v1/deployments/"+dep["id"].(string)+"/steps", "", nil)
	if code != 200 || len(got["items"].([]any)) != 2 {
		t.Fatalf("steps = %d %v", code, got)
	}
	if code, _ := call("GET", "/api/v1/servers/srv_"+suffix, "", nil); code != 200 {
		t.Fatalf("get server = %d", code)
	}

	// SSE over the real stack, and an open stream must not block graceful shutdown.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(runCtx, ln) }()
	req, _ := http.NewRequest("GET", "http://"+ln.Addr().String()+"/api/v1/deployments/"+dep["id"].(string)+"/events/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	gotFirst := false
	for sc.Scan() {
		if sc.Text() == "id: 1" {
			gotFirst = true
			break
		}
	}
	if !gotFirst {
		t.Fatal("stream did not deliver event 1")
	}
	stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown with open stream: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("open SSE stream blocked shutdown")
	}
}
