// Package logs exposes bounded runtime container logs to the Engine (#86):
// a one-shot Fetch with cursor/search options, and a follow Stream with
// bounded buffering and drop-oldest backpressure. Secrets are redacted before
// entries ever leave the agent.
//
// Security boundary (issue #86):
//
//   - Never host-wide logs. The package only ever asks its Docker source for a
//     single, already-validated container name; it has no listing operation and
//     no host path access.
//   - Only Axiom-managed runtimes. Container names are validated by the
//     ownership package (ownership.ValidateName) before use; the dispatcher
//     additionally authorizes each deployment (per-deployment auth) and only
//     passes container names it has already checked with ownership.
//   - Secrets are redacted where detectable (Redact): bearer tokens,
//     password=/token=/secret= pairs, JSON secret fields, URL credentials and
//     PEM private-key blocks become [REDACTED]. Redaction happens before an
//     entry is returned or streamed.
//
// The Docker source is a deliberately minimal interface (Logs) rather than the
// runtime/docker adapter's Tail, because timestamps and stream handling need
// `docker logs --timestamps`; the dispatcher (#80) wires a real implementation
// and tests inject a fake. This package never shells out itself.
package logs

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Log stream names. The Docker CLI merges stdout and stderr, so a parsed line
// is attributed to stdout unless a source explicitly marks it otherwise; the
// "system" stream carries agent-generated markers (truncation).
const (
	StreamStdout = "stdout"
	StreamStderr = "stderr"
	StreamSystem = "system"
)

// RedactionMarker replaces every recognized secret. The marker is kept so
// readers can tell a value was present without seeing the value itself.
const RedactionMarker = "[REDACTED]"

// TruncationMessage marks a gap introduced by drop-oldest backpressure.
const TruncationMessage = "[logs truncated: buffer overflow]"

// Bounds.
const (
	// StreamBuffer is the capacity of the Stream channel. When it fills, the
	// oldest entries are dropped and a TruncationMarker is queued.
	StreamBuffer = 100
	// DefaultLines is the history size when FetchOptions.Lines is unset.
	DefaultLines = 100
	// MaxLines caps the history size a caller may request.
	MaxLines = 10000

	defaultPollInterval = 500 * time.Millisecond
	maxMessageBytes     = 8192
)

// ErrNoDocker means the Fetcher has no Docker log source configured.
var ErrNoDocker = errors.New("logs: docker source not configured")

// Docker is the minimal bounded log source the Fetcher needs. Logs returns up
// to tail recent lines of the container's logs, prefixed with RFC3339Nano
// timestamps when timestamps is true. Implementations must only ever read the
// named container. The dispatcher wires a real implementation (backed by the
// runtime/docker adapter); tests fake it.
type Docker interface {
	Logs(ctx context.Context, container string, tail int, timestamps bool) (string, error)
}

// Entry is one parsed log line.
type Entry struct {
	Timestamp time.Time `json:"timestamp,omitempty"`
	Stream    string    `json:"stream"`
	Message   string    `json:"message"`
}

// TruncationMarker is queued when Stream drops entries under backpressure.
var TruncationMarker = Entry{Stream: StreamSystem, Message: TruncationMessage}

// Fetcher reads bounded logs from a Docker source and redacts them.
type Fetcher struct {
	// Docker is the log source. Required for Fetch/Stream.
	Docker Docker
	// Redact redacts one message. nil = the package Redact.
	Redact func(string) string
	// PollInterval is the Stream poll period. nil/0 = 500ms.
	PollInterval time.Duration
}

// FetchOptions bounds and filters a one-shot Fetch.
type FetchOptions struct {
	// Lines is the history size; <=0 = DefaultLines, capped at MaxLines.
	Lines int
	// Since keeps only entries at/after this time (entries without a parsed
	// timestamp are kept). Zero = no time filter.
	Since time.Time
	// Search keeps only entries whose message contains this substring
	// (case-insensitive). Empty = no filter.
	Search string
}

