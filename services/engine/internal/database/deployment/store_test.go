package deployment_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	deploymentdb "github.com/digitaleflex/axiom/services/engine/internal/database/deployment"
	domain "github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/migrations"
)

// openDB returns a migrated database in an isolated schema plus a factory
// for additional independent connection pools (simulating Engine restarts).
func openDB(t *testing.T) (*sql.DB, func() *sql.DB, context.Context) {
	t.Helper()
	url := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	schema := "depstore_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`) })
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	open := func() *sql.DB {
		db, err := sql.Open("pgx", url+sep+"search_path="+schema)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(25)
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	db := open()
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	seed := []string{
		`INSERT INTO users (id) VALUES ('usr_1')`,
		`INSERT INTO github_connections (id, user_id) VALUES ('ghc_1', 'usr_1')`,
		`INSERT INTO repositories (id, connection_id, external_id, full_name, clone_url) VALUES ('repo_1', 'ghc_1', '1', 'acme/web', 'https://github.com/acme/web.git')`,
		`INSERT INTO applications (id, repository_id, name) VALUES ('app_1', 'repo_1', 'acme-web')`,
		`INSERT INTO servers (id, name, address, status) VALUES ('srv_1', 'srv-eu-1', '203.0.113.10', 'ready')`,
	}
	for _, q := range seed {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return db, open, ctx
}

func addPlan(t *testing.T, ctx context.Context, db *sql.DB, id, env string) {
	t.Helper()
	_, err := db.ExecContext(ctx, `INSERT INTO deployment_plans (id, application_id, server_id, environment, ref, application_profile_version, fingerprint, body)
		VALUES ($1, 'app_1', 'srv_1', $2, 'main', 1, 'sha256:test', '{"steps":["BUILD","CREATE_RUNTIME","NETWORK","START","VERIFY"]}')`, id, env)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreatePersistsDeploymentStepsAndEvent(t *testing.T) {
	db, _, ctx := openDB(t)
	addPlan(t, ctx, db, "plan_1", "staging")
	store := deploymentdb.New(db)

	rec, created, err := store.Create(ctx, domain.CreateInput{ApplicationID: "app_1", PlanID: "plan_1", CreatedBy: "usr_1"})
	if err != nil || !created {
		t.Fatalf("create: %v created=%v", err, created)
	}
	if rec.Status != domain.StatePending || rec.Number != 1 || rec.Environment != "staging" || rec.ServerID != "srv_1" || !strings.HasPrefix(rec.ID, "dep_") {
		t.Fatalf("unexpected record %+v", rec)
	}
	steps, err := store.Steps(ctx, rec.ID)
	if err != nil || len(steps) != 5 || steps[0].Name != "BUILD" || steps[4].Name != "VERIFY" || steps[0].Status != domain.StepQueued {
		t.Fatalf("steps = %+v err=%v", steps, err)
	}
	events, err := store.Events(ctx, rec.ID, 0, 0)
	if err != nil || len(events) != 1 || events[0].Type != domain.EventCreated || events[0].Seq != 1 {
		t.Fatalf("events = %+v err=%v", events, err)
	}
}

func TestIdempotencySurvivesRestart(t *testing.T) {
	db, open, ctx := openDB(t)
	addPlan(t, ctx, db, "plan_1", "production")
	in := domain.CreateInput{ApplicationID: "app_1", PlanID: "plan_1", IdempotencyKey: "deploy:plan_1"}

	first, created, err := deploymentdb.New(db).Create(ctx, in)
	if err != nil || !created {
		t.Fatalf("first: %v", err)
	}
	// Simulated restart: a brand-new pool and store, no shared memory.
	restarted := deploymentdb.New(open())
	again, created, err := restarted.Create(ctx, in)
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("replay after restart must return %s, got %s created=%v err=%v", first.ID, again.ID, created, err)
	}
	addPlan(t, ctx, db, "plan_2", "production")
	if _, _, err := restarted.Create(ctx, domain.CreateInput{ApplicationID: "app_1", PlanID: "plan_2", IdempotencyKey: "deploy:plan_1"}); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("key reuse with different request must conflict, got %v", err)
	}
}

func TestConcurrentIdempotentRequestsResolveToOne(t *testing.T) {
	db, open, ctx := openDB(t)
	addPlan(t, ctx, db, "plan_1", "production")
	stores := []*deploymentdb.Store{deploymentdb.New(db), deploymentdb.New(open()), deploymentdb.New(open())}

	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := map[string]int{}
	createdCount := 0
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec, created, err := stores[i%len(stores)].Create(ctx, domain.CreateInput{ApplicationID: "app_1", PlanID: "plan_1", IdempotencyKey: "same-key"})
			if err != nil {
				t.Errorf("create %d: %v", i, err)
				return
			}
			mu.Lock()
			ids[rec.ID]++
			if created {
				createdCount++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if len(ids) != 1 || createdCount != 1 {
		t.Fatalf("expected exactly one deployment, got ids=%v created=%d", ids, createdCount)
	}
	var n int
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM deployments`).Scan(&n)
	if n != 1 {
		t.Fatalf("deployments rows = %d, want 1", n)
	}
}

func TestPlanSingleUseAndOwnership(t *testing.T) {
	db, _, ctx := openDB(t)
	addPlan(t, ctx, db, "plan_1", "production")
	store := deploymentdb.New(db)
	if _, _, err := store.Create(ctx, domain.CreateInput{ApplicationID: "app_1", PlanID: "plan_1"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Create(ctx, domain.CreateInput{ApplicationID: "app_1", PlanID: "plan_1"}); !errors.Is(err, domain.ErrPlanAlreadyUsed) {
		t.Fatalf("second use of plan: got %v", err)
	}
	if _, _, err := store.Create(ctx, domain.CreateInput{ApplicationID: "app_1", PlanID: "plan_missing"}); !errors.Is(err, domain.ErrPlanNotFound) {
		t.Fatalf("missing plan: got %v", err)
	}
	if _, _, err := store.Create(ctx, domain.CreateInput{ApplicationID: "app_other", PlanID: "plan_1"}); !errors.Is(err, domain.ErrPlanNotFound) {
		t.Fatalf("foreign application: got %v", err)
	}
}

func TestConcurrentNumbering(t *testing.T) {
	db, _, ctx := openDB(t)
	store := deploymentdb.New(db)
	const n = 10
	for i := 0; i < n; i++ {
		addPlan(t, ctx, db, "plan_"+string(rune('a'+i)), "production")
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, _, err := store.Create(ctx, domain.CreateInput{ApplicationID: "app_1", PlanID: "plan_" + string(rune('a'+i))}); err != nil {
				t.Errorf("create: %v", err)
			}
		}(i)
	}
	wg.Wait()
	var distinct, max int
	_ = db.QueryRowContext(ctx, `SELECT count(DISTINCT number), max(number) FROM deployments`).Scan(&distinct, &max)
	if distinct != n || max != n {
		t.Fatalf("numbers must be 1..%d without gaps or duplicates: distinct=%d max=%d", n, distinct, max)
	}
}

func TestTransitionsAreSerializedAndSurviveRestart(t *testing.T) {
	db, open, ctx := openDB(t)
	addPlan(t, ctx, db, "plan_1", "production")
	svc := domain.NewService(deploymentdb.New(db), nil)
	rec, _, err := svc.Create(ctx, domain.CreateInput{ApplicationID: "app_1", PlanID: "plan_1"})
	if err != nil {
		t.Fatal(err)
	}

	// Many concurrent PENDING -> ANALYZING attempts: exactly one may win.
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Transition(ctx, rec.ID, domain.StateAnalyzing); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			} else if !errors.Is(err, domain.ErrInvalidTransition) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("exactly one transition must win, got %d", wins)
	}

	for _, s := range []domain.State{domain.StatePlanning, domain.StateBuilding} {
		if _, err := svc.Transition(ctx, rec.ID, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.RecordStep(ctx, rec.ID, domain.StepChange{Name: "BUILD", Status: domain.StepRunning}); err != nil {
		t.Fatal(err)
	}
	exit := 2
	if err := svc.RecordStep(ctx, rec.ID, domain.StepChange{Name: "BUILD", Status: domain.StepFailed, ExitCode: &exit, ErrorCode: "BUILD_FAILED"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Fail(ctx, rec.ID, "BUILD_FAILED"); err != nil {
		t.Fatal(err)
	}

	// Restart: state, steps and ordered events are all recovered from PostgreSQL.
	restarted := deploymentdb.New(open())
	got, err := restarted.Get(ctx, rec.ID)
	if err != nil || got.Status != domain.StateFailed || got.ErrorCode != "BUILD_FAILED" || got.StartedAt == nil || got.CompletedAt == nil {
		t.Fatalf("after restart: %+v err=%v", got, err)
	}
	steps, _ := restarted.Steps(ctx, rec.ID)
	if steps[0].Status != domain.StepFailed || steps[0].ExitCode == nil || *steps[0].ExitCode != 2 {
		t.Fatalf("BUILD step = %+v", steps[0])
	}
	events, _ := restarted.Events(ctx, rec.ID, 0, 0)
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Fatalf("event seq gap at %d: %d", i, e.Seq)
		}
	}
	if last := events[len(events)-1]; last.Type != domain.EventStatusChanged || last.Data["status"] != "FAILED" || last.Data["errorCode"] != "BUILD_FAILED" {
		t.Fatalf("last event = %+v", last)
	}
	after, _ := restarted.Events(ctx, rec.ID, 3, 0)
	if len(after) != len(events)-3 || after[0].Seq != 4 {
		t.Fatalf("resume after seq 3 returned %d events starting at %d", len(after), after[0].Seq)
	}
	if _, err := svc.Transition(ctx, rec.ID, domain.StateBuilding); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("terminal state must reject transitions, got %v", err)
	}
	list, total, err := restarted.List(ctx, domain.ListFilter{ApplicationID: "app_1", Environment: "production"})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("list: %v total=%d", err, total)
	}
	if _, err := restarted.Get(ctx, "dep_missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing deployment: %v", err)
	}
}
