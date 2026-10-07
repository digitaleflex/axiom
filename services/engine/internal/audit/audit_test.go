package audit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/logs"
)

// --- In-memory store for round-trip tests -----------------------------------

type memStore struct{ events []Event }

func (m *memStore) Record(_ context.Context, e Event) error {
	if e.ID == "" {
		e.ID = "aud_test"
	}
	m.events = append(m.events, e)
	return nil
}

func (m *memStore) List(_ context.Context, f Filter) ([]Event, error) {
	out := []Event{}
	for _, e := range m.events {
		if f.ActorID != "" && e.ActorID != f.ActorID {
			continue
		}
		if f.TargetType != "" && e.TargetType != f.TargetType {
			continue
		}
		if f.TargetID != "" && e.TargetID != f.TargetID {
			continue
		}
		if f.OwnerID != "" && e.OwnerID != f.OwnerID {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// --- Service unit tests (no database) ---------------------------------------

func TestServiceRecordRedactsSecrets(t *testing.T) {
	ms := &memStore{}
	s := NewService(ms)
	e := Event{
		ActorID:    "usr_1",
		Action:     "deployment.create",
		TargetType: "deployment",
		TargetID:   "dep_1",
		Result:     ResultOK,
		Details:    map[string]string{"reason": "token=supersecret", "environment": "production"},
	}
	if err := s.Record(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	stored := ms.events[0]
	if strings.Contains(stored.Details["reason"], "supersecret") {
		t.Fatalf("secret not redacted: %q", stored.Details["reason"])
	}
	if !strings.Contains(stored.Details["reason"], logs.RedactionMarker) {
		t.Fatalf("redaction marker missing: %q", stored.Details["reason"])
	}
}

func TestServiceRecordDropsDisallowedDetailKeys(t *testing.T) {
	ms := &memStore{}
	s := NewService(ms)
	e := Event{
		ActorID:    "usr_1",
		Action:     "appconfig.set",
		TargetType: "config",
		TargetID:   "app_1/DATABASE_URL",
		Result:     ResultOK,
		Details: map[string]string{
			"name":        "DATABASE_URL",
			"password":    "hunter2",    // disallowed key: dropped
			"accessToken": "abc123",     // disallowed key: dropped
			"environment": "production", // allowed
		},
	}
	if err := s.Record(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	stored := ms.events[0]
	if _, ok := stored.Details["password"]; ok {
		t.Fatal("disallowed key password must be dropped")
	}
	if _, ok := stored.Details["accessToken"]; ok {
		t.Fatal("disallowed key accessToken must be dropped")
	}
	if stored.Details["name"] != "DATABASE_URL" || stored.Details["environment"] != "production" {
		t.Fatalf("allowed keys must survive: %v", stored.Details)
	}
}

func TestServiceRecordNormalizesResult(t *testing.T) {
	ms := &memStore{}
	s := NewService(ms)
	e := Event{ActorID: "usr_1", Action: "x", TargetType: "t", TargetID: "i"} // Result empty
	if err := s.Record(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if ms.events[0].Result != ResultOK {
		t.Fatalf("empty result must normalize to ok, got %q", ms.events[0].Result)
	}
	e = Event{ActorID: "usr_1", Action: "x", TargetType: "t", TargetID: "i", Result: "bogus"}
	if err := s.Record(context.Background(), e); err == nil {
		t.Fatal("invalid result must be rejected")
	}
}

func TestServiceNilStoreIsNoop(t *testing.T) {
	s := NewService(nil)
	if err := s.Record(context.Background(), Event{ActorID: "usr_1", Action: "x", TargetType: "t", TargetID: "i"}); err != nil {
		t.Fatalf("nil store Record must be a no-op: %v", err)
	}
	items, err := s.List(context.Background(), Filter{})
	if err != nil || len(items) != 0 {
		t.Fatalf("nil store List = %v, %v", items, err)
	}
}

func TestRecordQueryRoundTrip(t *testing.T) {
	ms := &memStore{}
	s := NewService(ms)
	ctx := context.Background()
	in := Event{
		ActorID: "usr_1", ActorName: "Jane", Action: "deployment.create",
		TargetType: "deployment", TargetID: "dep_1", Result: ResultOK,
		RequestID: "req_1", CorrelationID: "corr_1", DeploymentID: "dep_1",
		OccurredAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC),
		Details:    map[string]string{"environment": "production"},
	}
	if err := s.Record(ctx, in); err != nil {
		t.Fatal(err)
	}
	if len(ms.events) != 1 {
		t.Fatalf("stored %d events, want 1", len(ms.events))
	}
	got := ms.events[0]
	if got.ID == "" {
		t.Fatal("event ID must be assigned by the store")
	}
	if got.ActorID != "usr_1" || got.Action != "deployment.create" || got.TargetID != "dep_1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.OccurredAt.IsZero() {
		t.Fatal("occurred_at must be set")
	}

	// Query by actor.
	items, err := s.List(ctx, Filter{ActorID: "usr_1"})
	if err != nil || len(items) != 1 {
		t.Fatalf("actor query = %v, %v", items, err)
	}
	// Query by target.
	items, err = s.List(ctx, Filter{TargetType: "deployment", TargetID: "dep_1"})
	if err != nil || len(items) != 1 {
		t.Fatalf("target query = %v, %v", items, err)
	}
	// Query by owner.
	items, err = s.List(ctx, Filter{OwnerID: "usr_1"})
	if err != nil || len(items) != 0 {
		t.Fatalf("owner query (no owner set) = %v, %v", items, err)
	}
}
