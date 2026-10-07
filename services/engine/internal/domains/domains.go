// Package domains manages application hostnames (issue #64): registration,
// primary selection, routing targets and DNS verification. TLS issuance and
// routing state are reported by the Runtime Agent (#84); the Engine never
// handles certificates or proxy configuration.
package domains

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrNotFound is returned for unknown domains.
	ErrNotFound = errors.New("domain not found")
	// ErrTaken is returned when the hostname is already registered (any application).
	ErrTaken = errors.New("hostname is already in use")
	// ErrPrimary is returned when removing the primary domain while others exist.
	ErrPrimary = errors.New("set another primary domain before removing this one")
	// ErrNotRegistered is returned when planning with a hostname the application has not registered.
	ErrNotRegistered = errors.New("hostname is not registered for this application and environment")
	// ErrInvalidHostname is returned for malformed hostnames.
	ErrInvalidHostname = errors.New("invalid hostname")
)

var (
	hostnameRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	tldRe      = regexp.MustCompile(`^[a-z]{2,63}$`)
)

// Normalize lowercases and validates a hostname: dots-separated labels or a
// single label such as localhost. No scheme, port or path.
func Normalize(hostname string) (string, error) {
	hostname = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(hostname, ".")))
	if len(hostname) == 0 || len(hostname) > 253 || !hostnameRe.MatchString(hostname) {
		return "", fmt.Errorf("%w: must be a hostname such as app.example.com (lowercase, no scheme, no port, no path)", ErrInvalidHostname)
	}
	if strings.Contains(hostname, ".") {
		tld := hostname[strings.LastIndex(hostname, ".")+1:]
		if !tldRe.MatchString(tld) {
			return "", fmt.Errorf("%w: must be a hostname such as app.example.com (lowercase, no scheme, no port, no path)", ErrInvalidHostname)
		}
	}
	return hostname, nil
}

// Record is a registered hostname.
type Record struct {
	ID            string     `json:"id"`
	ApplicationID string     `json:"applicationId"`
	Environment   string     `json:"environment"`
	Hostname      string     `json:"hostname"`
	IsPrimary     bool       `json:"isPrimary"`
	DNSStatus     string     `json:"dnsStatus"`
	DNSExpected   string     `json:"dnsExpected,omitempty"`
	DNSObserved   string     `json:"dnsObserved,omitempty"`
	DNSCheckedAt  *time.Time `json:"dnsCheckedAt,omitempty"`
	TLSStatus     string     `json:"tlsStatus"`
	RoutingStatus string     `json:"routingStatus"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// Target is where a hostname currently routes: the latest LIVE deployment.
type Target struct {
	DeploymentID string `json:"deploymentId"`
	ServerID     string `json:"serverId"`
	Address      string `json:"-"`
}

// Store persists domains and resolves routing targets.
type Store interface {
	List(ctx context.Context, applicationID, environment string) ([]Record, error)
	Create(ctx context.Context, r Record) error
	SetPrimary(ctx context.Context, applicationID, environment, id string) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (Record, error)
	// RoutingTarget returns the latest LIVE deployment of the application in
	// the environment and its server address. ok=false when nothing serves.
	RoutingTarget(ctx context.Context, applicationID, environment string) (Target, bool, error)
	// SaveDNSCheck persists a DNS verification result.
	SaveDNSCheck(ctx context.Context, id, status, expected, observed string, at time.Time) error
}

// Resolver resolves hostnames to IPs (net.DefaultResolver in production).
type Resolver interface {
	LookupIP(ctx context.Context, host string) ([]string, error)
}

// Service implements domain management.
type Service struct {
	Store    Store
	Resolver Resolver
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Get returns a domain by ID (used to resolve ownership; callers must verify
// the application belongs to them).
func (s *Service) Get(ctx context.Context, id string) (Record, error) {
	return s.Store.Get(ctx, id)
}

// RoutingTarget returns where a hostname currently routes (latest LIVE deployment).
func (s *Service) RoutingTarget(ctx context.Context, applicationID, environment string) (Target, bool, error) {
	return s.Store.RoutingTarget(ctx, applicationID, environment)
}

// List returns the application's domains, optionally filtered by environment.
func (s *Service) List(ctx context.Context, applicationID, environment string) ([]Record, error) {
	return s.Store.List(ctx, applicationID, environment)
}

// Create registers a hostname. The first domain of an environment becomes primary.
func (s *Service) Create(ctx context.Context, applicationID, environment, hostname string) (Record, error) {
	name, err := Normalize(hostname)
	if err != nil {
		return Record{}, err
	}
	existing, err := s.Store.List(ctx, applicationID, environment)
	if err != nil {
		return Record{}, err
	}
	rec := Record{ID: "dom_" + randomHex(12), ApplicationID: applicationID, Environment: environment,
		Hostname: name, IsPrimary: len(existing) == 0, DNSStatus: "unknown", TLSStatus: "pending", RoutingStatus: "pending",
		CreatedAt: s.now()}
	if err := s.Store.Create(ctx, rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// SetPrimary makes a domain the primary hostname of its environment.
func (s *Service) SetPrimary(ctx context.Context, applicationID, id string) (Record, error) {
	rec, err := s.owned(ctx, applicationID, id)
	if err != nil {
		return Record{}, err
	}
	if err := s.Store.SetPrimary(ctx, applicationID, rec.Environment, id); err != nil {
		return Record{}, err
	}
	rec.IsPrimary = true
	return rec, nil
}

// Remove deletes a domain. The primary can only go after another primary is
// chosen, unless it is the last domain of its environment.
func (s *Service) Remove(ctx context.Context, applicationID, id string) error {
	rec, err := s.owned(ctx, applicationID, id)
	if err != nil {
		return err
	}
	if rec.IsPrimary {
		siblings, err := s.Store.List(ctx, applicationID, rec.Environment)
		if err != nil {
			return err
		}
		if len(siblings) > 1 {
			return ErrPrimary
		}
	}
	return s.Store.Delete(ctx, id)
}

// EnsureDomain resolves the hostname used for planning: an already registered
// name is returned; the first hostname of an environment is auto-registered
// as primary; any other unregistered name is rejected so traffic cannot be
// planned to a hostname the application does not own here.
func (s *Service) EnsureDomain(ctx context.Context, applicationID, environment, hostname string) (Record, bool, error) {
	name, err := Normalize(hostname)
	if err != nil {
		return Record{}, false, err
	}
	existing, err := s.Store.List(ctx, applicationID, environment)
	if err != nil {
		return Record{}, false, err
	}
	for _, r := range existing {
		if r.Hostname == name {
			return r, false, nil
		}
	}
	if len(existing) > 0 {
		return Record{}, false, ErrNotRegistered
	}
	rec, err := s.Create(ctx, applicationID, environment, name)
	return rec, true, err
}

func (s *Service) owned(ctx context.Context, applicationID, id string) (Record, error) {
	rec, err := s.Store.Get(ctx, id)
	if err != nil {
		return Record{}, err
	}
	if rec.ApplicationID != applicationID {
		return Record{}, ErrNotFound
	}
	return rec, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
