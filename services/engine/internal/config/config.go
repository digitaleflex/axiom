// Package config loads and validates Engine configuration from the environment.
// Invalid values are errors: the Engine fails fast instead of silently falling back.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultHost    = "0.0.0.0"
	DefaultPort    = "8080"
	DefaultVersion = "0.1.0-dev"
)

// Environment separates development, test and production behavior.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
	EnvProduction  Environment = "production"
)

type Config struct {
	Env             Environment
	Host            string
	Port            string
	Version         string
	LogLevel        string
	ShutdownTimeout time.Duration
	Database        DatabaseConfig
	// APIToken is the interim bearer token protecting /api/v1 until user
	// authentication (#125) lands. Required in production; never logged.
	APIToken string
}

type DatabaseConfig struct {
	URL             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	Required        bool
}

// Load reads configuration from the process environment and validates it.
func Load() (Config, error) { return LoadFrom(os.LookupEnv) }

// LoadFrom reads configuration through lookup (os.LookupEnv in production, a map in tests).
func LoadFrom(lookup func(string) (string, bool)) (Config, error) {
	r := reader{lookup: lookup}
	cfg := Config{
		Env:             Environment(r.str("AXIOM_ENV", string(EnvDevelopment))),
		Host:            r.str("AXIOM_ENGINE_HOST", DefaultHost),
		Port:            r.str("AXIOM_ENGINE_PORT", DefaultPort),
		Version:         r.str("AXIOM_ENGINE_VERSION", DefaultVersion),
		LogLevel:        strings.ToLower(r.str("AXIOM_LOG_LEVEL", "info")),
		ShutdownTimeout: r.duration("AXIOM_SHUTDOWN_TIMEOUT", 10*time.Second),
		Database: DatabaseConfig{
			URL:             r.str("DATABASE_URL", ""),
			MaxOpenConns:    r.int("AXIOM_DB_MAX_OPEN_CONNS", 10),
			MaxIdleConns:    r.int("AXIOM_DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: r.duration("AXIOM_DB_CONN_MAX_LIFETIME", 30*time.Minute),
		},
	}
	cfg.APIToken = r.str("AXIOM_API_TOKEN", "")
	// Production always requires the database; elsewhere it is opt-in.
	cfg.Database.Required = r.bool("AXIOM_DB_REQUIRED", cfg.Env == EnvProduction)

	if err := errors.Join(append(r.errs, cfg.Validate())...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks semantic rules. Messages never include secret values.
func (c Config) Validate() error {
	var errs []error
	switch c.Env {
	case EnvDevelopment, EnvTest, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("AXIOM_ENV must be development, test or production, got %q", c.Env))
	}
	if p, err := strconv.Atoi(c.Port); err != nil || p < 1 || p > 65535 {
		errs = append(errs, fmt.Errorf("AXIOM_ENGINE_PORT must be an integer between 1 and 65535, got %q", c.Port))
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("AXIOM_LOG_LEVEL must be debug, info, warn or error, got %q", c.LogLevel))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("AXIOM_SHUTDOWN_TIMEOUT must be positive"))
	}
	if c.Database.URL != "" {
		u, err := url.Parse(c.Database.URL)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
			errs = append(errs, errors.New("DATABASE_URL must be a postgres:// URL with a host"))
		}
	}
	if c.Database.Required && c.Database.URL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required (AXIOM_DB_REQUIRED or production environment)"))
	}
	if c.Env == EnvProduction && !c.Database.Required {
		errs = append(errs, errors.New("AXIOM_DB_REQUIRED cannot be false in production"))
	}
	if c.Env == EnvProduction && len(c.APIToken) < 32 {
		errs = append(errs, errors.New("AXIOM_API_TOKEN of at least 32 characters is required in production"))
	}
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns && c.Database.MaxOpenConns > 0 {
		errs = append(errs, errors.New("AXIOM_DB_MAX_IDLE_CONNS cannot exceed AXIOM_DB_MAX_OPEN_CONNS"))
	}
	return errors.Join(errs...)
}

// Addr returns the listen address.
func (c Config) Addr() string { return c.Host + ":" + c.Port }

type reader struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (r *reader) str(key, fallback string) string {
	if v, ok := r.lookup(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func (r *reader) int(key string, fallback int) int {
	raw := r.str(key, "")
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		r.errs = append(r.errs, fmt.Errorf("%s must be a non-negative integer, got %q", key, raw))
		return fallback
	}
	return v
}

func (r *reader) duration(key string, fallback time.Duration) time.Duration {
	raw := r.str(key, "")
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v < 0 {
		r.errs = append(r.errs, fmt.Errorf("%s must be a non-negative duration (e.g. 30s), got %q", key, raw))
		return fallback
	}
	return v
}

func (r *reader) bool(key string, fallback bool) bool {
	raw := r.str(key, "")
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s must be a boolean, got %q", key, raw))
		return fallback
	}
	return v
}
