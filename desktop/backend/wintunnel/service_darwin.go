//go:build darwin

package wintunnel

import (
	"context"
	"errors"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/samosvalishe/free-turn-proxy/desktop/backend/vpnservice"
)

const helperProtocol = 1

// RunService stays idle between connections. Each session owns its own utun,
// routes and DNS; loss of the client or daemon shutdown releases all three.
func RunService(uid uint32) error {
	if os.Geteuid() != 0 || uid == 0 || uid == ^uint32(0) {
		return errors.New("invalid privileged VPN service identity")
	}
	if err := vpnservice.SecureDirectory(vpnservice.SocketDirectory); err != nil {
		return err
	}
	path := vpnservice.SocketPath(int(uid))
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("VPN service socket path is occupied")
		}
		if err = os.Remove(path); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(path, 0o600); err != nil {
		return err
	}
	if err = os.Chown(path, int(uid), 0); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	return serveHelperListener(ctx, listener, uid, serveDarwinHelperSession)
}

func serveHelperListener(ctx context.Context, listener *net.UnixListener, uid uint32, serve func(*net.UnixConn, func() (func(), error)) error) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	clients := map[*net.UnixConn]bool{}
	closeClients := func() {
		_ = listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for c := range clients {
			_ = c.Close()
		}
	}
	cancel := context.AfterFunc(ctx, closeClients)
	defer func() { cancel(); closeClients(); wg.Wait() }()
	slots := make(chan struct{}, 8)
	active := make(chan struct{}, 1)
	acquire := func() (func(), error) {
		select {
		case active <- struct{}{}:
			return func() { <-active }, nil
		default:
			return nil, errors.New("another FreeTurn VPN session is already active")
		}
	}
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err = requirePeerUID(conn, uid); err != nil {
			_ = conn.Close()
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		mu.Lock()
		if ctx.Err() != nil {
			mu.Unlock()
			_ = conn.Close()
			<-slots
			return nil
		}
		clients[conn] = true
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			defer conn.Close()
			defer func() { mu.Lock(); delete(clients, conn); mu.Unlock() }()
			_ = serve(conn, acquire)
		}()
	}
}

// ParseServiceUID accepts only canonical decimal UIDs, never option strings.
func ParseServiceUID(value string) (uint32, error) {
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil || n == 0 || n == uint64(^uint32(0)) || strconv.FormatUint(n, 10) != value {
		return 0, errors.New("invalid VPN service UID")
	}
	return uint32(n), nil
}
