package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("AXIOM_ENGINE_HOST", "")
	t.Setenv("AXIOM_ENGINE_PORT", "")
	t.Setenv("AXIOM_ENGINE_VERSION", "")

	cfg := Load()

	if cfg.Host != DefaultHost {
		t.Fatalf("host = %q, want %q", cfg.Host, DefaultHost)
	}
	if cfg.Port != DefaultPort {
		t.Fatalf("port = %q, want %q", cfg.Port, DefaultPort)
	}
	if cfg.Version != DefaultVersion {
		t.Fatalf("version = %q, want %q", cfg.Version, DefaultVersion)
	}
}

func TestLoadEnvironment(t *testing.T) {
	t.Setenv("AXIOM_ENGINE_HOST", "127.0.0.1")
	t.Setenv("AXIOM_ENGINE_PORT", "9090")
	t.Setenv("AXIOM_ENGINE_VERSION", "0.1.1")

	cfg := Load()

	if cfg.Host != "127.0.0.1" || cfg.Port != "9090" || cfg.Version != "0.1.1" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}
