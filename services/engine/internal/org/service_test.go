package org_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/org"
)

// memStore is an in-memory Store. It mirrors the SQL constraints the real store
// enforces: slugs are unique, one membership per (org,user), and an invitation
// cannot be accepted twice.
type memStore struct {
	orgs        map[string]org.Record
	bySlug      map[string]string
	members     map[string]map[string]org.Member // orgID → userID → member
	invitations map[string]org.Invitation
	byToken     map[string]string // tokenHash → invitation id
	seq         int
}

func newMemStore() *memStore {
	return &memStore{
		orgs:        map[string]org.Record{},
		bySlug:      map[string]string{},
		members:     map[string]map[string]org.Member{},
		invitations: map[string]org.Invitation{},
		byToken:     map[string]string{},
	}
}

func (s *memStore) Create(_ context.Context, r org.Record) error {
	if _, ok := s.bySlug[r.Slug]; ok {
		return org.ErrAlreadyExists
	}
	s.orgs[r.ID] = r
	s.bySlug[r.Slug] = r.ID
	s.members[r.ID] = map[string]org.Member{}
	return nil
}

func (s *memStore) Get(_ context.Context, id string) (org.Record, error) {
	r, ok := s.orgs[id]
	if !ok {
		return org.Record{}, org.ErrNotFound
	}
	return r, nil
}

func (s *memStore) GetBySlug(_ context.Context, slug string) (org.Record, error) {
	id, ok := s.bySlug[slug]
	if !ok {
		return org.Record{}, org.ErrNotFound
	}
	return s.Get(context.Background(), id)
}

func (s *memStore) Update(_ context.Context, r org.Record) error {
	if _, ok := s.orgs[r.ID]; !ok {
		return org.ErrNotFound
	}
	s.orgs[r.ID] = r
	return nil
}

func (s *memStore) Delete(_ context.Context, id string) error {
	r, ok := s.orgs[id]
	if !ok {
		return org.ErrNotFound
	}
	delete(s.bySlug, r.Slug)
	delete(s.orgs, id)
	delete(s.members, id)
	for iid, inv := range s.invitations {
		if inv.OrgID == id {
			delete(s.byToken, inv.TokenHash)
			delete(s.invitations, iid)
		}
	}
	return nil
}

func (s *memStore) ListForUser(_ context.Context, userID string) ([]org.Record, error) {
	var out []org.Record
	for id := range s.orgs {
		if m, ok := s.members[id][userID]; ok {
			_ = m
			out = append(out, s.orgs[id])
		}
	}
	return out, nil
}

func (s *memStore) AddMember(_ context.Context, m org.Member) error {
	if _, ok := s.orgs[m.OrgID]; !ok {
		return org.ErrNotFound
	}
	if _, exists := s.members[m.OrgID][m.UserID]; exists {
		return org.ErrAlreadyExists
	}
	s.members[m.OrgID][m.UserID] = m
	return nil
}

func (s *memStore) GetMember(_ context.Context, orgID, userID string) (org.Member, error) {
	m, ok := s.members[orgID][userID]
	if !ok {
		return org.Member{}, org.ErrNotFound
	}
	return m, nil
}

func (s *memStore) ListMembers(_ context.Context, orgID string) ([]org.Member, error) {
	var out []org.Member
	for _, m := range s.members[orgID] {
		out = append(out, m)
	}
	return out, nil
}

func (s *memStore) UpdateMemberRole(_ context.Context, orgID, userID string, role org.Role) error {
	m, ok := s.members[orgID][userID]
	if !ok {
		return org.ErrNotFound
	}
	m.Role = role
	s.members[orgID][userID] = m
	return nil
}

func (s *memStore) RemoveMember(_ context.Context, orgID, userID string) error {
	if _, ok := s.members[orgID][userID]; !ok {
		return org.ErrNotFound
	}
	delete(s.members[orgID], userID)
	return nil
}

func (s *memStore) CountOwners(ctx context.Context, orgID string) (int, error) {
	members, _ := s.ListMembers(ctx, orgID)
	n := 0
	for _, m := range members {
		if m.Role == org.RoleOwner {
			n++
		}
	}
	return n, nil
}

