// Package analysis orchestrates repository understanding (#58/#59):
// ref → exact commit → bounded snapshot → evidence → canonical profile,
// persisted as a versioned analysis and profile revision. Repository code is
// never executed.
package analysis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/analyzer/evidence"
	"github.com/digitaleflex/axiom/services/engine/internal/analyzer/snapshot"
	"github.com/digitaleflex/axiom/services/engine/internal/application"
	"github.com/digitaleflex/axiom/services/engine/internal/github/repos"
	"github.com/digitaleflex/axiom/services/engine/internal/manifest"
	"github.com/digitaleflex/axiom/services/engine/internal/profile"
)

// Analysis statuses.
const (
	StatusRunning   = "RUNNING"
	StatusCompleted = "COMPLETED"
	StatusFailed    = "FAILED"
)

// Error codes recorded on failed analyses.
const (
	ErrorRefNotFound  = "REF_NOT_FOUND"
	ErrorSourceAccess = "SOURCE_ACCESS_FAILED"
	ErrorSnapshot     = "SNAPSHOT_REJECTED"
	// ErrorManifestInvalid is the fallback code for a rejected axiom.yaml;
	// the specific manifest code (MANIFEST_PARSE_ERROR,
	// MANIFEST_VERSION_UNSUPPORTED, MANIFEST_SCHEMA_INVALID,
	// MANIFEST_SECRET_VALUE, MANIFEST_STRATEGY_UNSUPPORTED,
	// MANIFEST_CONFLICT) is recorded when available.
	ErrorManifestInvalid = "MANIFEST_SCHEMA_INVALID"
)

var (
	ErrNotFound    = errors.New("analysis not found")
	ErrNoProfile   = errors.New("application has no profile yet")
	ErrInvalidRoot = errors.New("invalid application root")
)

// Record is a persisted analysis.
type Record struct {
	ID              string           `json:"analysisId"`
	ApplicationID   string           `json:"applicationId"`
	Ref             string           `json:"ref"`
	Commit          string           `json:"commit,omitempty"`
	Root            string           `json:"root"`
	Status          string           `json:"status"`
	AnalyzerVersion string           `json:"analyzerVersion,omitempty"`
	Result          *evidence.Result `json:"result,omitempty"`
	ErrorCode       string           `json:"errorCode,omitempty"`
	ProfileVersion  int              `json:"profileVersion,omitempty"`
	CreatedAt       time.Time        `json:"createdAt"`
	CompletedAt     *time.Time       `json:"completedAt,omitempty"`
}

// Store persists analyses, profiles and overrides.
type Store interface {
	CreateAnalysis(ctx context.Context, r Record) error
	// CompleteAnalysis finalizes the analysis and, when p is set, stores it as a
	// new profile revision in the same transaction.
	CompleteAnalysis(ctx context.Context, r Record, p *profile.Profile) (profileVersion int, err error)
	// SaveProfile stores a new profile revision (e.g. after overrides).
	SaveProfile(ctx context.Context, applicationID string, p profile.Profile) (int, error)
	GetAnalysis(ctx context.Context, applicationID, id string) (Record, error)
	CurrentProfile(ctx context.Context, applicationID string) (profile.Profile, error)
	Overrides(ctx context.Context, applicationID string) (profile.Hints, string, error)
	SaveOverrides(ctx context.Context, applicationID string, h profile.Hints, root string) error
}

// Repositories is the GitHub repository adapter subset used here.
type Repositories interface {
	ResolveRef(ctx context.Context, userID, repoID, ref string) (repos.Commit, error)
	Archive(ctx context.Context, userID, repoID, sha string) (io.ReadCloser, error)
}

// Service runs analyses.
type Service struct {
	Store   Store
	Repos   Repositories
	Limits  snapshot.Limits
	Timeout time.Duration
	Log     *slog.Logger
	NewID   func(prefix string) string
}

