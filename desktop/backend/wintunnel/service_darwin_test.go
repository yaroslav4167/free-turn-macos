//go:build darwin

package wintunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samosvalishe/free-turn-proxy/desktop/backend/vpnservice"
	"github.com/samosvalishe/free-turn-proxy/mobile"
	"golang.org/x/sys/unix"
)

func testServiceListener(t *testing.T) (*net.UnixListener, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ft-svc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return listener, path
}
func testServiceDial(t *testing.T, path string) *net.UnixConn {
	t.Helper()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	return conn
}
func TestMacPersistentServiceStatusAndInvalidRequestDoNotConfigureNetwork(t *testing.T) {
	listener, path := testServiceListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serveHelperListener(ctx, listener, uint32(os.Getuid()), serveDarwinHelperSession) }()
	for i := 0; i < 3; i++ {
		conn := testServiceDial(t, path)
		if err := json.NewEncoder(conn).Encode(helperRequest{Op: "status"}); err != nil {
			t.Fatal(err)
		}
		var reply helperReply
		if err := json.NewDecoder(conn).Decode(&reply); err != nil || reply.Protocol != helperProtocol {
			t.Fatalf("status: %+v %v", reply, err)
		}
		conn.Close()
	}
	conn := testServiceDial(t, path)
	// Invalid MTU fails before route lookup, utun creation or DNS changes.
	if err := json.NewEncoder(conn).Encode(helperRequest{Op: "init", Params: &mobile.TunnelParams{MTU: 1}}); err != nil {
		t.Fatal(err)
	}
	marker := make([]byte, 1)
	if _, err := io.ReadFull(conn, marker); err != nil || marker[0] != 0 {
		t.Fatalf("invalid request marker: %v", err)
	}
	var reply helperReply
	if err := json.NewDecoder(conn).Decode(&reply); err != nil || !strings.Contains(reply.Error, "MTU") {
		t.Fatalf("invalid request: %+v %v", reply, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
	}
}
func TestMacServiceRejectsOtherUID(t *testing.T) {
	listener, path := testServiceListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var called atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- serveHelperListener(ctx, listener, uint32(os.Getuid()+1), func(*net.UnixConn, func() (func(), error)) error { called.Store(true); return nil })
	}()
	conn := testServiceDial(t, path)
	var b [1]byte
	if _, err := conn.Read(b[:]); err == nil {
		t.Fatal("unauthorized client not disconnected")
	}
	if called.Load() {
		t.Fatal("unauthorized client reached helper protocol")
	}
	cancel()
	<-done
}

func TestMacHelperClientDisconnectCancelsPreparationAndReleasesSlot(t *testing.T) {
	listener, path := testServiceListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, released := make(chan struct{}, 2), make(chan struct{}, 2)
	done := make(chan error, 1)
	completed := make(chan struct{}, 2)
	prepare := func(ctx context.Context, _ *mobile.TunnelParams) (*helperState, helperDevice, error) {
		entered <- struct{}{}
		<-ctx.Done()
		released <- struct{}{}
		return nil, nil, ctx.Err()
	}
	go func() {
		done <- serveHelperListener(ctx, listener, uint32(os.Getuid()), func(conn *net.UnixConn, acquire func() (func(), error)) error {
			err := serveDarwinHelperSessionWithPrepare(conn, acquire, prepare)
			completed <- struct{}{}
			return err
		})
	}()
	for i := 0; i < 2; i++ {
		conn := testServiceDial(t, path)
		if err := json.NewEncoder(conn).Encode(helperRequest{Op: "init", Params: &mobile.TunnelParams{MTU: 1280}}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("preparation blocked by stale VPN session")
		}
		conn.Close()
		select {
		case <-released:
		case <-time.After(time.Second):
			t.Fatal("client EOF did not cancel preparation")
		}
		select {
		case <-completed:
		case <-time.After(time.Second):
			t.Fatal("cancelled session retained its slot")
		}

	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("service failed to stop")
	}
}

