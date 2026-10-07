package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// StaleAfter bounds heartbeat freshness: a ready/degraded server unseen for
// longer is reported offline. Tuned by the heartbeat work (#78) if needed.
var StaleAfter = 5 * time.Minute

// EffectiveStatusAt returns the status to display and decide on: stored
// status, except ready/degraded servers with a stale or missing-freshness
// heartbeat read as offline. Records without any heartbeat keep their stored
// status (freshness unknown, never assumed dead).
func EffectiveStatusAt(r Record, now time.Time) Status {
	if r.Status != StatusReady && r.Status != StatusDegraded {
		return r.Status
	}
	if r.LastSeenAt == "" {
		return r.Status
	}
	seen, err := time.Parse(time.RFC3339, r.LastSeenAt)
	if err != nil || now.Sub(seen) > StaleAfter {
		return StatusOffline
	}
	return r.Status
}

var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidName reports whether name is an acceptable server name.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// ValidAddress reports whether address is host or host:port (no scheme/path).
func ValidAddress(address string) bool {
	if address == "" || len(address) > 253 || strings.ContainsAny(address, " /") || strings.Contains(address, "://") {
		return false
	}
	host := address
	if strings.HasPrefix(address, "[") {
		end := strings.Index(address, "]")
		if end < 0 {
			return false
		}
		host = address[1:end]
		if rest := address[end+1:]; rest != "" {
			port, ok := strings.CutPrefix(rest, ":")
			if !ok {
				return false
			}
			if !validPort(port) {
				return false
			}
		}
	} else if strings.Count(address, ":") > 1 {
		return net.ParseIP(address) != nil // bare IPv6, no port
	} else if h, port, found := strings.Cut(address, ":"); found {
		if h == "" || !validPort(port) {
			return false
		}
		host = h
	}
	if ip := net.ParseIP(host); ip != nil {
		return true
	}
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

// RequiredCapabilities maps a plan preset to the server capabilities it
// needs. domain adds routing (Traefik). Mirrors the planner eligibility.
func validPort(port string) bool {
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

func RequiredCapabilities(preset string, domain bool) []Capability {
	caps := []Capability{CapabilityDocker}
	if preset == "compose" {
		caps = append(caps, CapabilityCompose)
	}
	if domain {
		caps = append(caps, CapabilityTraefik)
	}
	return caps
}

// Register creates a pending server record. The Runtime Agent binds to it
// during registration (#76); until the first heartbeat the server is PENDING.
func (s *Service) Register(ctx context.Context, ownerID, name, address string) (Record, error) {
	if s == nil || s.repo == nil {
		return Record{}, fmt.Errorf("server repository is required")
	}
	if !ValidName(strings.ToLower(name)) {
		return Record{}, ErrInvalidName
	}
	if !ValidAddress(address) {
		return Record{}, ErrInvalidAddress
	}
	rec := Record{ID: "srv_" + randomHex(12), Name: strings.ToLower(name), Address: address, OwnerID: ownerID, Status: StatusPending}
	if err := s.repo.Create(ctx, rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// Rename changes a server's name.
func (s *Service) Rename(ctx context.Context, id, name string) (Record, error) {
	if s == nil || s.repo == nil {
		return Record{}, fmt.Errorf("server repository is required")
	}
	if !ValidName(strings.ToLower(name)) {
		return Record{}, ErrInvalidName
	}
	if _, err := s.repo.Get(ctx, id); err != nil {
		return Record{}, err
	}
	if err := s.repo.Rename(ctx, id, strings.ToLower(name)); err != nil {
		return Record{}, err
	}
	return s.repo.Get(ctx, id)
}

// Remove deletes a server that hosts no active deployment.
func (s *Service) Remove(ctx context.Context, id string) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("server repository is required")
	}
	if _, err := s.repo.Get(ctx, id); err != nil {
		return err
	}
	n, err := s.ActiveDeployments(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: %d active deployment(s)", ErrInUse, n)
	}
	return s.repo.Delete(ctx, id)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
