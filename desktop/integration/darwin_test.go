//go:build darwin

package integration

import (
	"bytes"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMacAutostartEscapesBundlePaths(t *testing.T) {
	path := "/Applications/FreeTurn & 'Friends' <test>.app/Contents/MacOS/freeturn-desktop"
	data, err := autostartPlist(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	found := false
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		if text, ok := token.(xml.CharData); ok && string(text) == path {
			found = true
		}
	}
	if !found {
		t.Fatalf("executable was not preserved: %s", data)
	}
	file := filepath.Join(t.TempDir(), "login.plist")
	if err = os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/bin/plutil", "-lint", file).CombinedOutput(); err != nil {
		t.Fatalf("invalid launch agent: %s %v", out, err)
	}
	for _, bad := range []string{"relative.app", "/Applications/Bad\n.app"} {
		if _, err := autostartPlist(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
func TestMacDisableAutostartDoesNotRemoveOtherLoginItems(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := launchAgentPath()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(path), "other.plist")
	_ = os.WriteFile(other, []byte("keep"), 0o600)
	_ = os.WriteFile(path, []byte("our login item"), 0o600)
	if err = SetAutostart(false); err != nil {
		t.Fatal(err)
	}
	if enabled, err := AutostartEnabled(); err != nil || enabled {
		t.Fatalf("autostart: %v %v", enabled, err)
	}
	if data, err := os.ReadFile(other); err != nil || string(data) != "keep" {
		t.Fatal("other login item changed")
	}
}
