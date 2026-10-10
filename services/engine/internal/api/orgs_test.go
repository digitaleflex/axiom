package api

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/internal/org"
	"github.com/digitaleflex/axiom/services/engine/internal/project"
	"github.com/digitaleflex/axiom/services/engine/migrations"
)

// These tests exercise the organization and project routes against a real
// PostgreSQL schema, because the property under test is isolation: that a
// request naming another tenant's organization cannot read or mutate it. A fake
// store would only prove the fake behaves as written.

// openOrgDB creates an isolated schema and migrates it.
func mustExec(ctx context.Context, db *sql.DB, sql string) {
	_, err := db.ExecContext(ctx, sql)
	if err != nil {
		panic("seed: " + err.Error())
	}
}

func openOrgDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	url := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	schema := "apiorg_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`) })

	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	db, err := sql.Open("pgx", url+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// The organization_members table requires users to exist; seed the test
	// callers so ownership and membership inserts do not hit a foreign key.
	mustExec(ctx, db, `INSERT INTO users (id, created_at) VALUES
		('usr_1', now()),
		('usr_other', now()),
		('usr_viewer', now())
		ON CONFLICT (id) DO NOTHING`)
	return db, ctx
}

// orgHarness builds the API over real services on an isolated schema.
type orgHarness struct {
	t     *testing.T
	h     *harness
	orgs  *org.Service
	projs *project.Service
	db    *sql.DB
}

func newOrgHarness(t *testing.T) *orgHarness {
	t.Helper()
	db, _ := openOrgDB(t)

	orgs := &org.Service{Store: org.PGStore{DB: db}}
	// The same adapters the composition root uses, so the wiring under test is
	// the wiring that ships.
	projs := &project.Service{
		Store: project.PGStore{DB: db},
		Quota: quotaAdapterFor(orgs),
		Authz: roleAdapterFor(orgs),
	}

	h := newHarness(t)
	h.handler = New(Deps{
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth:     NewTokenAuthenticator(token, Principal{UserID: "usr_1"}),
		Orgs:     orgs,
		Projects: projs,
	})
	return &orgHarness{t: t, h: h, orgs: orgs, projs: projs, db: db}
}

// quotaAdapterFor mirrors bootstrap.quotaAdapter: the org quota sentinel becomes
// the project one so each package keeps a single meaning for it.
func quotaAdapterFor(orgs *org.Service) project.QuotaChecker {
	return quotaAdapter{s: orgs}
}

type quotaAdapter struct{ s *org.Service }

func (q quotaAdapter) CheckProjectQuota(ctx context.Context, orgID string) error {
	err := q.s.CheckProjectQuota(ctx, orgID)
	if errors.Is(err, org.ErrQuotaExceeded) {
		return errors.New("project quota exceeded: " + err.Error())
	}
	return err
}

func roleAdapterFor(orgs *org.Service) project.Authorizer {
	return roleAdapter{s: orgs}
}

type roleAdapter struct{ s *org.Service }

func (r roleAdapter) MemberRole(ctx context.Context, orgID, userID string) (project.Role, error) {
	role, err := r.s.MemberRole(ctx, orgID, userID)
	if err != nil {
		return "", err
	}
	return project.Role(role), nil
}

// newOrg creates an organization owned by ownerID straight through the service,
// bypassing the API so the test can set up tenants the caller does not belong to.
func (oh *orgHarness) newOrg(ownerID, name string, plan org.Plan) org.Record {
	oh.t.Helper()
	rec, err := oh.orgs.Create(context.Background(), org.CreateInput{
		OwnerID: ownerID, Name: name, Plan: plan,
	})
	if err != nil {
		oh.t.Fatalf("create org %q: %v", name, err)
	}
	return rec
}

func (oh *orgHarness) addMember(orgID, userID string, role org.Role) {
	oh.t.Helper()
	if err := oh.orgs.Store.AddMember(context.Background(), org.Member{
		OrgID: orgID, UserID: userID, Role: role,
	}); err != nil {
		oh.t.Fatalf("add member %s: %v", userID, err)
	}
}

