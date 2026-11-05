package backend

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/samosvalishe/free-turn-proxy/desktop/backend/routing"
)

// Linux already binds relay sockets to the physical interface. A global host
// route for the VPS would also force SSH outside the VPN, making management
// impossible on networks where that direct path is blocked. Leave ordinary
// sockets (including SSH) on the user's profile/split-tunnel routes instead.
func routeExclusions(platform string, resolved []string) []string {
	if platform == "linux" || platform == "darwin" {
		return nil
	}
	return resolved
}

// Resolve before stopping the current session or changing any system routes.
// The privileged helper deliberately continues to accept IP literals only.
func resolveExclusions(ctx context.Context, srv Server, resolver routing.Resolver) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	host, _, err := net.SplitHostPort(srv.Peer)
	if err != nil {
		return nil, err
	}
	hosts := []string{host}
	if srv.SSH != nil && srv.SSH.IP != "" {
		hosts = append(hosts, srv.SSH.IP)
	}
	seenHosts := map[string]bool{}
	seenIPs := map[netip.Addr]bool{}
	var result []string
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		key := strings.ToLower(host)
		if seenHosts[key] {
			continue
		}
		seenHosts[key] = true
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ip, parseErr := netip.ParseAddr(host)
		ips := []netip.Addr{ip}
		if parseErr != nil {
			ips, err = resolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("resolve %q: %w", host, err)
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("resolve %q: no IP addresses", host)
		}
		for _, ip := range ips {
			if !ip.IsValid() || ip.Zone() != "" {
				return nil, fmt.Errorf("resolve %q: invalid or scoped IP", host)
			}
			ip = ip.Unmap()
			if !seenIPs[ip] {
				result = append(result, ip.String())
				seenIPs[ip] = true
			}
		}
	}
	return result, ctx.Err()
}
