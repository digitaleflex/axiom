package server

import (
	"context"
	"errors"
)

type Status string

const (
	StatusUnknown  Status = "unknown"
	StatusPending  Status = "pending"
	StatusReady    Status = "ready"
	StatusDegraded Status = "degraded"
	StatusOffline  Status = "offline"
	StatusRevoked  Status = "revoked"
)

type Capability string

const (
	CapabilityDocker  Capability = "docker"
	CapabilityCompose Capability = "docker_compose"
	CapabilityTraefik Capability = "traefik"
	CapabilityTLS     Capability = "tls"
)

type Record struct {
	ID           string
	Name         string
	Address      string
	OwnerID      string
	Status       Status
	AgentVersion string
	Capabilities []Capability
	CPUCount     int
	MemoryMB     int
	DiskFreeMB   int
	LastSeenAt   string
}

type Repository interface {
	Create(ctx context.Context, r Record) error
	Get(ctx context.Context, id string) (Record, error)
	// ListFiltered lists the servers owned by ownerID, optionally filtered
	// by status. Ownership is matched strictly against the owner column
	// coalesced to the empty string, so an empty ownerID only selects
	// unowned records. Cross-owner listings are not part of this contract.
	ListFiltered(ctx context.Context, ownerID, status string, limit, offset int) ([]Record, int, error)
	Rename(ctx context.Context, id, name string) error
	Delete(ctx context.Context, id string) error
	// ActiveDeployments counts deployments on the server that are not
	// FAILED or CANCELLED (they still need the server).
	ActiveDeployments(ctx context.Context, serverID string) (int, error)
	UpdateHealth(ctx context.Context, id string, health Health) error
}

// ServerStore is implemented by database.ServerRepository.
type Health struct {
	Status       Status
	AgentVersion string
	Capabilities []Capability
	CPUCount     int
	MemoryMB     int
	DiskFreeMB   int
	LastSeenAt   string
}

type EligibilityRequest struct {
	RequiredCapabilities []Capability
}

type EligibilityResult struct {
	Eligible bool
	Reasons  []string
}

var (
	// ErrNotFound is returned when a server does not exist.
	ErrNotFound = errors.New("server not found")
	// ErrInUse is returned when a server still hosts active deployments.
	ErrInUse = errors.New("server still hosts active deployments")
	// ErrHasHistory is returned when only plans or past deployments reference the server.
	ErrHasHistory = errors.New("server has deployment history and cannot be removed")
	// ErrInvalidName is returned for malformed server names.
	ErrInvalidName = errors.New("invalid server name")
	// ErrInvalidAddress is returned for malformed server addresses.
	ErrInvalidAddress = errors.New("invalid server address")
)