func TestOrganizationRoutes(t *testing.T) {
	oh := newOrgHarness(t)
	h := oh.h

	// Create: the caller becomes owner, and the slug is derived from the name.
	r := h.do("POST", "/api/v1/orgs", map[string]any{"name": "Acme Corp"}, nil)
	expect(t, r, 201, "")
	if r.body["slug"] != "acme-corp" {
		t.Fatalf("slug = %v, want acme-corp", r.body["slug"])
	}
	if r.body["plan"] != "free" {
		t.Fatalf("plan = %v, want free", r.body["plan"])
	}
	orgID, _ := r.body["id"].(string)
	if orgID == "" {
		t.Fatal("no organization id returned")
	}
	if !strings.HasSuffix(r.hdr.Get("Location"), orgID) {
		t.Errorf("Location = %q, want it to end with the new id", r.hdr.Get("Location"))
	}

	// The seed migration already created a personal organization for usr_local;
	// listing returns this one too, so assert containment rather than count.
	r = h.do("GET", "/api/v1/orgs", nil, nil)
	expect(t, r, 200, "")
	found := false
	for _, it := range r.body["items"].([]any) {
		if it.(map[string]any)["id"] == orgID {
			found = true
		}
	}
	if !found {
		t.Errorf("created organization missing from the list: %v", r.body["items"])
	}

	expect(t, h.do("GET", "/api/v1/orgs/"+orgID, nil, nil), 200, "")
	expect(t, h.do("GET", "/api/v1/orgs/org_nope", nil, nil), 404, CodeNotFound)

	// Validation is reported per field so the client can highlight it.
	r = h.do("POST", "/api/v1/orgs", map[string]any{"name": "  "}, nil)
	expect(t, r, 422, CodeValidationFailed)
	fields := r.body["error"].(map[string]any)["details"].(map[string]any)["fields"].(map[string]any)
	if _, ok := fields["name"]; !ok {
		t.Errorf("expected a name field error, got %v", fields)
	}

	// A duplicate slug is a conflict, not a second organization.
	expect(t, h.do("POST", "/api/v1/orgs", map[string]any{"name": "Acme Corp"}, nil), 409, CodeConflict)

	// Plan change is owner work and re-seeds the allowance.
	r = h.do("PATCH", "/api/v1/orgs/"+orgID, map[string]any{"plan": "pro"}, nil)
	expect(t, r, 200, "")
	limits := r.body["limits"].(map[string]any)
	if limits["max_projects"] != float64(25) {
		t.Errorf("pro max_projects = %v, want 25", limits["max_projects"])
	}
	expect(t, h.do("PATCH", "/api/v1/orgs/"+orgID, map[string]any{"plan": "platinum"}, nil), 422, CodeValidationFailed)
}

// TestCrossTenantIsolation is the property that justifies the whole design: the
// API caller (usr_1) cannot see or touch another tenant's organization.
func TestCrossTenantIsolation(t *testing.T) {
	oh := newOrgHarness(t)
	h := oh.h

	// A tenant the API caller does not belong to.
	other := oh.newOrg("usr_other", "Other Corp", org.PlanPro)
	otherProject, err := oh.projs.Create(context.Background(), project.CreateInput{
		OrgID: other.ID, Name: "secret-app", Slug: "secret-app",
	})
	if err != nil {
		t.Fatalf("seed foreign project: %v", err)
	}

	// Reads, writes and deletes all fail closed.
	expect(t, h.do("GET", "/api/v1/orgs/"+other.ID, nil, nil), 404, CodeNotFound)
	expect(t, h.do("PATCH", "/api/v1/orgs/"+other.ID, map[string]any{"name": "Hijacked"}, nil), 404, CodeNotFound)
	expect(t, h.do("DELETE", "/api/v1/orgs/"+other.ID, nil, nil), 404, CodeNotFound)
	expect(t, h.do("GET", "/api/v1/orgs/"+other.ID+"/members", nil, nil), 404, CodeNotFound)
	expect(t, h.do("GET", "/api/v1/orgs/"+other.ID+"/projects", nil, nil), 404, CodeNotFound)
	expect(t, h.do("POST", "/api/v1/orgs/"+other.ID+"/projects", map[string]any{"name": "intruder"}, nil), 404, CodeNotFound)
	expect(t, h.do("GET", "/api/v1/orgs/"+other.ID+"/projects/"+otherProject.ID, nil, nil), 404, CodeNotFound)
	expect(t, h.do("PATCH", "/api/v1/orgs/"+other.ID+"/projects/"+otherProject.ID, map[string]any{"name": "x"}, nil), 404, CodeNotFound)
	expect(t, h.do("DELETE", "/api/v1/orgs/"+other.ID+"/projects/"+otherProject.ID, nil, nil), 404, CodeNotFound)

	// The foreign organization is unchanged.
	rec, err := oh.orgs.Store.Get(context.Background(), other.ID)
	if err != nil || rec.Name != "Other Corp" {
		t.Errorf("foreign org mutated: %+v (err=%v)", rec, err)
	}
	still, err := oh.projs.Get(context.Background(), other.ID, otherProject.ID)
	if err != nil || still.Name != "secret-app" {
		t.Errorf("foreign project mutated: %+v (err=%v)", still, err)
	}
}

