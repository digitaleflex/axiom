package deployment

import (
	"context"
	"testing"
)

func TestCorrelationIDPropagatesToEvents(t *testing.T) {
	store := NewMemoryStore()
	store.AddPlan(MemoryPlan{ID: "plan_1", ApplicationID: "app_1", ServerID: "srv_1", Environment: "production", Steps: []string{"BUILD", "VERIFY"}})
	svc := NewService(store, nil)
	ctx := context.Background()

	rec, _, err := svc.Create(ctx, CreateInput{ApplicationID: "app_1", PlanID: "plan_1", CorrelationID: "req_0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.CorrelationID != "req_0123456789abcdef" {
		t.Fatalf("record correlation = %q", rec.CorrelationID)
	}
	if _, err := svc.Transition(ctx, rec.ID, StateAnalyzing); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordStep(ctx, rec.ID, StepChange{Name: "BUILD", Status: StepRunning}); err != nil {
		t.Fatal(err)
	}

	events, err := store.Events(ctx, rec.ID, 0, 0)
	if err != nil || len(events) == 0 {
		t.Fatalf("events = %v %v", events, err)
	}
	for _, ev := range events {
		if ev.Data["correlationId"] != "req_0123456789abcdef" {
			t.Fatalf("event %s missing correlation: %+v", ev.Type, ev.Data)
		}
	}
}

func TestNoCorrelationLeavesEventsClean(t *testing.T) {
	store := NewMemoryStore()
	store.AddPlan(MemoryPlan{ID: "plan_1", ApplicationID: "app_1", ServerID: "srv_1", Environment: "production", Steps: []string{"BUILD"}})
	svc := NewService(store, nil)
	rec, _, _ := svc.Create(context.Background(), CreateInput{ApplicationID: "app_1", PlanID: "plan_1"})
	events, _ := store.Events(context.Background(), rec.ID, 0, 0)
	if _, ok := events[0].Data["correlationId"]; ok {
		t.Fatalf("empty correlation must not be added: %+v", events[0].Data)
	}
}
