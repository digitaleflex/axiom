package deployment

import (
	"sync"
	"time"
)

// Event types (docs/architecture/api-contract.md §14–§15).
const (
	EventCreated       = "deployment.created"
	EventStatusChanged = "deployment.status.changed"
	EventStepStarted   = "deployment.step.started"
	EventStepCompleted = "deployment.step.completed"
	EventStepFailed    = "deployment.step.failed"
	EventStepSkipped   = "deployment.step.skipped"
)

// Event is an ordered, persisted deployment event. Seq is strictly increasing
// per deployment and is used as the SSE event id for resumable streams.
type Event struct {
	ID           string         `json:"id"`
	Seq          int64          `json:"seq"`
	Type         string         `json:"type"`
	Version      int            `json:"version"`
	DeploymentID string         `json:"deploymentId"`
	OccurredAt   time.Time      `json:"occurredAt"`
	Data         map[string]any `json:"data,omitempty"`
}

// EventBus fans out committed events to live subscribers (in-process).
// Persistence is the source of truth; the bus is only a low-latency notifier.
type EventBus struct {
	mu   sync.RWMutex
	subs map[string]map[chan Event]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{subs: make(map[string]map[chan Event]struct{})}
}

// Subscribe returns a channel of events for deploymentID and an idempotent unsubscribe func.
func (b *EventBus) Subscribe(deploymentID string) (<-chan Event, func()) {
	ch := make(chan Event, 64)
	b.mu.Lock()
	if b.subs[deploymentID] == nil {
		b.subs[deploymentID] = make(map[chan Event]struct{})
	}
	b.subs[deploymentID][ch] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			if subscribers := b.subs[deploymentID]; subscribers != nil {
				delete(subscribers, ch)
				if len(subscribers) == 0 {
					delete(b.subs, deploymentID)
				}
			}
			close(ch)
			b.mu.Unlock()
		})
	}
}

// Publish delivers event to subscribers without blocking. A subscriber that
// falls behind misses live events and must resync from the store (by seq).
func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs[event.DeploymentID] {
		select {
		case ch <- event:
		default:
		}
	}
}
