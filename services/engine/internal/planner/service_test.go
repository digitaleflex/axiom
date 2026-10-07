package planner_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/internal/analysis"
	"github.com/digitaleflex/axiom/services/engine/internal/database"
	deploymentdb "github.com/digitaleflex/axiom/services/engine/internal/database/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/domains"
	"github.com/digitaleflex/axiom/services/engine/internal/planner"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
	"github.com/digitaleflex/axiom/services/engine/migrations"
)

func TestPlanLifecycleWithPostgreSQL(t *testing.T) {
	dsn := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, _ := sql.Open("pgx", dsn)
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS planner_it CASCADE; CREATE SCHEMA planner_it`); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS planner_it CASCADE`)
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, _ := sql.Open("pgx", dsn+sep+"search_path=planner_it")
	defer db.Close()
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id) VALUES ('usr_1')`,
		`INSERT INTO github_connections (id, user_id) VALUES ('ghc_1', 'usr_1')`,
		`INSERT INTO repositories (id, connection_id, external_id, full_name, clone_url) VALUES ('repo_1', 'ghc_1', '1', 'acme/web', 'x')`,
		`INSERT INTO applications (id, repository_id, name, owner_id) VALUES ('app_1', 'repo_1', 'acme-web', 'usr_1')`,
		`INSERT INTO analyses (id, application_id, ref, status) VALUES ('analysis_1', 'app_1', 'main', 'COMPLETED')`,
		`INSERT INTO servers (id, name, address, status, capabilities) VALUES ('srv_1', 'srv-eu-1', '203.0.113.10', 'ready', '["docker","traefik","tls"]')`,
		`INSERT INTO servers (id, name, address, status, capabilities) VALUES ('srv_2', 'srv-eu-2', '203.0.113.11', 'offline', '["docker"]')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	profiles := analysis.PGStore{DB: db}
	prof := profile.Profile{
		SchemaVersion: 1, Status: profile.StatusReady, Preset: "nextjs", AnalysisID: "analysis_1",
		Source:            profile.Source{RepositoryID: "repo_1", Ref: "main", Commit: strings.Repeat("a", 40)},
		PackageManager:    profile.Field[string]{Value: "pnpm", Provenance: profile.ProvenanceDetected},
		BuildCommand:      profile.Field[string]{Value: "pnpm run build", Provenance: profile.ProvenanceDetected},
		StartCommand:      profile.Field[string]{Value: "pnpm start", Provenance: profile.ProvenanceDetected},
		Port:              profile.Field[int]{Value: 3000, Provenance: profile.ProvenanceDefault},
		ContainerStrategy: profile.Field[string]{Value: "source", Provenance: profile.ProvenanceDefault},
		HealthCheck:       profile.Field[profile.HealthCheck]{Value: profile.HealthCheck{Type: "http", Path: "/"}, Provenance: profile.ProvenanceDefault},
	}
	if _, err := profiles.SaveProfile(ctx, "app_1", prof); err != nil {
		t.Fatal(err)
	}
	svc := &planner.Service{Engine: planner.New(), Profiles: profiles, Servers: database.NewRepositories(db).Servers,
		Domains: &domains.Service{Store: domains.PGStore{DB: db}, Resolver: stdResolver{}}, DB: db, NewID: deployment.NewID}

	plan, err := svc.Create(ctx, planner.CreateInput{ApplicationID: "app_1", ServerID: "srv_1", Environment: "production", Domain: "app.acme.dev"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plan.ID, "plan_") || plan.ApplicationProfileVersion != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	again, _ := svc.Create(ctx, planner.CreateInput{ApplicationID: "app_1", ServerID: "srv_1", Environment: "production", Domain: "app.acme.dev"})
	if again.Fingerprint != plan.Fingerprint || again.ID == plan.ID {
		t.Fatal("same inputs: same fingerprint, new immutable plan")
	}
	if _, err := svc.Create(ctx, planner.CreateInput{ApplicationID: "app_1", ServerID: "srv_2", Environment: "production", Domain: "app.acme.dev"}); !errors.Is(err, planner.ErrNotEligible) {
		t.Fatalf("offline server: %v", err)
	}

	view, err := svc.Get(ctx, plan.ID)
	if err != nil || view.Stale || view.DeploymentID != "" {
		t.Fatalf("view = %+v %v", view, err)
	}

	// The plan domain was auto-registered as primary (first of the environment).
	doms, err := svc.Domains.(*domains.Service).List(ctx, "app_1", "production")
	if err != nil || len(doms) != 1 || !doms[0].IsPrimary || doms[0].Hostname != "app.acme.dev" {
		t.Fatalf("auto-registered domains = %+v %v", doms, err)
	}
	// A different unregistered hostname is rejected while one exists.
	if _, err := svc.Create(ctx, planner.CreateInput{ApplicationID: "app_1", ServerID: "srv_1", Environment: "production", Domain: "other.acme.dev"}); !errors.Is(err, domains.ErrNotRegistered) {
		t.Fatalf("unregistered domain: %v", err)
	}
	// Another environment needs its own hostname (hostnames are global).
	staging, err := svc.Create(ctx, planner.CreateInput{ApplicationID: "app_1", ServerID: "srv_1", Environment: "staging", Domain: "staging.acme.dev"})
	if err != nil {
		t.Fatalf("staging plan: %v", err)
	}
	if staging.Environment != "staging" {
		t.Fatalf("staging plan = %+v", staging)
	}

	// The persisted plan is consumed by the deployment store (steps from body).
	deps := deployment.NewService(deploymentdb.New(db), nil)
	rec, _, err := deps.Create(ctx, deployment.CreateInput{ApplicationID: "app_1", PlanID: plan.ID})
	if err != nil || rec.Environment != "production" || rec.ServerID != "srv_1" {
		t.Fatalf("deployment from plan: %+v %v", rec, err)
	}
	steps, _ := deps.Store().Steps(ctx, rec.ID)
	if len(steps) != 5 {
		t.Fatalf("steps from plan = %d", len(steps))
	}
	view, _ = svc.Get(ctx, plan.ID)
	if view.DeploymentID != rec.ID {
		t.Fatalf("used plan must report its deployment: %+v", view)
	}

	// A new profile revision makes older plans stale.
	if _, err := profiles.SaveProfile(ctx, "app_1", prof); err != nil {
		t.Fatal(err)
	}
	if view, _ = svc.Get(ctx, again.ID); !view.Stale {
		t.Fatal("plan must be stale after a profile change")
	}
	if _, err := svc.Get(ctx, "plan_missing"); !errors.Is(err, planner.ErrPlanNotFound) {
		t.Fatalf("missing plan: %v", err)
	}
}
