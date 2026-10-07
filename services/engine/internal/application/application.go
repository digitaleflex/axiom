// Package application defines the Application resource: a deployable project
// derived from a repository (docs/architecture/api-contract.md §6).
package application

import (
	"context"
	"errors"
	"regexp"
	"time"
)

type Record struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	RepositoryID string    `json:"repositoryId"`
	OwnerID      string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

var (
	ErrNotFound           = errors.New("application not found")
	ErrRepositoryNotFound = errors.New("repository not found")
	ErrNameTaken          = errors.New("application name already in use")
)

// nameRe matches DNS-label-safe slugs (also enforced by the database).
var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidName reports whether name is an acceptable application name.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Store persists applications. List returns only applications owned by ownerID.
type Store interface {
	Create(ctx context.Context, r Record) (Record, error)
	Get(ctx context.Context, id string) (Record, error)
	List(ctx context.Context, ownerID string, limit, offset int) ([]Record, int, error)
}
