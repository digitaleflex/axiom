package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/build"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

type fakeServerChecker struct {
	records map[string]server.Record
}

func (f fakeServerChecker) Get(_ context.Context, id string) (server.Record, error) {
	r, ok := f.records[id]
	if !ok {
		return server.Record{}, server.ErrNotFound
	}
	return r, nil
}

func readyServerRecord() server.Record {
	return server.Record{ID: "srv_1", Name: "srv-eu-1", Status: server.StatusReady,
		Capabilities: []server.Capability{server.CapabilityDocker, server.CapabilityTraefik, server.CapabilityTLS},
		CPUCount:     4, MemoryMB: 8192, DiskFreeMB: 50000,
		LastSeenAt: time.Now().UTC().Format(time.RFC3339)}
}

type buildSpy struct {
	calls int
}

func (b *buildSpy) Build(_ context.Context, _ build.Input) (build.Result, error) {
	b.calls++
	return build.Result{ImageRef: "img", Artifact: build.Artifact{Digest: "sha256:x"}}, nil
}

func executeWithServers(t *testing.T, servers ServerChecker, mutate func(*server.Record)) (Result, error, *buildSpy, *deployment.Service, string) {
	t.Helper()
	svc, rec, plan := setup(t)
	if mutate != nil {
		r := readyServerRecord()
		mutate(&r)
		servers.(fakeServerChecker).records["srv_1"] = r
	}
	spy := &buildSpy{}
	ex := New(svc, spy, &fakeAgent{})
	ex.Backoff = nil
	ex.Servers = servers
	res, err := ex.Execute(context.Background(), Request{DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan})
	return res, err, spy, svc, rec.ID
}

func serversWith(rec server.Record) fakeServerChecker {
	return fakeServerChecker{records: map[string]server.Record{"srv_1": rec}}
}

func TestPreflightRejectsOfflineServer(t *testing.T) {
	_, err, spy, svc, id := executeWithServers(t, serversWith(readyServerRecord()), func(r *server.Record) {
		r.Status = server.StatusOffline
	})
	if err == nil {
		t.Fatal("expected pre-flight failure")
	}
	if spy.calls != 0 {
		t.Fatal("no build must run when the server is offline")
	}
	got, _ := svc.Get(context.Background(), id)
	if got.Status != deployment.StateFailed || got.ErrorCode != ErrorNotEligible {
		t.Fatalf("status=%s code=%s", got.Status, got.ErrorCode)
	}
	steps, _ := svc.Store().Steps(context.Background(), id)
	for _, s := range steps {
		if s.Status != deployment.StepQueued {
			t.Fatalf("no step may run before eligibility, %s = %s", s.Name, s.Status)
		}
	}
}

func TestPreflightRejectsMissingCapability(t *testing.T) {
	_, err, spy, _, _ := executeWithServers(t, serversWith(readyServerRecord()), func(r *server.Record) {
		r.Capabilities = []server.Capability{server.CapabilityDocker} // plan domain needs traefik
	})
	if err == nil || spy.calls != 0 {
		t.Fatalf("expected capability failure without build, got %v calls=%d", err, spy.calls)
	}
}

func TestPreflightRejectsStaleHeartbeat(t *testing.T) {
	_, err, spy, _, _ := executeWithServers(t, serversWith(readyServerRecord()), func(r *server.Record) {
		r.LastSeenAt = time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	})
	if err == nil || spy.calls != 0 {
		t.Fatalf("expected staleness failure, got %v calls=%d", err, spy.calls)
	}
}

func TestPreflightRejectsUnknownServer(t *testing.T) {
	_, err, spy, _, _ := executeWithServers(t, fakeServerChecker{records: map[string]server.Record{}}, nil)
	if err == nil || spy.calls != 0 {
		t.Fatalf("expected unknown-server failure, got %v calls=%d", err, spy.calls)
	}
}

func TestPreflightPassesDegradedServer(t *testing.T) {
	res, err, spy, _, _ := executeWithServers(t, serversWith(readyServerRecord()), func(r *server.Record) {
		r.Status = server.StatusDegraded
	})
	if err != nil || res.Deployment.Status != deployment.StateLive || spy.calls != 1 {
		t.Fatalf("degraded server must proceed: %v calls=%d", err, spy.calls)
	}
}

func TestNoCheckerSkipsPreflight(t *testing.T) {
	svc, rec, plan := setup(t)
	spy := &buildSpy{}
	ex := New(svc, spy, &fakeAgent{})
	ex.Backoff = nil // Servers nil
	res, err := ex.Execute(context.Background(), Request{DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan})
	if err != nil || res.Deployment.Status != deployment.StateLive {
		t.Fatalf("without checker: %v", err)
	}
}

func TestPolicyDeniesTamperedPlan(t *testing.T) {
	svc, rec, plan := setup(t)
	plan.Runtime.Port = 9999 // tampered after fingerprinting is out of scope here: invalid plan
	plan.Fingerprint = "sha256:" + strings.Repeat("0", 64)
	spy := &buildSpy{}
	ex := New(svc, spy, &fakeAgent{})
	ex.Backoff = nil
	_, err := ex.Execute(context.Background(), Request{DeploymentID: rec.ID, Container: "c", Source: fakeSource{}, Plan: plan})
	if err == nil || !strings.Contains(err.Error(), "denied by policy") {
		t.Fatalf("tampered plan must be denied, got %v", err)
	}
	if spy.calls != 0 {
		t.Fatal("denied plans must not build")
	}
	got, _ := svc.Get(context.Background(), rec.ID)
	if got.Status != deployment.StateFailed || got.ErrorCode != ErrorPolicyDenied {
		t.Fatalf("status=%s code=%s", got.Status, got.ErrorCode)
	}
}
