//go:build linux || windows || darwin

package wintunnel

import (
	"strings"
	"testing"
	"time"

	"github.com/samosvalishe/free-turn-proxy/mobile"
)

func TestWaitForConnectionCaptcha(t *testing.T) {
	now := time.Unix(1, 0)
	attempt := 0
	err := waitForConnection(time.Second, func() *mobile.Snapshot {
		attempt++
		state := mobile.StateCaptcha
		if attempt > 5 {
			state = mobile.StateConnecting
		}
		if attempt > 7 {
			state = mobile.StateConnected
		}
		return &mobile.Snapshot{State: state}
	}, func() time.Time { return now }, func(delay time.Duration) { now = now.Add(delay) })
	if err != nil {
		t.Fatal(err)
	}
	if now.Sub(time.Unix(1, 0)) <= time.Second {
		t.Fatal("test did not exceed the initial deadline")
	}
}

func TestWaitForConnectionTimeout(t *testing.T) {
	now := time.Unix(1, 0)
	err := waitForConnection(time.Second, func() *mobile.Snapshot {
		return &mobile.Snapshot{State: mobile.StateConnecting}
	}, func() time.Time { return now }, func(delay time.Duration) { now = now.Add(delay) })
	if err == nil || !strings.Contains(err.Error(), "within 1s") {
		t.Fatalf("unexpected timeout: %v", err)
	}
}

func TestWaitForConnectionStopsOnFailure(t *testing.T) {
	for _, state := range []string{mobile.StateIdle, mobile.StateError} {
		t.Run(state, func(t *testing.T) {
			err := waitForConnection(time.Minute, func() *mobile.Snapshot {
				return &mobile.Snapshot{State: state, ErrMsg: "TURN unavailable"}
			}, time.Now, func(time.Duration) { t.Fatal("waited after failure") })
			if err == nil {
				t.Fatal("expected connection failure")
			}
		})
	}
}