// Analyze analyzes app at ref synchronously and returns the completed (or failed) record.
func (s *Service) Analyze(ctx context.Context, userID string, app application.Record, ref string) (Record, error) {
	if !repos.ValidRef(ref) {
		return Record{}, repos.ErrInvalidRef
	}
	hints, root, err := s.Store.Overrides(ctx, app.ID)
	if err != nil {
		return Record{}, err
	}
	rec := Record{ID: s.NewID("analysis"), ApplicationID: app.ID, Ref: ref, Root: root, Status: StatusRunning, CreatedAt: time.Now().UTC()}
	if err := s.Store.CreateAnalysis(ctx, rec); err != nil {
		return Record{}, fmt.Errorf("create analysis: %w", err)
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	commit, err := s.Repos.ResolveRef(runCtx, userID, app.RepositoryID, ref)
	if err != nil {
		code := ErrorSourceAccess
		if errors.Is(err, repos.ErrRefNotFound) {
			code = ErrorRefNotFound
		}
		return s.fail(ctx, rec, code, err)
	}
	rec.Commit = commit.SHA
	limits := s.Limits
	if limits.MaxFiles == 0 {
		limits = snapshot.DefaultLimits
	}
	snap, err := snapshot.Fetch(runCtx, s.Repos, userID, app.RepositoryID, ref, commit.SHA, limits)
	if err != nil {
		code := ErrorSnapshot
		if !errors.Is(err, snapshot.ErrTooLarge) && !errors.Is(err, snapshot.ErrTooMany) && !errors.Is(err, snapshot.ErrMalformed) &&
			!errors.Is(err, snapshot.ErrUnsafePath) && !errors.Is(err, snapshot.ErrEmpty) {
			code = ErrorSourceAccess
		}
		return s.fail(ctx, rec, code, err)
	}

	// Optional axiom.yaml manifest (#124): validated hints and app.root,
	// loaded from the snapshot. An invalid manifest blocks profile
	// resolution — the analysis fails and no profile is stored.
	manHints, manRoot, err := manifest.Load(snap, root)
	if err != nil {
		code := ErrorManifestInvalid
		var merr *manifest.Error
		if errors.As(err, &merr) && merr.Code != "" {
			code = merr.Code
		}
		return s.fail(ctx, rec, code, err)
	}
	if manRoot != "" && root == "" {
		root = manRoot // manifest app.root selects the monorepo application
	}

	result := evidence.Analyze(snap, evidence.Options{Root: root})
	prof := profile.Build(result, profile.Source{RepositoryID: app.RepositoryID, Ref: ref, Commit: commit.SHA}, profile.Inputs{Manifest: manHints, Overrides: hints})
	prof.AnalysisID = rec.ID

	now := time.Now().UTC()
	rec.Status, rec.Result, rec.AnalyzerVersion, rec.CompletedAt = StatusCompleted, &result, result.AnalyzerVersion, &now
	version, err := s.Store.CompleteAnalysis(ctx, rec, &prof)
	if err != nil {
		return Record{}, fmt.Errorf("persist analysis: %w", err)
	}
	rec.ProfileVersion = version
	s.log().Info("analysis completed", "analysisId", rec.ID, "applicationId", app.ID, "commit", commit.SHA, "profileStatus", prof.Status, "profileVersion", version)
	return rec, nil
}

// Get returns an analysis of an application.
func (s *Service) Get(ctx context.Context, applicationID, id string) (Record, error) {
	return s.Store.GetAnalysis(ctx, applicationID, id)
}

// CurrentProfile returns the latest profile revision.
func (s *Service) CurrentProfile(ctx context.Context, applicationID string) (profile.Profile, error) {
	return s.Store.CurrentProfile(ctx, applicationID)
}

// UpdateOverrides stores explicit values and rebuilds the current profile from
// the latest completed analysis (no new repository access).
func (s *Service) UpdateOverrides(ctx context.Context, app application.Record, h profile.Hints) (profile.Profile, error) {
	current, err := s.Store.CurrentProfile(ctx, app.ID)
	if err != nil {
		return profile.Profile{}, err
	}
	_, root, err := s.Store.Overrides(ctx, app.ID)
	if err != nil {
		return profile.Profile{}, err
	}
	an, err := s.Store.GetAnalysis(ctx, app.ID, current.AnalysisID)
	if err != nil || an.Result == nil {
		return profile.Profile{}, ErrNoProfile
	}
	if err := s.Store.SaveOverrides(ctx, app.ID, h, root); err != nil {
		return profile.Profile{}, err
	}
	prof := profile.Build(*an.Result, current.Source, profile.Inputs{Overrides: h})
	prof.AnalysisID = an.ID
	version, err := s.Store.SaveProfile(ctx, app.ID, prof)
	if err != nil {
		return profile.Profile{}, err
	}
	prof.Version = version
	return prof, nil
}

// SetRoot selects the application directory for monorepos (applies on next analysis).
func (s *Service) SetRoot(ctx context.Context, app application.Record, root string) error {
	if len(root) > 200 || (root != "" && !repos.ValidRef(root)) {
		return ErrInvalidRoot
	}
	h, _, err := s.Store.Overrides(ctx, app.ID)
	if err != nil {
		return err
	}
	return s.Store.SaveOverrides(ctx, app.ID, h, root)
}

func (s *Service) fail(ctx context.Context, rec Record, code string, cause error) (Record, error) {
	now := time.Now().UTC()
	rec.Status, rec.ErrorCode, rec.CompletedAt = StatusFailed, code, &now
	if _, err := s.Store.CompleteAnalysis(ctx, rec, nil); err != nil {
		return Record{}, fmt.Errorf("persist failed analysis: %w", err)
	}
	s.log().Warn("analysis failed", "analysisId", rec.ID, "applicationId", rec.ApplicationID, "errorCode", code, "cause", cause.Error())
	return rec, nil
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}
