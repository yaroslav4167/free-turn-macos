//go:build linux

package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	appassets "github.com/samosvalishe/free-turn-proxy/desktop/assets"
	"github.com/samosvalishe/free-turn-proxy/desktop/backend/vpnservice"
)

const CanInstallUpdates = Supported

const Supported = true

func dataDir() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".local", "share"), err
}

func InstalledPath() (string, error) {
	dir, err := dataDir()
	return filepath.Join(dir, "FreeTurnDesktop", "freeturn-desktop"), err
}

func autostartPath() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "autostart", "freeturn.desktop"), err
}

// Exec has two escaping layers: desktop-entry values and command arguments.
func desktopEntry(executable string, autostart bool) (string, error) {
	if strings.ContainsAny(executable, "\r\n\x00") || !filepath.IsAbs(executable) {
		return "", errors.New("invalid application path")
	}
	arg := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$", "%", "%%").Replace(executable)
	command := "\"" + arg + "\""
	if autostart {
		command += " --autostart"
	}
	command = strings.ReplaceAll(command, "\\", "\\\\")
	return "[Desktop Entry]\nType=Application\nVersion=1.0\nName=FreeTurn\nGenericName=VPN client\nGenericName[ru]=VPN-клиент\nComment=VPN and TURN relay client\nComment[ru]=VPN и TURN-реле\nExec=" + command + "\nIcon=freeturn\nTerminal=false\nCategories=Network;Security;\nKeywords=VPN;WireGuard;AmneziaWG;TURN;\nStartupNotify=true\nStartupWMClass=freeturn\nX-FreeTurn-Managed=true\n", nil
}

func writeEntry(path, executable string, autostart bool) error {
	return writeEntryMode(path, executable, autostart, 0o644)
}

func writeEntryMode(path, executable string, autostart bool, mode os.FileMode) error {
	entry, err := desktopEntry(executable, autostart)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".freeturn-*.desktop")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(entry); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Install makes a stable per-user copy. No root, capabilities or shell are used.
func Install() (string, error) {
	target, err := InstalledPath()
	if err != nil {
		return "", err
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if executable != target {
		if err := copyExecutable(executable, target); err != nil {
			return "", err
		}
	}
	helperSource := filepath.Join(filepath.Dir(executable), "freeturn-vpn-helper")
	helperTarget := filepath.Join(filepath.Dir(target), "freeturn-vpn-helper")
	if helperSource != helperTarget {
		if _, err := os.Stat(helperSource); err == nil {
			if err := copyExecutable(helperSource, helperTarget); err != nil {
				return "", err
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	if err := installIcons(dir); err != nil {
		return "", fmt.Errorf("install application icon: %w", err)
	}
	if err := writeEntry(filepath.Join(dir, "applications", "freeturn.desktop"), target, false); err != nil {
		return "", err
	}
	return target, nil
}

func installIcons(dir string) error {
	icons := map[string][]byte{
		"scalable/apps/freeturn.svg": appassets.IconSVG,
		"512x512/apps/freeturn.png":  appassets.IconPNG,
		"64x64/apps/freeturn.png":    appassets.TrayIconPNG,
	}
	for name, data := range icons {
		path := filepath.Join(dir, "icons", "hicolor", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(path), ".freeturn-icon-*")
		if err != nil {
			return err
		}
		if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return err
		}
		if err := tmp.Chmod(0o644); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return err
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmp.Name())
			return err
		}
		if err := os.Rename(tmp.Name(), path); err != nil {
			_ = os.Remove(tmp.Name())
			return err
		}
	}
	return nil
}

func desktopDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(config, "user-dirs.dirs"))
	if os.IsNotExist(err) {
		return filepath.Join(home, "Desktop"), nil
	}
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found || strings.TrimSpace(key) != "XDG_DESKTOP_DIR" {
			continue
		}
		value, err = strconv.Unquote(strings.TrimSpace(value))
		if err != nil {
			return "", fmt.Errorf("invalid XDG_DESKTOP_DIR: %w", err)
		}
		for _, prefix := range []string{"$HOME", "${HOME}"} {
			if value == prefix || strings.HasPrefix(value, prefix+"/") {
				value = home + strings.TrimPrefix(value, prefix)
				break
			}
		}
		if !filepath.IsAbs(value) || strings.ContainsAny(value, "\r\n\x00") {
			return "", errors.New("invalid desktop directory")
		}
		value = filepath.Clean(value)
		if value == filepath.Clean(home) {
			return "", errors.New("рабочий стол отключён в XDG; ярлык доступен в меню приложений")
		}
		return value, nil
	}
	return filepath.Join(home, "Desktop"), nil
}

func CreateDesktopShortcut() (string, error) {
	dir, err := desktopDirectory()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "freeturn.desktop")
	if data, err := os.ReadFile(path); err == nil {
		if !strings.Contains(string(data), "X-FreeTurn-Managed=true\n") {
			return "", fmt.Errorf("ярлык %s уже существует и не управляется FreeTurn; переименуйте его перед созданием нового", path)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	executable, err := Install()
	if err != nil {
		return "", err
	}
	if err := writeEntryMode(path, executable, false, 0o755); err != nil {
		return "", err
	}
	// Desktop environments without GIO can still use the executable launcher.
	// GNOME may require an explicit “Allow Launching” if metadata is unavailable.
	if gio, err := exec.LookPath("gio"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, gio, "set", path, "metadata::trusted", "true").Run()
	}
	return path, nil
}

func copyExecutable(sourcePath, target string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	tmp, err := os.CreateTemp(filepath.Dir(target), ".freeturn-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, source); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

func VPNHelperAvailable() bool { return vpnservice.Available(os.Getuid()) }

func ConfigureVPNHelper(remove bool) error {
	if os.Getuid() <= 0 {
		return errors.New("запустите приложение от обычного пользователя")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	uid := strconv.Itoa(os.Getuid())
	args := []string{executable, "--remove-vpn-service", uid}
	if !remove {
		helper := filepath.Join(filepath.Dir(executable), "freeturn-vpn-helper")
		if _, err := os.Stat(helper); err != nil {
			return fmt.Errorf("не найден VPN-helper по пути %s: %w; запустите install-desktop.sh из полного архива", helper, err)
		}
		args = []string{executable, "--install-vpn-service", helper, uid}
	}
	out, err := exec.Command("pkexec", args...).CombinedOutput() //nolint:gosec,noctx
	if err != nil {
		return fmt.Errorf("установка системного VPN-helper: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func AutostartEnabled() (bool, error) {
	path, err := autostartPath()
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
	path, err := autostartPath()
	if err != nil {
		return err
	}
	if !enabled {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	executable, err := Install()
	if err != nil {
		return err
	}
	return writeEntry(path, executable, true)
}
