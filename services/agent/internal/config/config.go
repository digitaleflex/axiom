// Package config loads and validates the Runtime Agent configuration from the
// environment. Invalid values are errors: the agent fails fast instead of
// silently falling back.
//
// The style mirrors the Engine configuration (services/engine/internal/config):
// Load reads os.LookupEnv, LoadFrom takes an injectable lookup so tests never
// touch the process environment, and Validate collects every problem into a
// single joined error whose messages never contain a secret value.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/agent/internal/security/transport"
)

// Defaults.
const (
	DefaultVersion         = "0.1.0"
	DefaultDataRoot        = "/var/lib/axiom-agent"
	DefaultShutdownTimeout = 10 * time.Second
	DefaultHeartbeat       = 30 * time.Second
	DefaultDockerBinary    = "docker"
	DefaultTraefikDynamic  = "/etc/traefik/dynamic"
	// DefaultListenAddr binds the loopback interface only: the agent has no TLS
	// (#88), so its inbound operation listener is never exposed by accident.
	DefaultListenAddr = "127.0.0.1:9401"
)

// Environment separates development, test and production behavior.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
	EnvProduction  Environment = "production"
)

// Config is the fully resolved agent configuration.
type Config struct {
	Env Environment
	// ServerID is the server this agent serves (identity binding).
	ServerID string
	// EngineURL is the Engine base URL the agent dials (register, heartbeat,
	// rotate).
	EngineURL string
	// Version is the agent version reported at registration.
	Version string
	// Token is the one-time bootstrap credential presented at registration
	// (#76). It is never logged and never persisted by this package.
	Token string
	// DataRoot holds the identity, credential and operation state files.
	DataRoot string
	// ShutdownTimeout bounds graceful shutdown (in-flight operations drain).
	ShutdownTimeout time.Duration
	// HeartbeatInterval is the fallback liveness period; the registration
	// response may negotiate a different one.
	HeartbeatInterval time.Duration
	// Docker locates the container CLI the runtime adapter invokes.
	Docker DockerConfig
	// Traefik locates the Traefik file-provider directory the network adapter
	// owns.
	Traefik TraefikConfig
	// Listener is the inbound operation listener (the Engine's only way in).
	Listener ListenerConfig
}

// DockerConfig locates the container CLI. Every value is passed as argv and
// never through a shell.
type DockerConfig struct {
	Binary string
}

// TraefikConfig points the network adapter at the Traefik file-provider
// directory. The static Traefik configuration is operator-managed and lives
// outside it.
type TraefikConfig struct {
	DynamicDir string
}

// ListenerConfig is the inbound HTTP listener that receives protocol.Operation
// messages from the Engine (agentclient.DefaultOperationPath).
//
// Security note: the agent module has no TLS (#88) and no Engine→Agent
// credential (#77 / ADR-0008), so the listener defaults to the loopback
// interface and requires an explicit opt-in for any other bind address. The
// composition root additionally refuses unauthenticated operations, so binding
// more widely does not by itself make the listener useful — it only exposes the
// 401 surface.
type ListenerConfig struct {
	// Addr is the host:port to bind.
	Addr string
	// AllowPublic permits a non-loopback bind address. It is a separate,
	// explicit switch so the choice is visible in configuration and logs.
	AllowPublic bool
	// Path is the operation endpoint path. It must stay in step with the
	// Engine's agentclient.DefaultOperationPath until ADR-0008 fixes a
	// transport.
	Path string
}

// Load reads configuration from the process environment and validates it.
func Load() (Config, error) { return LoadFrom(os.LookupEnv) }

