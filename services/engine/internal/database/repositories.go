package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/digitaleflex/axiom/services/engine/internal/application"

	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

type UserRepository struct{ db *sql.DB }
type GitHubConnectionRepository struct{ db *sql.DB }
type RepositoryRepository struct{ db *sql.DB }
type ApplicationRepository struct{ db *sql.DB }
type ServerRepository struct{ db *sql.DB }

type Repositories struct {
	Users             UserRepository
	GitHubConnections GitHubConnectionRepository
	Repositories      RepositoryRepository
	Applications      ApplicationRepository
	Servers           ServerRepository
}

func NewRepositories(db *sql.DB) Repositories {
	return Repositories{
		Users:             UserRepository{db: db},
		GitHubConnections: GitHubConnectionRepository{db: db},
		Repositories:      RepositoryRepository{db: db},
		Applications:      ApplicationRepository{db: db},
		Servers:           ServerRepository{db: db},
	}
}

func (r UserRepository) Create(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO users (id) VALUES ($1)`, id)
	return wrap("create user", err)
}

func (r UserRepository) Get(ctx context.Context, id string) (User, error) {
	var v User
	err := r.db.QueryRowContext(ctx, `SELECT id, created_at FROM users WHERE id = $1`, id).Scan(&v.ID, &v.CreatedAt)
	return v, wrap("get user", err)
}

func (r GitHubConnectionRepository) Create(ctx context.Context, id, userID string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO github_connections (id, user_id) VALUES ($1, $2)`, id, userID)
	return wrap("create github connection", err)
}

func (r RepositoryRepository) Create(ctx context.Context, id, connectionID, externalID, fullName, cloneURL string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO repositories (id, connection_id, external_id, full_name, clone_url) VALUES ($1, $2, $3, $4, $5)`, id, connectionID, externalID, fullName, cloneURL)
	return wrap("create repository", err)
}

func (r ApplicationRepository) Create(ctx context.Context, id, repositoryID, name string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO applications (id, repository_id, name) VALUES ($1, $2, $3)`, id, repositoryID, name)
	return wrap("create application", err)
}

func (r ServerRepository) Create(ctx context.Context, id, name, address string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO servers (id, name, address) VALUES ($1, $2, $3)`, id, name, address)
	return wrap("create server", err)
}

func (r ServerRepository) Get(ctx context.Context, id string) (server.Record, error) {
	v, err := r.get(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return server.Record{}, server.ErrNotFound
	}
	return v, err
}

func (r ServerRepository) get(ctx context.Context, id string) (server.Record, error) {
	var v server.Record
	var rawCapabilities []byte
	var lastSeen sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, address, status, agent_version, capabilities, cpu_count, memory_mb, disk_free_mb, last_seen_at
		FROM servers WHERE id = $1
	`, id).Scan(
		&v.ID, &v.Name, &v.Address, &v.Status, &v.AgentVersion, &rawCapabilities,
		&v.CPUCount, &v.MemoryMB, &v.DiskFreeMB, &lastSeen,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return server.Record{}, err
		}
		return server.Record{}, wrap("get server", err)
	}
	if err := json.Unmarshal(rawCapabilities, &v.Capabilities); err != nil {
		return server.Record{}, wrap("decode server capabilities", err)
	}
	if lastSeen.Valid {
		v.LastSeenAt = lastSeen.Time.UTC().Format(time.RFC3339)
	}
	return v, nil
}

func (r ServerRepository) UpdateHealth(ctx context.Context, id string, health server.Health) error {
	raw, err := json.Marshal(health.Capabilities)
	if err != nil {
		return wrap("encode server capabilities", err)
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE servers
		SET status = $1, agent_version = $2, capabilities = $3, cpu_count = $4,
		    memory_mb = $5, disk_free_mb = $6, last_seen_at = $7
		WHERE id = $8
	`, health.Status, health.AgentVersion, raw, health.CPUCount, health.MemoryMB, health.DiskFreeMB, health.LastSeenAt, id)
	return wrap("update server health", err)
}

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// ApplicationStore implements application.Store.
type ApplicationStore struct{ db *sql.DB }

func NewApplicationStore(db *sql.DB) ApplicationStore { return ApplicationStore{db: db} }

const applicationColumns = `id, name, repository_id, COALESCE(owner_id, ''), created_at, updated_at`

func scanApplication(s interface{ Scan(...any) error }) (application.Record, error) {
	var a application.Record
	if err := s.Scan(&a.ID, &a.Name, &a.RepositoryID, &a.OwnerID, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return application.Record{}, err
	}
	a.CreatedAt, a.UpdatedAt = a.CreatedAt.UTC(), a.UpdatedAt.UTC()
	return a, nil
}

func (s ApplicationStore) Create(ctx context.Context, r application.Record) (application.Record, error) {
	var owner any
	if r.OwnerID != "" {
		owner = r.OwnerID
	}
	out, err := scanApplication(s.db.QueryRowContext(ctx, `INSERT INTO applications (id, name, repository_id, owner_id)
		VALUES ($1, $2, $3, $4) RETURNING `+applicationColumns, r.ID, r.Name, r.RepositoryID, owner))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			if pgErr.ConstraintName == "applications_repository_id_fkey" {
				return application.Record{}, application.ErrRepositoryNotFound
			}
		}
		return application.Record{}, wrap("create application", err)
	}
	return out, nil
}

func (s ApplicationStore) Get(ctx context.Context, id string) (application.Record, error) {
	a, err := scanApplication(s.db.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return application.Record{}, application.ErrNotFound
	}
	return a, wrap("get application", err)
}

func (s ApplicationStore) List(ctx context.Context, ownerID string, limit, offset int) ([]application.Record, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM applications WHERE COALESCE(owner_id, '') = $1`, ownerID).Scan(&total); err != nil {
		return nil, 0, wrap("count applications", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE COALESCE(owner_id, '') = $1
		ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`, ownerID, limit, offset)
	if err != nil {
		return nil, 0, wrap("list applications", err)
	}
	defer rows.Close()
	out := []application.Record{}
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, 0, wrap("scan application", err)
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// List returns servers ordered by name.
func (r ServerRepository) List(ctx context.Context, limit, offset int) ([]server.Record, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM servers`).Scan(&total); err != nil {
		return nil, 0, wrap("count servers", err)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM servers ORDER BY name, id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, wrap("list servers", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, wrap("scan server", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := make([]server.Record, 0, len(ids))
	for _, id := range ids {
		s, err := r.Get(ctx, id)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, total, nil
}
