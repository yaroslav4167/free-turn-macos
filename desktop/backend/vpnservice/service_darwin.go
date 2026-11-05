//go:build darwin

// Package vpnservice installs a per-user, root-owned launchd VPN helper.
package vpnservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const SocketDirectory = "/Library/PrivilegedHelperTools/org.freeturn.vpn.run"
const helperDirectory = "/Library/PrivilegedHelperTools"
const daemonDirectory = "/Library/LaunchDaemons"

func Label(uid uint32) string      { return fmt.Sprintf("org.freeturn.vpn.%d", uid) }
func HelperPath(uid uint32) string { return filepath.Join(helperDirectory, Label(uid)) }
func PlistPath(uid uint32) string  { return filepath.Join(daemonDirectory, Label(uid)+".plist") }
func SocketPath(uid int) string    { return fmt.Sprintf("%s/%d.sock", SocketDirectory, uid) }

func validateUID(uid uint32) error {
	if uid == 0 || uid == ^uint32(0) {
		return errors.New("a non-root user UID is required")
	}
	_, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	return err
}

// Never place a privileged executable or socket in a user-writable ancestor.
// The socket directory also lives below the protected helper directory.
func SecureDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("invalid system path")
	}
	current := "/"
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || stat.Uid != 0 || info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("unsafe system directory: %s", current)
		}
	}
	return nil
}
func makeSecureDirectory(path string) error {
	if err := SecureDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o755); err != nil && !os.IsExist(err) {
		return err
	}
	return SecureDirectory(path)
}
func atomicWrite(path string, source io.Reader, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".freeturn-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	n, writeErr := io.Copy(file, io.LimitReader(source, (128<<20)+1))
	if n > 128<<20 {
		writeErr = errors.New("VPN helper file exceeds size limit")
	}
	err = errors.Join(writeErr, file.Chmod(mode), file.Sync(), file.Close())
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
func control(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/launchctl", args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL=C"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %v: %w: %s", args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// VPN setup runs on a user connection request. Background classification
// throttles the helper and its route/ifconfig children on busy Macs. Unix IPC
// cannot supply the XPC transactions required by the Adaptive classification.
func daemonPlist(uid uint32) string {
	// Every substituted value is generated exclusively from a validated UID.
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>--service</string><string>%d</string></array>
<key>UserName</key><string>root</string><key>GroupName</key><string>wheel</string>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>ThrottleInterval</key><integer>5</integer><key>ExitTimeOut</key><integer>20</integer>
<key>ProcessType</key><string>Interactive</string><key>Umask</key><integer>63</integer>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>/usr/bin:/bin:/usr/sbin:/sbin</string></dict>
</dict></plist>
`, Label(uid), HelperPath(uid), uid)
}
func checkInstaller(uid uint32) error {
	if os.Geteuid() != 0 {
		return errors.New("VPN helper installation requires administrator authorization")
	}
	if err := validateUID(uid); err != nil {
		return err
	}
	return SecureDirectory(daemonDirectory)
}
func stop(uid uint32) error {
	if control("print", "system/"+Label(uid)) != nil {
		return nil
	}
	return control("bootout", "system/"+Label(uid))
}

// Install is invoked once through explicit administrator authorization. launchd
// subsequently executes this protected copy, never a binary inside the app.
func Install(uid uint32, source string) error {
	if err := checkInstaller(uid); err != nil {
		return err
	}
	if err := makeSecureDirectory(helperDirectory); err != nil {
		return err
	}
	if err := makeSecureDirectory(SocketDirectory); err != nil {
		return err
	}
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 128<<20 {
		return errors.New("invalid VPN helper executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--strict", source).CombinedOutput(); err != nil {
		return fmt.Errorf("VPN helper signature: %w: %s", err, out)
	}
	if err = stop(uid); err != nil {
		return err
	}
	if err = atomicWrite(HelperPath(uid), file, 0o755); err != nil {
		return err
	}
	if err = atomicWrite(PlistPath(uid), strings.NewReader(daemonPlist(uid)), 0o644); err != nil {
		return err
	}
	if err = control("enable", "system/"+Label(uid)); err != nil {
		return err
	}
	return control("bootstrap", "system", PlistPath(uid))
}
func Remove(uid uint32) error {
	if err := checkInstaller(uid); err != nil {
		return err
	}
	if err := SecureDirectory(helperDirectory); err != nil {
		return err
	}
	if err := stop(uid); err != nil {
		return err
	}
	for _, path := range []string{PlistPath(uid), HelperPath(uid)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := SecureDirectory(SocketDirectory); err == nil {
		if err = os.Remove(SocketPath(int(uid))); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
func Available(uid int) bool {
	if uid <= 0 || SecureDirectory(SocketDirectory) != nil {
		return false
	}
	info, err := os.Lstat(SocketPath(uid))
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(uid) && info.Mode()&os.ModeSocket != 0 && info.Mode().Perm() == 0o600
}
func Installed(uid int) bool {
	if uid <= 0 || SecureDirectory(daemonDirectory) != nil {
		return false
	}
	info, err := os.Lstat(PlistPath(uint32(uid)))
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0 && info.Mode().IsRegular() && info.Mode().Perm()&0o022 == 0
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// Configure is called only on first connection or an explicit repair/removal.
// It never stores an administrator password or modifies sudo authorization.
func Configure(remove bool) error {
	if os.Getuid() == 0 {
		return errors.New("run FreeTurn as a regular user")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	helper := filepath.Join(filepath.Dir(exe), "freeturn-vpn-helper")
	op := "--install-service"
	if remove {
		op = "--remove-service"
		if !Installed(os.Getuid()) {
			return nil
		}
	}
	command := shellQuote(helper) + " " + op + " " + strconv.Itoa(os.Getuid())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", "on run argv", "-e", "do shell script (item 1 of argv) with administrator privileges", "-e", "end run", command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("VPN helper authorization: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
