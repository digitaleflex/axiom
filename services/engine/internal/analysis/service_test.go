package analysis

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/digitaleflex/axiom/services/engine/internal/analyzer/snapshot"
	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/deployment"
	"github.com/digitaleflex/axiom/services/engine/internal/github/repos"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
	"github.com/digitaleflex/axiom/services/engine/migrations"
)

type fakeRepos struct{ files map[string]string }

var sha = strings.Repeat("c", 40)

func (f fakeRepos) ResolveRef(_ context.Context, _, _, ref string) (repos.Commit, error) {
	if ref != "main" {
		return repos.Commit{}, repos.ErrRefNotFound
	}
	return repos.Commit{SHA: sha}, nil
}

func (f fakeRepos) Archive(context.Context, string, string, string) (io.ReadCloser, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for p, c := range f.files {
		_ = tw.WriteHeader(&tar.Header{Name: "acme-web-ccc/" + p, Mode: 0o644, Size: int64(len(c)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(c))
	}
	_ = tw.Close()
	_ = gz.Close()
	return io.NopCloser(&buf), nil
}

func setup(t *testing.T, files map[string]string) (*Service, application.Record) {
	t.Helper()
	dsn := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AXIOM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	schema := "analysis_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	admin, _ := sql.Open("pgx", dsn)
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`) })
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, _ := sql.Open("pgx", dsn+sep+"search_path="+schema)
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id) VALUES ('usr_1')`,
		`INSERT INTO github_connections (id, user_id) VALUES ('ghc_1', 'usr_1')`,
		`INSERT INTO repositories (id, connection_id, external_id, full_name, clone_url) VALUES ('repo_1', 'ghc_1', '1', 'acme/web', 'x')`,
		`INSERT INTO applications (id, repository_id, name, owner_id) VALUES ('app_1', 'repo_1', 'acme-web', 'usr_1')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	return &Service{Store: PGStore{DB: db}, Repos: fakeRepos{files: files}, NewID: deployment.NewID},
		application.Record{ID: "app_1", RepositoryID: "repo_1", OwnerID: "usr_1"}
}

var nextFiles = map[string]string{
	"package.json":   `{"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"14"}}`,
	"pnpm-lock.yaml": "x", "yarn.lock": "y", "next.config.js": "x",
}

func TestAnalyzePersistsAnalysisAndVersionedProfile(t *testing.T) {
	svc, app := setup(t, nextFiles)
	ctx := context.Background()
	rec, err := svc.Analyze(ctx, "usr_1", app, "main")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != StatusCompleted || rec.Commit != sha || rec.ProfileVersion != 1 || rec.Result == nil || rec.AnalyzerVersion == "" {
		t.Fatalf("record = %+v", rec)
	}
	got, err := svc.Get(ctx, app.ID, rec.ID)
	if err != nil || got.Result == nil || len(got.Result.Findings) == 0 || got.ProfileVersion != 1 {
		t.Fatalf("stored analysis = %+v %v", got, err)
	}
	p, err := svc.CurrentProfile(ctx, app.ID)
	if err != nil || p.Version != 1 || p.Status != profile.StatusNeedsReview || p.AnalysisID != rec.ID || p.Source.Commit != sha {
		t.Fatalf("profile = %+v %v", p, err)
	}

	// Resolving the ambiguous package manager creates profile v2 without re-fetching.
	pm := "pnpm"
	p, err = svc.UpdateOverrides(ctx, app, profile.Hints{PackageManager: &pm})
	if err != nil || p.Version != 2 || p.Status != profile.StatusReady || p.PackageManager.Provenance != profile.ProvenanceOverride {
		t.Fatalf("after override = %+v %v", p, err)
	}
	// Overrides persist into the next analysis.
	rec, _ = svc.Analyze(ctx, "usr_1", app, "main")
	p, _ = svc.CurrentProfile(ctx, app.ID)
	if rec.ProfileVersion != 3 || p.Status != profile.StatusReady {
		t.Fatalf("re-analysis must keep overrides: v%d %s", rec.ProfileVersion, p.Status)
	}
	if _, err := svc.Get(ctx, "app_other", rec.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("analysis of another application: %v", err)
	}
}

func TestFailedAnalysesAreRecorded(t *testing.T) {
	svc, app := setup(t, nextFiles)
	ctx := context.Background()
	rec, err := svc.Analyze(ctx, "usr_1", app, "missing-branch")
	if err != nil || rec.Status != StatusFailed || rec.ErrorCode != ErrorRefNotFound {
		t.Fatalf("missing ref = %+v %v", rec, err)
	}
	if _, err := svc.CurrentProfile(ctx, app.ID); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("failed analysis must not create a profile: %v", err)
	}
	svc.Limits = snapshot.Limits{MaxArchiveBytes: 1 << 20, MaxTotalBytes: 1 << 20, MaxFiles: 1, MaxRetainedBytes: 1 << 10, MaxRetainedTotal: 1 << 10}
	rec, _ = svc.Analyze(ctx, "usr_1", app, "main")
	if rec.Status != StatusFailed || rec.ErrorCode != ErrorSnapshot {
		t.Fatalf("oversized snapshot = %+v", rec)
	}
	if _, err := svc.Analyze(ctx, "usr_1", app, "../etc"); !errors.Is(err, repos.ErrInvalidRef) {
		t.Fatalf("invalid ref: %v", err)
	}
}

func TestMonorepoRootSelection(t *testing.T) {
	svc, app := setup(t, map[string]string{
		"apps/web/package.json": `{"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"14"}}`,
		"apps/api/go.mod":       "module api\n\ngo 1.23\n",
		"apps/api/main.go":      "package main\nfunc main() {}",
	})
	ctx := context.Background()
	if _, err := svc.Analyze(ctx, "usr_1", app, "main"); err != nil {
		t.Fatal(err)
	}
	p, _ := svc.CurrentProfile(ctx, app.ID)
	if p.Status != profile.StatusUnsupported || p.Unsupported.Code != "AMBIGUOUS_APPLICATION_ROOT" {
		t.Fatalf("monorepo without root = %+v", p.Unsupported)
	}
	if err := svc.SetRoot(ctx, app, "apps/api"); err != nil {
		t.Fatal(err)
	}
	rec, _ := svc.Analyze(ctx, "usr_1", app, "main")
	p, _ = svc.CurrentProfile(ctx, app.ID)
	if rec.Root != "apps/api" || p.Preset != "go" || p.Source.Root != "apps/api" {
		t.Fatalf("root selection: rec.Root=%q preset=%q", rec.Root, p.Preset)
	}
	if err := svc.SetRoot(ctx, app, "../outside"); !errors.Is(err, ErrInvalidRoot) {
		t.Fatalf("invalid root: %v", err)
	}
}
