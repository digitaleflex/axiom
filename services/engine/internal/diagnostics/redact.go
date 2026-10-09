package diagnostics

import (
	"strings"
	"unicode/utf8"

	"github.com/digitaleflex/axiom/services/engine/internal/logs"
)

// Diagnostics re-applies the journal's own redaction on the way out. The
// log store already redacts before persistence (logs.Redact in Append), so
// this is defense in depth: a secret that reached a field the persisting
// redactor does not cover (a probe body captured from an upstream response,
// a message written by a component that bypassed the store) still cannot
// leave through the diagnostic endpoint.

// safeMessage returns msg with secrets redacted and its length bounded.
func safeMessage(msg string) string {
	return truncate(logs.Redact(msg))
}

// safeProbeBody returns a probe body that is safe to serve. It is redacted
// and truncated like any other free-text field.
func safeProbeBody(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	return truncate(logs.Redact(body))
}

// truncate bounds s to maxMessageRunes without splitting a rune, so the
// response is always valid UTF-8.
func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxMessageRunes {
		return s
	}
	count := 0
	for i := range s {
		if count == maxMessageRunes {
			return s[:i] + "…"
		}
		count++
	}
	return s
}
