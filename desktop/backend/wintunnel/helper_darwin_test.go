//go:build darwin

package wintunnel

import (
	"golang.org/x/sys/unix"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMacFullTunnelKeepsDefaultRoutesAndDeduplicates(t *testing.T) {
	got, err := routePrefixes("0.0.0.0/0,::/0,10.0.0.4/24,10.0.0.9/24")
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/1"), netip.MustParsePrefix("128.0.0.0/1"), netip.MustParsePrefix("::/1"), netip.MustParsePrefix("8000::/1"), netip.MustParsePrefix("10.0.0.0/24")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routes: %v", got)
	}
	for _, bad := range []string{"default", "10.0.0.0/8;id", "-interface en0", "::1%lo0/128"} {
		if _, err := routePrefixes(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
func TestMacDNSRejectsCommandsHostnamesAndScopedAddresses(t *testing.T) {
	for _, bad := range []string{"example.test", "1.1.1.1;id", "fe80::1%en0", "0.0.0.0", "224.0.0.1"} {
		if _, err := parseDNS(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	got, err := parseDNS("1.1.1.1, 2606:4700:4700::1111")
	if err != nil || len(got) != 2 {
		t.Fatalf("DNS: %v %v", got, err)
	}
}
func TestMacAuthorizationCommandDoesNotInterpretPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "File with 'quotes' and $(touch SHOULD_NOT_EXIST).app")
	out, err := exec.Command("/bin/sh", "-c", "printf %s "+shellQuote(path)).Output()
	if err != nil || string(out) != path {
		t.Fatalf("quoting: %q %v", out, err)
	}
}
func TestMacHelperAuthenticatesUnixSocketPeer(t *testing.T) {
	// Darwin Unix socket paths are limited to 104 bytes; macOS t.TempDir paths
	// are too long, so create a short private directory for this IPC test.
	dir, err := os.MkdirTemp("/tmp", "ft-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptUnix()
		if err == nil {
			defer conn.Close()
			err = requirePeerUID(conn, uint32(os.Getuid()))
		}
		done <- err
	}()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if os.Getuid() != 0 && requireRootPeer(conn) == nil {
		t.Fatal("non-root helper accepted")
	}
}

// Exercise the actual Darwin socket options without sending traffic or changing
// routes. The GUI must be able to protect both address families without root.
func TestMacProtectorBindsBothSocketFamilies(t *testing.T) {
	iface, err := net.InterfaceByName("lo0")
	if err != nil {
		t.Fatal(err)
	}
	protector := darwinProtector{v4: iface.Index, v6: iface.Index}
	for _, family := range []int{unix.AF_INET, unix.AF_INET6} {
		fd, err := unix.Socket(family, unix.SOCK_STREAM, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !protector.Protect(fd) {
			_ = unix.Close(fd)
			t.Fatalf("protect failed for family %d", family)
		}
		level, option := unix.IPPROTO_IP, unix.IP_BOUND_IF
		if family == unix.AF_INET6 {
			level, option = unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF
		}
		bound, err := unix.GetsockoptInt(fd, level, option)
		_ = unix.Close(fd)
		if err != nil || bound != iface.Index {
			t.Fatalf("bound interface: %d %v", bound, err)
		}
	}
}
