// Package logger builds the Engine's structured JSON logger (ADR-0006).
package logger

import (
	"io"
	"log/slog"
	"os"
)

// New returns a JSON logger at the given level ("debug", "info", "warn", "error").
func New(level string) *slog.Logger { return NewWriter(os.Stdout, level) }

// NewWriter is New with an explicit destination (tests).
func NewWriter(w io.Writer, level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: l})).With("service", "axiom-engine")
}
