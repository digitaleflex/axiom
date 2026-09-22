package deployment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
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
	GetDomainRecord(ctx context.Context, id string) (Record, error)
}

type Service struct {
	repo       Repository
	bus        *EventBus
	mu         sync.Mutex
	idempotent map[string]Record
}

func NewService(repo Repository, bus *EventBus) *Service {
	if bus == nil {
		bus = NewEventBus()
	}
	return &Service{repo: repo, bus: bus, idempotent: make(map[string]Record)}
}

func (s *Service) Events() *EventBus { return s.bus }

func (s *Service) Create(ctx context.Context, applicationID, serverID, environment, planID string) (Record, error) {
	return s.CreateIdempotent(ctx, "", applicationID, serverID, environment, planID)
}

func (s *Service) CreateIdempotent(ctx context.Context, key, applicationID, serverID, environment, planID string) (Record, error) {
	if applicationID == "" || serverID == "" || environment == "" || planID == "" {
		return Record{}, fmt.Errorf("applicationID, serverID, environment and planID are required")
	}
	if key != "" {
		s.mu.Lock()
		if existing, ok := s.idempotent[key]; ok {
			s.mu.Unlock()
			return existing, nil
		}
		s.mu.Unlock()
	}

	id := "dep_" + randomID()
	record := Record{ID: id, ApplicationID: applicationID, ServerID: serverID, Environment: environment, Status: StatePending, PlanID: planID}
	if err := s.repo.Create(ctx, id, applicationID, serverID, environment); err != nil {
		return Record{}, err
	}
	if key != "" {
		s.mu.Lock()
		s.idempotent[key] = record
		s.mu.Unlock()
	}
	s.publish(record, "deployment.created", map[string]any{"planId": planID, "status": StatePending})
	return record, nil
}

func (s *Service) Transition(ctx context.Context, id string, to State) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.repo.GetDomainRecord(ctx, id)
	if err != nil {
		return Record{}, err
	}
	if err := Transition(current.Status, to); err != nil {
		return current, err
	}
	from := current.Status
	if err := s.repo.SetStatus(ctx, id, string(to)); err != nil {
		return current, err
	}
	current.Status = to
	s.publish(current, "deployment.state.changed", map[string]any{"from": from, "to": to})
	return current, nil
}

func (s *Service) publish(record Record, typ string, data any) {
	s.bus.Publish(Event{ID: "evt_" + randomID(), Type: typ, Version: 1, DeploymentID: record.ID, OccurredAt: time.Now().UTC(), Data: data})
}

func randomID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
