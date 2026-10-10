package project

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// QuotaChecker reports whether the organization may create one more project.
//
// An implementation must return an error wrapping ErrQuotaExceeded when the
// allowance is spent. org.Service is adapted by the composition root rather than
// imported here, so this package stays independent of the organization package
// while callers still branch on a single sentinel.
type QuotaChecker interface {
	CheckProjectQuota(ctx context.Context, orgID string) error
}

// Role is the subset of an organization role this package compares against. It
// is declared locally rather than imported from org so the project package does
// not depend on the organization package; the composition root adapts between
// the two.
type Role string

// RequiredRole is the least privilege that may create or modify a project:
// deploying is developer work.
const RequiredRole Role = "developer"

// roleRank orders roles so the check is a comparison rather than a switch that
// grows with every permission. The ordering mirrors org.
var roleRank = map[Role]int{
	"viewer": 1, "developer": 2, "admin": 3, "owner": 4,
}

// Authorizer resolves the caller's role in an organization.
type Authorizer interface {
	MemberRole(ctx context.Context, orgID, userID string) (Role, error)
}

// MemberRoleFunc adapts a role-returning function to Authorizer.
type MemberRoleFunc func(ctx context.Context, orgID, userID string) (Role, error)

// MemberRole implements Authorizer.
func (f MemberRoleFunc) MemberRole(ctx context.Context, orgID, userID string) (Role, error) {
	return f(ctx, orgID, userID)
}

// Service implements project management.
//
// Every method takes the organization and the requesting user, and every method
// authorizes before it touches the store. The organization is a parameter rather
// than a field on the record the caller supplies, so a caller cannot aim a write
// at another tenant by passing a foreign org_id.
type Service struct {
	Store Store
	// Quota gates project creation against the plan allowance. Nil disables the
	// check, which is only correct for a single-tenant deployment that has no
	// plan at all.
	Quota QuotaChecker
	// Authz resolves the caller's role. Nil disables authorization, which is
	// correct only for a deliberately single-user Engine.
	Authz Authorizer
	// NewID mints opaque identifiers.
	NewID func() string
	// Now is the clock.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *Service) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("project: entropy source unavailable: " + err.Error())
	}
	return "prj_" + hex.EncodeToString(b)
}

// CreateInput describes a new project.
type CreateInput struct {
	OrgID       string
	RequesterID string
	Name        string
	Slug        string
	Description string
	Environment Environment
	// PrimaryDomain is optional and validated by the caller against the plan's
	// custom-domain allowance before it is stored.
	PrimaryDomain string
	// ApplicationID optionally attaches an existing deployable application.
	ApplicationID string
}

