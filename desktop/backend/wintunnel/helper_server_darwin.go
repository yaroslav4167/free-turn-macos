//go:build darwin

package wintunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	amtun "github.com/amnezia-vpn/amneziawg-go/v3/tun"
	"github.com/samosvalishe/free-turn-proxy/mobile"
	"golang.org/x/sys/unix"
)

type helperState struct {
	name, dev4, dev6     string
	physical4, physical6 physicalNetworkRoute
	run                  func(string, ...string) (string, error)
	routes               [][]string
	dns                  uintptr
	applied              bool
}

func RunHelper(socketPath string) error {
	if os.Geteuid() != 0 {
		return errors.New("macOS VPN helper requires administrator authorization")
	}
	info, err := os.Lstat(socketPath)
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid == 0 || info.Mode()&os.ModeSocket == 0 {
		return errors.New("VPN socket must belong to a non-root user")
	}
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return err
	}
	defer conn.Close()
	if err = requirePeerUID(conn, owner.Uid); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	cancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer cancel()
	return serveDarwinHelper(conn)
}
func readRequest(reader *bufio.Reader, req *helperRequest) error {
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return err
	}
	if len(line) > 64*1024 {
		return errors.New("VPN helper request too large")
	}
	return json.Unmarshal(line, req)
}
func serveDarwinHelper(conn *net.UnixConn) error {
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReaderSize(conn, 64*1024+1)
	var request helperRequest
	if err := readRequest(reader, &request); err != nil {
		return err
	}
	if request.Op != "init" || request.Params == nil {
		return errors.New("invalid VPN initialization")
	}
	state, dev, err := prepareHelper(request.Params)
	if err != nil {
		_, _, _ = conn.WriteMsgUnix([]byte{0}, nil, nil)
		_ = json.NewEncoder(conn).Encode(helperReply{Error: err.Error()})
		return err
	}
	defer dev.Close()
	defer state.cleanup()
	// SCM_RIGHTS is sent on a single marker byte; the JSON reply is a separate
	// frame so a short stream read cannot truncate initialization metadata.
	if _, _, err = conn.WriteMsgUnix([]byte{1}, unix.UnixRights(int(dev.File().Fd())), nil); err != nil {
		return err
	}
	if err = json.NewEncoder(conn).Encode(helperReply{Dev4: state.dev4, Dev6: state.dev6}); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Time{})
	for {
		var req helperRequest
		if err := readRequest(reader, &req); err != nil {
			return nil
		}
		var opErr error
		if req.Op != "routes" || state.applied || req.Params == nil || *req.Params != *request.Params {
			opErr = errors.New("invalid or duplicate VPN route request")
		} else {
			state.applied = true
			opErr = state.installRoutes(req.Params)
		}
		reply := helperReply{}
		if opErr != nil {
			reply.Error = opErr.Error()
		}
		if err := json.NewEncoder(conn).Encode(reply); err != nil {
			return err
		}
		if opErr != nil {
			return opErr
		}
	}
}
func parsePrefixes(value string) ([]netip.Prefix, error) {
	var result []netip.Prefix
	if strings.TrimSpace(value) == "" {
		return result, nil
	}
	for _, entry := range strings.Split(value, ",") {
		p, err := netip.ParsePrefix(strings.TrimSpace(entry))
		if err != nil || p.Addr().Zone() != "" {
			return nil, fmt.Errorf("invalid VPN prefix %q", entry)
		}
		result = append(result, p)
	}
	if len(result) > 4096 {
		return nil, errors.New("too many VPN routes")
	}
	return result, nil
}
func parseDNS(value string) ([]string, error) {
	var result []string
	if strings.TrimSpace(value) == "" {
		return result, nil
	}
	for _, entry := range strings.Split(value, ",") {
		ip, err := netip.ParseAddr(strings.TrimSpace(entry))
		if err != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
			return nil, errors.New("invalid VPN DNS address")
		}
		result = append(result, ip.String())
	}
	if len(result) > 16 {
		return nil, errors.New("too many DNS servers")
	}
	return result, nil
}
func command(path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL=C"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %v: %w: %s", path, args, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Keep the actual physical gateway before any VPN routes are added. A socket
// bound to en0 needs a route in en0's scope, not just a global default route.
type physicalNetworkRoute struct {
	device, gateway string
	ipv6            bool
	direct          bool
}

func parsePhysicalNetworkRoute(output string, ipv6 bool) (physicalNetworkRoute, error) {
	result := physicalNetworkRoute{ipv6: ipv6}
	gatewayRoute := false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "interface:":
			result.device = fields[1]
		case "gateway:":
			result.gateway = fields[1]
		case "flags:":
			gatewayRoute = strings.Contains(fields[1], "GATEWAY")
		}
	}
	if result.device == "" || strings.HasPrefix(result.device, "utun") {
		return result, errors.New("physical network interface not found or another VPN owns the route")
	}
	ip, err := netip.ParseAddr(result.gateway)
	if err != nil {
		if gatewayRoute {
			return result, fmt.Errorf("invalid physical gateway %q", result.gateway)
		}
		result.direct = true
		result.gateway = ""
	} else if ip.Is6() != ipv6 || (ip.Zone() != "" && ip.Zone() != result.device) {
		return result, errors.New("physical gateway has wrong address family or interface zone")
	}
	return result, nil
}
func discoverPhysicalNetworkRoute(ipv6 bool) (physicalNetworkRoute, error) {
	family, destination := "-inet", "1.1.1.1"
	if ipv6 {
		family, destination = "-inet6", "2606:4700:4700::1111"
	}
	out, err := command("/sbin/route", "-n", "get", family, destination)
	if err != nil {
		return physicalNetworkRoute{}, err
	}
	result, err := parsePhysicalNetworkRoute(out, ipv6)
	if err != nil {
		return result, err
	}
	_, err = net.InterfaceByName(result.device)
	return result, err
}
func (r physicalNetworkRoute) family() string {
	if r.ipv6 {
		return "-inet6"
	}
	return "-inet"
}
func (r physicalNetworkRoute) defaultArgs(operation string) []string {
	args := []string{"-n", operation, r.family(), "-ifscope", r.device, "default"}
	if r.direct {
		return append(args, "-interface", r.device)
	}
	return append(args, r.gateway)
}
func scopedDefaultExists(output, device string) bool {
	foundDevice, foundScope, foundDefault := false, false, false
	zeroDestination, zeroMask := false, false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "interface:":
			foundDevice = fields[1] == device
		case "destination:":
			foundDefault = fields[1] == "default"
			zeroDestination = fields[1] == "0.0.0.0" || fields[1] == "::"
		case "mask:":
			zeroMask = fields[1] == "default" || fields[1] == "0.0.0.0" || fields[1] == "::"
		case "flags:":
			for _, flag := range strings.Split(strings.Trim(fields[1], "<>"), ",") {
				foundScope = foundScope || flag == "IFSCOPE"
			}
		}
	}
	return foundDevice && foundScope && (foundDefault || (zeroDestination && zeroMask))
}
func (s *helperState) runCommand(path string, args ...string) (string, error) {
	if s.run != nil {
		return s.run(path, args...)
	}
	return command(path, args...)
}
func (s *helperState) ensurePhysicalDefault(r physicalNetworkRoute) error {
	if r.device == "" {
		return nil
	} // No physical IPv6 path is a valid IPv4-only host.
	get := []string{"-n", "get", r.family(), "-ifscope", r.device, "default"}
	out, err := s.runCommand("/sbin/route", get...)
	if err == nil && scopedDefaultExists(out, r.device) {
		return nil
	}
	if _, err = s.runCommand("/sbin/route", r.defaultArgs("add")...); err != nil {
		// A route installed concurrently belongs to its owner, not to this helper.
		if out, queryErr := s.runCommand("/sbin/route", get...); queryErr == nil && scopedDefaultExists(out, r.device) {
			return nil
		}
		return fmt.Errorf("preserve physical %s route on %s: %w", r.family(), r.device, err)
	}
	s.routes = append(s.routes, r.defaultArgs("delete"))
	return nil
}
func prepareHelper(params *mobile.TunnelParams) (*helperState, amtun.Device, error) {
	if params == nil || params.MTU < 576 || params.MTU > 9000 {
		return nil, nil, errors.New("invalid tunnel MTU")
	}
	addresses, err := parsePrefixes(params.Addresses)
	if err != nil || len(addresses) == 0 {
		return nil, nil, errors.New("VPN interface address is missing or invalid")
	}
	if _, err := parsePrefixes(params.AllowedIPs); err != nil {
		return nil, nil, err
	}
	if _, err := parseDNS(params.DNS); err != nil {
		return nil, nil, err
	}
	physical4, err := discoverPhysicalNetworkRoute(false)
	if err != nil {
		return nil, nil, err
	}
	physical6, _ := discoverPhysicalNetworkRoute(true)
	dev, err := amtun.CreateTUN("utun", params.MTU)
	if err != nil {
		return nil, nil, err
	}
	name, err := dev.Name()
	if err != nil {
		_ = dev.Close()
		return nil, nil, err
	}
	state := &helperState{name: name, dev4: physical4.device, dev6: physical6.device, physical4: physical4, physical6: physical6}
	for _, address := range addresses {
		var args []string
		if address.Addr().Is4() {
			args = []string{name, "inet", address.Addr().String(), address.Addr().String(), "netmask", "255.255.255.255", "alias"}
		} else {
			args = []string{name, "inet6", address.String(), "alias"}
		}
		if _, err = command("/sbin/ifconfig", args...); err != nil {
			_ = dev.Close()
			return nil, nil, err
		}
	}
	if _, err = command("/sbin/ifconfig", name, "up"); err != nil {
		_ = dev.Close()
		return nil, nil, err
	}
	return state, dev, nil
}