// Fetch returns up to opts.Lines recent entries, newest last, with timestamps
// parsed, search applied and secrets redacted.
func (f *Fetcher) Fetch(ctx context.Context, container string, opts FetchOptions) ([]Entry, error) {
	if f.Docker == nil {
		return nil, ErrNoDocker
	}
	tail := opts.Lines
	if tail <= 0 {
		tail = DefaultLines
	}
	if tail > MaxLines {
		tail = MaxLines
	}
	raw, err := f.Docker.Logs(ctx, container, tail, true)
	if err != nil {
		return nil, err
	}
	entries := parse(raw, f.redact)

	search := strings.ToLower(opts.Search)
	var out []Entry
	for _, e := range entries {
		if !opts.Since.IsZero() && !e.Timestamp.IsZero() && e.Timestamp.Before(opts.Since) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(e.Message), search) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// Stream returns a bounded channel of entries. When follow is true it keeps
// polling every PollInterval, emitting only new lines, until ctx is done; when
// false it emits the current history once and closes. The channel has capacity
// StreamBuffer and never blocks the poller: on overflow the oldest entries are
// dropped and a TruncationMarker is queued. The channel closes when the poller
// stops.
func (f *Fetcher) Stream(ctx context.Context, container string, follow bool) <-chan Entry {
	out := make(chan Entry, StreamBuffer)
	go f.streamLoop(ctx, container, follow, out)
	return out
}

func (f *Fetcher) streamLoop(ctx context.Context, container string, follow bool, out chan Entry) {
	defer close(out)
	if f.Docker == nil {
		return
	}
	interval := f.PollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}

	cursor := ""
	for {
		entries, err := f.Fetch(ctx, container, FetchOptions{Lines: DefaultLines})
		if err == nil {
			start := 0
			if cursor != "" {
				for i := len(entries) - 1; i >= 0; i-- {
					if entryKey(entries[i]) == cursor {
						start = i + 1
						break
					}
				}
			}
			for _, e := range entries[start:] {
				pushBounded(out, e)
			}
			if len(entries) > 0 {
				cursor = entryKey(entries[len(entries)-1])
			}
		}
		if !follow {
			return
		}
		if !sleepCtx(ctx, interval) {
			return
		}
	}
}

// pushBounded sends e, dropping the oldest entries and inserting a truncation
// marker whenever the buffer overflows. It never blocks indefinitely: it makes
// room before sending.
func pushBounded(out chan Entry, e Entry) {
	select {
	case out <- e:
		return
	default:
	}
	// Full: drop the oldest entry, then drop until there is room for a marker
	// and the new entry.
	<-out
	for len(out) > cap(out)-2 {
		select {
		case <-out:
		default:
		}
	}
	out <- TruncationMarker
	out <- e
}

func (f *Fetcher) redact(msg string) string {
	if f.Redact != nil {
		return f.Redact(msg)
	}
	return Redact(msg)
}

// parse splits docker log output into entries. With --timestamps each line is
// "<RFC3339Nano> <message>"; a line without a parseable timestamp is treated
// as an untimed stdout line.
func parse(raw string, redact func(string) string) []Entry {
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return nil
	}
	var out []Entry
	for _, line := range strings.Split(raw, "\n") {
		e := Entry{Stream: StreamStdout}
		msg := line
		if i := strings.IndexByte(line, ' '); i > 0 {
			if ts, err := time.Parse(time.RFC3339Nano, line[:i]); err == nil {
				e.Timestamp = ts
				msg = line[i+1:]
			}
		}
		msg = redact(msg)
		if len(msg) > maxMessageBytes {
			msg = msg[:maxMessageBytes]
		}
		e.Message = msg
		out = append(out, e)
	}
	return out
}

func entryKey(e Entry) string {
	return e.Timestamp.UTC().Format(time.RFC3339Nano) + "\x00" + e.Stream + "\x00" + e.Message
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

var (
	// PEM private key blocks, redacted wholesale.
	rePEM = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	// URLs with embedded credentials: https://user:pass@host → https://[REDACTED]@host.
	reURLCreds = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`)
	// Authorization: Bearer <token> header form.
	reAuthBearer = regexp.MustCompile(`(?i)(Authorization\s*:\s*Bearer\s+)\S+`)
	// JSON string secrets: "password": "…" → "password": "[REDACTED]".
	reJSONSecret = regexp.MustCompile(`(?i)"(password|token|secret)"(\s*:\s*)"[^"]*"`)
	// key=value secrets: password=…, token=…, secret=… (value ends at whitespace, & or quote).
	reKVSecret = regexp.MustCompile(`(?i)\b((?:password|token|secret)\s*=\s*)[^\s&'"]+`)
	// Standalone bearer tokens.
	reBearer = regexp.MustCompile(`(?i)\b(Bearer[\s:]+)\S+`)
)

// Redact returns msg with every recognized secret replaced by
// RedactionMarker. It mirrors the Engine's persistence-time convention
// (services/engine/internal/logs.Redact) so agent-side runtime logs and
// durable Engine logs redact identically.
func Redact(msg string) string {
	msg = rePEM.ReplaceAllString(msg, RedactionMarker)
	msg = reURLCreds.ReplaceAllString(msg, `${1}`+RedactionMarker+`@`)
	msg = reAuthBearer.ReplaceAllString(msg, `${1}`+RedactionMarker)
	msg = reJSONSecret.ReplaceAllString(msg, `"$1"$2"`+RedactionMarker+`"`)
	msg = reKVSecret.ReplaceAllString(msg, `${1}`+RedactionMarker)
	msg = reBearer.ReplaceAllString(msg, `${1}`+RedactionMarker)
	return msg
}
