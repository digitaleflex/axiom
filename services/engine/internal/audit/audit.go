// Package audit records security-relevant operations (issue #128): who did
// what to which resource, with which result, correlated to the request and
// deployment. Events are persisted redacted — values matching secret
// patterns are replaced before storage — and detail keys are allow-listed,
// so secrets can never reach the audit trail.
package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/logs"
)

// Result values for Event.Result.
const (
	ResultOK    = "ok"
	ResultError = "error"
)

// Event is one security-relevant action. Details keys are allow-listed by
// Service.Record; values are redacted via logs.Redact before persistence.
type Event struct {
	ID            string `json:"id"`
	ActorID       string `json:"actorId"`
	ActorName     string `json:"actorName,omitempty"`
	Action        string `json:"action"`
	TargetType    string `json:"targetType"`
	TargetID      string `json:"targetId"`
	Result        string `json:"result"`
	ErrorCode     string `json:"errorCode,omitempty"`
	RequestID     string `json:"requestId,omitempty"`
	CorrelationID string `json:"correlationId,omitempty"`
	DeploymentID  string `json:"deploymentId,omitempty"`
	// OwnerID denormalizes the resource owner so reads can be scoped to
	// resources the caller may read without joining every resource table.
	OwnerID    string            `json:"ownerId,omitempty"`
	OccurredAt time.Time         `json:"occurredAt"`
	Details    map[string]string `json:"details,omitempty"`
}

// allowedDetailKeys is the closed set of detail keys that may be persisted.
// Anything else is dropped by Service.Record. Never add a key whose value
// could carry a secret (tokens, passwords, credentials, keys).
var allowedDetailKeys = map[string]bool{
	"reason":        true,
	"environment":   true,
	"hostname":      true,
	"name":          true,
	"ref":           true,
	"root":          true,
	"status":        true,
	"serverId":      true,
	"planId":        true,
	"repositoryId":  true,
	"connectionId":  true,
	"agentId":       true,
	"sessionCount":  true,
	"credentialAge": true,
}

// Filter narrows an audit query. Empty fields are not filtered.
type Filter struct {
	ActorID    string
	TargetType string
	TargetID   string
	// OwnerID scopes results to resources the caller may read.
	OwnerID string
	Limit   int
}

// Store persists audit events.
type Store interface {
	Record(ctx context.Context, e Event) error
	List(ctx context.Context, f Filter) ([]Event, error)
}

// Service records audit events: it normalizes the result, redacts every
// string field through logs.Redact and drops disallowed detail keys before
// handing the event to the store.
type Service struct {
	store Store
	now   func() time.Time
}

// NewService builds a Service over store. A nil store makes Record a no-op
// that returns nil (used when the audit trail is not configured).
func NewService(store Store) *Service {
	return &Service{store: store, now: func() time.Time { return time.Now().UTC() }}
}

// Record validates, redacts and persists one event. Redaction runs before
// persistence, never at read time: stored events never contain raw secrets.
// A nil store makes Record a no-op (after redaction) for unconfigured
// deployments.
func (s *Service) Record(ctx context.Context, e Event) error {
	if e.Result == "" {
		e.Result = ResultOK
	}
	if e.Result != ResultOK && e.Result != ResultError {
		return fmt.Errorf("audit: invalid result %q", e.Result)
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = s.now()
	}
	// Redact every string field; a secret in any of them is replaced by
	// logs.RedactionMarker before it can reach the database.
	e.ActorID = logs.Redact(e.ActorID)
	e.ActorName = logs.Redact(e.ActorName)
	e.Action = logs.Redact(e.Action)
	e.TargetType = logs.Redact(e.TargetType)
	e.TargetID = logs.Redact(e.TargetID)
	e.ErrorCode = logs.Redact(e.ErrorCode)
	e.RequestID = logs.Redact(e.RequestID)
	e.CorrelationID = logs.Redact(e.CorrelationID)
	e.DeploymentID = logs.Redact(e.DeploymentID)
	e.OwnerID = logs.Redact(e.OwnerID)
	if len(e.Details) > 0 {
		clean := make(map[string]string, len(e.Details))
		for k, v := range e.Details {
			if !allowedDetailKeys[k] {
				continue // disallowed keys are dropped, never persisted
			}
			clean[k] = logs.Redact(v)
		}
		e.Details = clean
	}
	if s.store == nil {
		return nil
	}
	return s.store.Record(ctx, e)
}

// List returns events matching f, newest first.
func (s *Service) List(ctx context.Context, f Filter) ([]Event, error) {
	if s.store == nil {
		return []Event{}, nil
	}
	return s.store.List(ctx, f)
}
