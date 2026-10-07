package api

import (
	"io"
	"log/slog"
	"testing"

	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// authzHarness wires two users: usr_1 (owner) and usr_2 (non-owner).
// Only dependencies needed by the tested routes are wired.
func authzHarness(t *testing.T) (*harness, *harness) {
	t.Helper()
	apps := &fakeApps{items: map[string]application.Record{
		"app_1":     {ID: "app_1", Name: "acme-web", RepositoryID: "repo_1", OwnerID: "usr_1"},
		"app_other": {ID: "app_other", Name: "other", RepositoryID: "repo_2", OwnerID: "usr_2"},
	}}
	servers := &fakeServers{items: []server.Record{
		{ID: "srv_1", Name: "srv-eu-1", Address: "203.0.113.10", OwnerID: "usr_1", Status: server.StatusReady},
	}}
	deps := func(user string) Deps {
		return Deps{
			Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			Auth:         NewTokenAuthenticator(token, Principal{UserID: user}),
			Deployments:  newHarness(t).svc,
			Applications: apps,
			Servers:      servers,
			Logs:         &fakeLogs{},
		}
	}
	owner := newHarness(t)
	owner.handler = New(deps("usr_1"))
	other := newHarness(t)
	other.handler = New(deps("usr_2"))
	return owner, other
}

func TestNonOwnerGets404OnRead(t *testing.T) {
	_, other := authzHarness(t)
	// Reads of a foreign application are hidden (404, no existence leak).
	expect(t, other.do("GET", "/api/v1/applications/app_1", nil, nil), 404, CodeNotFound)
	expect(t, other.do("GET", "/api/v1/applications/app_1/deployments", nil, nil), 404, CodeNotFound)
}

func TestNonOwnerGets404OnWrite(t *testing.T) {
	_, other := authzHarness(t)
	// Writes targeting a known foreign ID return 404 (V0.1 rule: hide
	// existence everywhere; authz is defense-in-depth).
	expect(t, other.do("POST", "/api/v1/applications/app_1/deployments", map[string]any{"planId": "plan_1"}, nil), 404, CodeNotFound)
	expect(t, other.do("PATCH", "/api/v1/servers/srv_1", map[string]any{"name": "hacked"}, nil), 404, CodeNotFound)
	expect(t, other.do("DELETE", "/api/v1/servers/srv_1", nil, nil), 404, CodeNotFound)
}

func TestOwnerSucceeds(t *testing.T) {
	owner, _ := authzHarness(t)
	expect(t, owner.do("GET", "/api/v1/applications/app_1", nil, nil), 200, "")
	expect(t, owner.do("GET", "/api/v1/applications/app_1/deployments", nil, nil), 200, "")
}

func TestForeignDeploymentHidden(t *testing.T) {
	owner, other := authzHarness(t)
	// Create a deployment as the owner.
	id := createDeployment(owner, owner)
	// The non-owner cannot see or cancel it.
	expect(t, other.do("GET", "/api/v1/deployments/"+id, nil, nil), 404, CodeNotFound)
	expect(t, other.do("POST", "/api/v1/deployments/"+id+"/cancel", nil, nil), 404, CodeNotFound)
	expect(t, other.do("GET", "/api/v1/deployments/"+id+"/steps", nil, nil), 404, CodeNotFound)
	expect(t, other.do("GET", "/api/v1/deployments/"+id+"/events", nil, nil), 404, CodeNotFound)
	expect(t, other.do("GET", "/api/v1/deployments/"+id+"/logs", nil, nil), 404, CodeNotFound)
	expect(t, other.do("GET", "/api/v1/deployments/"+id+"/health", nil, nil), 404, CodeNotFound)
	// The owner can.
	expect(t, owner.do("GET", "/api/v1/deployments/"+id, nil, nil), 200, "")
}

func TestForeignServerHiddenOnWrite(t *testing.T) {
	_, other := authzHarness(t)
	// srv_1 is owned by usr_1; usr_2 gets 404 on rename/remove.
	expect(t, other.do("PATCH", "/api/v1/servers/srv_1", map[string]any{"name": "hacked"}, nil), 404, CodeNotFound)
	expect(t, other.do("DELETE", "/api/v1/servers/srv_1", nil, nil), 404, CodeNotFound)
}
