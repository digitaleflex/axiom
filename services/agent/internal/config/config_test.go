package config

import "testing"

func load(t *testing.T, env map[string]string) (Config, error) {
	t.Helper()
	return LoadFrom(func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	})
}

func validEnv(extra map[string]string) map[string]string {
	env := map[string]string{
		"AXIOM_ENV":                 "test",
		"AXIOM_SERVER_ID":           "srv_test",
		"AXIOM_ENGINE_URL":          "https://engine.example.com",
		"AXIOM_AGENT_DATA_DIR":      "/var/lib/axiom-agent",
		"AXIOM_DOCKER_BINARY":       "/usr/bin/docker",
		"AXIOM_TRAEFIK_DYNAMIC_DIR": "/etc/traefik/dynamic",
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := load(t, validEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != DefaultVersion {
		t.Errorf("version = %q, want %q", cfg.Version, DefaultVersion)
	}
	if cfg.Docker.Binary != "/usr/bin/docker" {
		t.Errorf("docker binary = %q", cfg.Docker.Binary)
	}
	if cfg.Listener.Addr != DefaultListenAddr {
		t.Errorf("listen addr = %q, want the loopback default", cfg.Listener.Addr)
	}
	if cfg.Listener.AllowPublic {
		t.Error("a public bind must never be the default")
	}
	if cfg.IdentityPath() != "/var/lib/axiom-agent/identity.json" ||
		cfg.CredentialPath() != "/var/lib/axiom-agent/credential.json" ||
		cfg.StatePath() != "/var/lib/axiom-agent/state.jsonl" {
		t.Errorf("unexpected state paths: %q %q %q", cfg.IdentityPath(), cfg.CredentialPath(), cfg.StatePath())
	}
}

// TestNonLoopbackBindRequiresOptIn is the security-relevant default: the agent
// has no TLS (#88), so it must never bind a routable address by accident.
func TestNonLoopbackBindRequiresOptIn(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:9401", ":9401", "10.0.0.5:9401"} {
		env := validEnv(map[string]string{"AXIOM_AGENT_LISTEN_ADDR": addr})
		if _, err := load(t, env); err == nil {
			t.Errorf("%s was accepted without an explicit opt-in", addr)
		}
		env["AXIOM_AGENT_ALLOW_PUBLIC_LISTENER"] = "true"
		if _, err := load(t, env); err != nil {
			t.Errorf("%s with the opt-in was rejected: %v", addr, err)
		}
	}
	for _, addr := range []string{"127.0.0.1:9401", "localhost:9401", "[::1]:9401"} {
		if _, err := load(t, validEnv(map[string]string{"AXIOM_AGENT_LISTEN_ADDR": addr})); err != nil {
			t.Errorf("loopback %s was rejected: %v", addr, err)
		}
	}
}

// TestEngineEndpointPolicyFailsClosed reuses the transport hardening (#88):
// http is refused in production and outside loopback in development.
func TestEngineEndpointPolicyFailsClosed(t *testing.T) {
	if _, err := load(t, validEnv(map[string]string{"AXIOM_ENV": "production", "AXIOM_ENGINE_URL": "http://engine.example.com"})); err == nil {
		t.Error("plaintext http to the Engine was accepted in production")
	}
	if _, err := load(t, validEnv(map[string]string{"AXIOM_ENGINE_URL": "http://10.0.0.1:8080"})); err == nil {
		t.Error("plaintext http to a non-loopback Engine was accepted in development")
	}
	if _, err := load(t, validEnv(map[string]string{"AXIOM_ENGINE_URL": "http://127.0.0.1:8080"})); err != nil {
		t.Errorf("loopback http in development was rejected: %v", err)
	}
}

func TestInvalidValuesRejected(t *testing.T) {
	cases := map[string]map[string]string{
		"no server":        {"AXIOM_SERVER_ID": ""},
		"no engine":        {"AXIOM_ENGINE_URL": ""},
		"relative root":    {"AXIOM_AGENT_DATA_DIR": "relative/path"},
		"relative traefik": {"AXIOM_TRAEFIK_DYNAMIC_DIR": "traefik"},
		"no heartbeat":     {"AXIOM_AGENT_HEARTBEAT_INTERVAL": "0s"},
		"no shutdown":      {"AXIOM_SHUTDOWN_TIMEOUT": "-1s"},
		"bad duration":     {"AXIOM_SHUTDOWN_TIMEOUT": "soon"},
		"bad bool":         {"AXIOM_AGENT_ALLOW_PUBLIC_LISTENER": "yes please"},
		"bad env":          {"AXIOM_ENV": "prod"},
		"relative path":    {"AXIOM_AGENT_OPERATION_PATH": "api/v1/agent/operations"},
		"bad port":         {"AXIOM_AGENT_LISTEN_ADDR": "127.0.0.1:http"},
	}
	for name, override := range cases {
		if _, err := load(t, validEnv(override)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
