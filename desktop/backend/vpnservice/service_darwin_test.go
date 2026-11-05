//go:build darwin

package vpnservice

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMacAuthorizationDoesNotInterpretApplicationPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "File with 'quotes' and $(touch SHOULD_NOT_EXIST).app")
	out, err := exec.Command("/bin/sh", "-c", "printf %s "+shellQuote(path)).Output()
	if err != nil || string(out) != path {
		t.Fatalf("quoting: %q %v", out, err)
	}
}
func TestMacProtectedDirectoryRejectsUntrustedAncestors(t *testing.T) {
	if err := SecureDirectory("/"); err != nil {
		t.Fatal(err)
	}
	if err := SecureDirectory(t.TempDir()); err == nil {
		t.Fatal("accepted user-owned or world-writable ancestor")
	}
	for _, path := range []string{"relative", "/Library/../Library"} {
		if SecureDirectory(path) == nil {
			t.Fatalf("accepted %s", path)
		}
	}
}
func TestMacInstallerRequiresRootBeforeWritingAnything(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("unprivileged guard test")
	}
	if Install(uint32(os.Getuid()), "missing") == nil {
		t.Fatal("unprivileged installation accepted")
	}
	if Remove(uint32(os.Getuid())) == nil {
		t.Fatal("unprivileged removal accepted")
	}
}
