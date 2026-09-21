package config

import "os"

const (
	DefaultHost    = "0.0.0.0"
	DefaultPort    = "8080"
	DefaultVersion = "0.1.0-dev"
)

type Config struct {
	Host    string
	Port    string
	Version string
}

func Load() Config {
	return Config{
		Host:    envOrDefault("AXIOM_ENGINE_HOST", DefaultHost),
		Port:    envOrDefault("AXIOM_ENGINE_PORT", DefaultPort),
		Version: envOrDefault("AXIOM_ENGINE_VERSION", DefaultVersion),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
