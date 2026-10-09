package server

import (
	"context"
	"fmt"
	"time"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Get(ctx context.Context, id string) (Record, error) {
	if s == nil || s.repo == nil {
		return Record{}, fmt.Errorf("server repository is required")
	}
	if id == "" {
		return Record{}, fmt.Errorf("server ID is required")
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) UpdateHealth(ctx context.Context, id string, health Health) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("server repository is required")
	}
	if id == "" {
		return fmt.Errorf("server ID is required")
	}
	if health.Status == "" {
		return fmt.Errorf("server health status is required")
	}
	if health.LastSeenAt == "" {
		health.LastSeenAt = time.Now().UTC().Format(time.RFC3339)
	}
	return s.repo.UpdateHealth(ctx, id, health)
}

// ListFiltered lists the servers owned by ownerID, optionally filtered by
// status. Ownership is strict (see Repository.ListFiltered): callers that
// need a cross-owner view do not belong on this method.
func (s *Service) ListFiltered(ctx context.Context, ownerID, status string, limit, offset int) ([]Record, int, error) {
	if s == nil || s.repo == nil {
		return nil, 0, fmt.Errorf("server repository is required")
	}
	return s.repo.ListFiltered(ctx, ownerID, status, limit, offset)
}

// ActiveDeployments counts deployments on the server that still need it.
func (s *Service) ActiveDeployments(ctx context.Context, id string) (int, error) {
	if s == nil || s.repo == nil {
		return 0, fmt.Errorf("server repository is required")
	}
	return s.repo.ActiveDeployments(ctx, id)
}

func (s *Service) CheckEligibility(record Record, req EligibilityRequest) EligibilityResult {
	return s.CheckEligibilityAt(record, req, time.Now().UTC())
}

// CheckEligibilityAt evaluates eligibility against the effective status:
// stale ready/degraded servers read as offline.
func (s *Service) CheckEligibilityAt(record Record, req EligibilityRequest, now time.Time) EligibilityResult {
	result := EligibilityResult{Eligible: true}

	if EffectiveStatusAt(record, now) != StatusReady {
		result.Eligible = false
		result.Reasons = append(result.Reasons, "server is not ready")
	}
	if record.CPUCount <= 0 {
		result.Eligible = false
		result.Reasons = append(result.Reasons, "CPU capacity is unknown")
	}
	if record.MemoryMB <= 0 {
		result.Eligible = false
		result.Reasons = append(result.Reasons, "memory capacity is unknown")
	}
	if record.DiskFreeMB <= 0 {
		result.Eligible = false
		result.Reasons = append(result.Reasons, "free disk capacity is unknown")
	}
	for _, required := range req.RequiredCapabilities {
		if !hasCapability(record.Capabilities, required) {
			result.Eligible = false
			result.Reasons = append(result.Reasons, "missing capability: "+string(required))
		}
	}
	return result
}

func hasCapability(capabilities []Capability, wanted Capability) bool {
	for _, capability := range capabilities {
		if capability == wanted {
			return true
		}
	}
	return false
}
