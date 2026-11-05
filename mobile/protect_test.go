package mobile

import (
	"errors"
	"strings"
	"testing"

	"github.com/samosvalishe/free-turn-proxy/internal/netctl"
)

type protectRawConn struct{ err error }

func (r protectRawConn) Control(fn func(uintptr)) error {
	if r.err != nil {
		return r.err
	}
	fn(42)
	return nil
}
func (r protectRawConn) Read(func(uintptr) bool) error  { return nil }
func (r protectRawConn) Write(func(uintptr) bool) error { return nil }
func TestSocketControlPropagatesProtectionFailure(t *testing.T) {
	defer SetProtect(nil)
	for _, ok := range []bool{false, true} {
		SetProtect(&testProtector{ok: ok})
		err := netctl.Apply("tcp4", "198.18.0.254:9", protectRawConn{})
		if ok && err != nil {
			t.Fatal(err)
		}
		if !ok && (err == nil || !strings.Contains(err.Error(), "protect socket")) {
			t.Fatalf("unprotected socket accepted: %v", err)
		}
	}
	expected := errors.New("raw socket unavailable")
	if err := netctl.Apply("tcp4", "unused", protectRawConn{err: expected}); !errors.Is(err, expected) {
		t.Fatalf("raw socket error lost: %v", err)
	}
	SetProtect(nil)
	if err := netctl.Apply("tcp4", "unused", protectRawConn{err: expected}); err != nil {
		t.Fatalf("nil protector should not touch socket: %v", err)
	}
}
