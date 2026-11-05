//go:build darwin

package integration

import (
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/samosvalishe/free-turn-proxy/desktop/backend/vpnservice"
)

const Supported = true

// macOS updates replace the entire verified bundle, including the VPN helper.
const CanInstallUpdates = true

func bundlePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(root, ".app") || filepath.Base(filepath.Dir(exe)) != "MacOS" {
		return "", errors.New("run FreeTurn from its complete .app bundle")
	}
	return root, nil
}
func InstalledPath() (string, error)       { return os.Executable() }
func VPNHelperAvailable() bool             { return vpnservice.Available(os.Getuid()) }
func ConfigureVPNHelper(remove bool) error { return vpnservice.Configure(remove) }

func Install() (string, error) {
	return "", errors.New("copy FreeTurn.app to Applications using Finder")
}
func Uninstall() error {
	return errors.New("quit FreeTurn, then move FreeTurn.app to Trash using Finder; profiles are retained")
}
func CreateDesktopShortcut() (string, error) {
	return "", errors.New("drag FreeTurn.app to the Dock using Finder")
}
func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "org.freeturn.desktop.plist"), err
}
func autostartPlist(executable string) ([]byte, error) {
	if !filepath.IsAbs(executable) || strings.ContainsAny(executable, "\x00\r\n") {
		return nil, errors.New("invalid application path")
	}
	var escaped strings.Builder
	if err := xml.EscapeText(&escaped, []byte(executable)); err != nil {
		return nil, err
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>org.freeturn.desktop</string>
<key>ProgramArguments</key><array><string>` + escaped.String() + `</string><string>--autostart</string></array>
<key>RunAtLoad</key><true/>
<key>LimitLoadToSessionType</key><string>Aqua</string>
</dict></plist>
`), nil
}
func AutostartEnabled() (bool, error) {
	path, err := launchAgentPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
func SetAutostart(enabled bool) error {
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
	if !enabled {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	bundle, err := bundlePath()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	// Downloads and temporary mounts are not stable login-item locations.
	if !strings.HasPrefix(bundle, "/Applications/") && !strings.HasPrefix(bundle, filepath.Join(home, "Applications")+string(os.PathSeparator)) {
		return errors.New("move FreeTurn.app to Applications before enabling start at login")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := autostartPlist(exe)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".freeturn-login-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return nil
}
func nativeSystemLanguage() string {
	out, err := exec.Command("/usr/bin/defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return "en"
	}
	text := strings.TrimSpace(strings.Trim(string(out), "()\n\t "))
	text = strings.Trim(strings.TrimSpace(strings.Split(text, ",")[0]), "\"")
	if text == "" {
		return "en"
	}
	return text
}
