//go:build !windows && !linux && !darwin

package wintunnel

import "errors"

var errUnsupported = errors.New("wintunnel: embedded VPN is only implemented on Windows")

func Available() bool { return false }

func Connect(configJSON, wgConfigText string, mtu int, exclusionIPs ...string) error {
	return errUnsupported
}

func ConnectWithRoutes(configJSON, wgConfigText string, mtu int, routes string, exclusionIPs ...string) error {
	return errUnsupported
}

func Disconnect() {}

func RunHelper(string) error { return errUnsupported }
