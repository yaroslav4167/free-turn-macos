//go:build darwin

package backend

import (
	"context"
	"fmt"
	"net"
	"os/exec"
)

func probeServerPing(ctx context.Context, host string) (float64, error) {
	path := "/sbin/ping"
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		path = "/sbin/ping6"
	}
	cmd := exec.CommandContext(ctx, path, "-n", "-c", "1", "-W", "2000", host)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL=C"}
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ICMP probe unavailable: %w", err)
	}
	return parsePingTime(string(out))
}
