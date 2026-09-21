package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Config contains PostgreSQL connection settings for the Engine.
type Config struct {
	URL             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// Open creates a PostgreSQL connection pool using the standard database/sql API.
// The pqx-compatible PostgreSQL driver is injected by the application package.
func Open(ctx context.Context, driverName string, cfg Config) (*sql.DB, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	db, err := sql.Open(driverName, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}
