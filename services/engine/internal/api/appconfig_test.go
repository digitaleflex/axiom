package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/application"
	appconfig "github.com/digitaleflex/axiom/services/engine/internal/secrets"
	secsecrets "github.com/digitaleflex/axiom/services/engine/internal/security/secrets"
)

type fakeAppConfig struct {
	values map[string]string
}

func (f *fakeAppConfig) Set(_ context.Context, appID, name, value string, _ bool) error {
	if !appconfig.ValidName(name) {
		return fmt.Errorf("appconfig: invalid configuration name %q", name)
	}
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[appID+"/"+name] = value
	return nil
}
func (f *fakeAppConfig) List(_ context.Context, appID string) ([]appconfig.Entry, error) {
	out := []appconfig.Entry{}
	for k := range f.values {
		if len(k) > len(appID) && k[:len(appID)+1] == appID+"/" {
			out = append(out, appconfig.Entry{Name: k[len(appID)+1:], Secret: true, IsSet: true})
		}
	}
	return out, nil
}
func (f *fakeAppConfig) Delete(_ context.Context, appID, name string) error {
	k := appID + "/" + name
	if _, ok := f.values[k]; !ok {
		return secsecrets.ErrNotFound
	}
	delete(f.values, k)
	return nil
}

func appConfigHarness(t *testing.T) (*harness, *fakeAppConfig) {
	t.Helper()
	h := newHarness(t)
	f := &fakeAppConfig{values: map[string]string{}}
	h.handler = New(Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: NewTokenAuthenticator(token, Principal{UserID: "usr_1"}),
		Applications: &fakeApps{items: map[string]application.Record{"app_1": {ID: "app_1", OwnerID: "usr_1"}}},
		Servers:      &fakeServers{}, Logs: &fakeLogs{}, AppConfig: f})
	return h, f
}

func TestApplicationConfigurationRoutes(t *testing.T) {
	h, f := appConfigHarness(t)
	r := h.do("PUT", "/api/v1/applications/app_1/configuration/DATABASE_URL", map[string]any{"value": "postgres://user:pass@db/app"}, nil)
	expect(t, r, 204, "")
	if f.values["app_1/DATABASE_URL"] == "" {
		t.Fatal("value not stored")
	}
	r = h.do("GET", "/api/v1/applications/app_1/configuration", nil, nil)
	expect(t, r, 200, "")
	items := r.body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("list = %v", r.body)
	}
	item := items[0].(map[string]any)
	if item["name"] != "DATABASE_URL" || item["secret"] != true || item["isSet"] != true {
		t.Fatalf("entry = %v", item)
	}
	if _, leaked := item["value"]; leaked {
		t.Fatal("value must never be returned")
	}
	// The stored plaintext must never appear in the listing response.
	if r2 := h.do("GET", "/api/v1/applications/app_1/configuration", nil, nil); contains(r2.body, "user:pass") {
		t.Fatal("secret leaked through listing")
	}
	expect(t, h.do("PUT", "/api/v1/applications/app_1/configuration/bad-name", map[string]any{"value": "x"}, nil), 422, CodeValidationFailed)
	expect(t, h.do("DELETE", "/api/v1/applications/app_1/configuration/DATABASE_URL", nil, nil), 204, "")
	expect(t, h.do("DELETE", "/api/v1/applications/app_1/configuration/MISSING", nil, nil), 404, CodeNotFound)
	expect(t, h.do("GET", "/api/v1/applications/app_other/configuration", nil, nil), 404, CodeNotFound)
}

func contains(v any, needle string) bool {
	s, _ := v.(map[string]any)
	raw := ""
	for _, x := range s {
		raw += toStr(x)
	}
	return len(raw) > 0 && len(needle) > 0 && indexOf(raw, needle) >= 0
}

func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		out := ""
		for _, e := range t {
			out += toStr(e)
		}
		return out
	case map[string]any:
		out := ""
		for _, e := range t {
			out += toStr(e)
		}
		return out
	}
	return ""
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