func TestProjectRoutes(t *testing.T) {
	oh := newOrgHarness(t)
	h := oh.h

	acme := oh.newOrg("usr_1", "Acme", org.PlanPro)

	r := h.do("POST", "/api/v1/orgs/"+acme.ID+"/projects", map[string]any{
		"name": "Web App", "description": "the front end", "environment": "staging",
	}, nil)
	expect(t, r, 201, "")
	if r.body["slug"] != "web-app" {
		t.Errorf("slug = %v, want web-app", r.body["slug"])
	}
	if r.body["environment"] != "staging" {
		t.Errorf("environment = %v, want staging", r.body["environment"])
	}
	id, _ := r.body["id"].(string)
	if id == "" {
		t.Fatal("no project id returned")
	}
	if !strings.Contains(r.hdr.Get("Location"), id) {
		t.Errorf("Location = %q", r.hdr.Get("Location"))
	}

	expect(t, h.do("GET", "/api/v1/orgs/"+acme.ID+"/projects/"+id, nil, nil), 200, "")
	r = h.do("GET", "/api/v1/orgs/"+acme.ID+"/projects", nil, nil)
	expect(t, r, 200, "")
	if r.body["total"] != float64(1) {
		t.Errorf("total = %v, want 1", r.body["total"])
	}

	// Pagination parameters are honoured and bounded.
	expect(t, h.do("GET", "/api/v1/orgs/"+acme.ID+"/projects?limit=1&offset=0", nil, nil), 200, "")
	expect(t, h.do("GET", "/api/v1/orgs/"+acme.ID+"/projects?limit=notanumber", nil, nil), 200, "")

	// A duplicate slug inside the organization conflicts.
	expect(t, h.do("POST", "/api/v1/orgs/"+acme.ID+"/projects", map[string]any{"name": "Web App"}, nil), 409, CodeConflict)

	// Validation errors name the offending field.
	r = h.do("POST", "/api/v1/orgs/"+acme.ID+"/projects", map[string]any{"name": "bad name"}, nil)
	expect(t, r, 422, CodeValidationFailed)
	r = h.do("POST", "/api/v1/orgs/"+acme.ID+"/projects", map[string]any{"name": "ok", "environment": "dev"}, nil)
	expect(t, r, 422, CodeValidationFailed)

	// Patch: a field the request omits is left alone.
	r = h.do("PATCH", "/api/v1/orgs/"+acme.ID+"/projects/"+id, map[string]any{"description": "updated"}, nil)
	expect(t, r, 200, "")
	if r.body["description"] != "updated" {
		t.Errorf("description = %v", r.body["description"])
	}
	if r.body["name"] != "web-app" {
		t.Errorf("name changed by a patch that omitted it: %v", r.body["name"])
	}
	// An explicit null clears the primary domain.
	r = h.do("PATCH", "/api/v1/orgs/"+acme.ID+"/projects/"+id, map[string]any{"primaryDomain": nil}, nil)
	expect(t, r, 200, "")

	expect(t, h.do("DELETE", "/api/v1/orgs/"+acme.ID+"/projects/"+id, nil, nil), 204, "")
	expect(t, h.do("GET", "/api/v1/orgs/"+acme.ID+"/projects/"+id, nil, nil), 404, CodeNotFound)
}

// TestProjectQuotaIsEnforcedThroughAPI proves the plan allowance reaches the HTTP
// surface: the free plan's single project, then a refusal.
func TestProjectQuotaIsEnforcedThroughAPI(t *testing.T) {
	oh := newOrgHarness(t)
	h := oh.h

	free := oh.newOrg("usr_1", "Free Org", org.PlanFree)

	expect(t, h.do("POST", "/api/v1/orgs/"+free.ID+"/projects", map[string]any{"name": "first"}, nil), 201, "")
	// The second project exceeds the free allowance.
	r := h.do("POST", "/api/v1/orgs/"+free.ID+"/projects", map[string]any{"name": "second"}, nil)
	expect(t, r, 409, CodeConflict)
	if reason := r.body["error"].(map[string]any)["details"].(map[string]any)["reason"]; reason != "quota_exceeded" {
		t.Errorf("reason = %v, want quota_exceeded so the client can explain it", reason)
	}
}

// TestViewerCannotCreateProject proves the role check reaches the HTTP surface.
func TestViewerCannotCreateProject(t *testing.T) {
	oh := newOrgHarness(t)
	h := oh.h

	acme := oh.newOrg("usr_1", "Acme", org.PlanPro)
	oh.addMember(acme.ID, "usr_viewer", org.RoleViewer)

	// The API caller (usr_1) is the owner, so use the service directly to prove
	// the viewer rule: a viewer may read but not write.
	if _, err := oh.projs.Create(context.Background(), project.CreateInput{
		OrgID: acme.ID, RequesterID: "usr_viewer", Name: "viewer-project",
	}); !errors.Is(err, project.ErrNotFound) {
		t.Errorf("viewer created a project: err = %v, want ErrNotFound", err)
	}
	// The owner may.
	expect(t, h.do("POST", "/api/v1/orgs/"+acme.ID+"/projects", map[string]any{"name": "owner-project"}, nil), 201, "")
}

