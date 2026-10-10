// Package org manages organizations (issue #146, M12.1): the ownership boundary
// that replaces the V0.1 single-user model. An organization owns projects, its
// members hold roles, and plan limits are stored with it.
//
// Roles are ordered, not arbitrary strings: an owner can do everything an admin
// can, an admin everything a developer can, and a developer everything a viewer
// can. Can() is therefore a comparison, not a switch that grows with every new
// permission.
package org

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Role is a member's permission level inside an organization.
type Role string

const (
	RoleOwner     Role = "owner"
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
	RoleViewer    Role = "viewer"
)

// rank orders the roles. A higher rank satisfies any requirement of a lower one,
// which is what lets Can compare two roles instead of enumerating permissions.
var rank = map[Role]int{
	RoleViewer:    1,
	RoleDeveloper: 2,
	RoleAdmin:     3,
	RoleOwner:     4,
}

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	_, ok := rank[r]
	return ok
}

// AtLeast reports whether r satisfies the required role.
func (r Role) AtLeast(required Role) bool {
	if !r.Valid() || !required.Valid() {
		return false
	}
	return rank[r] >= rank[required]
}

// Invitable reports whether r may be granted through an invitation. owner is
// deliberately excluded: ownership transfers are an explicit act, not a side
// effect of inviting somebody.
func (r Role) Invitable() bool {
	switch r {
	case RoleAdmin, RoleDeveloper, RoleViewer:
		return true
	default:
		return false
	}
}

// Plan is the organization's subscription tier.
type Plan string

const (
	PlanFree       Plan = "free"
	PlanStarter    Plan = "starter"
	PlanPro        Plan = "pro"
	PlanEnterprise Plan = "enterprise"
)

// Valid reports whether p is a known plan.
func (p Plan) Valid() bool {
	switch p {
	case PlanFree, PlanStarter, PlanPro, PlanEnterprise:
		return true
	default:
		return false
	}
}

// Limits are the quotas a plan grants. A zero value means "not granted", never
// "unlimited": an absent key must fail a quota check rather than open it.
type Limits struct {
	MaxProjects         int  `json:"max_projects"`
	MaxMemoryMB         int  `json:"max_memory_mb"`
	MaxCPUPercent       int  `json:"max_cpu_percent"`
	MaxDomains          int  `json:"max_domains"`
	MaxDeploymentsMonth int  `json:"max_deployments_month"`
	MaxTeamMembers      int  `json:"max_team_members"`
	CustomDomains       bool `json:"custom_domains"`
	SSLAuto             bool `json:"ssl_auto"`
	AuditLogs           bool `json:"audit_logs"`
}

// FreeLimits is the seeded allowance for a new organization.
func FreeLimits() Limits {
	return Limits{
		MaxProjects:         1,
		MaxMemoryMB:         512,
		MaxCPUPercent:       50,
		MaxDomains:          1,
		MaxDeploymentsMonth: 100,
		MaxTeamMembers:      1,
		CustomDomains:       false,
		SSLAuto:             true,
		AuditLogs:           false,
	}
}

// StarterLimits, ProLimits and EnterpriseLimits are the paid allowances.
// Enterprise is intentionally generous but finite: an implicit unlimited
// allowance would make an over-provisioning bug invisible until it is billed.
func StarterLimits() Limits {
	return Limits{
		MaxProjects:         5,
		MaxMemoryMB:         2048,
		MaxCPUPercent:       100,
		MaxDomains:          5,
		MaxDeploymentsMonth: 0, // 0 = governed by MaxProjects, not unlimited
		MaxTeamMembers:      5,
		CustomDomains:       true,
		SSLAuto:             true,
		AuditLogs:           false,
	}
}

func ProLimits() Limits {
	return Limits{
		MaxProjects:         25,
		MaxMemoryMB:         8192,
		MaxCPUPercent:       400,
		MaxDomains:          0, // 0 = no cap on domains
		MaxDeploymentsMonth: 0,
		MaxTeamMembers:      25,
		CustomDomains:       true,
		SSLAuto:             true,
		AuditLogs:           true,
	}
}

func EnterpriseLimits() Limits {
	return Limits{
		MaxProjects:         0,
		MaxMemoryMB:         0,
		MaxCPUPercent:       0,
		MaxDomains:          0,
		MaxDeploymentsMonth: 0,
		MaxTeamMembers:      0,
		CustomDomains:       true,
		SSLAuto:             true,
		AuditLogs:           true,
	}
}

// LimitsFor returns the allowance a plan grants.
func LimitsFor(p Plan) (Limits, error) {
	switch p {
	case PlanFree:
		return FreeLimits(), nil
	case PlanStarter:
		return StarterLimits(), nil
	case PlanPro:
		return ProLimits(), nil
	case PlanEnterprise:
		return EnterpriseLimits(), nil
	default:
		return Limits{}, ErrUnknownPlan
	}
}

// Usage is the organization's current consumption. Counters are denormalized on
// the organization so a quota check is one row read, not a table scan.
type Usage struct {
	Projects             int `json:"projects"`
	DeploymentsThisMonth int `json:"deployments_this_month"`
	MemoryMB             int `json:"memory_mb"`
	CPUPercent           int `json:"cpu_percent"`
	Domains              int `json:"domains"`
	TeamMembers          int `json:"team_members"`
}

