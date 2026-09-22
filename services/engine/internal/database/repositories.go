package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

type UserRepository struct{ db *sql.DB }
type GitHubConnectionRepository struct{ db *sql.DB }
type RepositoryRepository struct{ db *sql.DB }
type ApplicationRepository struct{ db *sql.DB }
type ServerRepository struct{ db *sql.DB }
type DeploymentRepository struct{ db *sql.DB }

type Repositories struct {
	Users             UserRepository
	GitHubConnections GitHubConnectionRepository
	Repositories      RepositoryRepository
	Applications      ApplicationRepository
	Servers           ServerRepository
	Deployments       DeploymentRepository
}

func NewRepositories(db *sql.DB) Repositories {
	return Repositories{
		Users: UserRepository{db: db},
		GitHubConnections: GitHubConnectionRepository{db: db},
		Repositories: RepositoryRepository{db: db},
		Applications: ApplicationRepository{db: db},
		Servers: ServerRepository{db: db},
		Deployments: DeploymentRepository{db: db},
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

func (r DeploymentRepository) Create(ctx context.Context, id, applicationID, serverID, environment string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO deployments (id, application_id, server_id, environment) VALUES ($1, $2, $3, $4)`, id, applicationID, serverID, environment)
	return wrap("create deployment", err)
}

func (r DeploymentRepository) SetStatus(ctx context.Context, id, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE deployments SET status = $1 WHERE id = $2`, status, id)
	return wrap("update deployment status", err)
}

func (r DeploymentRepository) Get(ctx context.Context, id string) (Deployment, error) {
	var v Deployment
	err := r.db.QueryRowContext(ctx, `SELECT id, application_id, server_id, environment, status FROM deployments WHERE id = $1`, id).
		Scan(&v.ID, &v.ApplicationID, &v.ServerID, &v.Environment, &v.Status)
	return v, wrap("get deployment", err)
}

func (r DeploymentRepository) GetDomainRecord(ctx context.Context, id string) (deployment.Record, error) {
	v, err := r.Get(ctx, id)
	if err != nil {
		return deployment.Record{}, err
	}
	return deployment.Record{ID: v.ID, ApplicationID: v.ApplicationID, ServerID: v.ServerID, Environment: v.Environment, Status: deployment.State(v.Status)}, nil
}

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
