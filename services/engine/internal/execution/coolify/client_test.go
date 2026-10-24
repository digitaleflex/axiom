package coolify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/execution"
)

func TestNewRequiresTokenAndHTTPSForRemoteHosts(t *testing.T) {
	if _, err := New(Config{BaseURL: "https://coolify.example.com"}); err == nil {
		t.Fatal("expected missing token to be rejected")
	}
	if _, err := New(Config{BaseURL: "http://coolify.example.com", Token: "secret"}); err == nil {
		t.Fatal("expected remote HTTP to be rejected")
	}
	if _, err := New(Config{BaseURL: "http://127.0.0.1:3000", Token: "secret"}); err != nil {
		t.Fatalf("loopback HTTP should be allowed for local testing: %v", err)
	}
}

func TestCreateApplicationUsesPrebuiltImageAndExplicitConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/applications/dockerimage" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["docker_registry_image_name"] != "ghcr.io/acme/web" {
			t.Fatalf("unexpected image name: %#v", body["docker_registry_image_name"])
		}
		if body["docker_registry_image_tag"] != "sha-abc1234" {
			t.Fatalf("unexpected image tag: %#v", body["docker_registry_image_tag"])
		}
		if body["instant_deploy"] != false {
			t.Fatalf("creation must not trigger an implicit deployment: %#v", body["instant_deploy"])
		}
		if body["domains"] != "https://web.example.com" {
			t.Fatalf("Coolify requires a scheme on domains, got %#v", body["domains"])
		}
		if body["build_pack"] != nil {
			t.Fatalf("Axiom's prebuilt image path must not request a Coolify build pack: %#v", body["build_pack"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"uuid":"coolify-app-1"}`))
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Token: "test-token", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	app, err := client.CreateApplication(context.Background(), execution.ApplicationSpec{
		Name: "acme-web", Image: "ghcr.io/acme/web", ImageTag: "sha-abc1234",
		Port: 3000, Domain: "web.example.com", HealthPath: "/healthz",
		ProjectID: "project-1", ServerID: "server-1", Environment: "production",
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.ProviderID != "coolify-app-1" || app.URL != "https://web.example.com" {
		t.Fatalf("unexpected application result: %#v", app)
	}
}

func TestDeployUpdatesImmutableTagThenQueuesDeployment(t *testing.T) {
	var patched bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/applications/app-1":
			patched = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode patch: %v", err)
			}
			if body["docker_registry_image_tag"] != "sha-deadbee" {
				t.Fatalf("unexpected image tag: %#v", body["docker_registry_image_tag"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"uuid":"app-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/deploy":
			if !patched {
				t.Fatal("deploy was queued before image tag update")
			}
			if r.URL.Query().Get("uuid") != "app-1" {
				t.Fatalf("unexpected deploy UUID: %q", r.URL.Query().Get("uuid"))
			}
			if r.Header.Get("Content-Type") != "" {
				t.Fatalf("deploy endpoint should not receive an invented JSON body")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"deployments":[{"message":"Deployment queued","resource_uuid":"app-1","deployment_uuid":"dep-1"}]}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Token: "test-token", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := client.Deploy(context.Background(), "app-1", "ghcr.io/acme/web", "sha-deadbee")
	if err != nil {
		t.Fatal(err)
	}
	if deployment.ProviderID != "dep-1" || deployment.Status != execution.StatusQueued {
		t.Fatalf("unexpected deployment result: %#v", deployment)
	}
}

func TestGetDeploymentNormalizesStatusWithoutExposingLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/deployments/dep-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"deployment_uuid":"dep-1","application_id":"app-1","status":"finished","logs":"TOKEN=do-not-expose","deployment_url":"https://web.example.com","created_at":"2026-10-24T10:00:00Z","updated_at":"2026-10-24T10:01:00Z"}`))
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Token: "test-token", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.GetDeployment(context.Background(), "dep-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != execution.StatusSucceeded {
		t.Fatalf("expected succeeded, got %q", got.Status)
	}
	if strings.Contains(got.Message, "do-not-expose") {
		t.Fatal("raw provider logs must not be copied to deployment messages")
	}
	if got.URL != "https://web.example.com" {
		t.Fatalf("unexpected URL: %q", got.URL)
	}
}

func TestCreateApplicationRequiresImmutableTag(t *testing.T) {
	client, err := New(Config{BaseURL: "https://coolify.example.com", Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateApplication(context.Background(), execution.ApplicationSpec{
		Name: "web", Image: "ghcr.io/acme/web", Port: 3000,
		ProjectID: "project-1", ServerID: "server-1", Environment: "production",
	})
	if err == nil || !strings.Contains(err.Error(), "immutable registry image tag") {
		t.Fatalf("expected immutable tag validation error, got %v", err)
	}
}

func TestAPIErrorDoesNotExposeResponseBodyWhenMessageMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("secret-provider-detail"))
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Token: "test-token", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Health(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T (%v)", err, err)
	}
	if strings.Contains(err.Error(), "secret-provider-detail") {
		t.Fatal("provider response body must not be copied into the error")
	}
}

func TestNormalizeStatusCoversCoolifyDeploymentStatuses(t *testing.T) {
	// The exact values of Coolify's ApplicationDeploymentStatus enum.
	cases := map[string]execution.Status{
		"queued":            execution.StatusQueued,
		"in_progress":       execution.StatusDeploying,
		"finished":          execution.StatusSucceeded,
		"failed":            execution.StatusFailed,
		"cancelled-by-user": execution.StatusCancelled,
		"":                  execution.StatusUnknown,
		"something-new":     execution.StatusUnknown,
	}
	for raw, want := range cases {
		if got := normalizeStatus(raw); got != want {
			t.Errorf("normalizeStatus(%q) = %q, want %q", raw, got, want)
		}
	}
}
