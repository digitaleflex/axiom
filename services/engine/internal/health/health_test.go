package health

import (
	"strings"
	"testing"
	"time"
)

func TestPolicyEvaluate(t *testing.T) {
	tests := []struct {
		name       string
		policy     Policy
		report     ProbeReport
		wantStatus string
		wantReason string // substring; empty means pass (no reason)
	}{
		// HTTP ranges.
		{"http range lower bound", Policy{Type: TypeHTTP, ExpectedStatus: "200-399"}, ProbeReport{StatusCode: 200}, StatusHealthy, ""},
		{"http range upper bound", Policy{Type: TypeHTTP, ExpectedStatus: "200-399"}, ProbeReport{StatusCode: 399}, StatusHealthy, ""},
		{"http range redirect", Policy{Type: TypeHTTP, ExpectedStatus: "200-399"}, ProbeReport{StatusCode: 301}, StatusHealthy, ""},
		{"http range below", Policy{Type: TypeHTTP, ExpectedStatus: "200-399"}, ProbeReport{StatusCode: 199}, StatusUnhealthy, "199"},
		{"http range above", Policy{Type: TypeHTTP, ExpectedStatus: "200-399"}, ProbeReport{StatusCode: 404}, StatusUnhealthy, "404"},
		{"http range server error", Policy{Type: TypeHTTP, ExpectedStatus: "200-399"}, ProbeReport{StatusCode: 503}, StatusUnhealthy, "503"},
		// HTTP single codes.
		{"http single match", Policy{Type: TypeHTTP, ExpectedStatus: "200"}, ProbeReport{StatusCode: 200}, StatusHealthy, ""},
		{"http single mismatch", Policy{Type: TypeHTTP, ExpectedStatus: "200"}, ProbeReport{StatusCode: 201}, StatusUnhealthy, "201"},
		{"http single created", Policy{Type: TypeHTTP, ExpectedStatus: "201"}, ProbeReport{StatusCode: 201}, StatusHealthy, ""},
		// Timeouts / no response.
		{"http no response", Policy{Type: TypeHTTP, ExpectedStatus: "200-399"}, ProbeReport{StatusCode: 0}, StatusUnhealthy, "no HTTP status"},
		{"http slow but healthy", Policy{Type: TypeHTTP, ExpectedStatus: "200-399", Timeout: time.Millisecond}, ProbeReport{StatusCode: 200, LatencyMs: 5000}, StatusHealthy, ""},
		// TCP.
		{"tcp connected", Policy{Type: TypeTCP}, ProbeReport{StatusCode: 0, LatencyMs: 3}, StatusHealthy, ""},
		{"tcp connect failed", Policy{Type: TypeTCP}, ProbeReport{StatusCode: 1}, StatusUnhealthy, "tcp connect failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, reason := tt.policy.Evaluate(tt.report)
			if status != tt.wantStatus {
				t.Fatalf("status = %s, want %s", status, tt.wantStatus)
			}
			if tt.wantReason == "" {
				if reason != "" {
					t.Fatalf("reason = %q, want empty", reason)
				}
			} else if !strings.Contains(reason, tt.wantReason) {
				t.Fatalf("reason = %q, want substring %q", reason, tt.wantReason)
			}
		})
	}
}

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy("", "", "", 0, 0, 0)
	if p.Type != TypeHTTP {
		t.Fatalf("type = %q, want http", p.Type)
	}
	if p.ExpectedStatus != "200-399" {
		t.Fatalf("expectedStatus = %q, want 200-399", p.ExpectedStatus)
	}
	if p.Timeout != 5*time.Second {
		t.Fatalf("timeout = %s, want 5s", p.Timeout)
	}
	if p.Retries != 3 {
		t.Fatalf("retries = %d, want 3", p.Retries)
	}
	if p.Interval != 2*time.Second {
		t.Fatalf("interval = %s, want 2s", p.Interval)
	}
	// Explicit values are preserved.
	p = DefaultPolicy(TypeTCP, "/healthz", "200", 10*time.Second, 5, time.Second)
	if p.Type != TypeTCP || p.Path != "/healthz" || p.ExpectedStatus != "200" ||
		p.Timeout != 10*time.Second || p.Retries != 5 || p.Interval != time.Second {
		t.Fatalf("explicit policy not preserved: %+v", p)
	}
}

func TestNewProbeReportTruncatesBody(t *testing.T) {
	body := strings.Repeat("x", MaxBodyBytes+100)
	r := NewProbeReport(200, 5, body, 1)
	if len(r.Body) != MaxBodyBytes {
		t.Fatalf("body len = %d, want %d", len(r.Body), MaxBodyBytes)
	}
	if r.StatusCode != 200 || r.LatencyMs != 5 || r.Attempt != 1 || r.CheckedAt.IsZero() {
		t.Fatalf("report = %+v", r)
	}
}
