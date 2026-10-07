package bootstrap

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
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
	cfg := testConfig(t, map[string]string{"DATABASE_URL": url, "AXIOM_DB_REQUIRED": "true"})
	app, err := New(context.Background(), cfg, logger.NewWriter(&bytes.Buffer{}, "error"))
	if err != nil {
		t.Fatal(err)
	}
	if app.DB() == nil {
		t.Fatal("expected database handle")
	}
	app.close()
}
