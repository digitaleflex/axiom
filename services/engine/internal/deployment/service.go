package deployment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/digitaleflex/axiom/services/engine/internal/health"
)

// Service is the only writer of deployment state (ADR-0010). It validates
// transitions against the state machine and persists them atomically.
type Service struct {
	store Store
	bus   *EventBus
}

func NewService(store Store, bus *EventBus) *Service {
	if bus == nil {
		bus = NewEventBus()
	}
	return &Service{store: store, bus: bus}
}

// Events returns the live event bus.
func (s *Service) Events() *EventBus { return s.bus }

// Store returns the underlying store (read access for API handlers).
func (s *Service) Store() Store { return s.store }

// ErrInvalidInput marks client input errors.
var ErrInvalidInput = errors.New("invalid deployment input")

// Create creates a deployment from a plan. With an idempotency key, retries
// (including concurrent ones and across restarts) resolve to one deployment.
func (s *Service) Create(ctx context.Context, in CreateInput) (Record, bool, error) {
	if strings.TrimSpace(in.ApplicationID) == "" || strings.TrimSpace(in.PlanID) == "" {
		return Record{}, false, fmt.Errorf("%w: applicationId and planId are required", ErrInvalidInput)
	}
	if len(in.IdempotencyKey) > 255 {
		return Record{}, false, fmt.Errorf("%w: Idempotency-Key exceeds 255 characters", ErrInvalidInput)
	}
	rec, created, err := s.store.Create(ctx, in)
	if err != nil {
		return Record{}, false, err
	}
	if created {
		if events, err := s.store.Events(ctx, rec.ID, 0, 1); err == nil {
			for _, e := range events {
				s.bus.Publish(e)
			}
		}
	}
	return rec, created, nil
}

// Get returns a deployment.
func (s *Service) Get(ctx context.Context, id string) (Record, error) { return s.store.Get(ctx, id) }

// Transition moves a deployment to a new state if the state machine allows it.
func (s *Service) Transition(ctx context.Context, id string, to State) (Record, error) {
	return s.apply(ctx, id, StatusChange{To: to}, nil)
}

// MarkLive records LIVE with the public URL. LIVE is only reachable from VERIFYING.
func (s *Service) MarkLive(ctx context.Context, id, url string) (Record, error) {
	return s.apply(ctx, id, StatusChange{To: StateLive, URL: url}, nil)
}

// Fail records FAILED with a canonical error code (API §18).
func (s *Service) Fail(ctx context.Context, id, errorCode string) (Record, error) {
	return s.apply(ctx, id, StatusChange{To: StateFailed, ErrorCode: errorCode}, nil)
}

// Cancel records CANCELLED when the current state permits it.
func (s *Service) Cancel(ctx context.Context, id string) (Record, error) {
	return s.apply(ctx, id, StatusChange{To: StateCancelled}, func(r Record) error {
		if !r.Status.Cancellable() {
			return fmt.Errorf("%w: deployment in %s cannot be cancelled", ErrInvalidTransition, r.Status)
		}
		return nil
	})
}

// RecordStep persists a step status change and publishes its event.
func (s *Service) RecordStep(ctx context.Context, id string, change StepChange) error {
	_, ev, err := s.store.UpdateStep(ctx, id, change)
	if err != nil {
		return err
	}
	s.bus.Publish(ev)
	return nil
}

// healthEventStore is implemented by stores that can append health events
// (MemoryStore and the PostgreSQL store). It is resolved by type assertion
// so the Store interface stays unchanged.
type healthEventStore interface {
	AppendEvent(ctx context.Context, id, typ string, data map[string]any) (Event, error)
}

// RecordHealth persists a health probe result as a deployment event (#65):
// health.passed when the report satisfies the policy, health.failed
// otherwise. The event data carries status (HEALTHY/UNHEALTHY), statusCode,
// latencyMs, path, checkedAt and attempt so GET /deployments/{id}/health
// can be served from the persisted event log (API contract §14, §16).
func (s *Service) RecordHealth(ctx context.Context, id string, policy health.Policy, report health.ProbeReport) error {
	appender, ok := s.store.(healthEventStore)
	if !ok {
		return fmt.Errorf("store %T does not support health events", s.store)
	}
	status, reason := policy.Evaluate(report)
	eventType := health.EventPassed
	if status != health.StatusHealthy {
		eventType = health.EventFailed
	}
	data := map[string]any{
		"status":     status,
		"statusCode": report.StatusCode,
		"latencyMs":  report.LatencyMs,
		"path":       policy.Path,
		"checkedAt":  report.CheckedAt,
		"attempt":    report.Attempt,
	}
	if report.Body != "" {
		data["body"] = report.Body
	}
	if reason != "" {
		data["reason"] = reason
	}
	ev, err := appender.AppendEvent(ctx, id, eventType, data)
	if err != nil {
		return err
	}
	s.bus.Publish(ev)
	return nil
}

func (s *Service) apply(ctx context.Context, id string, change StatusChange, extra func(Record) error) (Record, error) {
	rec, ev, err := s.store.UpdateStatus(ctx, id, change, func(current Record) error {
		if extra != nil {
			if err := extra(current); err != nil {
				return err
			}
		}
		return Transition(current.Status, change.To)
	})
	if err != nil {
		return rec, err
	}
	s.bus.Publish(ev)
	return rec, nil
}
