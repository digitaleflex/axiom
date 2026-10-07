package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// Record is the authoritative deployment state.
type Record struct {
	ID            string     `json:"id"`
	Number        int        `json:"number"`
	ApplicationID string     `json:"applicationId"`
	ServerID      string     `json:"serverId"`
	Environment   string     `json:"environment"`
	PlanID        string     `json:"planId"`
	Status        State      `json:"status"`
	URL           string     `json:"url,omitempty"`
	ErrorCode     string     `json:"errorCode,omitempty"`
	CreatedBy     string     `json:"createdBy,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	CompletedAt   *time.Time `json:"completedAt,omitempty"`
}

// Step is the persisted state of one plan step for a deployment.
type Step struct {
	Name        string     `json:"name"`
	Position    int        `json:"position"`
	Status      StepStatus `json:"status"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	ExitCode    *int       `json:"exitCode,omitempty"`
	ErrorCode   string     `json:"errorCode,omitempty"`
}

// StepStatus values (docs/design/screens/deployment-progress §1.3).
type StepStatus string

const (
	StepQueued    StepStatus = "QUEUED"
	StepRunning   StepStatus = "RUNNING"
	StepCompleted StepStatus = "COMPLETED"
	StepFailed    StepStatus = "FAILED"
	StepSkipped   StepStatus = "SKIPPED"
	StepCancelled StepStatus = "CANCELLED"
)

// CreateInput creates a deployment from a plan. Server and environment are
// taken from the plan, never from the client, so they cannot diverge.
type CreateInput struct {
	ApplicationID string
	PlanID        string
	CreatedBy     string
	// IdempotencyKey is optional; when set, retries return the same deployment.
	IdempotencyKey string
}

// RequestHash fingerprints the request bound to an idempotency key.
func (in CreateInput) RequestHash() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{in.ApplicationID, in.PlanID}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// IdempotencyScope namespaces keys per application.
func (in CreateInput) IdempotencyScope() string { return "deployments.create:" + in.ApplicationID }

// StatusChange describes a requested transition and optional outcome fields.
type StatusChange struct {
	To        State
	URL       string
	ErrorCode string
}

// StepChange describes a step status update.
type StepChange struct {
	Name      string
	Status    StepStatus
	ExitCode  *int
	ErrorCode string
}

// Store persists deployments, steps, events and idempotency keys.
// Every mutating method is atomic and appends the corresponding event.
type Store interface {
	// Create inserts a PENDING deployment, its queued steps and a
	// deployment.created event. created=false means an idempotent replay.
	Create(ctx context.Context, in CreateInput) (rec Record, created bool, err error)
	Get(ctx context.Context, id string) (Record, error)
	List(ctx context.Context, f ListFilter) ([]Record, int, error)
	Steps(ctx context.Context, id string) ([]Step, error)
	// UpdateStatus locks the deployment, calls validate(current) and, when it
	// returns nil, applies the change and appends a status event.
	UpdateStatus(ctx context.Context, id string, change StatusChange, validate func(Record) error) (Record, Event, error)
	UpdateStep(ctx context.Context, id string, change StepChange) (Step, Event, error)
	// Events returns persisted events with seq > afterSeq, ordered by seq.
	Events(ctx context.Context, id string, afterSeq int64, limit int) ([]Event, error)
}

// ListFilter filters deployments of an application.
type ListFilter struct {
	ApplicationID string
	Environment   string
	Status        string
	Limit         int
	Offset        int
}