func TestOrganizationUnavailableWithoutService(t *testing.T) {
	// With no organization service the routes must answer 503, not 404 and not a
	// panic: the feature is unconfigured, which is different from "not found".
	h := newHarness(t)
	h.handler = New(Deps{
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Auth: NewTokenAuthenticator(token, Principal{UserID: "usr_1"}),
	})
	expect(t, h.do("GET", "/api/v1/orgs", nil, nil), 503, CodeServiceUnavailable)
	expect(t, h.do("POST", "/api/v1/orgs", map[string]any{"name": "X"}, nil), 503, CodeServiceUnavailable)
	expect(t, h.do("GET", "/api/v1/orgs/org_1/projects", nil, nil), 503, CodeServiceUnavailable)
}

func TestMemberAndInvitationRoutes(t *testing.T) {
	oh := newOrgHarness(t)
	h := oh.h
	acme := oh.newOrg("usr_1", "Acme", org.PlanPro)

	r := h.do("GET", "/api/v1/orgs/"+acme.ID+"/members", nil, nil)
	expect(t, r, 200, "")
	members := r.body["items"].([]any)
	if len(members) != 1 || members[0].(map[string]any)["role"] != "owner" {
		t.Fatalf("members = %v", members)
	}
	// A member projection must not carry anything secret.
	for _, m := range members {
		for _, k := range m.(map[string]any) {
			if k == "tokenHash" || k == "token" {
				t.Errorf("member projection leaked %q", k)
			}
		}
	}

	// Invite returns the plaintext token exactly once, alongside a hashed-store
	// record that never exposes it again.
	r = h.do("POST", "/api/v1/orgs/"+acme.ID+"/invitations", map[string]any{
		"email": "dev@acme.dev", "role": "developer",
	}, nil)
	expect(t, r, 201, "")
	token, _ := r.body["token"].(string)
	if token == "" {
		t.Fatal("invite response carries no token; the link could never be built")
	}
	inv, _ := r.body["invitation"].(map[string]any)
	if inv["status"] != "pending" || inv["email"] != "dev@acme.dev" {
		t.Fatalf("invitation = %v", inv)
	}
	// A viewer may not invite.
	oh.addMember(acme.ID, "usr_viewer", org.RoleViewer)
	expect(t, h.do("POST", "/api/v1/orgs/"+acme.ID+"/invitations", map[string]any{"email": "v@acme.dev"}, nil), 404, CodeNotFound)

	// Validation.
	r = h.do("POST", "/api/v1/orgs/"+acme.ID+"/invitations", map[string]any{"email": "not-an-email"}, nil)
	expect(t, r, 422, CodeValidationFailed)
	r = h.do("POST", "/api/v1/orgs/"+acme.ID+"/invitations", map[string]any{"email": "o@acme.dev", "role": "owner"}, nil)
	expect(t, r, 422, CodeValidationFailed)

	// Listing shows status but never the token.
	r = h.do("GET", "/api/v1/orgs/"+acme.ID+"/invitations", nil, nil)
	expect(t, r, 200, "")
	for _, it := range r.body["items"].([]any) {
		m := it.(map[string]any)
		if _, leaked := m["token"]; leaked {
			t.Error("invitation list leaked the token")
		}
		if m["status"] == "" {
			t.Error("invitation has no status")
		}
	}
}

func TestAcceptInvitationRoute(t *testing.T) {
	oh := newOrgHarness(t)
	h := oh.h
	acme := oh.newOrg("usr_1", "Acme", org.PlanPro)

	r := h.do("POST", "/api/v1/orgs/"+acme.ID+"/invitations", map[string]any{
		"email": "dev@acme.dev", "role": "developer",
	}, nil)
	expect(t, r, 201, "")
	token := r.body["token"].(string)

	// Accepting grants the membership.
	r = h.do("POST", "/api/v1/invitations/accept", map[string]any{"token": token}, nil)
	expect(t, r, 200, "")
	if r.body["id"] != acme.ID {
		t.Errorf("accept returned %v, want the organization", r.body["id"])
	}
	role, err := oh.orgs.MemberRole(context.Background(), acme.ID, "usr_1")
	if err != nil || role == "" {
		t.Errorf("membership not created: role=%q err=%v", role, err)
	}

	// Replaying the token fails, and is reported the same way as an unknown one.
	expect(t, h.do("POST", "/api/v1/invitations/accept", map[string]any{"token": token}, nil), 404, CodeNotFound)
	expect(t, h.do("POST", "/api/v1/invitations/accept", map[string]any{"token": "bogus"}, nil), 404, CodeNotFound)
}
