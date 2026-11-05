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
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/samosvalishe/free-turn-proxy/desktop/backend/vpnservice"

	"github.com/samosvalishe/free-turn-proxy/mobile"
)

type helperRequest struct {
	Op         string               `json:"op"`
	Params     *mobile.TunnelParams `json:"params,omitempty"`
	Exclusions []string             `json:"exclusions,omitempty"`
}

type helperReply struct {
	Protocol int    `json:"protocol,omitempty"`
	Error    string `json:"error,omitempty"`
	Dev4     string `json:"dev4,omitempty"`
	Dev6     string `json:"dev6,omitempty"`
}

type helperClient struct {
	conn    *net.UnixConn
	mu      sync.Mutex
	exited  <-chan struct{}
	replies <-chan helperReply
}

func newHelperClient(conn *net.UnixConn) *helperClient {
	exited := make(chan struct{})
	replies := make(chan helperReply, 1)
	client := &helperClient{conn: conn, exited: exited, replies: replies}
	go func() {
		defer close(exited)
		defer close(replies)
		decoder := json.NewDecoder(conn)
		for {
			var reply helperReply
			if err := decoder.Decode(&reply); err != nil {
				return
			}
			replies <- reply
		}
	}()
	return client
}

func (c *helperClient) Close() {
	// EOF requests cleanup. Keep the read side open until the privileged
	// helper has released its routes/DNS and closed the connection.
	_ = c.conn.CloseWrite()
	select {
	case <-c.exited:
	case <-time.After(10 * time.Second):
	}
	_ = c.conn.Close()
}

func (c *helperClient) call(req helperRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := json.NewEncoder(c.conn).Encode(req); err != nil {
		return err
	}
	reply, ok := <-c.replies
	if !ok {
		return io.EOF
	}
	if reply.Error != "" {
		return errors.New(reply.Error)
	}
	return nil
}

func startHelper(params *mobile.TunnelParams) (*helperClient, int, string, string, error) {
	return startHelperContext(context.Background(), params)
}
func startHelperContext(ctx context.Context, params *mobile.TunnelParams) (*helperClient, int, string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, -1, "", "", err
	}
	conn, err := dialPersistentHelper()
	if err != nil {
		return nil, -1, "", "", err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	return initializeHelper(conn, params)
}

func dialPersistentHelper() (*net.UnixConn, error) {
	uid := os.Getuid()
	if uid == 0 {
		return nil, errors.New("run FreeTurn as a regular user")
	}
	if !vpnservice.Available(uid) {
		if vpnservice.Installed(uid) {
			// launchd may still be starting after boot. Never ask for authorization
			// repeatedly when an installed service is disabled or unhealthy.
			deadline := time.Now().Add(10 * time.Second)
			for !vpnservice.Available(uid) && time.Now().Before(deadline) {
				time.Sleep(100 * time.Millisecond)
			}
			if !vpnservice.Available(uid) {
				return nil, errors.New("VPN service is installed but unavailable; enable it in macOS Login Items or reinstall the VPN helper")
			}
		} else {
			if err := vpnservice.Configure(false); err != nil {
				return nil, err
			}
		}
	}
	// Older installed helpers keep running across app updates. Upgrade the
	// system copy once when cancellation/initialization support is missing.
	if err := requireHelperProtocol(func() (int, error) { return installedHelperProtocol(uid) }, func() error { return vpnservice.Configure(false) }); err != nil {
		return nil, err
	}
	return dialHelperSocket(uid)
}
func requireHelperProtocol(probe func() (int, error), upgrade func() error) error {
	protocol, err := probe()
	if err != nil {
		return err
	}
	if protocol >= helperProtocol {
		return nil
	}
	if err = upgrade(); err != nil {
		return err
	}
	protocol, err = probe()
	if err != nil {
		return err
	}
	if protocol < helperProtocol {
		return errors.New("installed VPN helper requires an upgrade")
	}
	return nil
}

func installedHelperProtocol(uid int) (int, error) {
	conn, err := dialHelperSocket(uid)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err = json.NewEncoder(conn).Encode(helperRequest{Op: "status"}); err != nil {
		return 0, err
	}
	var reply helperReply
	if err = json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&reply); err != nil {
		return 0, fmt.Errorf("VPN helper status: %w", err)
	}
	return reply.Protocol, nil
}
func dialHelperSocket(uid int) (*net.UnixConn, error) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: vpnservice.SocketPath(uid), Net: "unix"})
		if err == nil {
			if err = requireRootPeer(conn); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("VPN service did not start: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func initializeHelper(conn *net.UnixConn, params *mobile.TunnelParams) (*helperClient, int, string, string, error) {
	if err := conn.SetDeadline(time.Now().Add(60 * time.Second)); err != nil {
		_ = conn.Close()
		return nil, -1, "", "", err
	}
	// The helper must actually be privileged; a local process must not be able
	// to impersonate it on the temporary socket.
	if peerErr := requireRootPeer(conn); peerErr != nil {
		_ = conn.Close()
		return nil, -1, "", "", peerErr
	}
	if encodeErr := json.NewEncoder(conn).Encode(helperRequest{Op: "init", Params: params}); encodeErr != nil {
		_ = conn.Close()
		return nil, -1, "", "", encodeErr
	}
	data := make([]byte, 1)
	oob := make([]byte, unix.CmsgSpace(4))
	n, oobn, flags, _, err := conn.ReadMsgUnix(data, oob)
	if err != nil || n != 1 || flags&unix.MSG_CTRUNC != 0 {
		_ = conn.Close()
		return nil, -1, "", "", fmt.Errorf("receive utun descriptor: %v", err)
	}
	if data[0] == 0 {
		var reply helperReply
		decodeErr := json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&reply)
		_ = conn.Close()
		if decodeErr != nil {
			return nil, -1, "", "", decodeErr
		}
		return nil, -1, "", "", fmt.Errorf("macOS VPN helper: %s", reply.Error)
	}
	msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil || len(msgs) != 1 {
		_ = conn.Close()
		return nil, -1, "", "", errors.New("VPN helper did not pass a TUN descriptor")
	}
	fds, err := unix.ParseUnixRights(&msgs[0])
	if err != nil || len(fds) != 1 {
		for _, fd := range fds {
			_ = unix.Close(fd)
		}
		_ = conn.Close()
		return nil, -1, "", "", errors.New("invalid TUN descriptor from VPN helper")
	}
	unix.CloseOnExec(fds[0])
	if err := unix.SetNonblock(fds[0], true); err != nil {
		_ = unix.Close(fds[0])
		_ = conn.Close()
		return nil, -1, "", "", err
	}
	var reply helperReply
	if err := json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&reply); err != nil || reply.Error != "" {
		_ = unix.Close(fds[0])
		_ = conn.Close()
		return nil, -1, "", "", fmt.Errorf("VPN helper initialization: %v %s", err, reply.Error)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = unix.Close(fds[0])
		_ = conn.Close()
		return nil, -1, "", "", err
	}
	return newHelperClient(conn), fds[0], reply.Dev4, reply.Dev6, nil
}

func requireRootPeer(conn *net.UnixConn) error {
	return requirePeerUID(conn, 0)
}

func requirePeerUID(conn *net.UnixConn, uid uint32) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var peer *unix.Xucred
	if controlErr := raw.Control(func(fd uintptr) { peer, err = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED) }); controlErr != nil {
		return controlErr
	}
	if err != nil {
		return err
	}
	if peer == nil || peer.Uid != uid {
		return fmt.Errorf("unauthorized VPN socket peer (expected UID %d)", uid)
	}
	return nil
}