// Record is an organization.
type Record struct {
	ID                string
	Name              string
	Slug              string
	Plan              Plan
	Limits            Limits
	Usage             Usage
	BillingCustomerID string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Member is a user's membership in an organization.
type Member struct {
	OrgID     string
	UserID    string
	Role      Role
	InvitedBy string
	CreatedAt time.Time
}

// Can reports whether the member may perform an action requiring required.
// An organization must never be left without an owner, so owner-level actions
// are additionally checked by the service, not only by role comparison.
func Can(m Member, required Role) bool { return m.Role.AtLeast(required) }

// Errors are the stable failures callers branch on.
var (
	// ErrNotFound is returned for unknown organizations and members.
	ErrNotFound = errors.New("org: not found")
	// ErrAlreadyExists is returned when a slug or name collides.
	ErrAlreadyExists = errors.New("org: already exists")
	// ErrInvalidSlug is returned for a malformed slug.
	ErrInvalidSlug = errors.New("org: invalid slug")
	// ErrInvalidName is returned for an empty or oversized name.
	ErrInvalidName = errors.New("org: invalid name")
	// ErrNotAMember is returned when a user does not belong to the organization.
	ErrNotAMember = errors.New("org: user is not a member")
	// ErrLastOwner is returned when an operation would remove the last owner.
	ErrLastOwner = errors.New("org: an organization must keep at least one owner")
	// ErrCannotRemoveOwner is returned when a non-owner attempts to demote an owner.
	ErrCannotRemoveOwner = errors.New("org: only an owner may change an owner's role")
	// ErrRoleNotInvitable is returned when an invitation names a non-invitable role.
	ErrRoleNotInvitable = errors.New("org: role cannot be granted by invitation")
	// ErrUnknownPlan is returned for a plan with no defined allowance.
	ErrUnknownPlan = errors.New("org: unknown plan")
	// ErrQuotaExceeded is returned when a limit would be exceeded.
	ErrQuotaExceeded = errors.New("org: quota exceeded")
	// ErrInvalidEmail is returned for a malformed invitation address.
	ErrInvalidEmail = errors.New("org: invalid email")
)

// NormalizeSlug lowercases and validates a slug: lowercase letters, digits and
// inner hyphens, 1-63 characters. An empty slug is derived from the name.
func NormalizeSlug(slug, name string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(slug))
	if s == "" {
		s = slugify(name)
	}
	if !validSlug(s) {
		return "", ErrInvalidSlug
	}
	return s, nil
}

// NormalizeName validates an organization name.
func NormalizeName(name string) (string, error) {
	n := strings.TrimSpace(name)
	if len(n) == 0 || len(n) > 120 {
		return "", ErrInvalidName
	}
	return n, nil
}

func slugify(s string) string {
	var b strings.Builder
	lastDash := true // suppress a leading dash
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == ' ' || r == '_' || r == '.':
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 63 {
		out = strings.Trim(out[:63], "-")
	}
	return out
}

func validSlug(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-':
			if i == 0 || i == len(s)-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// NormalizeEmail lowercases and trims an invitation address.
func NormalizeEmail(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	at := strings.IndexByte(e, '@')
	if at <= 0 || at == len(e)-1 || strings.Count(e, "@") != 1 {
		return "", ErrInvalidEmail
	}
	domain := e[at+1:]
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", ErrInvalidEmail
	}
	return e, nil
}

// CheckQuota reports whether adding delta to current stays within the limit.
// limit == 0 means no cap (Enterprise). A negative delta is a release and is
// always allowed: shrinking usage must never be blocked by a quota.
func CheckQuota(current, delta, limit int) error {
	if limit == 0 {
		return nil
	}
	if delta < 0 {
		return nil
	}
	if current+delta > limit {
		return ErrQuotaExceeded
	}
	return nil
}

// Store persists organizations and memberships.
type Store interface {
	Create(ctx context.Context, r Record) error
	Get(ctx context.Context, id string) (Record, error)
	GetBySlug(ctx context.Context, slug string) (Record, error)
	Update(ctx context.Context, r Record) error
	Delete(ctx context.Context, id string) error
	ListForUser(ctx context.Context, userID string) ([]Record, error)

	AddMember(ctx context.Context, m Member) error
	GetMember(ctx context.Context, orgID, userID string) (Member, error)
	ListMembers(ctx context.Context, orgID string) ([]Member, error)
	UpdateMemberRole(ctx context.Context, orgID, userID string, role Role) error
	RemoveMember(ctx context.Context, orgID, userID string) error
	CountOwners(ctx context.Context, orgID string) (int, error)

	CreateInvitation(ctx context.Context, inv Invitation) error
	GetInvitationByTokenHash(ctx context.Context, tokenHash string) (Invitation, error)
	ListInvitations(ctx context.Context, orgID string) ([]Invitation, error)
	AcceptInvitation(ctx context.Context, id, userID string) error
	DeleteInvitation(ctx context.Context, id string) error
}

// Invitation is a pending membership offer. TokenHash is the SHA-256 of the
// opaque token; the plaintext is returned once in the invitation link.
type Invitation struct {
	ID         string
	OrgID      string
	Email      string
	Role       Role
	TokenHash  string
	InvitedBy  string
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	CreatedAt  time.Time
}

// Pending reports whether the invitation can still be redeemed.
func (i Invitation) Pending(now time.Time) bool { return !i.Accepted() && !i.Expired(now) }

// Expired reports whether the invitation can no longer be accepted.
func (i Invitation) Expired(now time.Time) bool { return now.After(i.ExpiresAt) }

// Accepted reports whether the invitation was already used.
func (i Invitation) Accepted() bool { return i.AcceptedAt != nil }
