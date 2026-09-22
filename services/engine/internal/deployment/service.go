package deployment

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Record struct {
	ID            string
	ApplicationID string
	ServerID      string
	Environment   string
	Status        State
	PlanID        string
}

type Repository interface {
	Create(ctx context.Context, id, applicationID, serverID, environment string) error
	SetStatus(ctx context.Context, id, status string) error
	Get(ctx context.Context, id string) (Record, error)
}

type Service struct {
	repo Repository
	bus  *EventBus
	mu   sync.Mutex
}

func NewService(repo Repository, bus *EventBus) *Service {
	if bus == nil {
		bus = NewEventBus()
	}
	return &Service{repo: repo, bus: bus}
}

func (s *Service) Events() *EventBus { return s.bus }

func (s *Service) Create(ctx context.Context, applicationID, serverID, environment, planID string) (Record, error) {
	if applicationID == "" || serverID == "" || environment == "" || planID == "" {
		return Record{}, fmt.Errorf("applicationID, serverID, environment and planID are required")
	}
	id := "dep_" + uuid.NewString()
	record := Record{ID: id, ApplicationID: applicationID, ServerID: serverID, Environment: environment, Status: StatePending, PlanID: planID}
	if err := s.repo.Create(ctx, id, applicationID, serverID, environment); err != nil {
		return Record{}, err
	}
	s.publish(record, "deployment.created", map[string]any{"planId": planID, "status": StatePending})
	return record, nil
}

func (s *Service) Transition(ctx context.Context, id string, to State) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return Record{}, err
	}
	if err := Transition(current.Status, to); err != nil {
		return current, err
	}
	if err := s.repo.SetStatus(ctx, id, string(to)); err != nil {
		return current, err
	}
	current.Status = to
	s.publish(current, "deployment.state.changed", map[string]any{"from": current.Status, "to": to})
	return current, nil
}

func (s *Service) publish(record Record, typ string, data any) {
	s.bus.Publish(Event{ID: "evt_" + uuid.NewString(), Type: typ, Version: 1, DeploymentID: record.ID, OccurredAt: time.Now().UTC(), Data: data})
}
