// Package deployment is the PostgreSQL implementation of the deployment
// store (issue #116): durable deployments, steps, ordered events and
// idempotency keys, with every mutation in a single transaction.
package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	domain "github.com/digitaleflex/axiom/services/engine/internal/deployment"
)

// Store implements domain.Store on PostgreSQL.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

var _ domain.Store = (*Store)(nil)

func New(db *sql.DB) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

const recordColumns = `id, COALESCE(number, 0), application_id, server_id, environment, COALESCE(plan_id, ''), status,
	COALESCE(url, ''), COALESCE(error_code, ''), COALESCE(created_by, ''), COALESCE(correlation_id, ''), created_at, updated_at, started_at, completed_at`

type scanner interface{ Scan(...any) error }

func scanRecord(s scanner) (domain.Record, error) {
	var r domain.Record
	var started, completed sql.NullTime
	err := s.Scan(&r.ID, &r.Number, &r.ApplicationID, &r.ServerID, &r.Environment, &r.PlanID, &r.Status,
		&r.URL, &r.ErrorCode, &r.CreatedBy, &r.CorrelationID, &r.CreatedAt, &r.UpdatedAt, &started, &completed)
	if err != nil {
		return domain.Record{}, err
	}
	r.CreatedAt, r.UpdatedAt = r.CreatedAt.UTC(), r.UpdatedAt.UTC()
	if started.Valid {
		t := started.Time.UTC()
		r.StartedAt = &t
	}
	if completed.Valid {
		t := completed.Time.UTC()
		r.CompletedAt = &t
	}
	return r, nil
}

// Create inserts a deployment from its plan. Concurrent requests with the same
// idempotency key serialize on the idempotency_keys primary key: the loser
// waits for the winner's commit and then returns the winner's deployment.
func (s *Store) Create(ctx context.Context, in domain.CreateInput) (domain.Record, bool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		rec, created, err := s.create(ctx, in)
		if errors.Is(err, errRetry) {
			continue
		}
		return rec, created, err
	}
	return domain.Record{}, false, fmt.Errorf("create deployment: idempotency retry exhausted")
}

var errRetry = errors.New("retry")

