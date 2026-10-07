package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestLevelMapping(t *testing.T) {
	cases := map[string]slog.Level{
		"DEBUG":    slog.LevelDebug,
		"debug":    slog.LevelDebug,
		" INFO ":   slog.LevelInfo,
		"WARN":     slog.LevelWarn,
		"warning":  slog.LevelWarn,
		"ERROR":    slog.LevelError,
		"":         slog.LevelInfo,
		"nonsense": slog.LevelInfo,
		"Info":     slog.LevelInfo,
	}
	for name, want := range cases {
		if got := Level(name); got != want {
			t.Errorf("Level(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestLevelName(t *testing.T) {
	cases := map[slog.Level]string{
		slog.LevelDebug:     "DEBUG",
		slog.LevelInfo:      "INFO",
		slog.LevelWarn:      "WARN",
		slog.LevelError:     "ERROR",
		slog.LevelError + 4: "ERROR",
		slog.LevelDebug - 4: "DEBUG",
	}
	for level, want := range cases {
		if got := LevelName(level); got != want {
			t.Errorf("LevelName(%v) = %q, want %q", level, got, want)
		}
	}
}

func TestNewLoggerWithPrefillsServiceAndComponent(t *testing.T) {
	var buf bytes.Buffer
	log := NewLoggerWith(slog.NewJSONHandler(&buf, nil), "axiom-engine", "executor")
	log.Info("hello")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("invalid JSON log: %v (%s)", err, buf.String())
	}
	if rec[FieldService] != "axiom-engine" || rec[FieldComponent] != "executor" {
		t.Fatalf("service/component missing: %v", rec)
	}
}

func TestNewLoggerInheritsDefaultHandler(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	NewLogger("axiom-engine", "api").Info("inherited")
	if !strings.Contains(buf.String(), `"component":"api"`) {
		t.Fatalf("NewLogger did not inherit default handler: %s", buf.String())
	}
}

func TestWithPrefillsContextAttrs(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	ctx := WithRequestID(context.Background(), "req_1")
	ctx = WithCorrelationID(ctx, "req_1")
	ctx = WithDeploymentID(ctx, "dep_1")

	With(log, ctx).Info("scoped")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("invalid JSON log: %v", err)
	}
	for k, want := range map[string]string{
		FieldRequestID: "req_1", FieldCorrelationID: "req_1", FieldDeploymentID: "dep_1",
	} {
		if rec[k] != want {
			t.Errorf("%s = %v, want %v", k, rec[k], want)
		}
	}
}

func TestWithNilLoggerFallsBack(t *testing.T) {
	// Must not panic.
	if got := With(nil, context.Background()); got == nil {
		t.Fatal("With(nil, ctx) returned nil")
	}
}
