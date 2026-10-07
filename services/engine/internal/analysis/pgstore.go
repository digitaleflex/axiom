package analysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/digitaleflex/axiom/services/engine/internal/analyzer/evidence"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
)

// PGStore implements Store on PostgreSQL (migration 005).
type PGStore struct{ DB *sql.DB }

func (p PGStore) CreateAnalysis(ctx context.Context, r Record) error {
	_, err := p.DB.ExecContext(ctx, `INSERT INTO analyses (id, application_id, ref, root, status, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		r.ID, r.ApplicationID, r.Ref, r.Root, r.Status, r.CreatedAt)
	return err
}

func (p PGStore) CompleteAnalysis(ctx context.Context, r Record, prof *profile.Profile) (version int, err error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var result any
	if r.Result != nil {
		raw, err := json.Marshal(r.Result)
		if err != nil {
			return 0, err
		}
		result = raw
	}
	if _, err = tx.ExecContext(ctx, `UPDATE analyses SET status = $2, commit_sha = $3, analyzer_version = $4, result = $5,
		error_code = NULLIF($6, ''), completed_at = $7 WHERE id = $1`, r.ID, r.Status, r.Commit, r.AnalyzerVersion, result, r.ErrorCode, r.CompletedAt); err != nil {
		return 0, fmt.Errorf("update analysis: %w", err)
	}
	if prof != nil {
		if version, err = insertProfile(ctx, tx, r.ApplicationID, *prof); err != nil {
			return 0, err
		}
	}
	return version, tx.Commit()
}

func (p PGStore) SaveProfile(ctx context.Context, applicationID string, prof profile.Profile) (version int, err error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if version, err = insertProfile(ctx, tx, applicationID, prof); err != nil {
		return 0, err
	}
	return version, tx.Commit()
}

// insertProfile assigns the next version under an application row lock.
func insertProfile(ctx context.Context, tx *sql.Tx, applicationID string, prof profile.Profile) (int, error) {
	if _, err := tx.ExecContext(ctx, `SELECT id FROM applications WHERE id = $1 FOR UPDATE`, applicationID); err != nil {
		return 0, fmt.Errorf("lock application: %w", err)
	}
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM application_profiles WHERE application_id = $1`, applicationID).Scan(&version); err != nil {
		return 0, err
	}
	prof.Version = version
	body, err := json.Marshal(prof)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO application_profiles (application_id, version, analysis_id, status, body) VALUES ($1, $2, $3, $4, $5)`,
		applicationID, version, prof.AnalysisID, prof.Status, body); err != nil {
		return 0, fmt.Errorf("insert profile: %w", err)
	}
	return version, nil
}

func (p PGStore) GetAnalysis(ctx context.Context, applicationID, id string) (Record, error) {
	var r Record
	var result []byte
	var errorCode sql.NullString
	var completed sql.NullTime
	err := p.DB.QueryRowContext(ctx, `SELECT a.id, a.application_id, a.ref, a.commit_sha, a.root, a.status, a.analyzer_version, a.result, a.error_code,
			a.created_at, a.completed_at, COALESCE((SELECT MAX(version) FROM application_profiles WHERE analysis_id = a.id), 0)
		FROM analyses a WHERE a.id = $1 AND a.application_id = $2`, id, applicationID).
		Scan(&r.ID, &r.ApplicationID, &r.Ref, &r.Commit, &r.Root, &r.Status, &r.AnalyzerVersion, &result, &errorCode, &r.CreatedAt, &completed, &r.ProfileVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	r.CreatedAt = r.CreatedAt.UTC()
	r.ErrorCode = errorCode.String
	if completed.Valid {
		t := completed.Time.UTC()
		r.CompletedAt = &t
	}
	if len(result) > 0 {
		var res evidence.Result
		if err := json.Unmarshal(result, &res); err != nil {
			return Record{}, err
		}
		r.Result = &res
	}
	return r, nil
}

func (p PGStore) CurrentProfile(ctx context.Context, applicationID string) (profile.Profile, error) {
	var body []byte
	err := p.DB.QueryRowContext(ctx, `SELECT body FROM application_profiles WHERE application_id = $1 ORDER BY version DESC LIMIT 1`, applicationID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return profile.Profile{}, ErrNoProfile
	}
	if err != nil {
		return profile.Profile{}, err
	}
	var prof profile.Profile
	return prof, json.Unmarshal(body, &prof)
}

func (p PGStore) Overrides(ctx context.Context, applicationID string) (profile.Hints, string, error) {
	var raw []byte
	var root string
	err := p.DB.QueryRowContext(ctx, `SELECT hints, root FROM application_overrides WHERE application_id = $1`, applicationID).Scan(&raw, &root)
	if errors.Is(err, sql.ErrNoRows) {
		return profile.Hints{}, "", nil
	}
	if err != nil {
		return profile.Hints{}, "", err
	}
	var h profile.Hints
	return h, root, json.Unmarshal(raw, &h)
}

func (p PGStore) SaveOverrides(ctx context.Context, applicationID string, h profile.Hints, root string) error {
	raw, err := json.Marshal(h)
	if err != nil {
		return err
	}
	_, err = p.DB.ExecContext(ctx, `INSERT INTO application_overrides (application_id, hints, root) VALUES ($1, $2, $3)
		ON CONFLICT (application_id) DO UPDATE SET hints = EXCLUDED.hints, root = EXCLUDED.root, updated_at = now()`, applicationID, raw, root)
	return err
}
