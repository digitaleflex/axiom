package logs

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// DefaultMaxEntries is the per-deployment retention cap used when
// NewPGStore receives a non-positive value.
const DefaultMaxEntries = 10_000

const (
	defaultListLimit = 100
	maxListLimit     = 1000
)

// PGStore is the PostgreSQL implementation of Store (issue #66). Every
// Append runs in a single transaction: entries are inserted redacted, then
// the deployment's history is trimmed to maxPerDeployment entries.
type PGStore struct {
	db               *sql.DB
	now              func() time.Time
	maxPerDeployment int
}

var _ Store = (*PGStore)(nil)

// NewPGStore returns a Store on PostgreSQL. maxPerDeployment bounds the
// number of entries kept per deployment; values <= 0 select
// DefaultMaxEntries.
func NewPGStore(db *sql.DB, maxPerDeployment int) *PGStore {
	if maxPerDeployment <= 0 {
		maxPerDeployment = DefaultMaxEntries
	}
	return &PGStore{db: db, now: func() time.Time { return time.Now().UTC() }, maxPerDeployment: maxPerDeployment}
}

// MaxPerDeployment reports the configured per-deployment retention cap.
func (s *PGStore) MaxPerDeployment() int { return s.maxPerDeployment }

func (s *PGStore) Append(ctx context.Context, entries ...Entry) error {
	if len(entries) == 0 {
		return nil
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	deployments := make(map[string]struct{}, 1)
	for i := range entries {
		e := &entries[i]
		if e.ID == "" {
			e.ID = NewID()
		}
		if e.OccurredAt.IsZero() {
			e.OccurredAt = now
		}
		// Secrets are redacted before persistence, never at read time.
		e.Message = Redact(e.Message)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO deployment_logs (id, deployment_id, occurred_at, level, step, source, message)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			e.ID, e.DeploymentID, e.OccurredAt.UTC(), e.Level, e.Step, e.Source, e.Message); err != nil {
			return fmt.Errorf("insert log entry: %w", err)
		}
		deployments[e.DeploymentID] = struct{}{}
	}

	// Retention bound: keep only the newest maxPerDeployment entries per
	// deployment, in the same transaction as the inserts.
	for deploymentID := range deployments {
		if _, err := trimTx(ctx, tx, deploymentID, s.maxPerDeployment); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (s *PGStore) List(ctx context.Context, deploymentID string, f Filter) ([]Entry, string, error) {
	limit := f.Limit
	if limit <= 0 || limit > maxListLimit {
		limit = defaultListLimit
	}

	where := `deployment_id = $1`
	args := []any{deploymentID}
	add := func(cond string, arg any) {
		args = append(args, arg)
		where += fmt.Sprintf(cond, len(args))
	}

	switch {
	case len(f.Levels) > 0:
		levels := make([]string, len(f.Levels))
		for i, l := range f.Levels {
			levels[i] = string(l)
		}
		add(` AND level = ANY($%d)`, levels)
	case f.MinLevel != "":
		levels, err := levelsAtLeast(f.MinLevel)
		if err != nil {
			return nil, "", err
		}
		names := make([]string, len(levels))
		for i, l := range levels {
			names[i] = string(l)
		}
		add(` AND level = ANY($%d)`, names)
	}
	if f.Step != "" {
		add(` AND step = $%d`, f.Step)
	}
	if f.Source != "" {
		add(` AND source = $%d`, f.Source)
	}
	if f.Search != "" {
		add(` AND message ILIKE '%%' || $%d || '%%' ESCAPE '\'`, escapeLike(f.Search))
	}
	if f.Cursor != "" {
		// Keyset pagination on (occurred_at, id); an unknown cursor yields an
		// empty page.
		add(` AND (occurred_at, id) > (SELECT occurred_at, id FROM deployment_logs WHERE id = $%d)`, f.Cursor)
	}

	// Fetch one extra row to tell a full page from the last page.
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, deployment_id, occurred_at, level, step, source, message
		 FROM deployment_logs WHERE `+where+`
		 ORDER BY occurred_at, id LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, "", fmt.Errorf("list log entries: %w", err)
	}
	defer rows.Close()

	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.DeploymentID, &e.OccurredAt, &e.Level, &e.Step, &e.Source, &e.Message); err != nil {
			return nil, "", fmt.Errorf("scan log entry: %w", err)
		}
		e.OccurredAt = e.OccurredAt.UTC()
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("list log entries: %w", err)
	}

	nextCursor := ""
	if len(out) > limit {
		nextCursor = out[limit-1].ID
		out = out[:limit]
	}
	return out, nextCursor, nil
}

func (s *PGStore) Count(ctx context.Context, deploymentID string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM deployment_logs WHERE deployment_id = $1`, deploymentID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count log entries: %w", err)
	}
	return n, nil
}

func (s *PGStore) Trim(ctx context.Context, deploymentID string, keepLastN int) (int, error) {
	if keepLastN < 0 {
		keepLastN = 0
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	deleted, err := trimTx(ctx, tx, deploymentID, keepLastN)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return deleted, nil
}

// trimTx deletes the oldest entries of deploymentID beyond keepLastN and
// returns the number of deleted rows. Callers hold the transaction.
func trimTx(ctx context.Context, tx *sql.Tx, deploymentID string, keepLastN int) (int, error) {
	res, err := tx.ExecContext(ctx,
		`DELETE FROM deployment_logs WHERE deployment_id = $1 AND id NOT IN (
			SELECT id FROM deployment_logs WHERE deployment_id = $1
			ORDER BY occurred_at DESC, id DESC LIMIT $2)`,
		deploymentID, keepLastN)
	if err != nil {
		return 0, fmt.Errorf("trim log entries: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// escapeLike escapes LIKE wildcards in a user-supplied search substring.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
