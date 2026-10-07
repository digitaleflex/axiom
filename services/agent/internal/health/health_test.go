package health

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a deterministic Clock/Sleep pair: Sleep advances the clock by
// the requested duration, so grace/retry behaviour can be tested without
// waiting.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
	return nil
}

func TestCheckFlakyThenHealthy(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewChecker()
	rep, err := c.Check(context.Background(), Spec{
		URL:      srv.URL,
		Retries:  3,
		Interval: time.Millisecond,
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.StatusCode != 200 || rep.Attempt != 3 {
		t.Fatalf("report = %+v, want status 200 attempt 3", rep)
	}
	if rep.Body != "ok" {
		t.Fatalf("body = %q, want ok", rep.Body)
	}
	if rep.CheckedAt.IsZero() {
		t.Fatal("checkedAt must be set")
	}
}

func TestCheckAlwaysUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewChecker()
	rep, err := c.Check(context.Background(), Spec{
		URL:      srv.URL,
		Retries:  2,
		Interval: time.Millisecond,
		Timeout:  time.Second,
	})
	var ue *UnhealthyError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v, want *UnhealthyError", err)
	}
	if !errors.Is(err, ErrUnhealthy) {
		t.Fatal("err must match ErrUnhealthy")
	}
	if ue.StatusCode != 503 || ue.Attempt != 3 {
		t.Fatalf("unhealthy = %+v, want status 503 attempt 3", ue)
	}
	if rep.Attempt != 3 || rep.StatusCode != 503 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestCheckRetryCounting(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewChecker()
	_, err := c.Check(context.Background(), Spec{
		URL:      srv.URL,
		Retries:  4,
		Interval: time.Millisecond,
		Timeout:  time.Second,
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if got := atomic.LoadInt32(&calls); got != 5 {
		t.Fatalf("probes = %d, want 5 (1 + 4 retries)", got)
	}
}

func TestCheckSlowBeyondTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewChecker()
	start := time.Now()
	_, err := c.Check(context.Background(), Spec{
		URL:      srv.URL,
		Retries:  0,
		Timeout:  30 * time.Millisecond,
		Interval: time.Millisecond,
	})
	elapsed := time.Since(start)
	if !errors.Is(err, ErrNoResponse) {
		t.Fatalf("err = %v, want ErrNoResponse", err)
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("check took %v; each attempt must be bounded by Timeout", elapsed)
	}
}

func TestCheckExpectedStatusRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent) // 204
	}))
	defer srv.Close()

	c := NewChecker()
	if _, err := c.Check(context.Background(), Spec{URL: srv.URL, ExpectedStatus: "200-299", Timeout: time.Second}); err != nil {
		t.Fatalf("204 within 200-299 must pass: %v", err)
	}
	_, err := c.Check(context.Background(), Spec{URL: srv.URL, ExpectedStatus: "200", Timeout: time.Second})
	if !errors.Is(err, ErrUnhealthy) {
		t.Fatalf("204 outside single 200 must fail: %v", err)
	}
}

func TestCheckGracePeriodSkipsEarlyFailures(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	clock := newFakeClock()
	c := &Checker{Clock: clock.Now, Sleep: clock.Sleep}
	// Retries=0 would fail after one attempt without grace; the grace window
	// (30s, advanced 10s per interval) skips the first three failures.
	rep, err := c.Check(context.Background(), Spec{
		URL:         srv.URL,
		Retries:     0,
		Interval:    10 * time.Second,
		GracePeriod: 30 * time.Second,
		Timeout:     time.Second,
	})
	if err != nil {
		t.Fatalf("grace period must tolerate early failures: %v", err)
	}
	if rep.Attempt != 4 || rep.StatusCode != 200 {
		t.Fatalf("report = %+v, want attempt 4 status 200", rep)
	}

	// Control: without grace the same server fails on the first attempt.
	var calls2 int32
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls2, 1) <= 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv2.Close()
	_, err = c.Check(context.Background(), Spec{URL: srv2.URL, Retries: 0, Interval: time.Second, Timeout: time.Second})
	if !errors.Is(err, ErrUnhealthy) {
		t.Fatalf("without grace, err = %v, want ErrUnhealthy", err)
	}
}

func TestCheckContextCancelMidPoll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewChecker()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err := c.Check(ctx, Spec{
		URL:      srv.URL,
		Retries:  1000,
		Interval: 50 * time.Millisecond,
		Timeout:  time.Second,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestCheckTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	c := NewChecker()
	rep, err := c.Check(context.Background(), Spec{
		Type: TypeTCP, Host: "127.0.0.1", Port: port, Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("tcp check: %v", err)
	}
	if rep.Attempt != 1 {
		t.Fatalf("attempt = %d", rep.Attempt)
	}
}

func TestCheckTCPRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	ln.Close() // now refused

	c := NewChecker()
	_, err = c.Check(context.Background(), Spec{
		Type: TypeTCP, Host: "127.0.0.1", Port: port, Timeout: 500 * time.Millisecond,
	})
	if !errors.Is(err, ErrNoResponse) {
		t.Fatalf("err = %v, want ErrNoResponse", err)
	}
}

func TestCheckInvalidSpec(t *testing.T) {
	c := NewChecker()
	if _, err := c.Check(context.Background(), Spec{Type: "udp"}); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("err = %v, want ErrInvalidSpec", err)
	}
	if _, err := c.Check(context.Background(), Spec{Type: TypeTCP, Host: "h", Port: 0}); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("err = %v, want ErrInvalidSpec", err)
	}
	if _, err := c.Check(context.Background(), Spec{Type: TypeHTTP}); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("err = %v, want ErrInvalidSpec", err)
	}
	if _, err := c.Check(context.Background(), Spec{URL: "http://x", ExpectedStatus: "abc"}); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("err = %v, want ErrInvalidSpec", err)
	}
}

func TestReportToProtocol(t *testing.T) {
	rep := Report{StatusCode: 200, LatencyMs: 84, Attempt: 2}
	got := rep.ToProtocol()
	if got.StatusCode != 200 || got.LatencyMs != 84 || got.Attempt != 2 {
		t.Fatalf("ToProtocol = %+v", got)
	}
}
