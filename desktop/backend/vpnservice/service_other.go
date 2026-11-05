//go:build !linux && !darwin

package vpnservice

import "errors"

func Available(uid int) bool { return false }
func Install(uid uint32, source string) error {
	return errors.New("system VPN helper is only supported on Linux")
}
func Remove(uid uint32) error { return errors.New("system VPN helper is only supported on Linux") }