func TestMacHelperInitializationOutlivesRequestReadTimeout(t *testing.T) {
	listener, path := testServiceListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- serveHelperListener(ctx, listener, uint32(os.Getuid()), func(conn *net.UnixConn, acquire func() (func(), error)) error {
			return serveDarwinHelperSessionWithPrepare(conn, acquire, func(ctx context.Context, _ *mobile.TunnelParams) (*helperState, helperDevice, error) {
				select {
				case <-time.After(16 * time.Second):
					return nil, nil, errors.New("slow initialization completed")
				case <-ctx.Done():
					return nil, nil, ctx.Err()
				}
			})
		})
	}()
	conn := testServiceDial(t, path)
	_ = conn.SetDeadline(time.Now().Add(25 * time.Second))
	if err := json.NewEncoder(conn).Encode(helperRequest{Op: "init", Params: &mobile.TunnelParams{MTU: 1280}}); err != nil {
		t.Fatal(err)
	}
	var marker [1]byte
	if _, err := io.ReadFull(conn, marker[:]); err != nil {
		t.Fatalf("setup cancelled by obsolete 15-second read deadline: %v", err)
	}
	var reply helperReply
	if err := json.NewDecoder(conn).Decode(&reply); err != nil || reply.Error != "slow initialization completed" {
		t.Fatalf("slow setup: %+v %v", reply, err)
	}
	conn.Close()
	cancel()
	<-done
}

func TestMacHelperCommandCancellationStopsInFlightCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := commandContext(ctx, "/bin/sleep", "30"); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation cause lost: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("helper command survived session cancellation")
	}
}
func TestMacServiceDisconnectAndShutdownReleaseSessions(t *testing.T) {
	listener, path := testServiceListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, released := make(chan struct{}, 3), make(chan struct{}, 3)
	done := make(chan error, 1)
	go func() {
		done <- serveHelperListener(ctx, listener, uint32(os.Getuid()), func(c *net.UnixConn, acquire func() (func(), error)) error {
			release, err := acquire()
			if err != nil {
				_ = json.NewEncoder(c).Encode(helperReply{Error: err.Error()})
				return err
			}
			defer func() { release(); released <- struct{}{} }()
			entered <- struct{}{}
			_, err = io.Copy(io.Discard, c)
			return err
		})
	}()
	wait := func(ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("session cleanup timed out")
		}
	}
	first := testServiceDial(t, path)
	wait(entered)
	second := testServiceDial(t, path)
	var reply helperReply
	if err := json.NewDecoder(second).Decode(&reply); err != nil || reply.Error == "" {
		t.Fatalf("parallel VPN was accepted: %+v %v", reply, err)
	}
	first.Close()
	wait(released)
	third := testServiceDial(t, path)
	wait(entered)
	cancel()
	wait(released)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("service shutdown timed out")
	}
	if _, err := third.Read(make([]byte, 1)); err == nil {
		t.Fatal("shutdown retained client")
	}
}

