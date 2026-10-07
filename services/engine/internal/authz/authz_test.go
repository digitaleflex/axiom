package authz

import (
	"context"
	"errors"
	"testing"
)

func TestResolveOwnership(t *testing.T) {
	r := NewResolver(nil)
	ctx := context.Background()
	actor := Actor{UserID: "usr_1", Name: "Jane"}

	rel, err := r.Resolve(ctx, actor, Resource{Type: "application", ID: "app_1", OwnerID: "usr_1"})
	if err != nil || rel != RelationshipOwner {
		t.Fatalf("owner resolve = %v, %v", rel, err)
	}
	rel, err = r.Resolve(ctx, actor, Resource{Type: "application", ID: "app_2", OwnerID: "usr_2"})
	if err != nil || rel != RelationshipNone {
		t.Fatalf("non-owner resolve = %v, %v", rel, err)
	}
	rel, err = r.Resolve(ctx, actor, Resource{Type: "server", ID: "srv_1", OwnerID: ""})
	if err != nil || rel != RelationshipNone {
		t.Fatalf("empty owner resolve = %v, %v", rel, err)
	}
}

type errRoles struct{ err error }

func (e errRoles) IsMember(context.Context, string, string) (bool, error) { return false, e.err }

type memberRoles struct{}

func (memberRoles) IsMember(_ context.Context, userID, orgID string) (bool, error) {
	return userID == "usr_member" && orgID == "org_1", nil
}

func TestResolveMembershipAndRoleErrors(t *testing.T) {
	ctx := context.Background()

	// Membership (future boundary) resolves to Member, not Owner.
	r := NewResolver(memberRoles{})
	rel, err := r.Resolve(ctx, Actor{UserID: "usr_member"}, Resource{Type: "application", ID: "app_9", OwnerID: "usr_2", OrgID: "org_1"})
	if err != nil || rel != RelationshipMember {
		t.Fatalf("member resolve = %v, %v", rel, err)
	}
	// Ownership outranks membership.
	rel, err = r.Resolve(ctx, Actor{UserID: "usr_member"}, Resource{Type: "application", ID: "app_9", OwnerID: "usr_member", OrgID: "org_1"})
	if err != nil || rel != RelationshipOwner {
		t.Fatalf("owner outranks member = %v, %v", rel, err)
	}

	// Role resolution failure surfaces as an error (fail closed downstream).
	r = NewResolver(errRoles{err: errors.New("roles unavailable")})
	if _, err := r.Resolve(ctx, Actor{UserID: "usr_1"}, Resource{Type: "application", ID: "app_1", OrgID: "org_1"}); err == nil {
		t.Fatal("role error must propagate")
	}
}

func TestAuthorizeV01SingleUserOwnership(t *testing.T) {
	r := NewResolver(nil)
	ctx := context.Background()
	owner := Actor{UserID: "usr_1"}
	other := Actor{UserID: "usr_2"}

	cases := []struct {
		name    string
		actor   Actor
		action  Action
		allowed bool
		reason  string
	}{
		{"owner reads application", owner, ActionApplicationRead, true, ""},
		{"owner writes application", owner, ActionApplicationWrite, true, ""},
		{"owner deletes application", owner, ActionApplicationDelete, true, ""},
		{"owner creates deployment", owner, ActionDeploymentCreate, true, ""},
		{"owner cancels deployment", owner, ActionDeploymentCancel, true, ""},
		{"owner reads server", owner, ActionServerRead, true, ""},
		{"owner registers server", owner, ActionServerRegister, true, ""},
		{"owner removes server", owner, ActionServerRemove, true, ""},
		{"owner writes domain", owner, ActionDomainWrite, true, ""},
		{"owner writes config", owner, ActionConfigWrite, true, ""},
		{"owner manages github", owner, ActionGitHubManage, true, ""},
		{"non-owner reads application", other, ActionApplicationRead, false, "forbidden: caller does not own this resource"},
		{"non-owner writes application", other, ActionApplicationWrite, false, "forbidden: caller does not own this resource"},
		{"non-owner creates deployment", other, ActionDeploymentCreate, false, "forbidden: caller does not own this resource"},
		{"non-owner cancels deployment", other, ActionDeploymentCancel, false, "forbidden: caller does not own this resource"},
		{"non-owner removes server", other, ActionServerRemove, false, "forbidden: caller does not own this resource"},
		{"non-owner writes domain", other, ActionDomainWrite, false, "forbidden: caller does not own this resource"},
		{"non-owner writes config", other, ActionConfigWrite, false, "forbidden: caller does not own this resource"},
		{"non-owner manages github", other, ActionGitHubManage, false, "forbidden: caller does not own this resource"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed, reason := r.Authorize(ctx, tc.actor, tc.action, Resource{Type: "application", ID: "app_1", OwnerID: "usr_1"})
			if allowed != tc.allowed || reason != tc.reason {
				t.Fatalf("Authorize(%s, %s) = %v, %q; want %v, %q", tc.actor.UserID, tc.action, allowed, reason, tc.allowed, tc.reason)
			}
		})
	}
}

func TestAuthorizeMemberFutureBoundary(t *testing.T) {
	r := NewResolver(memberRoles{})
	ctx := context.Background()
	member := Actor{UserID: "usr_member"}

	// Members may read but not write (future org boundary).
	if ok, reason := r.Authorize(ctx, member, ActionApplicationRead, Resource{Type: "application", ID: "app_9", OwnerID: "usr_2", OrgID: "org_1"}); !ok {
		t.Fatalf("member read must be allowed, reason=%q", reason)
	}
	if ok, _ := r.Authorize(ctx, member, ActionApplicationWrite, Resource{Type: "application", ID: "app_9", OwnerID: "usr_2", OrgID: "org_1"}); ok {
		t.Fatal("member write must be forbidden")
	}
}

func TestActionSetIsClosed(t *testing.T) {
	// Every action the API uses must be a declared constant; the compiler
	// enforces the closed set. This test pins the V0.1 vocabulary.
	actions := []Action{
		ActionApplicationRead, ActionApplicationWrite, ActionApplicationDelete,
		ActionDeploymentCreate, ActionDeploymentCancel,
		ActionServerRead, ActionServerWrite, ActionServerRegister, ActionServerRemove,
		ActionDomainWrite, ActionConfigWrite, ActionGitHubManage,
	}
	for _, a := range actions {
		if a == "" {
			t.Fatal("empty action in vocabulary")
		}
	}
}
