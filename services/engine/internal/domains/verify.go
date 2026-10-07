package domains

import (
	"context"
	"strings"
	"time"
)

// DNS statuses.
const (
	DNSUnknown  = "unknown"
	DNSPending  = "pending"
	DNSOk       = "ok"
	DNSMismatch = "mismatch"
	DNSError    = "error"
)

// CheckDNS verifies that hostname resolves to the address serving the
// application in its environment. Without a LIVE deployment there is nothing
// to point to yet (pending). Failures to resolve are recorded as errors with
// the observed value empty, never as success.
func (s *Service) CheckDNS(ctx context.Context, applicationID, id string) (Record, error) {
	rec, err := s.owned(ctx, applicationID, id)
	if err != nil {
		return Record{}, err
	}
	target, ok, err := s.Store.RoutingTarget(ctx, applicationID, rec.Environment)
	if err != nil {
		return Record{}, err
	}
	now := s.now()
	if !ok || target.Address == "" {
		return s.save(ctx, rec, DNSPending, "", "", now)
	}
	ips, err := s.Resolver.LookupIP(ctx, rec.Hostname)
	if err != nil {
		return s.save(ctx, rec, DNSError, target.Address, "", now)
	}
	observed := dedupe(ips)
	expected := target.Address
	if !isIP(expected) {
		addrs, err := s.Resolver.LookupIP(ctx, expected)
		if err != nil {
			return s.save(ctx, rec, DNSError, expected, strings.Join(observed, ","), now)
		}
		expected = strings.Join(dedupe(addrs), ",")
	}
	status := DNSMismatch
	for _, ip := range observed {
		if ip == target.Address || containsIP(expected, ip) {
			status = DNSOk
			break
		}
	}
	// When the expected server address is a hostname, compare resolved sets.
	if status == DNSMismatch && !isIP(target.Address) {
		for _, ip := range observed {
			if containsIP(expected, ip) {
				status = DNSOk
				break
			}
		}
	}
	return s.save(ctx, rec, status, target.Address, strings.Join(observed, ","), now)
}

func (s *Service) save(ctx context.Context, rec Record, status, expected, observed string, at time.Time) (Record, error) {
	if err := s.Store.SaveDNSCheck(ctx, rec.ID, status, expected, observed, at); err != nil {
		return Record{}, err
	}
	rec.DNSStatus, rec.DNSExpected, rec.DNSObserved, rec.DNSCheckedAt = status, expected, observed, &at
	return rec, nil
}

func dedupe(ips []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, ip := range ips {
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	return out
}

func containsIP(list, ip string) bool {
	for _, v := range strings.Split(list, ",") {
		if strings.TrimSpace(v) == ip {
			return true
		}
	}
	return false
}

func isIP(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && c != '.' && c != ':' && !(c >= 'a' && c <= 'f') && !(c >= 'A' && c <= 'F') {
			return false
		}
	}
	return strings.Contains(s, ".") || strings.Contains(s, ":")
}