// Keep the physical default routes. Two more-specific halves carry full-tunnel
// traffic, while IP_BOUND_IF/IPV6_BOUND_IF protect only the relay's sockets.
func routePrefixes(value string) ([]netip.Prefix, error) {
	prefixes, err := parsePrefixes(value)
	if err != nil {
		return nil, err
	}
	var result []netip.Prefix
	seen := map[netip.Prefix]bool{}
	for _, p := range prefixes {
		p = p.Masked()
		parts := []netip.Prefix{p}
		if p.Bits() == 0 {
			if p.Addr().Is4() {
				parts = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/1"), netip.MustParsePrefix("128.0.0.0/1")}
			} else {
				parts = []netip.Prefix{netip.MustParsePrefix("::/1"), netip.MustParsePrefix("8000::/1")}
			}
		}
		for _, part := range parts {
			if !seen[part] {
				seen[part] = true
				result = append(result, part)
			}
		}
	}
	return result, nil
}
func (s *helperState) installRoutes(params *mobile.TunnelParams) error {
	prefixes, err := routePrefixes(params.AllowedIPs)
	if err != nil {
		return err
	}
	if err := s.ensurePhysicalDefault(s.physical4); err != nil {
		return err
	}
	if err := s.ensurePhysicalDefault(s.physical6); err != nil {
		return err
	}
	for _, p := range prefixes {
		family := "-inet"
		if p.Addr().Is6() {
			family = "-inet6"
		}
		args := []string{"-n", "add", family, p.String(), "-interface", s.name}
		if _, err := s.runCommand("/sbin/route", args...); err != nil {
			return err
		}
		s.routes = append(s.routes, []string{"-n", "delete", family, p.String(), "-interface", s.name})
	}
	dns, err := parseDNS(params.DNS)
	if err != nil {
		return err
	}
	if len(dns) > 0 {
		s.dns, err = installSessionDNS(s.name, dns)
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *helperState) cleanup() {
	if s.dns != 0 {
		removeSessionDNS(s.dns)
		s.dns = 0
	}
	for i := len(s.routes) - 1; i >= 0; i-- {
		_, _ = s.runCommand("/sbin/route", s.routes[i]...)
	}
	s.routes = nil
}
