package database

import (
	"context"
	"testing"
	"time"
)

func TestOpenRequiresURL(t *testing.T) {
	_, err := Open(context.Background(), "postgres", Config{})
	if err == nil {
		t.Fatal("expected missing database URL error")
	}
}

func TestConfigAcceptsPoolSettings(t *testing.T) {
	cfg := Config{
		URL:             "postgres://example",
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Minute,
	}
	if cfg.MaxOpenConns != 10 || cfg.MaxIdleConns != 5 || cfg.ConnMaxLifetime != time.Minute {
		t.Fatal("unexpected database pool configuration")
	}
}
