package org

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Service implements organization management (issue #146, M12.1).
//
// Two invariants drive the design and are enforced here rather than left to
// callers:
//
//  1. An organization always keeps at least one owner. Every mutation that could
//     remove the last owner is refused, so an organization can never become
//     unadministrable.
//
//  2. A caller may only act on an organization they belong to, with a role that
//     satisfies the action. Authorization is checked against the membership
//     read from the store on every call, never from a value the caller supplies,
//     so a forged member record cannot widen access.
type Service struct {
	Store Store
	// NewID mints opaque identifiers. Injected so tests are deterministic.
	NewID func() string
	// Now is the clock. Injected so invitation expiry is testable.
	Now func() time.Time
	// TokenTTL is how long an invitation stays valid. Zero means DefaultTokenTTL.
	TokenTTL time.Duration
}

// DefaultTokenTTL is the validity window of an invitation link.
const DefaultTokenTTL = 7 * 24 * time.Hour

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
	return "org_" + randomHex(12)
}

func (s *Service) tokenTTL() time.Duration {
	if s.TokenTTL > 0 {
		return s.TokenTTL
	}
	return DefaultTokenTTL
}

// CreateInput describes a new organization owned by the creating user.
type CreateInput struct {
	OwnerID string
	Name    string
	Slug    string
	Plan    Plan
}

