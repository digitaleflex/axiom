package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// PGStore is the PostgreSQL implementation of Store.
//
// Every statement carries the organization id in its WHERE clause. That is the
// isolation boundary: a query for a project belonging to another tenant returns
// no rows rather than that tenant's row, so a caller cannot reach across even by
// passing a valid project id from elsewhere.
type PGStore struct{ DB *sql.DB }

var _ Store = PGStore{}

const columns = `id, org_id, name, slug, description, environment,
	coalesce(primary_domain,''), coalesce(application_id,''),
	coalesce(created_by,''), created_at, updated_at`

// pgErrCode extracts the SQLSTATE so a unique violation is distinguishable from
// a check violation without string-matching driver messages.
func pgErrCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func (s PGStore) Create(ctx context.Context, r Record) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO projects
			(id, org_id, name, slug, description, environment, primary_domain,
			 application_id, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,nullif($7,''),nullif($8,''),nullif($9,''),
		        coalesce($10, now()), now())`,
		r.ID, r.OrgID, r.Name, r.Slug, r.Description, string(r.Environment),
		r.PrimaryDomain, r.ApplicationID, r.CreatedBy, nullTime(r.CreatedAt))
	if pgErrCode(err) == "23505" {
		return ErrAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("project: create: %w", err)
	}
	return nil
}

func (s PGStore) Get(ctx context.Context, orgID, id string) (Record, error) {
	var (
		r    Record
		env  string
		apps sql.NullString
	)
	err := s.DB.QueryRowContext(ctx,
		`SELECT `+columns+` FROM projects WHERE org_id = $1 AND id = $2`, orgID, id).
		Scan(&r.ID, &r.OrgID, &r.Name, &r.Slug, &r.Description, &env, &r.PrimaryDomain,
			&apps, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("project: get: %w", err)
	}
	r.Environment = Environment(env)
	r.ApplicationID = apps.String
	return r, nil
}

// servingDeployment returns the newest deployment serving the project. It is
// read separately from the project row rather than denormalized into it, so the
// pointer cannot drift out of step with the deployment table.
func (s PGStore) servingDeployment(ctx context.Context, orgID, id string) (string, error) {
	var dep sql.NullString
	err := s.DB.QueryRowContext(ctx, `
		SELECT d.id
		  FROM deployments d
		 WHERE d.org_id = $1 AND d.project_id = $2 AND d.status = 'LIVE'
		 ORDER BY d.completed_at DESC NULLS LAST, d.created_at DESC
		 LIMIT 1`, orgID, id).Scan(&dep)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("project: serving deployment: %w", err)
	}
	return dep.String, nil
}

func (s PGStore) List(ctx context.Context, orgID string, limit, offset int) ([]Record, int, error) {
	var total int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM projects WHERE org_id = $1`, orgID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("project: count: %w", err)
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+columns+` FROM projects WHERE org_id = $1
		 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, orgID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("project: list: %w", err)
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var (
			r    Record
			env  string
			apps sql.NullString
		)
		if err := rows.Scan(&r.ID, &r.OrgID, &r.Name, &r.Slug, &r.Description, &env,
			&r.PrimaryDomain, &apps, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("project: scan row: %w", err)
		}
		r.Environment = Environment(env)
		r.ApplicationID = apps.String
		r.DeploymentID, _ = s.servingDeployment(ctx, orgID, r.ID)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("project: iterate: %w", err)
	}
	return out, total, nil
}

func (s PGStore) Update(ctx context.Context, r Record) error {
	tag, err := s.DB.ExecContext(ctx, `
		UPDATE projects
		   SET name = $3, description = $4, environment = $5,
		       primary_domain = nullif($6,''), updated_at = now()
		 WHERE org_id = $1 AND id = $2`,
		r.OrgID, r.ID, r.Name, r.Description, string(r.Environment), r.PrimaryDomain)
	if err != nil {
		return fmt.Errorf("project: update: %w", err)
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s PGStore) Delete(ctx context.Context, orgID, id string) error {
	tag, err := s.DB.ExecContext(ctx,
		`DELETE FROM projects WHERE org_id = $1 AND id = $2`, orgID, id)
	if err != nil {
		return fmt.Errorf("project: delete: %w", err)
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s PGStore) AttachApplication(ctx context.Context, orgID, id, applicationID string) error {
	tag, err := s.DB.ExecContext(ctx, `
		UPDATE projects SET application_id = $3, updated_at = now()
		 WHERE org_id = $1 AND id = $2`, orgID, id, applicationID)
	if err != nil {
		return fmt.Errorf("project: attach application: %w", err)
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s PGStore) CountDeployments(ctx context.Context, orgID, id string) (int, error) {
	var n int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM deployments WHERE org_id = $1 AND project_id = $2`,
		orgID, id).Scan(&n); err != nil {
		return 0, fmt.Errorf("project: count deployments: %w", err)
	}
	return n, nil
}

// nullTime converts the zero time into SQL NULL so a caller that does not set a
// timestamp gets the column default rather than year 1.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
