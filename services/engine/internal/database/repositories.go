package database

import (
	"context"
	"database/sql"
	"fmt"
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
		Users:             UserRepository{db: db},
		GitHubConnections: GitHubConnectionRepository{db: db},
		Repositories:      RepositoryRepository{db: db},
		Applications:      ApplicationRepository{db: db},
		Servers:           ServerRepository{db: db},
		Deployments:       DeploymentRepository{db: db},
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

func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}


// GetRecord adapts the persistence model to the deployment domain without
// exposing database concerns to the domain service.
func (r DeploymentRepository) GetRecord(ctx context.Context, id string) (string, string, string, string, error) {
	v, err := r.Get(ctx, id)
	if err != nil {
		return "", "", "", "", err
	}
	return v.ID, v.ApplicationID, v.ServerID, v.Environment, nil
}
