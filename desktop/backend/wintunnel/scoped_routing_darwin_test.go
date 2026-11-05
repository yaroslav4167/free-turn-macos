//go:build darwin

package wintunnel

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/samosvalishe/free-turn-proxy/mobile"
)

// Run only in an isolated macOS CI runner. The two temporary routes cover
// documentation/benchmark destinations only: no default route is replaced and
// no VPN credentials or external server are needed. The zero host route also
// exposes Darwin's unscoped-default fallback after a more-specific VPN route.
func TestMacPrivilegedScopedRouting(t *testing.T) {
	if os.Getenv("FREETURN_PRIVILEGED_TESTS") != "1" || os.Geteuid() != 0 {
		t.Skip("requires explicit opt-in on an isolated privileged macOS runner")
	}
	params := &mobile.TunnelParams{MTU: 1280, Addresses: "198.19.255.1/32", AllowedIPs: "0.0.0.0/32,198.18.0.254/32"}
	state, dev, err := prepareHelper(params)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	defer state.cleanup()
	protector := darwinProtector{v4: interfaceIndex(state.dev4), v6: interfaceIndex(state.dev6)}
	dial := func() (net.Conn, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		d := net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
			ok := false
			if err := raw.Control(func(fd uintptr) { ok = protector.Protect(int(fd)) }); err != nil {
				return err
			}
			if !ok {
				return errors.New("socket protection failed")
			}
			return nil
		}}
		// UDP connect performs a kernel route lookup; no packet is transmitted.
		return d.DialContext(ctx, "udp4", "198.18.0.254:9")
	}
	before, err := dial()
	if err != nil {
		t.Fatalf("physical route unavailable before VPN routes: %v", err)
	}
	_ = before.Close()
	if err = state.installRoutes(params); err != nil {
		t.Fatal(err)
	}
	unbound, err := command("/sbin/route", "-n", "get", "-inet", "198.18.0.254")
	if err != nil || !strings.Contains(unbound, "interface: "+state.name) {
		t.Fatalf("ordinary traffic must follow VPN: %s %v", unbound, err)
	}
	scoped, routeErr := command("/sbin/route", "-n", "get", "-inet", "-ifscope", state.dev4, "198.18.0.254")
	after, dialErr := dial()
	if after != nil {
		defer after.Close()
	}
	if routeErr != nil || dialErr != nil {
		t.Fatalf("protected relay became unreachable after VPN routes: route=%s %v, UDP connect=%v", scoped, routeErr, dialErr)
	}
	if !strings.Contains(scoped, "interface: "+state.dev4) {
		t.Fatalf("protected relay used wrong interface: %s", scoped)
	}
	t.Logf("unbound -> %s; bound relay -> %s; UDP route lookup succeeds", state.name, state.dev4)
}
