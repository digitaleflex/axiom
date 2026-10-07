package api

import (
	"context"
	"sync"

	"github.com/digitaleflex/axiom/services/engine/internal/audit"
)

// fakeAuditService captures audit events in memory for tests.
type fakeAuditService struct {
	mu     sync.Mutex
	events []audit.Event
}

func (f *fakeAuditService) Record(_ context.Context, e audit.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func (f *fakeAuditService) List(_ context.Context, filter audit.Filter) ([]audit.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []audit.Event{}
	for _, e := range f.events {
		if filter.ActorID != "" && e.ActorID != filter.ActorID {
			continue
		}
		if filter.TargetID != "" && e.TargetID != filter.TargetID {
			continue
		}
		if filter.OwnerID != "" && e.OwnerID != filter.OwnerID {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeAuditService) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

func (f *fakeAuditService) action(action string) []audit.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []audit.Event
	for _, e := range f.events {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}
