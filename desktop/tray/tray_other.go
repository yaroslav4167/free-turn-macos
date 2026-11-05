//go:build !windows && !linux && !darwin

package tray

import "errors"

// Tray is a no-op placeholder for platforms without tray support.
type Tray struct{}

// Start reports that a tray cannot be created on this platform.
func Start(title string, onShow func(), onQuit func()) (*Tray, error) {
	return nil, errors.New("system tray is not supported on this platform")
}

// Close is a no-op on unsupported platforms.
func (t *Tray) Close() {
}

func (t *Tray) Available() bool { return false }
