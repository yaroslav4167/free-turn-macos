//go:build darwin

package wintunnel

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

type helperDevice interface {
	File() *os.File
	Descriptor() int
	Name() (string, error)
	Close() error
}
type helperUTUN struct {
	file *os.File
	fd   int
	name string
}

func (t *helperUTUN) File() *os.File        { return t.file }
func (t *helperUTUN) Descriptor() int       { return t.fd }
func (t *helperUTUN) Name() (string, error) { return t.name, nil }
func (t *helperUTUN) Close() error          { return t.file.Close() }

// Only the GUI consumes WireGuard events and packets. The privileged service
// owns a bare utun descriptor; it must not start a second, unread event queue.
func createHelperTUN(mtu int) (helperDevice, error) {
	syscall.ForkLock.RLock()
	fd, err := unix.Socket(unix.AF_SYSTEM, unix.SOCK_DGRAM, 2)
	if err == nil {
		unix.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = unix.Close(fd)
		}
	}()
	info := &unix.CtlInfo{}
	copy(info.Name[:], "com.apple.net.utun_control")
	if err = unix.IoctlCtlInfo(fd, info); err != nil {
		return nil, fmt.Errorf("utun control: %w", err)
	}
	if err = unix.Connect(fd, &unix.SockaddrCtl{ID: info.Id}); err != nil {
		return nil, fmt.Errorf("create utun: %w", err)
	}
	name, err := unix.GetsockoptString(fd, 2, 2)
	if err != nil {
		return nil, fmt.Errorf("utun name: %w", err)
	}
	if err = unix.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	control, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(control)
	unix.CloseOnExec(control)
	request := &unix.IfreqMTU{MTU: int32(mtu)}
	copy(request.Name[:], name)
	if err = unix.IoctlSetIfreqMTU(control, request); err != nil {
		return nil, fmt.Errorf("utun MTU: %w", err)
	}
	success = true
	return &helperUTUN{file: os.NewFile(uintptr(fd), "freeturn-utun"), fd: fd, name: name}, nil
}
