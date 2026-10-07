package logging

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(buf)
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("invalid JSON log line %q: %v", sc.Text(), err)
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return out
}

func TestLogStartEndOutputShape(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	ctx := WithRequestID(context.Background(), "req_42")
	ctx = WithCorrelationID(ctx, "req_42")
	ctx = WithDeploymentID(ctx, "dep_42")

	start := LogStart(ctx, log, "BUILD", "step", "BUILD")
	// Ensure a measurable (possibly zero) duration.
	LogEnd(ctx, log, "BUILD", start, "ok", "step", "BUILD")

	recs := decodeLines(t, &buf)
	if len(recs) != 2 {
		t.Fatalf("got %d log lines, want 2: %v", len(recs), recs)
	}

	startRec, endRec := recs[0], recs[1]
	if startRec["msg"] != "BUILD start" {
		t.Errorf("start msg = %v", startRec["msg"])
	}
	if endRec["msg"] != "BUILD end" {
		t.Errorf("end msg = %v", endRec["msg"])
	}
	for _, rec := range recs {
		if rec[FieldOperation] != "BUILD" {
			t.Errorf("operation = %v, want BUILD", rec[FieldOperation])
		}
		if rec[FieldRequestID] != "req_42" || rec[FieldCorrelationID] != "req_42" || rec[FieldDeploymentID] != "dep_42" {
			t.Errorf("correlation attrs missing on %v", rec)
		}
		if rec["step"] != "BUILD" {
			t.Errorf("extra attr step missing on %v", rec)
		}
	}

	if endRec[FieldResult] != "ok" {
		t.Errorf("result = %v, want ok", endRec[FieldResult])
	}
	d, ok := endRec[FieldDurationMs].(float64)
	if !ok {
		t.Fatalf("durationMs missing or not numeric: %v", endRec[FieldDurationMs])
	}
	if d < 0 {
		t.Errorf("durationMs = %v, want >= 0", d)
	}
	if endRec["level"] != "INFO" {
		t.Errorf("end level = %v, want INFO", endRec["level"])
	}
}

func TestLogEndErrorResultIsErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	start := time.Now().Add(-5 * time.Millisecond)

	LogEnd(context.Background(), log, "VERIFY", start, "HEALTH_CHECK_FAILED")

	recs := decodeLines(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("got %d lines, want 1", len(recs))
	}
	if recs[0][FieldResult] != "HEALTH_CHECK_FAILED" {
		t.Errorf("result = %v", recs[0][FieldResult])
	}
	if recs[0]["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", recs[0]["level"])
	}
	if recs[0][FieldDurationMs].(float64) < 5 {
		t.Errorf("durationMs = %v, want >= 5", recs[0][FieldDurationMs])
	}
}

func TestLogEndEmptyResultNormalizesToOk(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	LogEnd(context.Background(), log, "START", time.Now(), "")

	recs := decodeLines(t, &buf)
	if recs[0][FieldResult] != "ok" || recs[0]["level"] != "INFO" {
		t.Fatalf("empty result not normalized: %v", recs[0])
	}
}

func TestOperationHelpersNilLoggerAndContext(t *testing.T) {
	// Must not panic.
	start := LogStart(nil, nil, "OP")
	LogEnd(nil, nil, "OP", start, "ok")
}
