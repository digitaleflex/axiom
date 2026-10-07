package planner_test

import (
	"context"
	"net"
)

// stdResolver resolves DNS through the system resolver.
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
