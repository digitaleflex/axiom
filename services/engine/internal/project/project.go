// Package project defines the Project resource (issue #146, M12.1): the
// client-facing unit a tenant actually names.
//
// A project is a thin grouping over resources that already exist. It does not
// replace the Application (which the planner and executor are built around) and
// it does not duplicate the Deployment. What it adds is the tenant boundary:
// which organization owns this, which plan allowance it consumes, and the
// identity the client dashboard routes on.
//
// Because the grouping is additive, an application created before multi-tenancy
// keeps working with no project attached; project_id is nullable throughout.
package project

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Environment mirrors the V0.1 environment vocabulary (production, staging,
// preview). It is redeclared here rather than imported from the planner so the
// project package stays free of planning concerns.
type Environment string

const (
	EnvProduction Environment = "production"
	EnvStaging    Environment = "staging"
	EnvPreview    Environment = "preview"
)

// ValidEnvironment reports whether e is a known environment.
func ValidEnvironment(e Environment) bool {
	switch e {
	case EnvProduction, EnvStaging, EnvPreview:
		return true
	default:
		return false
	}
}

// Record is a project.
type Record struct {
	ID            string      `json:"id"`
	OrgID         string      `json:"orgId"`
	Name          string      `json:"name"`
	Slug          string      `json:"slug"`
	Description   string      `json:"description"`
	Environment   Environment `json:"environment"`
	PrimaryDomain string      `json:"primaryDomain,omitempty"`
	// ApplicationID is the deployable resource behind the project. It is
	// optional so a project can be created before a repository is attached.
	ApplicationID string `json:"applicationId,omitempty"`
	// DeploymentID is the deployment currently serving the project, empty when
	// it has never been deployed. It is a convenience pointer for the dashboard,
	// never a source of truth: the deployment list remains authoritative.
	DeploymentID  string    `json:"deploymentId,omitempty"`
	CreatedBy     string    `json:"createdBy,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Deployed reports whether the project has ever been deployed.
func (r Record) Deployed() bool { return r.DeploymentID != "" }

// nameRe matches the same shape the database enforces for name and slug, so a
// name accepted here cannot be rejected by the insert.
var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidName reports whether name is an acceptable project name or slug.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Store persists projects. Every read and write is scoped by organization:
// there is no store method that can reach a project across tenants, so isolation
// does not depend on a caller remembering to filter.
type Store interface {
	Create(ctx context.Context, r Record) error
	Get(ctx context.Context, orgID, id string) (Record, error)
	List(ctx context.Context, orgID string, limit, offset int) ([]Record, int, error)
	Update(ctx context.Context, r Record) error
	Delete(ctx context.Context, orgID, id string) error
	// AttachApplication binds a deployable application to the project.
	AttachApplication(ctx context.Context, orgID, id, applicationID string) error
	// CountDeployments returns the number of deployments ever recorded for the
	// project, used to refuse removal while history still references it.
	CountDeployments(ctx context.Context, orgID, id string) (int, error)
}

// Errors are the stable failures callers branch on.
var (
	// ErrNotFound is returned for an unknown project. It is also returned for a
	// project belonging to another organization: the two are indistinguishable so
	// an id from another tenant cannot be probed.
	ErrNotFound = errors.New("project not found")
	// ErrAlreadyExists is returned when a slug collides inside the organization.
	ErrAlreadyExists = errors.New("project slug already in use")
	// ErrInvalidName is returned for a malformed name or slug.
	ErrInvalidName = errors.New("project: invalid name")
	// ErrInvalidEnvironment is returned for an unknown environment.
	ErrInvalidEnvironment = errors.New("project: invalid environment")
	// ErrInvalidDescription is returned for an oversized description.
	ErrInvalidDescription = errors.New("project: invalid description")
	// ErrInUse is returned when a project still has deployments.
	ErrInUse = errors.New("project still has deployments")
	// ErrQuotaExceeded is returned when the plan's project allowance is spent.
	ErrQuotaExceeded = errors.New("project quota exceeded")
	// ErrOrgRequired is returned when a caller supplies no organization.
	ErrOrgRequired = errors.New("project: organization is required")
)

// MaxDescriptionLength bounds the free-text description.
const MaxDescriptionLength = 500

// NormalizeName validates and lowercases a project name or slug.
func NormalizeName(s string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if !nameRe.MatchString(v) {
		return "", ErrInvalidName
	}
	return v, nil
}

// NormalizeEnvironment validates an environment, defaulting an empty value to
// production so a caller that omits it gets the safe environment rather than an
// error.
func NormalizeEnvironment(e Environment) (Environment, error) {
	if e == "" {
		return EnvProduction, nil
	}
	if !ValidEnvironment(e) {
		return "", ErrInvalidEnvironment
	}
	return e, nil
}

// NormalizeDescription validates the optional description.
func NormalizeDescription(d string) (string, error) {
	if len(d) > MaxDescriptionLength {
		return "", ErrInvalidDescription
	}
	return strings.TrimSpace(d), nil
}