func (s *memStore) CreateInvitation(_ context.Context, inv org.Invitation) error {
	for _, existing := range s.invitations {
		if existing.OrgID == inv.OrgID && existing.Email == inv.Email && !existing.Accepted() {
			return org.ErrAlreadyExists
		}
	}
	s.invitations[inv.ID] = inv
	s.byToken[inv.TokenHash] = inv.ID
	return nil
}

func (s *memStore) GetInvitationByTokenHash(_ context.Context, tokenHash string) (org.Invitation, error) {
	id, ok := s.byToken[tokenHash]
	if !ok {
		return org.Invitation{}, org.ErrNotFound
	}
	return s.invitations[id], nil
}

func (s *memStore) ListInvitations(_ context.Context, orgID string) ([]org.Invitation, error) {
	var out []org.Invitation
	for _, inv := range s.invitations {
		if inv.OrgID == orgID {
			out = append(out, inv)
		}
	}
	return out, nil
}

func (s *memStore) AcceptInvitation(_ context.Context, id, userID string) error {
	inv, ok := s.invitations[id]
	if !ok || inv.Accepted() {
		return org.ErrNotFound
	}
	if _, exists := s.members[inv.OrgID][userID]; exists {
		return org.ErrAlreadyExists
	}
	now := time.Now().UTC()
	inv.AcceptedAt = &now
	s.invitations[id] = inv
	s.members[inv.OrgID][userID] = org.Member{
		OrgID: inv.OrgID, UserID: userID, Role: inv.Role, InvitedBy: inv.InvitedBy, CreatedAt: now,
	}
	return nil
}

func (s *memStore) DeleteInvitation(_ context.Context, id string) error {
	if inv, ok := s.invitations[id]; ok {
		delete(s.byToken, inv.TokenHash)
		delete(s.invitations, id)
	}
	return nil
}

// newService wires a service over the fake store with a deterministic clock and
// identifier sequence. The returned advance moves the clock forward so
// invitation expiry is testable without sleeping.
func newService(t *testing.T) (*org.Service, *memStore, func(time.Duration)) {
	t.Helper()
	store := newMemStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seq := 0
	svc := &org.Service{
		Store: store,
		NewID: func() string {
			seq++
			return fmt.Sprintf("org_gen_%02d", seq)
		},
		Now: func() time.Time { return now },
	}
	advance := func(d time.Duration) { now = now.Add(d) }
	return svc, store, advance
}

// seed creates an organization with the given owner and plan.
func seed(t *testing.T, svc *org.Service, ownerID, name string, plan org.Plan) org.Record {
	t.Helper()
	rec, err := svc.Create(context.Background(), org.CreateInput{
		OwnerID: ownerID, Name: name, Slug: "", Plan: plan,
	})
	if err != nil {
		t.Fatalf("seed organization %q: %v", name, err)
	}
	return rec
}

