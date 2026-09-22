package config

import "os"

type Config struct {
	ServerID  string
	EngineURL string
	Version   string
	Token     string
}

func Load() Config {
	return Config{
		ServerID:  os.Getenv("AXIOM_SERVER_ID"),
		EngineURL: os.Getenv("AXIOM_ENGINE_URL"),
		Version:   env("AXIOM_AGENT_VERSION", "0.1.0"),
		Token:     os.Getenv("AXIOM_AGENT_TOKEN"),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
