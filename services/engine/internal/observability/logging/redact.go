package logging

import (
	"context"
	"log/slog"
	"strings"

	"github.com/digitaleflex/axiom/services/engine/internal/logs"
)

// Redact returns msg with every recognized secret replaced by
// logs.RedactionMarker. It is the single redaction entry point for Engine log
// lines; the underlying implementation is shared with durable deployment logs
// (internal/logs.Redact) so live logs and persisted logs redact identically.
func Redact(msg string) string { return logs.Redact(msg) }

// RedactError renders err for logging with secrets redacted. A nil error
// yields "".
func RedactError(err error) string {
	if err == nil {
		return ""
	}
	return Redact(err.Error())
}

// NewRedactingHandler wraps next so the record message and every attribute are
// redacted before they reach the sink. Redaction combines two rules:
//
//   - value patterns via Redact (bearer tokens, password=/token=/secret=
//     pairs, JSON secret fields, URL credentials, PEM private keys); and
//   - sensitive attribute keys (password, secret, token, apiKey, credential,
//     authorization, privateKey and any camelCase suffix of these), whose
//     entire value is replaced by the marker even when the value itself looks
//     innocuous.
//
// Nested groups and error values are handled too. Wrapping the base handler
// once at the composition root turns "secrets never reach the log output" into
// a property of the logger rather than a discipline each call site must
// remember.
//
// It is a safety net, not a licence to log secrets: fields that are never
// logged (see docs/architecture/logging.md) must still be kept out by callers.
func NewRedactingHandler(next slog.Handler) slog.Handler {
	if next == nil {
		next = slog.Default().Handler()
	}
	return &redactingHandler{next: next}
}

type redactingHandler struct{ next slog.Handler }

func (h *redactingHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, Redact(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = redactAttr(a)
	}
	return &redactingHandler{next: h.next.WithAttrs(red)}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{next: h.next.WithGroup(name)}
}

// redactAttr redacts a single attribute, recursing into groups and converting
// error values to their redacted message. A sensitive attribute key wins over
// the value: the whole value becomes the marker regardless of its shape.
func redactAttr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if sensitiveKey(a.Key) {
		a.Value = slog.StringValue(logs.RedactionMarker)
		return a
	}
	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(Redact(a.Value.String()))
	case slog.KindGroup:
		g := a.Value.Group()
		for i := range g {
			g[i] = redactAttr(g[i])
		}
		a.Value = slog.GroupValue(g...)
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			a.Value = slog.StringValue(Redact(err.Error()))
		}
	}
	return a
}

// sensitiveSuffixes are matched against a normalized attribute key (lowercase,
// separators removed) so camelCase and snake_case spellings are covered:
// apiToken, api_token and github-token all end in "token".
var sensitiveSuffixes = []string{
	"password", "passwd", "secret", "token",
	"apikey", "credential", "authorization", "privatekey",
}

// sensitiveKey reports whether key names a value that must never be logged.
func sensitiveKey(key string) bool {
	k := strings.ToLower(key)
	k = strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(k)
	if k == "" {
		return false
	}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}
	return false
}
