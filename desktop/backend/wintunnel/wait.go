//go:build linux || windows || darwin

package wintunnel

import (
	"errors"
	"fmt"
	"time"

	"github.com/samosvalishe/free-turn-proxy/mobile"
)

// The platform wrapper must not abort before the core's own deadline expires.
const connectTimeout = mobile.DefaultConnectTimeout + 15*time.Second

func waitConnected(timeout time.Duration) error {
	return waitForConnection(timeout, mobile.GetState, time.Now, time.Sleep)
}

// Captcha is a user interaction, not a stalled network handshake. Start a new
// network budget while waiting for it, just as the core's watchdog does.
func waitForConnection(timeout time.Duration, state func() *mobile.Snapshot, now func() time.Time, sleep func(time.Duration)) error {
	deadline := now().Add(timeout)
	for {
		snapshot := state()
		switch snapshot.State {
		case mobile.StateConnected:
			return nil
		case mobile.StateError:
			return fmt.Errorf("VPN session failed: %s", snapshot.ErrMsg)
		case mobile.StateIdle:
			return errors.New("VPN session stopped before connecting")
		case mobile.StateCaptcha:
			deadline = now().Add(timeout)
		}
		if !now().Before(deadline) {
			return fmt.Errorf("VPN did not connect within %s", timeout)
		}
		sleep(300 * time.Millisecond)
	}
}
