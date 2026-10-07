package domains

import (
	"context"
	"database/sql"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/migrations"
)

type stdResolver struct{}

func (stdResolver) LookupIP(ctx context.Context, host string) ([]string, error) {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(ips))
	for i, ip := range ips {
		out[i] = ip.String()
	}
	return out, nil
}

func TestPostgreSQLAndRealDNS(t *testing.T) {
	dsn := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	admin, _ := sql.Open("pgx", dsn)
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS dompg CASCADE; CREATE SCHEMA dompg`); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS dompg CASCADE`)
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, _ := sql.Open("pgx", dsn+sep+"search_path=dompg")
	defer db.Close()
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id) VALUES ('usr_1')`,
		`INSERT INTO github_connections (id, user_id) VALUES ('ghc_1', 'usr_1')`,
		`INSERT INTO repositories (id, connection_id, external_id, full_name, clone_url) VALUES ('repo_1', 'ghc_1', '1', 'acme/web', 'x')`,
		`INSERT INTO applications (id, repository_id, name) VALUES ('app_1', 'repo_1', 'web')`,
		`INSERT INTO servers (id, name, address, status) VALUES ('srv_1', 'srv', '127.0.0.1', 'ready')`,
		`INSERT INTO deployment_plans (id, application_id, server_id, environment, ref, application_profile_version, fingerprint, body) VALUES ('plan_1', 'app_1', 'srv_1', 'production', 'main', 1, 'sha256:x', '{}')`,
		`INSERT INTO deployments (id, application_id, server_id, environment, plan_id, status) VALUES ('dep_1', 'app_1', 'srv_1', 'production', 'plan_1', 'LIVE')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	svc := &Service{Store: PGStore{DB: db}, Resolver: stdResolver{}}

	rec, err := svc.Create(ctx, "app_1", "production", "localhost")
	if err != nil {
		t.Fatal(err)
	}
	if !rec.IsPrimary {
		t.Fatal("first domain must be primary")
	}
	if _, err := svc.Create(ctx, "app_1", "production", "localhost"); err == nil {
		t.Fatal("duplicate hostname must be rejected")
	}
	got, err := svc.CheckDNS(ctx, "app_1", rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DNSStatus != DNSOk || got.DNSExpected != "127.0.0.1" || got.DNSCheckedAt == nil {
		t.Fatalf("localhost → 127.0.0.1 must verify: %+v", got)
	}
	// Persisted across reads.
	store := PGStore{DB: db}
	stored, err := store.Get(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.DNSStatus != DNSOk || stored.DNSObserved == "" {
		t.Fatalf("stored = %+v", stored)
	}
	// Routing target resolves the serving deployment.
	target, ok, err := store.RoutingTarget(ctx, "app_1", "production")
	if err != nil || !ok || target.DeploymentID != "dep_1" || target.Address != "127.0.0.1" {
		t.Fatalf("target = %+v %v %v", target, ok, err)
	}
	if _, ok, _ := store.RoutingTarget(ctx, "app_1", "staging"); ok {
		t.Fatal("no staging deployment serves")
	}
}
