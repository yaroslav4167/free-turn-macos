//go:build !linux && !darwin

package integration

import "errors"

const CanInstallUpdates = Supported

const Supported = false

func Uninstall() error { return errors.New("uninstallation is only supported on Linux") }

func VPNHelperAvailable() bool { return false }
func CreateDesktopShortcut() (string, error) {
	return "", errors.New("desktop shortcuts are only supported on Linux")
}
func ConfigureVPNHelper(remove bool) error {
	return errors.New("system VPN helper is only supported on Linux")
}

func InstalledPath() (string, error)  { return "", nil }
func Install() (string, error)        { return "", errors.New("installation is only supported on Linux") }
func AutostartEnabled() (bool, error) { return false, nil }
func SetAutostart(enabled bool) error { return errors.New("autostart is only supported on Linux") }
