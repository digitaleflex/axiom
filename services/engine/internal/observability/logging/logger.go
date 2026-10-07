package logging

import (
	"context"
	"log/slog"
	"strings"
)

// Level maps the canonical level names (DEBUG/INFO/WARN/ERROR) used by
// configuration, environment variables and this documentation to slog levels.
// "WARNING" is accepted as an alias of WARN; unknown names default to INFO.
func Level(name string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// LevelName is the inverse of Level, used for log lines and documentation.
func LevelName(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "DEBUG"
	case l < slog.LevelWarn:
		return "INFO"
	case l < slog.LevelError:
		return "WARN"
	default:
		return "ERROR"
	}
}

// NewLogger returns a logger pre-filled with the service and component fields,
// inheriting the handler of slog.Default(). Call it once per component at the
// composition root and pass the result down.
func NewLogger(service, component string) *slog.Logger {
	return NewLoggerWith(slog.Default().Handler(), service, component)
}

// NewLoggerWith is NewLogger with an explicit handler, for tests and custom
// sinks. A nil handler falls back to slog.Default()'s handler.
func NewLoggerWith(h slog.Handler, service, component string) *slog.Logger {
	if h == nil {
		h = slog.Default().Handler()
	}
	return slog.New(h).With(
		slog.String(FieldService, service),
		slog.String(FieldComponent, component),
	)
}

// With returns log pre-filled with the correlation attrs from ctx. It is the
// middleware-agnostic bridge between a context and a logger: callers that
// already have both avoid repeating requestId/correlationId/deploymentId at
// every call site.
func With(log *slog.Logger, ctx context.Context) *slog.Logger {
	if log == nil {
		log = slog.Default()
	}
	return log.With(Attrs(ctx)...)
}
