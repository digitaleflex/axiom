package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRepo struct {
	records map[string]Record
	active  map[string]int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{records: map[string]Record{}, active: map[string]int{}}
}

func (r *fakeRepo) Create(_ context.Context, rec Record) error {
	r.records[rec.ID] = rec
	return nil
}
func (r *fakeRepo) Get(_ context.Context, id string) (Record, error) {
	rec, ok := r.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return rec, nil
}
func (r *fakeRepo) ListFiltered(_ context.Context, ownerID, status string, _, _ int) ([]Record, int, error) {
	var out []Record
	for _, rec := range r.records {
		if rec.OwnerID != ownerID {
			continue
		}
		if status == "" || string(rec.Status) == status {
			out = append(out, rec)
		}
	}
	return out, len(out), nil
}
func (r *fakeRepo) Rename(_ context.Context, id, name string) error {
	rec, ok := r.records[id]
	if !ok {
		return ErrNotFound
	}
	rec.Name = name
	r.records[id] = rec
	return nil
}
func (r *fakeRepo) Delete(_ context.Context, id string) error {
	delete(r.records, id)
	return nil
}
func (r *fakeRepo) ActiveDeployments(_ context.Context, id string) (int, error) {
	return r.active[id], nil
}
func (r *fakeRepo) UpdateHealth(_ context.Context, id string, h Health) error {
	rec := r.records[id]
	rec.Status, rec.LastSeenAt = h.Status, h.LastSeenAt
	r.records[id] = rec
	return nil
}

func TestRegisterValidates(t *testing.T) {
	svc := NewService(newFakeRepo())
	ctx := context.Background()
	rec, err := svc.Register(ctx, "usr_1", "srv-eu-1", "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != StatusPending || rec.OwnerID != "usr_1" || rec.Name != "srv-eu-1" {
		t.Fatalf("record = %+v", rec)
	}
	for _, bad := range []struct{ name, addr string }{{"Bad Name", "h"}, {"ok", "http://h/"}, {"ok", ""}, {"ok", "h:99999"}} {
		if _, err := svc.Register(ctx, "u", bad.name, bad.addr); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestRenameAndRemove(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	ctx := context.Background()
	rec, _ := svc.Register(ctx, "u", "srv-1", "host.example.com")
	got, err := svc.Rename(ctx, rec.ID, "srv-2")
	if err != nil || got.Name != "srv-2" {
		t.Fatalf("rename = %+v %v", got, err)
	}
	if _, err := svc.Rename(ctx, rec.ID, "bad name"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("bad rename: %v", err)
	}
	if _, err := svc.Rename(ctx, "srv_x", "srv-3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing rename: %v", err)
	}
	repo.active[rec.ID] = 2
	if err := svc.Remove(ctx, rec.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("remove in use: %v", err)
	}
	repo.active[rec.ID] = 0
	if err := svc.Remove(ctx, rec.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := svc.Remove(ctx, rec.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove missing: %v", err)
	}
}

func TestEffectiveStatus(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Add(-time.Minute).Format(time.RFC3339)
	stale := now.Add(-time.Hour).Format(time.RFC3339)
	cases := []struct {
		status Status
		seen   string
		want   Status
	}{
		{StatusReady, fresh, StatusReady},
		{StatusReady, stale, StatusOffline},
		{StatusDegraded, stale, StatusOffline},
		{StatusReady, "", StatusReady},        // unknown freshness: keep stored
		{StatusPending, stale, StatusPending}, // awaiting first heartbeat
		{StatusOffline, fresh, StatusOffline},
		{StatusRevoked, fresh, StatusRevoked},
	}
	for _, c := range cases {
		if got := EffectiveStatusAt(Record{Status: c.status, LastSeenAt: c.seen}, now); got != c.want {
			t.Errorf("%s/%q = %s, want %s", c.status, c.seen, got, c.want)
		}
	}
	svc := NewService(nil)
	staleReady := Record{ID: "s", Status: StatusReady, CPUCount: 2, MemoryMB: 1024, DiskFreeMB: 1000,
		Capabilities: []Capability{CapabilityDocker}, LastSeenAt: stale}
	if res := svc.CheckEligibilityAt(staleReady, EligibilityRequest{RequiredCapabilities: []Capability{CapabilityDocker}}, now); res.Eligible {
		t.Fatal("stale server must be ineligible")
	}
}

func TestValidAddress(t *testing.T) {
	for _, ok := range []string{"203.0.113.10", "host.example.com", "srv-1", "10.0.0.1:2222", "example.com:22", "[::1]:22", "2001:db8::1"} {
		if !ValidAddress(ok) {
			t.Errorf("rejected %q", ok)
		}
	}
	for _, bad := range []string{"", "http://h/", "host/path", "h:0", "h:99999", "a b", "http://h", "[::1", "-bad-.com", strings.Repeat("a", 300)} {
		if ValidAddress(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestRequiredCapabilities(t *testing.T) {
	if got := RequiredCapabilities("nextjs", true); !reflect_Contains(got, CapabilityDocker) || !reflect_Contains(got, CapabilityTraefik) {
		t.Fatalf("node+domain = %v", got)
	}
	if got := RequiredCapabilities("compose", false); !reflect_Contains(got, CapabilityCompose) {
		t.Fatalf("compose = %v", got)
	}
	if got := RequiredCapabilities("go", false); len(got) != 1 {
		t.Fatalf("go = %v", got)
	}
}

func reflect_Contains(caps []Capability, want Capability) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}