func (s *Store) create(ctx context.Context, in domain.CreateInput) (rec domain.Record, created bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Record{}, false, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	id := domain.NewID("dep")
	if in.IdempotencyKey != "" {
		res, err := tx.ExecContext(ctx, `INSERT INTO idempotency_keys (scope, key, request_hash, resource_id)
			VALUES ($1, $2, $3, $4) ON CONFLICT (scope, key) DO NOTHING`,
			in.IdempotencyScope(), in.IdempotencyKey, in.RequestHash(), id)
		if err != nil {
			return domain.Record{}, false, fmt.Errorf("reserve idempotency key: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			// Key already committed by another request: replay or conflict.
			var hash, existing string
			err := tx.QueryRowContext(ctx, `SELECT request_hash, resource_id FROM idempotency_keys WHERE scope = $1 AND key = $2`,
				in.IdempotencyScope(), in.IdempotencyKey).Scan(&hash, &existing)
			if errors.Is(err, sql.ErrNoRows) {
				return domain.Record{}, false, errRetry // winner rolled back
			}
			if err != nil {
				return domain.Record{}, false, fmt.Errorf("read idempotency key: %w", err)
			}
			if hash != in.RequestHash() {
				return domain.Record{}, false, domain.ErrIdempotencyConflict
			}
			r, err := scanRecord(tx.QueryRowContext(ctx, `SELECT `+recordColumns+` FROM deployments WHERE id = $1`, existing))
			if err != nil {
				return domain.Record{}, false, fmt.Errorf("read idempotent deployment: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return domain.Record{}, false, fmt.Errorf("commit: %w", err)
			}
			return r, false, nil
		}
	}

	// Lock the application row: validates existence and serializes numbering.
	var appID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM applications WHERE id = $1 FOR UPDATE`, in.ApplicationID).Scan(&appID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Record{}, false, domain.ErrPlanNotFound
		}
		return domain.Record{}, false, fmt.Errorf("lock application: %w", err)
	}

	var serverID, environment string
	var body []byte
	err = tx.QueryRowContext(ctx, `SELECT server_id, environment, body FROM deployment_plans
		WHERE id = $1 AND application_id = $2 AND status = 'READY'`, in.PlanID, in.ApplicationID).Scan(&serverID, &environment, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Record{}, false, domain.ErrPlanNotFound
	}
	if err != nil {
		return domain.Record{}, false, fmt.Errorf("read plan: %w", err)
	}

	now := s.now()
	var createdBy any
	if in.CreatedBy != "" {
		createdBy = in.CreatedBy
	}
	row := tx.QueryRowContext(ctx, `INSERT INTO deployments
			(id, application_id, server_id, environment, plan_id, status, number, created_by, correlation_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'PENDING',
			(SELECT COALESCE(MAX(number), 0) + 1 FROM deployments WHERE application_id = $2), $6, $8, $7, $7)
		RETURNING `+recordColumns, id, in.ApplicationID, serverID, environment, in.PlanID, createdBy, now, in.CorrelationID)
	rec, err = scanRecord(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "deployments_plan_id_key" {
			return domain.Record{}, false, domain.ErrPlanAlreadyUsed
		}
		return domain.Record{}, false, fmt.Errorf("insert deployment: %w", err)
	}

	var plan struct {
		Steps []string `json:"steps"`
	}
	if err := json.Unmarshal(body, &plan); err != nil {
		return domain.Record{}, false, fmt.Errorf("decode plan steps: %w", err)
	}
	for i, name := range plan.Steps {
		if _, err := tx.ExecContext(ctx, `INSERT INTO deployment_steps (deployment_id, name, position) VALUES ($1, $2, $3)`, rec.ID, name, i+1); err != nil {
			return domain.Record{}, false, fmt.Errorf("insert step %s: %w", name, err)
		}
	}
	if _, err := appendEvent(ctx, tx, rec.ID, domain.EventCreated, domain.WithCorrelation(map[string]any{"status": domain.StatePending, "planId": in.PlanID, "number": rec.Number}, rec.CorrelationID), now); err != nil {
		return domain.Record{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Record{}, false, fmt.Errorf("commit: %w", err)
	}
	return rec, true, nil
}

func (s *Store) Get(ctx context.Context, id string) (domain.Record, error) {
	r, err := scanRecord(s.db.QueryRowContext(ctx, `SELECT `+recordColumns+` FROM deployments WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Record{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Record{}, fmt.Errorf("get deployment: %w", err)
	}
	return r, nil
}

func (s *Store) List(ctx context.Context, f domain.ListFilter) ([]domain.Record, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 20
	}
	where := `application_id = $1 AND ($2 = '' OR environment = $2) AND ($3 = '' OR status = $3)`
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM deployments WHERE `+where, f.ApplicationID, f.Environment, f.Status).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count deployments: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+recordColumns+` FROM deployments WHERE `+where+`
		ORDER BY created_at DESC, number DESC LIMIT $4 OFFSET $5`, f.ApplicationID, f.Environment, f.Status, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list deployments: %w", err)
	}
	defer rows.Close()
	out := []domain.Record{}
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan deployment: %w", err)
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func (s *Store) Steps(ctx context.Context, id string) ([]domain.Step, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT name, position, status, started_at, completed_at, exit_code, COALESCE(error_code, '')
		FROM deployment_steps WHERE deployment_id = $1 ORDER BY position`, id)
	if err != nil {
		return nil, fmt.Errorf("list steps: %w", err)
	}
	defer rows.Close()
	out := []domain.Step{}
	for rows.Next() {
		st, err := scanStep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func scanStep(s scanner) (domain.Step, error) {
	var st domain.Step
	var started, completed sql.NullTime
	var exit sql.NullInt64
	if err := s.Scan(&st.Name, &st.Position, &st.Status, &started, &completed, &exit, &st.ErrorCode); err != nil {
		return domain.Step{}, fmt.Errorf("scan step: %w", err)
	}
	if started.Valid {
		t := started.Time.UTC()
		st.StartedAt = &t
	}
	if completed.Valid {
		t := completed.Time.UTC()
		st.CompletedAt = &t
	}
	if exit.Valid {
		v := int(exit.Int64)
		st.ExitCode = &v
	}
	return st, nil
}

// UpdateStatus applies a transition under a row lock so concurrent writers
// cannot both pass validation from the same state.
func (s *Store) UpdateStatus(ctx context.Context, id string, change domain.StatusChange, validate func(domain.Record) error) (rec domain.Record, ev domain.Event, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Record{}, domain.Event{}, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	rec, err = scanRecord(tx.QueryRowContext(ctx, `SELECT `+recordColumns+` FROM deployments WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Record{}, domain.Event{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Record{}, domain.Event{}, fmt.Errorf("lock deployment: %w", err)
	}
	if validate != nil {
		if err = validate(rec); err != nil {
			return rec, domain.Event{}, err
		}
	}
	from := rec.Status
	now := s.now()
	domain.ApplyStatus(&rec, change, now)
	if _, err = tx.ExecContext(ctx, `UPDATE deployments SET status = $2, url = NULLIF($3, ''), error_code = NULLIF($4, ''),
		updated_at = $5, started_at = $6, completed_at = $7 WHERE id = $1`,
		id, rec.Status, rec.URL, rec.ErrorCode, rec.UpdatedAt, rec.StartedAt, rec.CompletedAt); err != nil {
		return domain.Record{}, domain.Event{}, fmt.Errorf("update deployment: %w", err)
	}
	ev, err = appendEvent(ctx, tx, id, domain.EventStatusChanged, domain.WithCorrelation(domain.StatusEventData(from, rec), rec.CorrelationID), now)
	if err != nil {
		return domain.Record{}, domain.Event{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.Record{}, domain.Event{}, fmt.Errorf("commit: %w", err)
	}
	return rec, ev, nil
}

func (s *Store) UpdateStep(ctx context.Context, id string, change domain.StepChange) (st domain.Step, ev domain.Event, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Step{}, domain.Event{}, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	// Lock the parent deployment to serialize event sequencing.
	var depID, correlationID string
	if err = tx.QueryRowContext(ctx, `SELECT id, COALESCE(correlation_id, '') FROM deployments WHERE id = $1 FOR UPDATE`, id).Scan(&depID, &correlationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Step{}, domain.Event{}, domain.ErrNotFound
		}
		return domain.Step{}, domain.Event{}, fmt.Errorf("lock deployment: %w", err)
	}
	st, err = scanStep(tx.QueryRowContext(ctx, `SELECT name, position, status, started_at, completed_at, exit_code, COALESCE(error_code, '')
		FROM deployment_steps WHERE deployment_id = $1 AND name = $2`, id, change.Name))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(errors.Unwrap(err), sql.ErrNoRows) {
			return domain.Step{}, domain.Event{}, fmt.Errorf("%w: %s", domain.ErrStepNotFound, change.Name)
		}
		return domain.Step{}, domain.Event{}, err
	}
	now := s.now()
	domain.ApplyStep(&st, change, now)
	if _, err = tx.ExecContext(ctx, `UPDATE deployment_steps SET status = $3, started_at = $4, completed_at = $5, exit_code = $6, error_code = NULLIF($7, '')
		WHERE deployment_id = $1 AND name = $2`, id, st.Name, st.Status, st.StartedAt, st.CompletedAt, st.ExitCode, st.ErrorCode); err != nil {
		return domain.Step{}, domain.Event{}, fmt.Errorf("update step: %w", err)
	}
	ev, err = appendEvent(ctx, tx, id, domain.StepEventType(st.Status), domain.WithCorrelation(domain.StepEventData(st), correlationID), now)
	if err != nil {
		return domain.Step{}, domain.Event{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.Step{}, domain.Event{}, fmt.Errorf("commit: %w", err)
	}
	return st, ev, nil
}

func (s *Store) Events(ctx context.Context, id string, afterSeq int64, limit int) ([]domain.Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, seq, type, data, occurred_at FROM deployment_events
		WHERE deployment_id = $1 AND seq > $2 ORDER BY seq LIMIT $3`, id, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	out := []domain.Event{}
	for rows.Next() {
		ev := domain.Event{DeploymentID: id, Version: 1}
		var data []byte
		if err := rows.Scan(&ev.ID, &ev.Seq, &ev.Type, &data, &ev.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		ev.OccurredAt = ev.OccurredAt.UTC()
		if err := json.Unmarshal(data, &ev.Data); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// AppendEvent persists a standalone event (health results, #65) without a
// step or status change. The deployment row is locked to serialize event
// sequencing, mirroring UpdateStep.
func (s *Store) AppendEvent(ctx context.Context, id, typ string, data map[string]any) (domain.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var depID string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM deployments WHERE id = $1 FOR UPDATE`, id).Scan(&depID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Event{}, domain.ErrNotFound
		}
		return domain.Event{}, fmt.Errorf("lock deployment: %w", err)
	}
	ev, err := appendEvent(ctx, tx, id, typ, data, s.now())
	if err != nil {
		return domain.Event{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.Event{}, fmt.Errorf("commit: %w", err)
	}
	return ev, nil
}

// appendEvent assigns the next seq. Callers hold the deployment row lock.
func appendEvent(ctx context.Context, tx *sql.Tx, deploymentID, typ string, data map[string]any, at time.Time) (domain.Event, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return domain.Event{}, fmt.Errorf("encode event: %w", err)
	}
	ev := domain.Event{ID: domain.NewID("evt"), Type: typ, Version: 1, DeploymentID: deploymentID, OccurredAt: at, Data: data}
	err = tx.QueryRowContext(ctx, `INSERT INTO deployment_events (deployment_id, seq, id, type, data, occurred_at)
		VALUES ($1, (SELECT COALESCE(MAX(seq), 0) + 1 FROM deployment_events WHERE deployment_id = $1), $2, $3, $4, $5)
		RETURNING seq`, deploymentID, ev.ID, typ, raw, at).Scan(&ev.Seq)
	if err != nil {
		return domain.Event{}, fmt.Errorf("append event: %w", err)
	}
	// Round-trip data through JSON so callers see the same shape as readers.
	_ = json.Unmarshal(raw, &ev.Data)
	return ev, nil
}