func TestCreateMakesOwnerAndSeedsLimits(t *testing.T) {
	svc, _, _ := newService(t)

	rec := seed(t, svc, "usr_1", "Acme Corp", "")
	if rec.Plan != org.PlanFree {
		t.Errorf("plan = %q, want free by default", rec.Plan)
	}
	if rec.Limits != org.FreeLimits() {
		t.Errorf("limits = %+v, want the free allowance", rec.Limits)
	}
	// The slug is derived from the name, so a caller need not supply one.
	if rec.Slug != "acme-corp" {
		t.Errorf("slug = %q, want acme-corp", rec.Slug)
	}

	members, err := svc.ListMembers(context.Background(), rec.ID, "usr_1")
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 1 || members[0].Role != org.RoleOwner {
		t.Fatalf("creator is not the sole owner: %+v", members)
	}
	// The creator consumes a seat, so usage starts at one.
	if rec.Usage.TeamMembers != 1 {
		t.Errorf("team usage = %d, want 1", rec.Usage.TeamMembers)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, org.CreateInput{OwnerID: "usr_1", Name: "   "}); !errors.Is(err, org.ErrInvalidName) {
		t.Errorf("blank name: err = %v, want ErrInvalidName", err)
	}
	if _, err := svc.Create(ctx, org.CreateInput{OwnerID: "usr_1", Name: "X", Slug: "Not A Slug"}); !errors.Is(err, org.ErrInvalidSlug) {
		t.Errorf("invalid slug: err = %v, want ErrInvalidSlug", err)
	}
	if _, err := svc.Create(ctx, org.CreateInput{OwnerID: "usr_1", Name: "X", Plan: "platinum"}); !errors.Is(err, org.ErrUnknownPlan) {
		t.Errorf("unknown plan: err = %v, want ErrUnknownPlan", err)
	}

	// A duplicate slug is refused rather than silently reusing another org.
	seed(t, svc, "usr_1", "Acme", org.PlanFree)
	if _, err := svc.Create(ctx, org.CreateInput{OwnerID: "usr_2", Name: "Acme", Slug: "acme"}); !errors.Is(err, org.ErrAlreadyExists) {
		t.Errorf("duplicate slug: err = %v, want ErrAlreadyExists", err)
	}
}

// TestNonMemberCannotSeeOrMutate is the isolation guarantee: knowing an
// organization id must not grant any access to it.
func TestNonMemberCannotSeeOrMutate(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	acme := seed(t, svc, "usr_1", "Acme", org.PlanFree)

	if _, err := svc.Get(ctx, acme.ID, "usr_intruder"); !errors.Is(err, org.ErrNotAMember) {
		t.Errorf("get as non-member: err = %v, want ErrNotAMember", err)
	}
	if err := svc.Delete(ctx, acme.ID, "usr_intruder"); !errors.Is(err, org.ErrNotAMember) {
		t.Errorf("delete as non-member: err = %v, want ErrNotAMember", err)
	}
	if _, err := svc.Update(ctx, acme.ID, "usr_intruder", org.UpdateInput{Name: "Hijacked"}); !errors.Is(err, org.ErrNotAMember) {
		t.Errorf("update as non-member: err = %v, want ErrNotAMember", err)
	}

	// The organization is untouched by the refused calls.
	rec, err := svc.Get(ctx, acme.ID, "usr_1")
	if err != nil || rec.Name != "Acme" {
		t.Errorf("organization changed by a non-member: %+v (err=%v)", rec, err)
	}
}