func TestMacServiceUnprivilegedProbe(t *testing.T) {
	if os.Getenv("FREETURN_TEST_SERVICE_PROBE") != "1" {
		t.Skip("child of isolated privileged lifecycle test")
	}
	if os.Getuid() == 0 {
		t.Fatal("probe must run without root")
	}
	for i := 0; i < 3; i++ {
		conn, err := dialPersistentHelper()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if err = json.NewEncoder(conn).Encode(helperRequest{Op: "status"}); err != nil {
			t.Fatal(err)
		}
		var reply helperReply
		err = json.NewDecoder(conn).Decode(&reply)
		conn.Close()
		if err != nil || reply.Protocol != helperProtocol {
			t.Fatalf("root helper status: %+v %v", reply, err)
		}
	}
	for _, address := range []string{"198.19.255.1/32", "198.19.255.1/32,fd00:ff::1/128", "198.19.255.1/32"} {
		params := &mobile.TunnelParams{MTU: 1280, Addresses: address}
		client, fd, _, _, err := startHelper(params)
		if err != nil {
			t.Fatal(err)
		}
		flags, flagErr := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if flagErr != nil || flags&unix.O_NONBLOCK == 0 {
			unix.Close(fd)
			client.Close()
			t.Fatalf("utun descriptor is blocking: flags=%d error=%v", flags, flagErr)
		}
		// No VPN route or DNS is installed; exercise real utun descriptor passing.
		other, otherFD, _, _, overlapErr := startHelper(params)
		if overlapErr == nil {
			unix.Close(otherFD)
			other.Close()
			t.Error("overlapping real session accepted")
		}
		unix.Close(fd)
		client.Close()
	}

}
func TestMacPrivilegedPersistentService(t *testing.T) {
	if os.Getenv("FREETURN_PRIVILEGED_TESTS") != "1" || os.Geteuid() != 0 {
		t.Skip("explicit opt-in on an isolated macOS CI runner only")
	}
	uid, err := ParseServiceUID(os.Getenv("FREETURN_TEST_SERVICE_UID"))
	if err != nil {
		t.Fatal(err)
	}
	source := os.Getenv("FREETURN_TEST_HELPER_BINARY")
	if source == "" {
		t.Fatal("missing signed helper binary")
	}
	if vpnservice.Installed(int(uid)) {
		t.Fatal("refusing to replace a pre-existing system service")
	}
	t.Cleanup(func() {
		if err := vpnservice.Remove(uid); err != nil {
			t.Error(err)
		}
	})
	if err = vpnservice.Install(uid, source); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{vpnservice.HelperPath(uid), vpnservice.PlistPath(uid)} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm()&0o022 != 0 {
			t.Fatalf("unsafe installed file: %s %v", path, err)
		}
	}
	probe := func() {
		exe, _ := os.Executable()
		cmd := exec.Command("/usr/bin/sudo", "-n", "-u", "#"+strconv.FormatUint(uint64(uid), 10), "/usr/bin/env", "FREETURN_TEST_SERVICE_PROBE=1", exe, "-test.run", "^TestMacServiceUnprivilegedProbe$", "-test.v", "-test.timeout", "25s")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("unprivileged connection: %s %v", out, err)
		}
	}
	// No osascript or password cache exists in this subprocess. The installed
	// daemon accepts repeated user sessions, also after a launchd restart.
	probe()
	policy, policyErr := exec.Command("/bin/launchctl", "print", "system/"+vpnservice.Label(uid)).CombinedOutput()
	if policyErr != nil || !strings.Contains(string(policy), "spawn type = interactive") {
		t.Fatalf("VPN service is subject to background throttling: %s %v", policy, policyErr)
	}
	if out, err := exec.Command("/bin/launchctl", "kickstart", "-k", "system/"+vpnservice.Label(uid)).CombinedOutput(); err != nil {
		t.Fatalf("restart: %s %v", out, err)
	}
	probe()
	if err = vpnservice.Remove(uid); err != nil {
		t.Fatal(err)
	}
	if vpnservice.Installed(int(uid)) || vpnservice.Available(int(uid)) {
		t.Fatal("service access retained after removal")
	}
	t.Log(fmt.Sprintf("UID %d: install, repeated unprivileged sessions, restart and removal passed", uid))
}

func TestMacHelperProtocolUpgradeIsRequestedOnlyForOldService(t *testing.T) {
	for _, tc := range []struct {
		name     string
		version  int
		deny     bool
		upgrades int
	}{
		{"compatible", helperProtocol, false, 0}, {"newer", helperProtocol + 1, false, 0}, {"old", helperProtocol - 1, false, 1}, {"authorization declined", helperProtocol - 1, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			version, calls := tc.version, 0
			denied := errors.New("authorization declined")
			probe := func() (int, error) { return version, nil }
			upgrade := func() error {
				calls++
				if tc.deny {
					return denied
				}
				version = helperProtocol
				return nil
			}
			err := requireHelperProtocol(probe, upgrade)
			if tc.deny {
				if !errors.Is(err, denied) {
					t.Fatalf("lost authorization cancellation: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err = requireHelperProtocol(probe, upgrade); err != nil {
					t.Fatal(err)
				}
			}
			if calls != tc.upgrades {
				t.Fatalf("authorization requests=%d, expected %d", calls, tc.upgrades)
			}
		})
	}
}
func TestMacCancelledHelperStartDoesNotInstallService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, _, err := startHelperContext(ctx, &mobile.TunnelParams{MTU: 1280}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled setup continued: %v", err)
	}
}
