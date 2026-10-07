package logs

import "regexp"

// RedactionMarker replaces every recognized secret value before persistence.
// The marker is kept so readers can tell a value was present without ever
// seeing the value itself (API contract §14: secrets are never returned).
const RedactionMarker = "[REDACTED]"

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
// RedactionMarker. Redaction runs in Append before persistence; stored
// messages never contain raw secrets.
func Redact(msg string) string {
	msg = rePEM.ReplaceAllString(msg, RedactionMarker)
	msg = reURLCreds.ReplaceAllString(msg, `${1}`+RedactionMarker+`@`)
	msg = reAuthBearer.ReplaceAllString(msg, `${1}`+RedactionMarker)
	msg = reJSONSecret.ReplaceAllString(msg, `"$1"$2"`+RedactionMarker+`"`)
	msg = reKVSecret.ReplaceAllString(msg, `${1}`+RedactionMarker)
	msg = reBearer.ReplaceAllString(msg, `${1}`+RedactionMarker)
	return msg
}
