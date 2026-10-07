package planner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/digitaleflex/axiom/services/engine/internal/domains"
	"github.com/digitaleflex/axiom/services/engine/internal/server"
)

// ErrPlanNotFound is returned for unknown or inaccessible plans.
var ErrPlanNotFound = errors.New("deployment plan not found")

// Profiles returns the current profile of an application.
type Profiles interface {
	CurrentProfile(ctx context.Context, applicationID string) (ApplicationProfile, error)
}

// Servers loads server records.
type Servers interface {
	Get(ctx context.Context, id string) (server.Record, error)
}

// Domains resolves the hostname used for planning.
type Domains interface {
	EnsureDomain(ctx context.Context, applicationID, environment, hostname string) (domains.Record, bool, error)
}

// Service generates and persists plans.
type Service struct {
	Engine   *Engine
	Profiles Profiles
	Servers  Servers
	Domains  Domains
	DB       *sql.DB
	NewID    func(prefix string) string
}

// CreateInput are the user's deployment configuration choices.
type CreateInput struct {
	ApplicationID string
	ServerID      string
	Environment   string
	Domain        string
}

// Create generates a plan from the current profile and persists it.
func (s *Service) Create(ctx context.Context, in CreateInput) (Plan, error) {
	prof, err := s.Profiles.CurrentProfile(ctx, in.ApplicationID)
	if err != nil {
		return Plan{}, err
	}
	srv, err := s.Servers.Get(ctx, in.ServerID)
	if err != nil {
		return Plan{}, err
	}
	if s.Domains != nil {
		if _, _, err := s.Domains.EnsureDomain(ctx, in.ApplicationID, in.Environment, in.Domain); err != nil {
			return Plan{}, err
		}
		if in.Domain, err = domains.Normalize(in.Domain); err != nil {
			return Plan{}, err
		}
	}
	caps := make([]string, 0, len(srv.Capabilities))
	for _, c := range srv.Capabilities {
		caps = append(caps, string(c))
	}
	plan, err := s.Engine.Generate(Request{
		ApplicationID: in.ApplicationID, Profile: prof, Environment: in.Environment, Domain: strings.TrimSpace(in.Domain),
		Server: ServerProfile{ID: srv.ID, Name: srv.Name, Status: strings.ToUpper(string(srv.Status)), Capabilities: caps, MemoryMB: srv.MemoryMB, DiskFreeMB: srv.DiskFreeMB},
	})
	if err != nil {
		return Plan{}, err
	}
	plan.ID = s.NewID("plan")
	body, err := json.Marshal(plan)
	if err != nil {
		return Plan{}, err
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO deployment_plans
			(id, application_id, server_id, environment, ref, commit_sha, application_profile_version, status, fingerprint, body)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		plan.ID, plan.ApplicationID, plan.ServerID, plan.Environment, plan.Source.Ref, plan.Source.Commit,
		plan.ApplicationProfileVersion, plan.Status, plan.Fingerprint, body); err != nil {
		return Plan{}, fmt.Errorf("persist plan: %w", err)
	}
	return plan, nil
}

// PlanView is a plan with derived review state.
type PlanView struct {
	Plan
	// Stale is true when the application profile changed since generation.
	Stale bool `json:"stale"`
	// DeploymentID is set when the plan was already used (plans are single-use).
	DeploymentID string `json:"deploymentId,omitempty"`
}

// Get returns a plan of an application the caller owns (ownership is checked by the caller via applicationID lookup).
func (s *Service) Get(ctx context.Context, id string) (PlanView, error) {
	var body []byte
	var depID sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT p.body, d.id FROM deployment_plans p LEFT JOIN deployments d ON d.plan_id = p.id WHERE p.id = $1`, id).Scan(&body, &depID)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanView{}, ErrPlanNotFound
	}
	if err != nil {
		return PlanView{}, err
	}
	var v PlanView
	if err := json.Unmarshal(body, &v.Plan); err != nil {
		return PlanView{}, err
	}
	v.DeploymentID = depID.String
	if cur, err := s.Profiles.CurrentProfile(ctx, v.ApplicationID); err == nil && cur.Version != v.ApplicationProfileVersion {
		v.Stale = true
	}
	return v, nil
}
