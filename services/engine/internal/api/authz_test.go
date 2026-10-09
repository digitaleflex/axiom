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
		{ID: "srv_other", Name: "srv-us-1", Address: "203.0.113.20", OwnerID: "usr_2", Status: server.StatusReady},
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

func TestForeignServerHiddenOnRead(t *testing.T) {
	owner, other := authzHarness(t)
	// srv_1 is owned by usr_1: usr_2 cannot read it nor its health (404,
	// no existence leak).
	expect(t, other.do("GET", "/api/v1/servers/srv_1", nil, nil), 404, CodeNotFound)
	expect(t, other.do("GET", "/api/v1/servers/srv_1/health", nil, nil), 404, CodeNotFound)
	// The owner still reads their own server and health.
	expect(t, owner.do("GET", "/api/v1/servers/srv_1", nil, nil), 200, "")
	expect(t, owner.do("GET", "/api/v1/servers/srv_1/health", nil, nil), 200, "")
	// Symmetry: usr_1 cannot read usr_2's server either.
	expect(t, owner.do("GET", "/api/v1/servers/srv_other", nil, nil), 404, CodeNotFound)
	expect(t, owner.do("GET", "/api/v1/servers/srv_other/health", nil, nil), 404, CodeNotFound)
}

// serverIDs extracts the id column of a server page response.
func serverIDs(t *testing.T, r resp) []string {
	t.Helper()
	items, _ := r.body["items"].([]any)
	var ids []string
	for _, it := range items {
		ids = append(ids, it.(map[string]any)["id"].(string))
	}
	return ids
}

func TestServerListScopedToOwner(t *testing.T) {
	owner, other := authzHarness(t)
	// Each subject only ever sees their own servers: no cross-user
	// enumeration through the listing endpoint.
	r := owner.do("GET", "/api/v1/servers", nil, nil)
	expect(t, r, 200, "")
	if ids := serverIDs(t, r); len(ids) != 1 || ids[0] != "srv_1" {
		t.Fatalf("owner listing = %v", ids)
	}
	r = other.do("GET", "/api/v1/servers", nil, nil)
	expect(t, r, 200, "")
	if ids := serverIDs(t, r); len(ids) != 1 || ids[0] != "srv_other" {
		t.Fatalf("foreign listing = %v", ids)
	}
	// The status filter keeps the owner scope.
	r = owner.do("GET", "/api/v1/servers?status=ready", nil, nil)
	expect(t, r, 200, "")
	if ids := serverIDs(t, r); len(ids) != 1 || ids[0] != "srv_1" {
		t.Fatalf("filtered owner listing = %v", ids)
	}
}

func TestOwnerKeepsFullServerAccess(t *testing.T) {
	owner, _ := authzHarness(t)
	// The scoped reads do not restrict the owner: register, rename and
	// removal keep working on their own server.
	expect(t, owner.do("POST", "/api/v1/servers", map[string]any{"name": "srv-eu-3", "address": "203.0.113.12"}, nil), 201, "")
	expect(t, owner.do("PATCH", "/api/v1/servers/srv_1", map[string]any{"name": "srv-eu-1b"}, nil), 200, "")
	expect(t, owner.do("GET", "/api/v1/servers/srv_1", nil, nil), 200, "")
	expect(t, owner.do("GET", "/api/v1/servers/srv_1/health", nil, nil), 200, "")
}