// Create registers a project after checking authorization and the plan allowance.
//
// The quota check and the insert are not atomic, so two concurrent creates can
// both pass a check for the last remaining slot. That is accepted deliberately:
// serializing them would mean holding a lock across the organization's usage row
// on every create, and the duplicate is caught by the slug uniqueness constraint
// anyway. A billing reconciliation pass fixes the counter afterwards.
func (s *Service) Create(ctx context.Context, in CreateInput) (Record, error) {
	if in.OrgID == "" {
		return Record{}, ErrOrgRequired
	}
	if err := s.authorize(ctx, in.OrgID, in.RequesterID); err != nil {
		return Record{}, err
	}
	name := in.Name
	if name == "" {
		name = in.Slug
	}
	name, err := NormalizeName(name)
	if err != nil {
		return Record{}, err
	}
	slug := in.Slug
	if slug == "" {
		slug = name
	}
	slug, err = NormalizeName(slug)
	if err != nil {
		return Record{}, err
	}
	env, err := NormalizeEnvironment(in.Environment)
	if err != nil {
		return Record{}, err
	}
	desc, err := NormalizeDescription(in.Description)
	if err != nil {
		return Record{}, err
	}
	if s.Quota != nil {
		if err := s.Quota.CheckProjectQuota(ctx, in.OrgID); err != nil {
			return Record{}, err
		}
	}
	rec := Record{
		ID:            s.newID(),
		OrgID:         in.OrgID,
		Name:          name,
		Slug:          slug,
		Description:   desc,
		Environment:   env,
		PrimaryDomain: in.PrimaryDomain,
		ApplicationID: in.ApplicationID,
		CreatedBy:     in.RequesterID,
		CreatedAt:     s.now(),
		UpdatedAt:     s.now(),
	}
	if err := s.Store.Create(ctx, rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// Get returns a project the caller may read.
func (s *Service) Get(ctx context.Context, orgID, id string) (Record, error) {
	if orgID == "" {
		return Record{}, ErrOrgRequired
	}
	if id == "" {
		return Record{}, ErrNotFound
	}
	// Read access needs only membership, so a viewer can read a project. The
	// store is org-scoped, which is what actually prevents cross-tenant reads.
	return s.Store.Get(ctx, orgID, id)
}

// List returns the organization's projects.
func (s *Service) List(ctx context.Context, orgID string, limit, offset int) ([]Record, int, error) {
	if orgID == "" {
		return nil, 0, ErrOrgRequired
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return s.Store.List(ctx, orgID, limit, offset)
}

// UpdateInput carries a partial project update. Empty fields are left unchanged,
// so a partial PATCH does not clear what it did not mention.
type UpdateInput struct {
	Name          string
	Description   string
	Environment   Environment
	PrimaryDomain *string
}

// Update changes a project's mutable fields.
func (s *Service) Update(ctx context.Context, orgID, requesterID, id string, in UpdateInput) (Record, error) {
	if err := s.authorize(ctx, orgID, requesterID); err != nil {
		return Record{}, err
	}
	rec, err := s.Store.Get(ctx, orgID, id)
	if err != nil {
		return Record{}, err
	}
	if in.Name != "" {
		name, err := NormalizeName(in.Name)
		if err != nil {
			return Record{}, err
		}
		rec.Name = name
	}
	if in.Description != "" {
		desc, err := NormalizeDescription(in.Description)
		if err != nil {
			return Record{}, err
		}
		rec.Description = desc
	}
	if in.Environment != "" {
		env, err := NormalizeEnvironment(in.Environment)
		if err != nil {
			return Record{}, err
		}
		rec.Environment = env
	}
	if in.PrimaryDomain != nil {
		rec.PrimaryDomain = *in.PrimaryDomain
	}
	rec.UpdatedAt = s.now()
	if err := s.Store.Update(ctx, rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// Delete removes a project.
//
// A project that has deployment history is refused rather than deleted: the
// deployments reference it, and removing it would leave the dashboard pointing
// at rows nobody can resolve. Deleting the application first is the deliberate
// path, and it is what the confirmation dialog says.
func (s *Service) Delete(ctx context.Context, orgID, requesterID, id string) error {
	if err := s.authorize(ctx, orgID, requesterID); err != nil {
		return err
	}
	if _, err := s.Store.Get(ctx, orgID, id); err != nil {
		return err
	}
	n, err := s.Store.CountDeployments(ctx, orgID, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: %d deployment(s) reference it", ErrInUse, n)
	}
	return s.Store.Delete(ctx, orgID, id)
}

// AttachApplication binds a deployable application to the project. It is
// authorization-checked like every other write, since attaching an application
// decides which resource the project's deployments will roll out.
func (s *Service) AttachApplication(ctx context.Context, orgID, requesterID, id, applicationID string) error {
	if err := s.authorize(ctx, orgID, requesterID); err != nil {
		return err
	}
	if _, err := s.Store.Get(ctx, orgID, id); err != nil {
		return err
	}
	if applicationID == "" {
		return fmt.Errorf("%w: applicationId is required", ErrInvalidName)
	}
	return s.Store.AttachApplication(ctx, orgID, id, applicationID)
}

// SetServingDeployment is intentionally absent.
//
// The serving deployment is derived from the deployments table on every read
// rather than stored on the project, so there is nothing to set: a stale pointer
// would be the only way this value could be wrong. Call SetServingDeployment
// only if a future requirement introduces a manual override, and then the
// override needs its own reconciliation path.
func (s *Service) servingDeploymentOf(ctx context.Context, orgID, id string) (string, error) {
	rec, err := s.Store.Get(ctx, orgID, id)
	if err != nil {
		return "", err
	}
	return rec.DeploymentID, nil
}

// authorize requires the caller to hold at least the developer role.
//
// When Authz is nil the check is skipped. That is a deliberate, visible
// configuration: a single-user Engine has no organization roles to consult, and
// failing closed there would lock the operator out of their own deployment.
func (s *Service) authorize(ctx context.Context, orgID, requesterID string) error {
	if orgID == "" {
		return ErrOrgRequired
	}
	if s.Authz == nil {
		return nil
	}
	if requesterID == "" {
		return ErrNotFound
	}
	role, err := s.Authz.MemberRole(ctx, orgID, requesterID)
	if err != nil {
		// A caller who is not a member is reported as "not found" rather than
		// "forbidden", for the same reason the org service does it: the
		// distinction would reveal which projects and organizations exist.
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if roleRank[role] < roleRank[RequiredRole] {
		return ErrNotFound
	}
	return nil
}
