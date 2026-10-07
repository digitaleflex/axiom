package deployment

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func newTestService() (*Service, *MemoryStore) {
	store := NewMemoryStore()
	store.AddPlan(MemoryPlan{ID: "plan_1", ApplicationID: "app_1", ServerID: "srv_1", Environment: "production", Steps: []string{"BUILD", "VERIFY"}})
	store.AddPlan(MemoryPlan{ID: "plan_2", ApplicationID: "app_1", ServerID: "srv_1", Environment: "staging", Steps: []string{"BUILD"}})
	return NewService(store, nil), store
}

func TestCreateIdempotent(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	in := CreateInput{ApplicationID: "app_1", PlanID: "plan_1", IdempotencyKey: "request-1"}
	first, created, err := svc.Create(ctx, in)
	if err != nil || !created {
		t.Fatalf("first create: created=%v err=%v", created, err)
	}
	second, created, err := svc.Create(ctx, in)
	if err != nil || created || first.ID != second.ID {
		t.Fatalf("replay must return %s without creating, got %s created=%v err=%v", first.ID, second.ID, created, err)
	}
	if _, _, err := svc.Create(ctx, CreateInput{ApplicationID: "app_1", PlanID: "plan_2", IdempotencyKey: "request-1"}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("reusing a key with a different request must conflict, got %v", err)
	}
}

func TestConcurrentCreateResolvesToOne(t *testing.T) {
	svc, _ := newTestService()
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, _, err := svc.Create(context.Background(), CreateInput{ApplicationID: "app_1", PlanID: "plan_1", IdempotencyKey: "k"})
			if err == nil {
				ids <- r.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Fatalf("expected one deployment, got %d", len(seen))
	}
}

func TestPlanRules(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	if _, _, err := svc.Create(ctx, CreateInput{ApplicationID: "app_2", PlanID: "plan_1"}); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("plan of another application must be rejected, got %v", err)
	}
	if _, _, err := svc.Create(ctx, CreateInput{ApplicationID: "app_1", PlanID: "plan_1"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Create(ctx, CreateInput{ApplicationID: "app_1", PlanID: "plan_1"}); !errors.Is(err, ErrPlanAlreadyUsed) {
		t.Fatalf("plans are single-use, got %v", err)
	}
	if _, _, err := svc.Create(ctx, CreateInput{ApplicationID: "app_1"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing plan must be invalid input, got %v", err)
	}
}

func TestServiceTransitionAndEvents(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	rec, _, err := svc.Create(ctx, CreateInput{ApplicationID: "app_1", PlanID: "plan_1"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Number != 1 || rec.Environment != "production" || rec.ServerID != "srv_1" {
		t.Fatalf("server/environment must come from the plan: %+v", rec)
	}
	ch, unsubscribe := svc.Events().Subscribe(rec.ID)
	defer unsubscribe()

	rec, err = svc.Transition(ctx, rec.ID, StateAnalyzing)
	if err != nil || rec.Status != StateAnalyzing || rec.StartedAt == nil {
		t.Fatalf("transition: %+v %v", rec, err)
	}
	if ev := <-ch; ev.Type != EventStatusChanged || ev.Seq != 2 {
		t.Fatalf("unexpected live event %+v", ev)
	}
	if _, err := svc.Transition(ctx, rec.ID, StateLive); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ANALYZING -> LIVE must be rejected, got %v", err)
	}
	if err := svc.RecordStep(ctx, rec.ID, StepChange{Name: "BUILD", Status: StepRunning}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordStep(ctx, rec.ID, StepChange{Name: "PREPARE", Status: StepRunning}); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("unknown step must be rejected, got %v", err)
	}
	events, _ := store.Events(ctx, rec.ID, 0, 0)
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Fatalf("events must have contiguous seq, got %d at %d", e.Seq, i)
		}
	}
	if _, err := svc.Fail(ctx, rec.ID, "BUILD_FAILED"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(ctx, rec.ID)
	if got.Status != StateFailed || got.ErrorCode != "BUILD_FAILED" || got.CompletedAt == nil {
		t.Fatalf("unexpected failed record %+v", got)
	}
}

func TestCancelRules(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	rec, _, _ := svc.Create(ctx, CreateInput{ApplicationID: "app_1", PlanID: "plan_1"})
	for _, s := range []State{StateAnalyzing, StatePlanning, StateBuilding, StateDeploying, StateVerifying} {
		if _, err := svc.Transition(ctx, rec.ID, s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Cancel(ctx, rec.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("VERIFYING must not be cancellable, got %v", err)
	}
	rec2, _, _ := svc.Create(ctx, CreateInput{ApplicationID: "app_1", PlanID: "plan_2"})
	if r, err := svc.Cancel(ctx, rec2.ID); err != nil || r.Status != StateCancelled {
		t.Fatalf("PENDING must be cancellable: %+v %v", r, err)
	}
}