// LoadFrom reads configuration through lookup (os.LookupEnv in production, a
// map in tests) and validates the result.
func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	r := reader{lookup: lookup}
	cfg := Config{
		Env:               Environment(r.str("AXIOM_ENV", string(EnvDevelopment))),
		ServerID:          r.str("AXIOM_SERVER_ID", ""),
		EngineURL:         strings.TrimRight(r.str("AXIOM_ENGINE_URL", ""), "/"),
		Version:           r.str("AXIOM_AGENT_VERSION", DefaultVersion),
		Token:             r.str("AXIOM_AGENT_TOKEN", ""),
		DataRoot:          r.str("AXIOM_AGENT_DATA_DIR", DefaultDataRoot),
		ShutdownTimeout:   r.duration("AXIOM_SHUTDOWN_TIMEOUT", DefaultShutdownTimeout),
		HeartbeatInterval: r.duration("AXIOM_AGENT_HEARTBEAT_INTERVAL", DefaultHeartbeat),
		Docker:            DockerConfig{Binary: r.str("AXIOM_DOCKER_BINARY", DefaultDockerBinary)},
		Traefik:           TraefikConfig{DynamicDir: r.str("AXIOM_TRAEFIK_DYNAMIC_DIR", DefaultTraefikDynamic)},
		Listener: ListenerConfig{
			Addr:        r.str("AXIOM_AGENT_LISTEN_ADDR", DefaultListenAddr),
			AllowPublic: r.bool("AXIOM_AGENT_ALLOW_PUBLIC_LISTENER", false),
			Path:        r.str("AXIOM_AGENT_OPERATION_PATH", "/api/v1/agent/operations"),
		},
	}
	if err := errors.Join(append(r.errs, cfg.Validate())...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks the semantic rules. Messages never include secret values.
func (c Config) Validate() error {
	var errs []error
	switch c.Env {
	case EnvDevelopment, EnvTest, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("AXIOM_ENV must be development, test or production, got %q", c.Env))
	}
	if strings.TrimSpace(c.ServerID) == "" {
		errs = append(errs, errors.New("AXIOM_SERVER_ID is required"))
	}
	if strings.TrimSpace(c.EngineURL) == "" {
		errs = append(errs, errors.New("AXIOM_ENGINE_URL is required"))
	} else {
		errs = append(errs, c.validateEngineURL())
	}
	if strings.TrimSpace(c.Version) == "" {
		errs = append(errs, errors.New("AXIOM_AGENT_VERSION must not be empty"))
	}
	if strings.TrimSpace(c.DataRoot) == "" || !filepath.IsAbs(c.DataRoot) {
		errs = append(errs, fmt.Errorf("AXIOM_AGENT_DATA_DIR must be an absolute path, got %q", c.DataRoot))
	}
	if strings.TrimSpace(c.Docker.Binary) == "" {
		errs = append(errs, errors.New("AXIOM_DOCKER_BINARY must not be empty"))
	}
	if strings.TrimSpace(c.Traefik.DynamicDir) == "" || !filepath.IsAbs(c.Traefik.DynamicDir) {
		errs = append(errs, fmt.Errorf("AXIOM_TRAEFIK_DYNAMIC_DIR must be an absolute path, got %q", c.Traefik.DynamicDir))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("AXIOM_SHUTDOWN_TIMEOUT must be positive"))
	}
	if c.HeartbeatInterval <= 0 {
		errs = append(errs, errors.New("AXIOM_AGENT_HEARTBEAT_INTERVAL must be positive"))
	}
	if !strings.HasPrefix(c.Listener.Path, "/") {
		errs = append(errs, fmt.Errorf("AXIOM_AGENT_OPERATION_PATH must start with '/', got %q", c.Listener.Path))
	}
	errs = append(errs, c.Listener.validateAddr())
	return errors.Join(errs...)
}

// validateEngineURL reuses the transport hardening package (#88): https is
// required in production, http only for loopback in development, and the host
// must be the single allow-listed Engine host taken from the URL itself.
func (c Config) validateEngineURL() error {
	u, err := url.Parse(c.EngineURL)
	if err != nil {
		return fmt.Errorf("AXIOM_ENGINE_URL is not a valid URL")
	}
	if err := transport.RequireEngineEndpoint(c.EngineURL, []string{u.Host}, c.Env == EnvProduction); err != nil {
		return fmt.Errorf("AXIOM_ENGINE_URL: %w", err)
	}
	return nil
}

// validateAddr fails closed on a non-loopback bind address unless the operator
// opted in explicitly. The agent has no TLS (#88), so an accidental 0.0.0.0
// would expose the operation listener in plaintext.
func (l ListenerConfig) validateAddr() error {
	host, port, err := net.SplitHostPort(l.Addr)
	if err != nil {
		return fmt.Errorf("AXIOM_AGENT_LISTEN_ADDR must be host:port, got %q", l.Addr)
	}
	p, err := strconv.Atoi(port)
	// Port 0 is allowed: it asks the kernel for an ephemeral port, which is
	// what tests and socket-activated deployments use.
	if err != nil || p < 0 || p > 65535 {
		return fmt.Errorf("AXIOM_AGENT_LISTEN_ADDR has an invalid port %q", port)
	}
	if host == "" {
		if l.AllowPublic {
			return nil
		}
		return fmt.Errorf("AXIOM_AGENT_LISTEN_ADDR %q binds every interface; set AXIOM_AGENT_ALLOW_PUBLIC_LISTENER=true to allow it", l.Addr)
	}
	if isLoopbackHost(host) {
		return nil
	}
	if l.AllowPublic {
		return nil
	}
	return fmt.Errorf("AXIOM_AGENT_LISTEN_ADDR %q is not a loopback address; set AXIOM_AGENT_ALLOW_PUBLIC_LISTENER=true to allow it", l.Addr)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// IdentityPath is the persisted agent identity file.
func (c Config) IdentityPath() string { return filepath.Join(c.DataRoot, "identity.json") }

// CredentialPath is the persisted agent credential file.
func (c Config) CredentialPath() string { return filepath.Join(c.DataRoot, "credential.json") }

// StatePath is the durable operation state log.
func (c Config) StatePath() string { return filepath.Join(c.DataRoot, "state.jsonl") }

// Production reports whether the agent runs in production.
func (c Config) Production() bool { return c.Env == EnvProduction }

// reader reads typed values from an injectable lookup and accumulates parsing
// errors, so one bad variable never hides the next.
type reader struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (r *reader) raw(key string) string {
	v, _ := r.lookup(key)
	return strings.TrimSpace(v)
}

func (r *reader) str(key, fallback string) string {
	if v := r.raw(key); v != "" {
		return v
	}
	return fallback
}

func (r *reader) bool(key string, fallback bool) bool {
	v := r.raw(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s must be a boolean, got %q", key, v))
		return fallback
	}
	return b
}

func (r *reader) duration(key string, fallback time.Duration) time.Duration {
	v := r.raw(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s must be a duration such as 30s, got %q", key, v))
		return fallback
	}
	return d
}