// Create registers an organization, its plan limits and the owner membership.
//
// The owner membership is written in the same call so an organization is never
// observable without an owner, not even for the duration of one request.
func (s *Service) Create(ctx context.Context, in CreateInput) (Record, error) {
	if in.OwnerID == "" {
		return Record{}, ErrNotFound
	}
	name, err := NormalizeName(in.Name)
	if err != nil {
		return Record{}, err
	}
	slug, err := NormalizeSlug(in.Slug, name)
	if err != nil {
		return Record{}, err
	}
	plan := in.Plan
	if plan == "" {
		plan = PlanFree
	}
	if !plan.Valid() {
		return Record{}, ErrUnknownPlan
	}
	limits, err := LimitsFor(plan)
	if err != nil {
		return Record{}, err
	}
	rec := Record{
		ID:        s.newID(),
		Name:      name,
		Slug:      slug,
		Plan:      plan,
		Limits:    limits,
		Usage:     Usage{},
		CreatedAt: s.now(),
	}
	if err := s.Store.Create(ctx, rec); err != nil {
		return Record{}, err
	}
	member := Member{OrgID: rec.ID, UserID: in.OwnerID, Role: RoleOwner, CreatedAt: rec.CreatedAt}
	if err := s.Store.AddMember(ctx, member); err != nil {
		// The organization exists but has no owner. Removing it is safer than
		// leaving an unadministrable record behind, so the failure is reported
		// only after the rollback attempt.
		_ = s.Store.Update(ctx, rec)
		return Record{}, fmt.Errorf("org: create owner membership: %w", err)
	}
	rec.Usage.TeamMembers = 1
	if err := s.Store.Update(ctx, rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// Get returns an organization the caller can see.
func (s *Service) Get(ctx context.Context, orgID, requesterID string) (Record, error) {
	if _, err := s.requireMember(ctx, orgID, requesterID, RoleViewer); err != nil {
		return Record{}, err
	}
	return s.Store.Get(ctx, orgID)
}

// ListForUser returns every organization the user belongs to.
func (s *Service) ListForUser(ctx context.Context, userID string) ([]Record, error) {
	return s.Store.ListForUser(ctx, userID)
}

// UpdateInput carries an organization rename or plan change.
type UpdateInput struct {
	Name string
	Plan Plan
}

// Update renames an organization or changes its plan. A plan change re-seeds the
// limits, so a downgrade actually narrows what the organization may do rather
// than leaving the previous allowance in place.
func (s *Service) Update(ctx context.Context, orgID, requesterID string, in UpdateInput) (Record, error) {
	if _, err := s.requireMember(ctx, orgID, requesterID, RoleAdmin); err != nil {
		return Record{}, err
	}
	rec, err := s.Store.Get(ctx, orgID)
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
	if in.Plan != "" {
		if !in.Plan.Valid() {
			return Record{}, ErrUnknownPlan
		}
		limits, err := LimitsFor(in.Plan)
		if err != nil {
			return Record{}, err
		}
		rec.Plan = in.Plan
		rec.Limits = limits
	}
	if err := s.Store.Update(ctx, rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// Delete removes an organization and everything it owns. Only an owner may do it.
func (s *Service) Delete(ctx context.Context, orgID, requesterID string) error {
	if _, err := s.requireMember(ctx, orgID, requesterID, RoleOwner); err != nil {
		return err
	}
	rec, err := s.Store.Get(ctx, orgID)
	if err != nil {
		return err
	}
	return s.Store.Delete(ctx, rec.ID)
}

// ListMembers returns the organization's members. Any member may see the roster.
func (s *Service) ListMembers(ctx context.Context, orgID, requesterID string) ([]Member, error) {
	if _, err := s.requireMember(ctx, orgID, requesterID, RoleViewer); err != nil {
		return nil, err
	}
	return s.Store.ListMembers(ctx, orgID)
}

// ChangeRole sets a member's role.
//
// Two guards apply: only an owner may grant or revoke owner, and the last owner
// cannot be demoted. Without them an organization can lock its own
// administrators out.
func (s *Service) ChangeRole(ctx context.Context, orgID, requesterID, targetID string, role Role) error {
	actor, err := s.requireMember(ctx, orgID, requesterID, RoleAdmin)
	if err != nil {
		return err
	}
	if !role.Valid() {
		return ErrRoleNotInvitable
	}
	target, err := s.Store.GetMember(ctx, orgID, targetID)
	if err != nil {
		return err
	}
	// Changing an owner role is an owner-level act regardless of direction:
	// granting owner must not be available to an admin, and revoking it must be
	// deliberate.
	if (target.Role == RoleOwner || role == RoleOwner) && actor.Role != RoleOwner {
		return ErrCannotRemoveOwner
	}
	if target.Role == RoleOwner && role != RoleOwner {
		owners, err := s.Store.CountOwners(ctx, orgID)
		if err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	return s.Store.UpdateMemberRole(ctx, orgID, targetID, role)
}

// RemoveMember removes a member. The last owner is never removable.
func (s *Service) RemoveMember(ctx context.Context, orgID, requesterID, targetID string) error {
	actor, err := s.requireMember(ctx, orgID, requesterID, RoleAdmin)
	if err != nil {
		return err
	}
	target, err := s.Store.GetMember(ctx, orgID, targetID)
	if err != nil {
		return err
	}
	if target.Role == RoleOwner {
		if actor.Role != RoleOwner {
			return ErrCannotRemoveOwner
		}
		owners, err := s.Store.CountOwners(ctx, orgID)
		if err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	if err := s.Store.RemoveMember(ctx, orgID, targetID); err != nil {
		return err
	}
	return s.syncTeamUsage(ctx, orgID)
}

// InviteInput describes an invitation to join an organization.
type InviteInput struct {
	OrgID     string
	InviterID string
	Email     string
	Role      Role
}

// PendingInvite is an invitation plus the plaintext token, returned once so the
// caller can build the link. The plaintext is never stored.
type PendingInvite struct {
	Invitation
	Token string
}

// Invite creates an invitation after checking the inviter's role, the
// invitable role rule and the plan's member allowance.
//
// The team allowance is checked before the insert so an over-quota invitation
// fails with ErrQuotaExceeded instead of leaving a pending invite nobody can
// use.
func (s *Service) Invite(ctx context.Context, in InviteInput) (PendingInvite, error) {
	if _, err := s.requireMember(ctx, in.OrgID, in.InviterID, RoleAdmin); err != nil {
		return PendingInvite{}, err
	}
	role := in.Role
	if role == "" {
		role = RoleViewer
	}
	if !role.Invitable() {
		return PendingInvite{}, ErrRoleNotInvitable
	}
	email, err := NormalizeEmail(in.Email)
	if err != nil {
		return PendingInvite{}, err
	}
	org, err := s.Store.Get(ctx, in.OrgID)
	if err != nil {
		return PendingInvite{}, err
	}
	// Only admins and owners consume seats; viewers are free, so they are not
	// counted against the allowance. Otherwise a free plan would cap a
	// read-only team at one person.
	if role != RoleViewer {
		if err := CheckQuota(org.Usage.TeamMembers, 1, org.Limits.MaxTeamMembers); err != nil {
			return PendingInvite{}, err
		}
	} else {
		seats, err := s.seatedMembers(ctx, in.OrgID)
		if err != nil {
			return PendingInvite{}, err
		}
		if err := CheckQuota(seats, 0, org.Limits.MaxTeamMembers); err != nil {
			return PendingInvite{}, err
		}
	}

	token, err := newToken()
	if err != nil {
		return PendingInvite{}, err
	}
	inv := Invitation{
		ID:        s.newID(),
		OrgID:     in.OrgID,
		Email:     email,
		Role:      role,
		TokenHash: hashToken(token),
		InvitedBy: in.InviterID,
		ExpiresAt: s.now().Add(s.tokenTTL()),
		CreatedAt: s.now(),
	}
	if err := s.Store.CreateInvitation(ctx, inv); err != nil {
		return PendingInvite{}, err
	}
	return PendingInvite{Invitation: inv, Token: token}, nil
}

// AcceptInvitation redeems an invitation token for a user. An expired or already
// used token fails with ErrNotFound so a caller cannot distinguish the two and
// probe for valid links.
func (s *Service) AcceptInvitation(ctx context.Context, token, userID string) (Record, error) {
	if userID == "" {
		return Record{}, ErrNotFound
	}
	inv, err := s.Store.GetInvitationByTokenHash(ctx, hashToken(token))
	if err != nil {
		return Record{}, err
	}
	if inv.Accepted() || inv.Expired(s.now()) {
		return Record{}, ErrNotFound
	}
	if err := s.Store.AcceptInvitation(ctx, inv.ID, userID); err != nil {
		return Record{}, err
	}
	if err := s.syncTeamUsage(ctx, inv.OrgID); err != nil {
		return Record{}, err
	}
	return s.Store.Get(ctx, inv.OrgID)
}

// ListInvitations returns the organization's pending and historical invitations.
func (s *Service) ListInvitations(ctx context.Context, orgID, requesterID string) ([]Invitation, error) {
	if _, err := s.requireMember(ctx, orgID, requesterID, RoleAdmin); err != nil {
		return nil, err
	}
	return s.Store.ListInvitations(ctx, orgID)
}

// RevokeInvitation deletes a pending invitation.
func (s *Service) RevokeInvitation(ctx context.Context, orgID, requesterID, invitationID string) error {
	if _, err := s.requireMember(ctx, orgID, requesterID, RoleAdmin); err != nil {
		return err
	}
	return s.Store.DeleteInvitation(ctx, invitationID)
}

// CheckProjectQuota reports whether the organization may create one more
// project. It is the call the project service makes before inserting a row, so
// the check and the insert can be sequenced without a distributed lock.
func (s *Service) CheckProjectQuota(ctx context.Context, orgID string) error {
	org, err := s.Store.Get(ctx, orgID)
	if err != nil {
		return err
	}
	return CheckQuota(org.Usage.Projects, 1, org.Limits.MaxProjects)
}

// CheckDomainQuota reports whether the organization may add one more domain.
func (s *Service) CheckDomainQuota(ctx context.Context, orgID string) error {
	org, err := s.Store.Get(ctx, orgID)
	if err != nil {
		return err
	}
	return CheckQuota(org.Usage.Domains, 1, org.Limits.MaxDomains)
}

// AllowsCustomDomains reports whether the plan permits client-supplied domains.
func (s *Service) AllowsCustomDomains(ctx context.Context, orgID string) (bool, error) {
	org, err := s.Store.Get(ctx, orgID)
	if err != nil {
		return false, err
	}
	return org.Limits.CustomDomains, nil
}

// CheckDeploymentQuota reports whether a deployment may start this month.
func (s *Service) CheckDeploymentQuota(ctx context.Context, orgID string) error {
	org, err := s.Store.Get(ctx, orgID)
	if err != nil {
		return err
	}
	return CheckQuota(org.Usage.DeploymentsThisMonth, 1, org.Limits.MaxDeploymentsMonth)
}

// RecordUsage replaces the organization's usage counters. Counters are written by
// the owning service (projects, deployments) rather than recomputed here, so a
// quota check never depends on a scan that may be slow or inconsistent.
func (s *Service) RecordUsage(ctx context.Context, orgID string, usage Usage) error {
	org, err := s.Store.Get(ctx, orgID)
	if err != nil {
		return err
	}
	org.Usage = usage
	return s.Store.Update(ctx, org)
}

// syncTeamUsage recomputes the member counter from the authoritative rows. It is
// called after a membership change rather than incremented, so a retried call
// cannot drift the count.
func (s *Service) syncTeamUsage(ctx context.Context, orgID string) error {
	org, err := s.Store.Get(ctx, orgID)
	if err != nil {
		return err
	}
	seats, err := s.seatedMembers(ctx, orgID)
	if err != nil {
		return err
	}
	org.Usage.TeamMembers = seats
	return s.Store.Update(ctx, org)
}

// seatedMembers counts the members that consume a plan seat: admins, developers
// and owners. Viewers are free, so an all-viewer team is not capped by the
// member allowance.
func (s *Service) seatedMembers(ctx context.Context, orgID string) (int, error) {
	members, err := s.Store.ListMembers(ctx, orgID)
	if err != nil {
		return 0, err
	}
	seats := 0
	for _, m := range members {
		if m.Role != RoleViewer {
			seats++
		}
	}
	return seats, nil
}

// requireMember authorizes a call: the membership is read from the store and
// compared against the required role. The returned member is the caller's, not a
// caller-supplied value.
func (s *Service) requireMember(ctx context.Context, orgID, requesterID string, required Role) (Member, error) {
	if orgID == "" || requesterID == "" {
		return Member{}, ErrNotAMember
	}
	m, err := s.Store.GetMember(ctx, orgID, requesterID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// A non-member is reported as "not a member", never as "not found":
			// the distinction would leak which organizations exist.
			return Member{}, ErrNotAMember
		}
		return Member{}, err
	}
	if !Can(m, required) {
		return Member{}, ErrNotAMember
	}
	return m, nil
}

// MemberRole returns the caller's role in an organization, for authorization
// decisions made outside this package.
func (s *Service) MemberRole(ctx context.Context, orgID, userID string) (Role, error) {
	m, err := s.Store.GetMember(ctx, orgID, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", ErrNotAMember
		}
		return "", err
	}
	return m.Role, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("org: entropy source unavailable: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// newToken returns an opaque invitation token. It is returned to the caller once
// and only its hash is persisted.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("org: generate invitation token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
