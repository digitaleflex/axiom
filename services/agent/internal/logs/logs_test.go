package logs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDocker is a scripted Docker source: each call returns the next response
// (the last is repeated once exhausted).
type fakeDocker struct {
	mu        sync.Mutex
	outputs   []string
	calls     int
	tails     []int
	times     []bool
	container []string
}

func (d *fakeDocker) Logs(_ context.Context, container string, tail int, timestamps bool) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tails = append(d.tails, tail)
	d.times = append(d.times, timestamps)
	d.container = append(d.container, container)
	i := d.calls
	d.calls++
	if len(d.outputs) == 0 {
		return "", nil
	}
	if i >= len(d.outputs) {
		i = len(d.outputs) - 1
	}
	return d.outputs[i], nil
}

func (d *fakeDocker) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func TestFetchParsesTimestamps(t *testing.T) {
	d := &fakeDocker{outputs: []string{
		"2026-10-07T12:00:00.000000001Z hello\n" +
			"2026-10-07T12:00:01.500000000Z world\n",
	}}
	f := &Fetcher{Docker: d}
	entries, err := f.Fetch(context.Background(), "axiom-app-1", FetchOptions{Lines: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Message != "hello" || entries[1].Message != "world" {
		t.Fatalf("messages = %+v", entries)
	}
	if entries[0].Stream != StreamStdout {
		t.Fatalf("stream = %q", entries[0].Stream)
	}
	if entries[0].Timestamp.UTC().Format(time.RFC3339Nano) != "2026-10-07T12:00:00.000000001Z" {
		t.Fatalf("timestamp = %v", entries[0].Timestamp)
	}
	if d.tails[0] != 50 || !d.times[0] || d.container[0] != "axiom-app-1" {
		t.Fatalf("docker call = tail %d timestamps %v container %q", d.tails[0], d.times[0], d.container[0])
	}
}

func TestFetchUntimedLine(t *testing.T) {
	d := &fakeDocker{outputs: []string{"no timestamp here\n"}}
	f := &Fetcher{Docker: d}
	entries, err := f.Fetch(context.Background(), "axiom-app-1", FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].Timestamp.IsZero() || entries[0].Message != "no timestamp here" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestFetchSearchAndSince(t *testing.T) {
	d := &fakeDocker{outputs: []string{
		"2026-10-07T12:00:00Z boot ok\n" +
			"2026-10-07T12:00:05Z request ERROR handled\n" +
			"2026-10-07T12:00:10Z request ok\n",
	}}
	f := &Fetcher{Docker: d}
	entries, err := f.Fetch(context.Background(), "axiom-app-1", FetchOptions{Search: "request"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || strings.Contains(entries[0].Message, "boot") {
		t.Fatalf("search entries = %+v", entries)
	}
	since := time.Date(2026, 10, 7, 12, 0, 6, 0, time.UTC)
	entries, err = f.Fetch(context.Background(), "axiom-app-1", FetchOptions{Since: since})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Message != "request ok" {
		t.Fatalf("since entries = %+v", entries)
	}
}

func TestFetchRedacts(t *testing.T) {
	d := &fakeDocker{outputs: []string{
		"2026-10-07T12:00:00Z connecting password=s3cret done\n",
	}}
	f := &Fetcher{Docker: d}
	entries, err := f.Fetch(context.Background(), "axiom-app-1", FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Message != "connecting password=[REDACTED] done" {
		t.Fatalf("message = %q", entries[0].Message)
	}
}

func TestFetchLinesBounds(t *testing.T) {
	d := &fakeDocker{outputs: []string{"x\n"}}
	f := &Fetcher{Docker: d}
	_, _ = f.Fetch(context.Background(), "axiom-app-1", FetchOptions{Lines: 0})
	_, _ = f.Fetch(context.Background(), "axiom-app-1", FetchOptions{Lines: 10 * MaxLines})
	if d.tails[0] != DefaultLines || d.tails[1] != MaxLines {
		t.Fatalf("tails = %v", d.tails)
	}
}

func TestFetchNoDocker(t *testing.T) {
	f := &Fetcher{}
	if _, err := f.Fetch(context.Background(), "axiom-app-1", FetchOptions{}); !errors.Is(err, ErrNoDocker) {
		t.Fatalf("err = %v, want ErrNoDocker", err)
	}
}

func TestStreamFollowEmitsNewLinesThenCloses(t *testing.T) {
	d := &fakeDocker{outputs: []string{
		"2026-10-07T12:00:00Z line1\n",
		"2026-10-07T12:00:00Z line1\n2026-10-07T12:00:01Z line2\n",
		"2026-10-07T12:00:00Z line1\n2026-10-07T12:00:01Z line2\n2026-10-07T12:00:02Z line3\n",
	}}
	f := &Fetcher{Docker: d, PollInterval: 2 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	ch := f.Stream(ctx, "axiom-app-1", true)

	var msgs []string
	deadline := time.After(3 * time.Second)
	for len(msgs) < 3 {
		select {
		case e, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed early after %v", msgs)
			}
			msgs = append(msgs, e.Message)
		case <-deadline:
			t.Fatalf("timed out waiting for lines, got %v", msgs)
		}
	}
	if strings.Join(msgs, ",") != "line1,line2,line3" {
		t.Fatalf("msgs = %v, want no duplicates", msgs)
	}

	cancel()
	closed := false
	drain := time.After(3 * time.Second)
	for !closed {
		select {
		case _, ok := <-ch:
			if !ok {
				closed = true
			}
		case <-drain:
			t.Fatal("channel did not close after cancel")
		}
	}
}

func TestStreamNonFollowEmitsOnce(t *testing.T) {
	d := &fakeDocker{outputs: []string{"2026-10-07T12:00:00Z only\n"}}
	f := &Fetcher{Docker: d}
	ch := f.Stream(context.Background(), "axiom-app-1", false)
	var got []string
	for e := range ch {
		got = append(got, e.Message)
	}
	if len(got) != 1 || got[0] != "only" {
		t.Fatalf("got = %v", got)
	}
	if d.callCount() != 1 {
		t.Fatalf("calls = %d, want 1 (non-follow must not poll)", d.callCount())
	}
}

func TestPushBoundedDropsOldestWithMarker(t *testing.T) {
	out := make(chan Entry, StreamBuffer)
	for i := 0; i < 250; i++ {
		pushBounded(out, Entry{Stream: StreamStdout, Message: fmt.Sprintf("line-%d", i)})
	}
	if len(out) > StreamBuffer {
		t.Fatalf("buffer holds %d entries, bound is %d", len(out), StreamBuffer)
	}
	var entries []Entry
	for len(out) > 0 {
		entries = append(entries, <-out)
	}
	var sawMarker, sawFirst, sawLast bool
	for _, e := range entries {
		if e.Stream == StreamSystem && strings.Contains(e.Message, "truncat") {
			sawMarker = true
		}
		if e.Message == "line-0" {
			sawFirst = true
		}
		if e.Message == "line-249" {
			sawLast = true
		}
	}
	if !sawMarker {
		t.Fatalf("expected a truncation marker in %+v", entries)
	}
	if sawFirst {
		t.Fatal("oldest entry must have been dropped")
	}
	if !sawLast {
		t.Fatal("newest entry must be retained")
	}
}

func TestStreamBoundsBufferUnderBackpressure(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 250; i++ {
		fmt.Fprintf(&b, "2026-10-07T12:00:00.%09dZ line-%d\n", i, i)
	}
	d := &fakeDocker{outputs: []string{b.String()}}
	f := &Fetcher{Docker: d}
	ch := f.Stream(context.Background(), "axiom-app-1", false)

	// Let the producer fill (and overflow) the buffer before we consume, so
	// drop-oldest backpressure is exercised rather than a fast reader.
	time.Sleep(50 * time.Millisecond)
	n := 0
	for range ch {
		n++
	}
	if n > StreamBuffer {
		t.Fatalf("stream delivered %d entries, buffer bound is %d", n, StreamBuffer)
	}
	if n == 0 {
		t.Fatal("stream delivered no entries")
	}
}

func TestStreamNoDockerCloses(t *testing.T) {
	f := &Fetcher{}
	ch := f.Stream(context.Background(), "axiom-app-1", true)
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("channel did not close")
	}
}

func TestRedact(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"authorization bearer header", "Authorization: Bearer eyJhbGciOi", "Authorization: Bearer [REDACTED]"},
		{"bearer token", "calling api with Bearer abc123.xyz done", "calling api with Bearer [REDACTED] done"},
		{"password key=value", "connecting password=s3cret done", "connecting password=[REDACTED] done"},
		{"token key=value", "refreshed token=abc123 expires soon", "refreshed token=[REDACTED] expires soon"},
		{"secret key=value", "loaded api secret=hunter2 ok", "loaded api secret=[REDACTED] ok"},
		{"uppercase keys", "PASSWORD=hunter2", "PASSWORD=[REDACTED]"},
		{"json password", `{"password":"abc123"}`, `{"password":"[REDACTED]"}`},
		{"json token no spaces", `{"token":"abc123"}`, `{"token":"[REDACTED]"}`},
		{"pem private key block", "-----BEGIN RSA PRIVATE KEY-----\nMIIabc\n-----END RSA PRIVATE KEY-----", RedactionMarker},
		{"pem ec private key block", "-----BEGIN EC PRIVATE KEY-----\nxyz\n-----END EC PRIVATE KEY-----", RedactionMarker},
		{"url with credentials", "clone https://user:pass@github.com/acme/web.git done", "clone https://[REDACTED]@github.com/acme/web.git done"},
		{"plain text untouched", "starting server on port 3000", "starting server on port 3000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Redact(tc.in); got != tc.want {
				t.Errorf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
