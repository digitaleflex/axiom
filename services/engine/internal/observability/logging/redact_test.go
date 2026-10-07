package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestRedactWrapsLogsRedaction(t *testing.T) {
	cases := []struct{ in, secret string }{
		{"token=supersecrettoken", "supersecrettoken"},
		{"Authorization: Bearer abc.def.ghi", "abc.def.ghi"},
		{"clone https://user:pass@github.com/acme/web.git", "pass"},
		{`{"password":"hunter2"}`, "hunter2"},
	}
	for _, tc := range cases {
		out := Redact(tc.in)
		if strings.Contains(out, tc.secret) {
			t.Errorf("Redact(%q) leaked %q: %q", tc.in, tc.secret, out)
		}
		if !strings.Contains(out, "[REDACTED]") {
			t.Errorf("Redact(%q) = %q, want marker", tc.in, out)
		}
	}
}

func TestRedactError(t *testing.T) {
	if got := RedactError(nil); got != "" {
		t.Fatalf("RedactError(nil) = %q", got)
	}
	got := RedactError(errors.New("connecting with password=hunter2"))
	if strings.Contains(got, "hunter2") {
		t.Fatalf("RedactError leaked secret: %q", got)
	}
}

// TestNoSecretReachesOutput proves the redacting handler scrubs the message,
// string attrs, nested groups and error values before they reach the sink.
func TestNoSecretReachesOutput(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewRedactingHandler(slog.NewJSONHandler(&buf, nil)))

	log.Error("auth failed token=msgsecret",
		"password", "attrsecret",
		"err", errors.New("Authorization: Bearer errsecret"),
		slog.Group("conn", "url", "https://user:passsecret@host/x"),
	)

	out := buf.String()
	for _, secret := range []string{"msgsecret", "attrsecret", "errsecret", "passsecret"} {
		if strings.Contains(out, secret) {
			t.Errorf("secret %q leaked into log output: %s", secret, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected redaction marker in output: %s", out)
	}
	if !strings.Contains(out, `"msg":"auth failed token=[REDACTED]"`) {
		t.Errorf("message not redacted: %s", out)
	}
}

func TestRedactingHandlerPreservesStructureAndLevel(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewRedactingHandler(slog.NewJSONHandler(&buf, nil)))
	log.Warn("clean message", "container", "axiom-web", "port", 8080)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if rec["msg"] != "clean message" || rec["level"] != "WARN" {
		t.Errorf("structure changed: %v", rec)
	}
	if rec["container"] != "axiom-web" {
		t.Errorf("non-secret string attr altered: %v", rec["container"])
	}
	if rec["port"].(float64) != 8080 {
		t.Errorf("non-string attr altered: %v", rec["port"])
	}
}

func TestRedactingHandlerWithAttrsAndGroup(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(NewRedactingHandler(slog.NewJSONHandler(&buf, nil)))
	log := base.With("apiToken", "withsecret").WithGroup("g").With("secret", "groupsecret")
	log.Info("ok")

	out := buf.String()
	for _, secret := range []string{"withsecret", "groupsecret"} {
		if strings.Contains(out, secret) {
			t.Errorf("secret %q leaked: %s", secret, out)
		}
	}
}

func TestSensitiveKey(t *testing.T) {
	sensitive := []string{"password", "Password", "apiToken", "api_token", "github-token",
		"clientSecret", "apiKey", "authorization", "private_key", "bootstrap_credential"}
	for _, k := range sensitive {
		if !sensitiveKey(k) {
			t.Errorf("sensitiveKey(%q) = false, want true", k)
		}
	}
	benign := []string{"", "tokenCount", "secretary", "component", "deploymentId",
		"requestId", "correlationId", "port", "errorCode", "url"}
	for _, k := range benign {
		if sensitiveKey(k) {
			t.Errorf("sensitiveKey(%q) = true, want false", k)
		}
	}
}

func TestRedactingHandlerNilFallsBack(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	NewRedactingHandler(nil).Handle(context.Background(), slog.NewRecord(
		time.Now(), slog.LevelInfo, "token=fallbacksecret", 0))
	if strings.Contains(buf.String(), "fallbacksecret") {
		t.Fatalf("nil handler fallback did not redact: %s", buf.String())
	}
}
