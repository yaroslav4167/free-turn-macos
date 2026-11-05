//go:build darwin

package wintunnel

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	amtun "github.com/amnezia-vpn/amneziawg-go/v3/tun"
	"github.com/samosvalishe/free-turn-proxy/mobile"
	"golang.org/x/sys/unix"
)

const DefaultMTU = 1280

var mu sync.Mutex
var active *helperClient
var connecting bool
var generation uint64

func Available() bool { return true }

type darwinProtector struct{ v4, v6 int }

func (p darwinProtector) Protect(fd int) bool {
	address, err := unix.Getsockname(fd)
	if err != nil {
		return false
	}
	if _, ipv6 := address.(*unix.SockaddrInet6); ipv6 {
		return p.v6 > 0 && unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, p.v6) == nil
	}
	return p.v4 > 0 && unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_BOUND_IF, p.v4) == nil
}
func interfaceIndex(name string) int {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return 0
	}
	return iface.Index
}
func Connect(configJSON, wgConfigText string, mtu int, exclusions ...string) error {
	return ConnectWithRoutes(configJSON, wgConfigText, mtu, "", exclusions...)
}
func ConnectWithRoutes(configJSON, wgConfigText string, mtu int, routes string, exclusions ...string) (err error) {
	mu.Lock()
	if active != nil || connecting {
		mu.Unlock()
		return errors.New("macOS VPN is already connecting or active")
	}
	connecting = true
	current := generation
	mu.Unlock()
	defer func() { mu.Lock(); connecting = false; mu.Unlock() }()
	if mtu <= 0 {
		mtu = DefaultMTU
	}
	params, err := mobile.ParseTunnelConfig(wgConfigText, mtu)
	if err != nil {
		return err
	}
	if routes != "" {
		params.AllowedIPs = routes
	}
	client, fd, dev4, dev6, err := startHelper(params)
	if err != nil {
		return err
	}
	mu.Lock()
	if generation != current {
		mu.Unlock()
		_ = unix.Close(fd)
		client.Close()
		return errors.New("VPN connection was cancelled")
	}
	active = client
	mu.Unlock()
	go watchHelper(client)
	defer func() {
		if err != nil {
			Disconnect()
		}
	}()
	protector := darwinProtector{interfaceIndex(dev4), interfaceIndex(dev6)}
	probe, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err != nil {
		_ = unix.Close(fd)
		return err
	}
	protected := protector.Protect(probe)
	_ = unix.Close(probe)
	if !protected {
		_ = unix.Close(fd)
		return fmt.Errorf("cannot protect relay sockets on %q", dev4)
	}
	mobile.SetProtect(protector)
	// The root helper has already set the MTU. A zero MTU avoids privileged
	// interface ioctls when the GUI wraps the transferred utun descriptor.
	dev, err := amtun.CreateTUNFromFile(os.NewFile(uintptr(fd), "freeturn-utun"), 0)
	if err != nil {
		return fmt.Errorf("open macOS utun: %w", err)
	}
	if err = mobile.StartTunnelDevice(configJSON, dev, nil); err != nil {
		return err
	}
	if err = waitConnected(connectTimeout); err != nil {
		return err
	}
	return client.call(helperRequest{Op: "routes", Params: params})
}
func watchHelper(client *helperClient) {
	<-client.exited
	mu.Lock()
	defer mu.Unlock()
	if active == client {
		active = nil
		generation++
		mobile.Stop()
		mobile.SetProtect(nil)
	}
}
func Disconnect() {
	mu.Lock()
	generation++
	client := active
	active = nil
	mu.Unlock()
	mobile.Stop()
	mobile.SetProtect(nil)
	if client != nil {
		client.Close()
	}
}
