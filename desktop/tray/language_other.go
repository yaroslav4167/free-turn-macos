//go:build !linux && !darwin

package tray

// Native Windows menu titles are read on every popup; other platforms have no tray.
func (t *Tray) RefreshLanguage() {}
