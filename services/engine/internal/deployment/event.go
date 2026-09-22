package deployment

import (
	"sync"
	"time"
)

type Event struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Version      int       `json:"version"`
	DeploymentID string    `json:"deploymentId"`
	OccurredAt   time.Time `json:"occurredAt"`
	Data         any       `json:"data,omitempty"`
}

type EventBus struct {
	mu   sync.RWMutex
	subs map[string]map[chan Event]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{subs: make(map[string]map[chan Event]struct{})}
}

func (b *EventBus) Subscribe(deploymentID string) (<-chan Event, func()) {
	ch := make(chan Event, 32)
	b.mu.Lock()
	if b.subs[deploymentID] == nil {
		b.subs[deploymentID] = make(map[chan Event]struct{})
	}
	b.subs[deploymentID][ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if subscribers := b.subs[deploymentID]; subscribers != nil {
			delete(subscribers, ch)
			if len(subscribers) == 0 {
				delete(b.subs, deploymentID)
			}
		}
		close(ch)
		b.mu.Unlock()
	}
}

func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs[event.DeploymentID] {
		select {
		case ch <- event:
		default:
			// A slow subscriber must never block the deployment engine.
		}
	}
}
