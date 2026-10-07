package logs

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"clean message", "fetching source at abc1234", "fetching source at abc1234"},
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
		{"url without credentials kept", "clone https://github.com/acme/web.git done", "clone https://github.com/acme/web.git done"},
		{"marker never contains the secret", "password=hunter2", "password=[REDACTED]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Redact(tc.in); got != tc.want {
				t.Errorf("Redact(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRedactKeepsMarkerNotValue(t *testing.T) {
	out := Redact("token=supersecrettoken password=hunter2")
	if out != "token=[REDACTED] password=[REDACTED]" {
		t.Fatalf("redacted = %q", out)
	}
	for _, secret := range []string{"supersecrettoken", "hunter2"} {
		if strings.Contains(out, secret) {
			t.Errorf("secret %q leaked into %q", secret, out)
		}
	}
}