func TestRoleHierarchy(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	acme := seed(t, svc, "usr_owner", "Acme", org.PlanPro)

	add := func(id string, role org.Role) {
		t.Helper()
		if err := svc.Store.AddMember(ctx, org.Member{OrgID: acme.ID, UserID: id, Role: role}); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	add("usr_admin", org.RoleAdmin)
	add("usr_dev", org.RoleDeveloper)
	add("usr_viewer", org.RoleViewer)

	// Renaming is organization-level administration, so a developer is refused.
	if _, err := svc.Update(ctx, acme.ID, "usr_dev", org.UpdateInput{Name: "Nope"}); !errors.Is(err, org.ErrNotAMember) {
		t.Errorf("developer renamed the org: err = %v, want ErrNotAMember", err)
	}
	if _, err := svc.Update(ctx, acme.ID, "usr_viewer", org.UpdateInput{Name: "Nope"}); !errors.Is(err, org.ErrNotAMember) {
		t.Errorf("viewer renamed the org: err = %v, want ErrNotAMember", err)
	}
	// An admin may.
	if _, err := svc.Update(ctx, acme.ID, "usr_admin", org.UpdateInput{Name: "Acme Renamed"}); err != nil {
		t.Errorf("admin could not rename: %v", err)
	}
	// Invitations are admin work too.
	if _, err := svc.ListInvitations(ctx, acme.ID, "usr_dev"); !errors.Is(err, org.ErrNotAMember) {
		t.Errorf("developer listed invitations: err = %v, want ErrNotAMember", err)
	}
	// Reading the roster is open to every member.
	if _, err := svc.ListMembers(ctx, acme.ID, "usr_viewer"); err != nil {
		t.Errorf("viewer could not read the roster: %v", err)
	}
	// Deleting the organization is owner-only.
	if err := svc.Delete(ctx, acme.ID, "usr_admin"); !errors.Is(err, org.ErrNotAMember) {
		t.Errorf("admin deleted the org: err = %v, want ErrNotAMember", err)
	}
}

// TestPlanChangeReseedsLimits proves a downgrade actually narrows the allowance
// instead of leaving the previous one in place.
func TestPlanChangeReseedsLimits(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	acme := seed(t, svc, "usr_1", "Acme", org.PlanEnterprise)

	if acme.Limits.MaxProjects != 0 {
		t.Fatalf("enterprise should have no project cap, got %d", acme.Limits.MaxProjects)
	}

	downgraded, err := svc.Update(ctx, acme.ID, "usr_1", org.UpdateInput{Plan: org.PlanFree})
	if err != nil {
		t.Fatalf("downgrade: %v", err)
	}
	if downgraded.Plan != org.PlanFree || downgraded.Limits != org.FreeLimits() {
		t.Errorf("downgraded org = plan %q limits %+v, want free with the free allowance", downgraded.Plan, downgraded.Limits)
	}
}

// TestQuotaBlocksProjectsBeyondPlan is the billing guarantee: the free plan
// allows exactly one project.
func TestQuotaBlocksProjectsBeyondPlan(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	free := seed(t, svc, "usr_1", "Free Org", org.PlanFree)
	pro := seed(t, svc, "usr_2", "Pro Org", org.PlanPro)

	// The creator's own organization is not yet counted as a project.
	if err := svc.CheckProjectQuota(ctx, free.ID); err != nil {
		t.Fatalf("first project refused: %v", err)
	}
	if err := svc.RecordUsage(ctx, free.ID, org.Usage{Projects: 1}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if err := svc.CheckProjectQuota(ctx, free.ID); !errors.Is(err, org.ErrQuotaExceeded) {
		t.Errorf("second project on the free plan: err = %v, want ErrQuotaExceeded", err)
	}

	// The pro plan has room; 0 means "no cap" for that plan's domains.
	if err := svc.RecordUsage(ctx, pro.ID, org.Usage{Projects: 25}); err != nil {
		t.Fatalf("record pro usage: %v", err)
	}
	if err := svc.CheckProjectQuota(ctx, pro.ID); !errors.Is(err, org.ErrQuotaExceeded) {
		t.Errorf("26th project on pro: err = %v, want ErrQuotaExceeded", err)
	}
	// Enterprise has no project cap at all.
	ent := seed(t, svc, "usr_3", "Ent Org", org.PlanEnterprise)
	if err := svc.RecordUsage(ctx, ent.ID, org.Usage{Projects: 10_000}); err != nil {
		t.Fatalf("record enterprise usage: %v", err)
	}
	if err := svc.CheckProjectQuota(ctx, ent.ID); err != nil {
		t.Errorf("enterprise blocked by a project quota: %v", err)
	}
}

func TestCheckQuotaSemantics(t *testing.T) {
	cases := []struct {
		name           string
		current, delta int
		limit          int
		wantErr        bool
	}{
		{"within limit", 1, 1, 5, false},
		{"exactly at limit", 4, 1, 5, false},
		{"one over limit", 5, 1, 5, true},
		{"zero limit means uncapped", 10_000, 1, 0, false},
		// Shrinking usage must never be blocked, even above a limit: a release
		// that fails on a quota would strand resources.
		{"negative delta always allowed", 99, -1, 5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := org.CheckQuota(tc.current, tc.delta, tc.limit)
			if tc.wantErr != (err != nil) {
				t.Errorf("CheckQuota(%d, %d, %d) = %v, wantErr=%v", tc.current, tc.delta, tc.limit, err, tc.wantErr)
			}
		})
	}
}

// TestInviteAndAcceptFlow walks the whole invitation lifecycle, including that
// the plaintext token is never what the store sees.
func TestInviteAndAcceptFlow(t *testing.T) {
	svc, store, _ := newService(t)
	ctx := context.Background()
	acme := seed(t, svc, "usr_owner", "Acme", org.PlanPro)

	inv, err := svc.Invite(ctx, org.InviteInput{
		OrgID: acme.ID, InviterID: "usr_owner", Email: "Dev@Acme.Dev", Role: org.RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if inv.Token == "" {
		t.Fatal("invitation returned no plaintext token")
	}
	if inv.Email != "dev@acme.dev" {
		t.Errorf("email = %q, want the lowercased address", inv.Email)
	}
	// The stored invitation carries the hash, never the token itself.
	stored, err := store.GetInvitationByTokenHash(ctx, hashOf(inv.Token))
	if err != nil {
		t.Fatalf("stored invitation not found by token hash: %v", err)
	}
	if stored.TokenHash == inv.Token {
		t.Error("the invitation token was persisted in clear")
	}

	// Accepting grants the role and records the seat.
	if _, err := svc.AcceptInvitation(ctx, inv.Token, "usr_dev"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	role, err := svc.MemberRole(ctx, acme.ID, "usr_dev")
	if err != nil || role != org.RoleDeveloper {
		t.Errorf("member role = %q (err=%v), want developer", role, err)
	}

	// A used token cannot be replayed, and an unknown token is indistinguishable
	// from an expired one.
	if _, err := svc.AcceptInvitation(ctx, inv.Token, "usr_other"); !errors.Is(err, org.ErrNotFound) {
		t.Errorf("replayed token: err = %v, want ErrNotFound", err)
	}
	if _, err := svc.AcceptInvitation(ctx, "deadbeef", "usr_other"); !errors.Is(err, org.ErrNotFound) {
		t.Errorf("unknown token: err = %v, want ErrNotFound", err)
	}
}

func TestInviteRejectsNonInvitableRoleAndBadEmail(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	acme := seed(t, svc, "usr_1", "Acme", org.PlanPro)

	if _, err := svc.Invite(ctx, org.InviteInput{
		OrgID: acme.ID, InviterID: "usr_1", Email: "x@acme.dev", Role: org.RoleOwner,
	}); !errors.Is(err, org.ErrRoleNotInvitable) {
		t.Errorf("owner invitation: err = %v, want ErrRoleNotInvitable", err)
	}
	for _, bad := range []string{"", "no-at-sign", "a@b", "@acme.dev", "a@acme", "a@b@c.dev"} {
		if _, err := svc.Invite(ctx, org.InviteInput{
			OrgID: acme.ID, InviterID: "usr_1", Email: bad, Role: org.RoleViewer,
		}); !errors.Is(err, org.ErrInvalidEmail) {
			t.Errorf("email %q: err = %v, want ErrInvalidEmail", bad, err)
		}
	}
	// A viewer cannot invite.
	if _, err := svc.Invite(ctx, org.InviteInput{
		OrgID: acme.ID, InviterID: "usr_viewer", Email: "v@acme.dev", Role: org.RoleViewer,
	}); !errors.Is(err, org.ErrNotAMember) {
		t.Errorf("viewer invited: err = %v, want ErrNotAMember", err)
	}
}

func TestInviteRefusedWhenSeatsExhausted(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	// The free plan allows one seat, already consumed by the owner.
	free := seed(t, svc, "usr_owner", "Free Org", org.PlanFree)

	if _, err := svc.Invite(ctx, org.InviteInput{
		OrgID: free.ID, InviterID: "usr_owner", Email: "dev@acme.dev", Role: org.RoleDeveloper,
	}); !errors.Is(err, org.ErrQuotaExceeded) {
		t.Errorf("invite beyond the seat allowance: err = %v, want ErrQuotaExceeded", err)
	}
	// Viewers do not consume a seat, so they are still allowed.
	if _, err := svc.Invite(ctx, org.InviteInput{
		OrgID: free.ID, InviterID: "usr_owner", Email: "viewer@acme.dev", Role: org.RoleViewer,
	}); err != nil {
		t.Errorf("viewer invitation refused: %v", err)
	}
}

// TestLastOwnerProtected is the invariant that stops an organization locking
// itself out of administration.
func TestLastOwnerProtected(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	acme := seed(t, svc, "usr_1", "Acme", org.PlanFree)

	// The only owner cannot be demoted.
	if err := svc.ChangeRole(ctx, acme.ID, "usr_1", "usr_1", org.RoleAdmin); !errors.Is(err, org.ErrLastOwner) {
		t.Errorf("self-demotion: err = %v, want ErrLastOwner", err)
	}
	// Nor removed.
	if err := svc.RemoveMember(ctx, acme.ID, "usr_1", "usr_1"); !errors.Is(err, org.ErrLastOwner) {
		t.Errorf("self-removal: err = %v, want ErrLastOwner", err)
	}

	// With a second owner, demotion becomes possible.
	if err := svc.Store.AddMember(ctx, org.Member{OrgID: acme.ID, UserID: "usr_2", Role: org.RoleOwner}); err != nil {
		t.Fatalf("add second owner: %v", err)
	}
	if err := svc.ChangeRole(ctx, acme.ID, "usr_1", "usr_2", org.RoleAdmin); err != nil {
		t.Errorf("demotion refused with two owners present: %v", err)
	}

	// usr_1 is now the only owner left, so it is protected even from itself.
	if err := svc.ChangeRole(ctx, acme.ID, "usr_1", "usr_1", org.RoleAdmin); !errors.Is(err, org.ErrLastOwner) {
		t.Errorf("last-owner self-demotion: err = %v, want ErrLastOwner", err)
	}
	// Nor can it be removed.
	if err := svc.RemoveMember(ctx, acme.ID, "usr_1", "usr_1"); !errors.Is(err, org.ErrLastOwner) {
		t.Errorf("last-owner self-removal: err = %v, want ErrLastOwner", err)
	}

	// An admin demoting themselves is ordinary work and stays allowed.
	if err := svc.ChangeRole(ctx, acme.ID, "usr_2", "usr_2", org.RoleDeveloper); err != nil {
		t.Errorf("admin could not step down: %v", err)
	}
}

func TestAdminCannotTouchOwner(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	acme := seed(t, svc, "usr_owner", "Acme", org.PlanPro)
	for id, role := range map[string]org.Role{"usr_admin": org.RoleAdmin, "usr_dev": org.RoleDeveloper} {
		if err := svc.Store.AddMember(ctx, org.Member{OrgID: acme.ID, UserID: id, Role: role}); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}

	// An admin may not demote an owner, nor promote somebody to owner.
	if err := svc.ChangeRole(ctx, acme.ID, "usr_admin", "usr_owner", org.RoleDeveloper); !errors.Is(err, org.ErrCannotRemoveOwner) {
		t.Errorf("admin demoted the owner: err = %v, want ErrCannotRemoveOwner", err)
	}
	if err := svc.ChangeRole(ctx, acme.ID, "usr_admin", "usr_dev", org.RoleOwner); !errors.Is(err, org.ErrCannotRemoveOwner) {
		t.Errorf("admin granted owner: err = %v, want ErrCannotRemoveOwner", err)
	}
	if err := svc.RemoveMember(ctx, acme.ID, "usr_admin", "usr_owner"); !errors.Is(err, org.ErrCannotRemoveOwner) {
		t.Errorf("admin removed the owner: err = %v, want ErrCannotRemoveOwner", err)
	}

	// An owner may do both.
	if err := svc.ChangeRole(ctx, acme.ID, "usr_owner", "usr_dev", org.RoleOwner); err != nil {
		t.Errorf("owner could not promote: %v", err)
	}
}

func TestInvitedDeveloperCanBeDemotedByAdmin(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	acme := seed(t, svc, "usr_owner", "Acme", org.PlanPro)
	for _, id := range []string{"usr_admin", "usr_dev"} {
		if err := svc.Store.AddMember(ctx, org.Member{OrgID: acme.ID, UserID: id, Role: org.RoleDeveloper}); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	if err := svc.Store.UpdateMemberRole(ctx, acme.ID, "usr_admin", org.RoleAdmin); err != nil {
		t.Fatalf("promote to admin: %v", err)
	}
	// Managing a non-owner is ordinary admin work and must not be blocked.
	if err := svc.ChangeRole(ctx, acme.ID, "usr_admin", "usr_dev", org.RoleViewer); err != nil {
		t.Errorf("admin could not demote a developer: %v", err)
	}
}

func TestInvitationExpiry(t *testing.T) {
	svc, _, advance := newService(t)
	ctx := context.Background()
	acme := seed(t, svc, "usr_owner", "Acme", org.PlanPro)

	inv, err := svc.Invite(ctx, org.InviteInput{
		OrgID: acme.ID, InviterID: "usr_owner", Email: "late@acme.dev", Role: org.RoleDeveloper,
	})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	// Inside the window the token still works.
	advance(org.DefaultTokenTTL - time.Hour)
	if _, err := svc.AcceptInvitation(ctx, inv.Token, "usr_dev"); err != nil {
		t.Fatalf("accept before expiry: %v", err)
	}

	// A second invitation, left to expire.
	inv2, err := svc.Invite(ctx, org.InviteInput{
		OrgID: acme.ID, InviterID: "usr_owner", Email: "late2@acme.dev", Role: org.RoleViewer,
	})
	if err != nil {
		t.Fatalf("second invite: %v", err)
	}
	advance(org.DefaultTokenTTL + time.Hour)
	if _, err := svc.AcceptInvitation(ctx, inv2.Token, "usr_late"); !errors.Is(err, org.ErrNotFound) {
		t.Errorf("accept after expiry: err = %v, want ErrNotFound", err)
	}
}

func TestDeleteRemovesEverythingItOwns(t *testing.T) {
	svc, store, _ := newService(t)
	ctx := context.Background()
	acme := seed(t, svc, "usr_1", "Acme", org.PlanPro)
	if err := svc.Delete(ctx, acme.ID, "usr_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Membership is gone with the organization.
	if _, err := store.GetMember(ctx, acme.ID, "usr_1"); !errors.Is(err, org.ErrNotFound) {
		t.Errorf("membership survived deletion: err = %v, want ErrNotFound", err)
	}
	// Reading through the service now fails closed. Get reports "not a member"
	// rather than "not found" on purpose: distinguishing the two would tell an
	// unauthenticated caller which organizations exist.
	if _, err := svc.Get(ctx, acme.ID, "usr_1"); !errors.Is(err, org.ErrNotAMember) {
		t.Errorf("Get after deletion: err = %v, want ErrNotAMember", err)
	}
	// The record itself is gone from the store.
	if _, err := store.Get(ctx, acme.ID); !errors.Is(err, org.ErrNotFound) {
		t.Errorf("organization row survived deletion: err = %v, want ErrNotFound", err)
	}
}

func TestSlugAndEmailNormalization(t *testing.T) {
	t.Run("slug derived from name", func(t *testing.T) {
		for _, tc := range []struct{ in, want string }{
			{"Acme Corp", "acme-corp"},
			{"  Hello  World  ", "hello-world"},
			{"Über_Cool App", "ber-cool-app"},
			{"a.b.c", "a-b-c"},
		} {
			got, err := org.NormalizeSlug("", tc.in)
			if err != nil || got != tc.want {
				t.Errorf("NormalizeSlug(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		}
	})

	t.Run("explicit slug is normalized, not rejected", func(t *testing.T) {
		// Case is folded rather than refused: the database constraint requires
		// lowercase, so normalizing here keeps a caller-supplied "Acme-Corp"
		// working instead of failing on something the user can see corrected.
		got, err := org.NormalizeSlug("Acme-Corp", "")
		if err != nil || got != "acme-corp" {
			t.Errorf("NormalizeSlug(%q) = %q, %v; want acme-corp", "Acme-Corp", got, err)
		}
	})

	t.Run("structurally invalid slugs", func(t *testing.T) {
		// The valid set matches the database constraint
		// ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ exactly, so a slug accepted here
		// cannot be rejected by the insert. Note that inner double hyphens are
		// permitted by both; slugify simply never produces them.
		for _, bad := range []string{"-lead", "trail-", "has space", "under_score", "sym!"} {
			if _, err := org.NormalizeSlug(bad, ""); err == nil {
				t.Errorf("NormalizeSlug(%q) accepted an invalid slug", bad)
			}
		}
		// A name that yields nothing usable is an error, not an empty slug.
		if _, err := org.NormalizeSlug("", "!!!"); err == nil {
			t.Error("NormalizeSlug accepted a name with no usable characters")
		}
	})

	t.Run("Go validation agrees with the database constraint", func(t *testing.T) {
		// Every slug this package accepts must survive the CHECK constraint, and
		// vice versa. A drift between the two would mean a rejected insert after
		// a successful validation.
		const dbPattern = `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`
		re := regexp.MustCompile(dbPattern)
		for _, in := range []string{
			"a", "ab", "a-b", "a--b", "a1", "1a", "a-b-c", "0-9-z",
			"-a", "a-", "a b", "a_b", "A", "aB", "a.b", "é",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		} {
			got, err := org.NormalizeSlug(in, "")
			if err != nil {
				if re.MatchString(in) {
					t.Errorf("NormalizeSlug(%q) rejected a slug the database accepts", in)
				}
				continue
			}
			if !re.MatchString(got) {
				t.Errorf("NormalizeSlug(%q) = %q, which the database constraint rejects", in, got)
			}
		}
	})

	t.Run("email normalized", func(t *testing.T) {
		got, err := org.NormalizeEmail("  Dev@Acme.Dev ")
		if err != nil || got != "dev@acme.dev" {
			t.Errorf("NormalizeEmail = %q, %v", got, err)
		}
	})
}

func TestRoleOrdering(t *testing.T) {
	if !org.RoleOwner.AtLeast(org.RoleViewer) {
		t.Error("owner should satisfy viewer")
	}
	if org.RoleViewer.AtLeast(org.RoleDeveloper) {
		t.Error("viewer must not satisfy developer")
	}
	if !org.RoleAdmin.AtLeast(org.RoleDeveloper) {
		t.Error("admin should satisfy developer")
	}
	if org.Role("superuser").AtLeast(org.RoleViewer) {
		t.Error("an unknown role must satisfy nothing")
	}
	if org.RoleOwner.Invitable() {
		t.Error("owner must not be invitable")
	}
	for _, r := range []org.Role{org.RoleAdmin, org.RoleDeveloper, org.RoleViewer} {
		if !r.Invitable() {
			t.Errorf("%q should be invitable", r)
		}
	}
}

func TestRecordUsageIsOrganizationScoped(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	a := seed(t, svc, "usr_1", "A Org", org.PlanStarter)
	b := seed(t, svc, "usr_2", "B Org", org.PlanStarter)

	if err := svc.RecordUsage(ctx, a.ID, org.Usage{Projects: 3, DeploymentsThisMonth: 42}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	// The other organization's counters are untouched.
	gotB, err := svc.Get(ctx, b.ID, "usr_2")
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if gotB.Usage.Projects != 0 || gotB.Usage.DeploymentsThisMonth != 0 {
		t.Errorf("usage leaked across organizations: %+v", gotB.Usage)
	}

	if err := svc.CheckDeploymentQuota(ctx, a.ID); err != nil {
		t.Errorf("deployment quota check failed: %v", err)
	}
	// Starter has no deployment cap; the free plan does.
	if err := svc.CheckDeploymentQuota(ctx, a.ID); err != nil {
		t.Errorf("starter deployment quota: %v", err)
	}
}

// hashOf returns the stored hash for a plaintext token. The test computes it
// with the same rule the service uses (SHA-256, hex) rather than reaching into
// the service, so it asserts the persisted shape independently.
func hashOf(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
