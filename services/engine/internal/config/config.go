package config

import (
	"os"
	"strconv"
	"time"
)

const (
	DefaultHost    = "0.0.0.0"
	DefaultPort    = "8080"
	DefaultVersion = "0.1.0-dev"
)

type Config struct {
	Host     string
	Port     string
	Version  string
	Database DatabaseConfig
}

type DatabaseConfig struct {
	URL             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	Required        bool
}

func Load() Config {
	return Config{
		Host:    envOrDefault("AXIOM_ENGINE_HOST", DefaultHost),
		Port:    envOrDefault("AXIOM_ENGINE_PORT", DefaultPort),
		Version: envOrDefault("AXIOM_ENGINE_VERSION", DefaultVersion),
		Database: DatabaseConfig{
			URL:             os.Getenv("DATABASE_URL"),
			MaxOpenConns:    envOrDefaultInt("AXIOM_DB_MAX_OPEN_CONNS", 10),
			MaxIdleConns:    envOrDefaultInt("AXIOM_DB_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: envOrDefaultDuration("AXIOM_DB_CONN_MAX_LIFETIME", 30*time.Minute),
			Required:        envOrDefaultBool("AXIOM_DB_REQUIRED", false),
		},
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func envOrDefaultDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(key))
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func envOrDefaultBool(key string, fallback bool) bool {
	value, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return value
}
