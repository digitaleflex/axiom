package logging

import (
	"context"
	"log/slog"
	"time"
)

// LogStart logs the start of a named operation at INFO and returns the start
// time to pass to LogEnd. Context correlation attrs (requestId, correlationId,
// deploymentId) and any extra attrs are included.
//
// Use it for operations that span a non-trivial duration — executor steps,
// builds, Agent dispatches, long repository or analysis jobs — so that start
// and end lines join on the "operation" field.
func LogStart(ctx context.Context, log *slog.Logger, op string, attrs ...any) time.Time {
	start := time.Now()
	args := make([]any, 0, 2+len(attrs))
	args = append(args, FieldOperation, op)
	args = append(args, Attrs(ctx)...)
	args = append(args, attrs...)
	logger(log).Info(op+" start", args...)
	return start
}

// LogEnd logs the completion of an operation started by LogStart. It emits the
// elapsed durationMs and the result code, at INFO when result is "ok" or empty
// and at ERROR otherwise. Context correlation attrs and extra attrs are
// included; the "operation" and "durationMs" fields make the pair queryable.
func LogEnd(ctx context.Context, log *slog.Logger, op string, start time.Time, result string, attrs ...any) {
	if result == "" {
		result = "ok"
	}
	level := slog.LevelInfo
	if result != "ok" {
		level = slog.LevelError
	}
	args := make([]any, 0, 6+len(attrs))
	args = append(args, FieldOperation, op, FieldDurationMs, time.Since(start).Milliseconds(), FieldResult, result)
	args = append(args, Attrs(ctx)...)
	args = append(args, attrs...)
	logger(log).Log(ctx, level, op+" end", args...)
}

func logger(log *slog.Logger) *slog.Logger {
	if log != nil {
		return log
	}
	return slog.Default()
}
