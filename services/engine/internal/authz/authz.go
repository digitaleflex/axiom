// Package authz is the server-side authorization boundary (issue #127). It
// resolves an actor's relationship to a resource and decides whether a
// closed set of actions is permitted.
//
// V0.1 policy is single-user ownership: an actor may act on a resource only
// when they own it (application.ownerId == actor). Organization membership
// (future) extends the same decision through Roles without changing the
// call sites.
//
// Deliberate divergence from plain FORBIDDEN semantics: the API keeps
// answering 404 for resource IDs the caller does not own, on reads AND on
// writes, so resource existence is never leaked. authz is enforced as
// defense-in-depth beneath that hiding rule: if a future change ever makes a
// foreign resource visible, writes still fail closed with FORBIDDEN. See
// docs/architecture/authorization.md.
package authz

import "context"

// Action is the closed set of operations the API can authorize. Callers
// cannot invent actions at runtime; unknown actions are denied.
type Action string

const (
	ActionApplicationRead   Action = "application.read"
	ActionApplicationWrite  Action = "application.write"
	ActionApplicationDelete Action = "application.delete"
	ActionDeploymentCreate  Action = "deployment.create"
	ActionDeploymentCancel  Action = "deployment.cancel"
	// ActionDeploymentRead guards read-only deployment projections, notably
	// the diagnostics API (#102). It is a read action: an organization
	// member may inspect a failure without being able to change it.
	ActionDeploymentRead Action = "deployment.read"
	ActionServerRead     Action = "server.read"
	ActionServerWrite    Action = "server.write"
	ActionServerRegister Action = "server.register"
	ActionServerRemove   Action = "server.remove"
	ActionDomainWrite    Action = "domain.write"
	ActionConfigWrite    Action = "config.write"
	ActionGitHubManage   Action = "github.manage"
)

// Actor is the authenticated principal mirrored from api.Principal. The
// authz package cannot import api (api imports authz), so the identity
// fields are duplicated here by design.
type Actor struct {
	UserID string
	Name   string
}

// Resource describes the target of an authorization decision. OwnerID is
// the V0.1 ownership anchor: when it equals the actor, the actor is the
// owner. ApplicationID scopes child resources (deployments, domains,
// config) for audit and future policy rules. OrgID is the future
// organization boundary (V0.1 leaves it empty).
type Resource struct {
	Type          string // application, deployment, server, domain, config, github
	ID            string
	ApplicationID string
	OwnerID       string
	OrgID         string
}

// Relationship is the actor's relationship to a resource.
type Relationship int

const (
	// RelationshipNone: the actor has no relationship to the resource.
	RelationshipNone Relationship = iota
	// RelationshipMember: the actor belongs to an organization that may
	// access the resource (future; V0.1 never returns it).
	RelationshipMember
	// RelationshipOwner: the actor owns the resource.
	RelationshipOwner
)

// String renders the relationship for logs and tests.
func (r Relationship) String() string {
	switch r {
	case RelationshipOwner:
		return "owner"
	case RelationshipMember:
		return "member"
	default:
		return "none"
	}
}

// Roles resolves organization membership for the future multi-tenant
// boundary. V0.1 ships NoRoles: there are no organizations yet.
type Roles interface {
	IsMember(ctx context.Context, userID, orgID string) (bool, error)
}

// NoRoles is the V0.1 Roles implementation: no organization membership.
type NoRoles struct{}

// IsMember always reports false in V0.1.
func (NoRoles) IsMember(context.Context, string, string) (bool, error) { return false, nil }

// Resolver resolves an actor's relationship to a resource. It is
// stateless: ownership is derived from the resource's OwnerID, which the
// API loads before calling Resolve.
type Resolver struct {
	roles Roles
}

// NewResolver builds a Resolver. A nil roles selects NoRoles (V0.1).
func NewResolver(roles Roles) *Resolver {
	if roles == nil {
		roles = NoRoles{}
	}
	return &Resolver{roles: roles}
}

// Resolve returns the actor's relationship to the resource. Owner wins;
// otherwise organization membership (future) is consulted; otherwise None.
func (r *Resolver) Resolve(ctx context.Context, actor Actor, res Resource) (Relationship, error) {
	if res.OwnerID != "" && res.OwnerID == actor.UserID {
		return RelationshipOwner, nil
	}
	// Future: organization membership grants RelationshipMember for
	// resources shared with the actor's organizations. V0.1 has no orgs.
	if r.roles != nil && res.OrgID != "" {
		if ok, err := r.roles.IsMember(ctx, actor.UserID, res.OrgID); err != nil {
			return RelationshipNone, err
		} else if ok {
			return RelationshipMember, nil
		}
	}
	return RelationshipNone, nil
}

// Authorize reports whether the actor may perform action on resource, with a
// human-readable reason when denied. V0.1 policy: owners may act on their
// resources; members (future orgs) may act when the action is read-shaped;
// everyone else is forbidden. The reason is stable for logs and tests.
func (r *Resolver) Authorize(ctx context.Context, actor Actor, action Action, res Resource) (bool, string) {
	rel, err := r.Resolve(ctx, actor, res)
	if err != nil {
		return false, "relationship_resolution_failed"
	}
	switch rel {
	case RelationshipOwner:
		return true, ""
	case RelationshipMember:
		// Future org boundary: members may read, not write.
		if isReadAction(action) {
			return true, ""
		}
		return false, "organization members may not modify this resource"
	default:
		return false, "forbidden: caller does not own this resource"
	}
}

// isReadAction reports whether an action only observes state.
func isReadAction(action Action) bool {
	switch action {
	case ActionApplicationRead, ActionServerRead, ActionDeploymentRead:
		return true
	default:
		return false
	}
}
