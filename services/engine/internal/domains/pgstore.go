package domains

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// PGStore implements Store on PostgreSQL (migrations 003 + 006).
type PGStore struct{ DB *sql.DB }

const columns = `id, application_id, environment, hostname, is_primary, dns_status, dns_expected, dns_observed, dns_checked_at, tls_status, routing_status, created_at`

type scanner interface{ Scan(...any) error }

func scan(s scanner) (Record, error) {
	var r Record
	var checked sql.NullTime
	var created time.Time
	err := s.Scan(&r.ID, &r.ApplicationID, &r.Environment, &r.Hostname, &r.IsPrimary, &r.DNSStatus,
		&r.DNSExpected, &r.DNSObserved, &checked, &r.TLSStatus, &r.RoutingStatus, &created)
	if err != nil {
		return Record{}, err
	}
	r.CreatedAt = created.UTC()
	if checked.Valid {
		t := checked.Time.UTC()
		r.DNSCheckedAt = &t
	}
	return r, nil
}

func (p PGStore) List(ctx context.Context, applicationID, environment string) ([]Record, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+columns+` FROM domains
		WHERE application_id = $1 AND ($2 = '' OR environment = $2) ORDER BY hostname`, applicationID, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p PGStore) Create(ctx context.Context, r Record) error {
	_, err := p.DB.ExecContext(ctx, `INSERT INTO domains (id, application_id, environment, hostname, is_primary, dns_status, tls_status, routing_status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, r.ID, r.ApplicationID, r.Environment, r.Hostname, r.IsPrimary,
		r.DNSStatus, r.TLSStatus, r.RoutingStatus, r.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrTaken
		}
		return fmt.Errorf("create domain: %w", err)
	}
	return nil
}

func (p PGStore) SetPrimary(ctx context.Context, applicationID, environment, id string) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var found bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM domains WHERE id = $1 AND application_id = $2 AND environment = $3)`, id, applicationID, environment).Scan(&found); err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `UPDATE domains SET is_primary = false WHERE application_id = $1 AND environment = $2`, applicationID, environment); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE domains SET is_primary = true WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (p PGStore) Delete(ctx context.Context, id string) error {
	res, err := p.DB.ExecContext(ctx, `DELETE FROM domains WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p PGStore) Get(ctx context.Context, id string) (Record, error) {
	r, err := scan(p.DB.QueryRowContext(ctx, `SELECT `+columns+` FROM domains WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	return r, err
}

func (p PGStore) RoutingTarget(ctx context.Context, applicationID, environment string) (Target, bool, error) {
	var t Target
	err := p.DB.QueryRowContext(ctx, `SELECT d.id, d.server_id, s.address FROM deployments d
		JOIN servers s ON s.id = d.server_id
		WHERE d.application_id = $1 AND d.environment = $2 AND d.status = 'LIVE'
		ORDER BY d.created_at DESC, d.number DESC NULLS LAST LIMIT 1`, applicationID, environment).
		Scan(&t.DeploymentID, &t.ServerID, &t.Address)
	if errors.Is(err, sql.ErrNoRows) {
		return Target{}, false, nil
	}
	return t, true, err
}

func (p PGStore) SaveDNSCheck(ctx context.Context, id, status, expected, observed string, at time.Time) error {
	_, err := p.DB.ExecContext(ctx, `UPDATE domains SET dns_status = $2, dns_expected = $3, dns_observed = $4, dns_checked_at = $5 WHERE id = $1`, id, status, expected, observed, at)
	return err
}
