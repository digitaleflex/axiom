package domains

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type memStore struct {
	mu      sync.Mutex
	records map[string]Record
	target  Target
	hasLive bool
}

func newMemStore() *memStore { return &memStore{records: map[string]Record{}} }

func (m *memStore) List(_ context.Context, app, env string) ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Record{}
	for _, r := range m.records {
		if r.ApplicationID == app && (env == "" || r.Environment == env) {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memStore) Create(_ context.Context, r Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.records {
		if e.Hostname == r.Hostname {
			return ErrTaken
		}
	}
	m.records[r.ID] = r
	return nil
}
func (m *memStore) SetPrimary(_ context.Context, app, env, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[id]; !ok {
		return ErrNotFound
	}
	for k, r := range m.records {
		if r.ApplicationID == app && r.Environment == env {
			r.IsPrimary = k == id
			m.records[k] = r
		}
	}
	return nil
}
func (m *memStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[id]; !ok {
		return ErrNotFound
	}
	delete(m.records, id)
	return nil
}
func (m *memStore) Get(_ context.Context, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r, nil
}
func (m *memStore) RoutingTarget(context.Context, string, string) (Target, bool, error) {
	return m.target, m.hasLive, nil
}
func (m *memStore) SaveDNSCheck(_ context.Context, id, status, expected, observed string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.records[id]
	r.DNSStatus, r.DNSExpected, r.DNSObserved, r.DNSCheckedAt = status, expected, observed, &at
	m.records[id] = r
	return nil
}

type fakeResolver struct {
	ips map[string][]string
	err map[string]bool
}

func (f fakeResolver) LookupIP(_ context.Context, host string) ([]string, error) {
	if f.err[host] {
		return nil, errors.New("no such host")
	}
	return f.ips[host], nil
}

func TestNormalize(t *testing.T) {
	if n, err := Normalize("App.Example.COM."); err != nil || n != "app.example.com" {
		t.Fatalf("normalize = %q %v", n, err)
	}
	for _, bad := range []string{"", "http://x.com/", "app_example", "-bad.com", "a..com", "h:8080", "app/", strings.Repeat("a", 64) + ".com"} {
		if _, err := Normalize(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestCreatePrimaryAndRemove(t *testing.T) {
	st := newMemStore()
	svc := &Service{Store: st}
	ctx := context.Background()
	first, err := svc.Create(ctx, "app_1", "production", "app.acme.dev")
	if err != nil || !first.IsPrimary {
		t.Fatalf("first = %+v %v", first, err)
	}
	second, err := svc.Create(ctx, "app_1", "production", "www.acme.dev")
	if err != nil || second.IsPrimary {
		t.Fatalf("second = %+v %v", second, err)
	}
	if _, err := svc.Create(ctx, "app_2", "production", "app.acme.dev"); !errors.Is(err, ErrTaken) {
		t.Fatalf("duplicate hostname across apps: %v", err)
	}
	if err := svc.Remove(ctx, "app_1", first.ID); !errors.Is(err, ErrPrimary) {
		t.Fatalf("remove primary with siblings: %v", err)
	}
	if _, err := svc.SetPrimary(ctx, "app_1", second.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Remove(ctx, "app_1", first.ID); err != nil {
		t.Fatalf("remove demoted: %v", err)
	}
	if err := svc.Remove(ctx, "app_1", second.ID); err != nil {
		t.Fatalf("remove sole domain: %v", err)
	}
	if _, err := svc.SetPrimary(ctx, "app_1", "dom_x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("primary missing: %v", err)
	}
	if err := svc.Remove(ctx, "app_other", second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-app remove: %v", err)
	}
}

func TestEnsureDomain(t *testing.T) {
	st := newMemStore()
	svc := &Service{Store: st}
	ctx := context.Background()
	rec, created, err := svc.EnsureDomain(ctx, "app_1", "production", "app.acme.dev")
	if err != nil || !created || !rec.IsPrimary {
		t.Fatalf("first ensure auto-registers primary: %+v %v %v", rec, created, err)
	}
	same, created, err := svc.EnsureDomain(ctx, "app_1", "production", "app.acme.dev")
	if err != nil || created || same.ID != rec.ID {
		t.Fatalf("known hostname returns existing: %+v %v %v", same, created, err)
	}
	if _, _, err := svc.EnsureDomain(ctx, "app_1", "production", "other.acme.dev"); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("second unregistered hostname: %v", err)
	}
}

func TestCheckDNS(t *testing.T) {
	newSvc := func(target Target, live bool, ips map[string][]string, errs ...string) (*Service, *memStore) {
		st := newMemStore()
		st.target, st.hasLive = target, live
		res := fakeResolver{ips: ips, err: map[string]bool{}}
		for _, h := range errs {
			res.err[h] = true
		}
		return &Service{Store: st, Resolver: res}, st
	}
	seed := func(st *memStore) Record {
		st.records["dom_1"] = Record{ID: "dom_1", ApplicationID: "app_1", Environment: "production", Hostname: "app.acme.dev", IsPrimary: true}
		return st.records["dom_1"]
	}
	ctx := context.Background()

	svc, st := newSvc(Target{}, false, nil)
	seed(st)
	if rec, err := svc.CheckDNS(ctx, "app_1", "dom_1"); err != nil || rec.DNSStatus != DNSPending {
		t.Fatalf("no live deployment → pending: %+v %v", rec, err)
	}
	svc, st = newSvc(Target{DeploymentID: "dep_1", ServerID: "srv_1", Address: "203.0.113.10"}, true,
		map[string][]string{"app.acme.dev": {"203.0.113.10"}})
	seed(st)
	if rec, err := svc.CheckDNS(ctx, "app_1", "dom_1"); err != nil || rec.DNSStatus != DNSOk || rec.DNSExpected != "203.0.113.10" || rec.DNSObserved != "203.0.113.10" || rec.DNSCheckedAt == nil {
		t.Fatalf("match → ok: %+v %v", rec, err)
	}
	svc, st = newSvc(Target{DeploymentID: "dep_1", ServerID: "srv_1", Address: "203.0.113.10"}, true,
		map[string][]string{"app.acme.dev": {"198.51.100.7"}})
	seed(st)
	if rec, _ := svc.CheckDNS(ctx, "app_1", "dom_1"); rec.DNSStatus != DNSMismatch {
		t.Fatalf("mismatch: %+v", rec)
	}
	svc, st = newSvc(Target{DeploymentID: "dep_1", ServerID: "srv_1", Address: "203.0.113.10"}, true, nil, "app.acme.dev")
	seed(st)
	if rec, _ := svc.CheckDNS(ctx, "app_1", "dom_1"); rec.DNSStatus != DNSError || rec.DNSObserved != "" {
		t.Fatalf("nxdomain: %+v", rec)
	}
	// Server address is itself a hostname: resolved sets are compared.
	svc, st = newSvc(Target{DeploymentID: "dep_1", ServerID: "srv_1", Address: "srv.example.net"}, true,
		map[string][]string{"app.acme.dev": {"10.0.0.5"}, "srv.example.net": {"10.0.0.5", "10.0.0.6"}})
	seed(st)
	if rec, _ := svc.CheckDNS(ctx, "app_1", "dom_1"); rec.DNSStatus != DNSOk {
		t.Fatalf("hostname target: %+v", rec)
	}
}
