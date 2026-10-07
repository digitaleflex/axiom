package audit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// newID returns an opaque event ID (same shape as logs.NewID).
func newID() string {
	var b [14]byte
	binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixNano()))
	if _, err := rand.Read(b[8:]); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	return "aud_" + hex.EncodeToString(b[:])
}

// PGStore is the PostgreSQL implementation of Store (issue #128), backed by
// the audit_events table created by migration 013.
type PGStore struct {
	db  *sql.DB
	now func() time.Time
}

// Compile-time interface check.
var _ Store = (*PGStore)(nil)

// NewPGStore returns a Store on PostgreSQL.
func NewPGStore(db *sql.DB) *PGStore {
	return &PGStore{db: db, now: func() time.Time { return time.Now().UTC() }}
}

// Record inserts one event. The event must already be redacted and
// detail-filtered (Service.Record does this); the store persists verbatim.
func (s *PGStore) Record(ctx context.Context, e Event) error {
	if e.ID == "" {
		e.ID = newID()
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = s.now()
	}
	details, err := json.Marshal(e.Details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_events
			(id, actor_id, actor_name, action, target_type, target_id, result,
			 error_code, request_id, correlation_id, deployment_id, owner_id,
			 occurred_at, details)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		e.ID, e.ActorID, e.ActorName, e.Action, e.TargetType, e.TargetID, e.Result,
		e.ErrorCode, e.RequestID, e.CorrelationID, e.DeploymentID, e.OwnerID,
		e.OccurredAt.UTC(), details); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

// List returns events matching f, newest first, bounded by f.Limit.
func (s *PGStore) List(ctx context.Context, f Filter) ([]Event, error) {
	const maxLimit = 1000
	limit := f.Limit
	if limit <= 0 || limit > maxLimit {
		limit = 100
	}

	where := `WHERE ($1 = '' OR actor_id = $1)
	          AND ($2 = '' OR target_type = $2)
	          AND ($3 = '' OR target_id = $3)
	          AND ($4 = '' OR owner_id = $4)`
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, actor_id, actor_name, action, target_type, target_id, result,
		        error_code, request_id, correlation_id, deployment_id, owner_id,
		        occurred_at, details
		 FROM audit_events `+where+`
		 ORDER BY occurred_at DESC, id DESC
		 LIMIT $5`,
		f.ActorID, f.TargetType, f.TargetID, f.OwnerID, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	out := []Event{}
	for rows.Next() {
		var e Event
		var details []byte
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorName, &e.Action, &e.TargetType,
			&e.TargetID, &e.Result, &e.ErrorCode, &e.RequestID, &e.CorrelationID,
			&e.DeploymentID, &e.OwnerID, &e.OccurredAt, &details); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		e.OccurredAt = e.OccurredAt.UTC()
		if len(details) > 0 && string(details) != "null" {
			_ = json.Unmarshal(details, &e.Details)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return out, nil
}

// setNow overrides the clock; used by tests for deterministic timestamps.
func (s *PGStore) setNow(fn func() time.Time) { s.now = fn }
