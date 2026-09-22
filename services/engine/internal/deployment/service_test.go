package deployment

import (
	"context"
	"testing"
)

type fakeRepo struct{ records map[string]Record }

func newFakeRepo() *fakeRepo { return &fakeRepo{records: make(map[string]Record)} }

func (r *fakeRepo) Create(_ context.Context, id, applicationID, serverID, environment string) error {
	r.records[id] = Record{ID: id, ApplicationID: applicationID, ServerID: serverID, Environment: environment, Status: StatePending}
	return nil
}
func (r *fakeRepo) SetStatus(_ context.Context, id, status string) error {
	v := r.records[id]
	v.Status = State(status)
	r.records[id] = v
	return nil
}
func (r *fakeRepo) GetDomainRecord(_ context.Context, id string) (Record, error) {
	return r.records[id], nil
}

func TestCreateIdempotent(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	ctx := context.Background()

	first, err := svc.CreateIdempotent(ctx, "request-1", "app_1", "srv_1", "production", "plan_1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateIdempotent(ctx, "request-1", "app_1", "srv_1", "production", "plan_1")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected idempotent request to return %s, got %s", first.ID, second.ID)
	}
}

func TestServiceTransition(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	ctx := context.Background()

	record, err := svc.Create(ctx, "app_1", "srv_1", "production", "plan_1")
	if err != nil {
		t.Fatal(err)
	}
	record, err = svc.Transition(ctx, record.ID, StateAnalyzing)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StateAnalyzing {
		t.Fatalf("expected ANALYZING, got %s", record.Status)
	}
	if _, err := svc.Transition(ctx, record.ID, StateLive); err == nil {
		t.Fatal("expected invalid PENDING/ANALYZING -> LIVE transition to fail")
	}
}
