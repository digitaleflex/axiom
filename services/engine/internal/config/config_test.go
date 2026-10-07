package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := LoadFrom(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env != EnvDevelopment || cfg.Host != DefaultHost || cfg.Port != DefaultPort || cfg.Version != DefaultVersion {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Database.Required {
		t.Fatal("database must be optional in development by default")
	}
}

func TestLoadEnvironment(t *testing.T) {
	cfg, err := LoadFrom(env(map[string]string{
		"AXIOM_ENGINE_HOST": "127.0.0.1", "AXIOM_ENGINE_PORT": "9090", "AXIOM_ENGINE_VERSION": "0.1.1",
		"AXIOM_LOG_LEVEL": "DEBUG", "DATABASE_URL": "postgres://u:p@db:5432/axiom",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr() != "127.0.0.1:9090" || cfg.Version != "0.1.1" || cfg.LogLevel != "debug" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestProductionRequiresDatabase(t *testing.T) {
	_, err := LoadFrom(env(map[string]string{"AXIOM_ENV": "production"}))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatalf("expected database requirement error, got %v", err)
	}
	_, err = LoadFrom(env(map[string]string{"AXIOM_ENV": "production", "DATABASE_URL": "postgres://db/axiom", "AXIOM_DB_REQUIRED": "false"}))
	if err == nil {
		t.Fatal("production must not allow AXIOM_DB_REQUIRED=false")
	}
	cfg, err := LoadFrom(env(map[string]string{"AXIOM_ENV": "production", "DATABASE_URL": "postgres://db/axiom"}))
	if err != nil || !cfg.Database.Required {
		t.Fatalf("valid production config rejected: %v", err)
	}
}

func TestInvalidValuesFailFast(t *testing.T) {
	cases := map[string]map[string]string{
		"port":     {"AXIOM_ENGINE_PORT": "99999"},
		"port nan": {"AXIOM_ENGINE_PORT": "http"},
		"env":      {"AXIOM_ENV": "staging"},
		"level":    {"AXIOM_LOG_LEVEL": "verbose"},
		"int":      {"AXIOM_DB_MAX_OPEN_CONNS": "-1"},
		"duration": {"AXIOM_SHUTDOWN_TIMEOUT": "soon"},
		"bool":     {"AXIOM_DB_REQUIRED": "maybe"},
		"url":      {"DATABASE_URL": "mysql://db/axiom"},
		"pool":     {"AXIOM_DB_MAX_OPEN_CONNS": "2", "AXIOM_DB_MAX_IDLE_CONNS": "5"},
	}
	for name, m := range cases {
		if _, err := LoadFrom(env(m)); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestErrorsNeverLeakDatabaseCredentials(t *testing.T) {
	_, err := LoadFrom(env(map[string]string{"DATABASE_URL": "mysql://admin:s3cret@db/axiom"}))
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("error must not contain credentials: %v", err)
	}
}
